package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// cloudWatchConfig holds provider-specific settings for a CloudWatch log source.
type cloudWatchConfig struct {
	Region          string `json:"region"`
	LogGroupName    string `json:"log_group_name"`
	FilterPattern   string `json:"filter_pattern"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
}

// s3LogConfig holds provider-specific settings for an S3 log source.
type s3LogConfig struct {
	Region          string `json:"region"`
	Bucket          string `json:"bucket"`
	Prefix          string `json:"prefix"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
}

// normalizeRe strips timestamps, hex addresses, and UUIDs from a log line so
// that repeated occurrences of the same error produce the same fingerprint.
var normalizeRe = regexp.MustCompile(
	`(?i)(` +
		`\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b` + // uuid
		`|0x[0-9a-f]+` + // hex address
		`|\b\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})?\b` + // timestamp
		`|\b\d{10,13}\b` + // epoch ms/s
		`)`,
)

func normalizeLine(msg string) string {
	return normalizeRe.ReplaceAllString(msg, "<x>")
}

func logFingerprint(msg string) string {
	sample := msg
	if len(sample) > 200 {
		sample = sample[:200]
	}
	normalized := normalizeLine(sample)
	h := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(h[:8])
}

// looksLikeError returns true when a log line appears to be an error entry.
func looksLikeError(line string) bool {
	upper := strings.ToUpper(line)
	return strings.Contains(upper, "ERROR") ||
		strings.Contains(upper, "EXCEPTION") ||
		strings.Contains(upper, "FATAL") ||
		strings.Contains(upper, "PANIC") ||
		strings.Contains(upper, "CRITICAL")
}

// startLogSourcePoller runs a background goroutine that polls all enabled log
// sources at their configured intervals and upserts error patterns.
func startLogSourcePoller(ctx context.Context, queries *db.Queries, h *handler.Handler) {
	if h == nil {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}

		sources, err := queries.ListAllEnabledLogSources(ctx)
		if err != nil {
			slog.Debug("log source poller: list sources failed", "error", err)
			continue
		}

		now := time.Now()
		for _, source := range sources {
			// Check whether this source is due for a poll.
			if source.LastPolledAt.Valid {
				nextPoll := source.LastPolledAt.Time.Add(
					time.Duration(source.PollIntervalMinutes) * time.Minute,
				)
				if now.Before(nextPoll) {
					continue
				}
			}

			pollLogSource(ctx, queries, h, source)
		}
	}
}

type errorEntry struct {
	message string
}

func pollLogSource(ctx context.Context, queries *db.Queries, h *handler.Handler, source db.LogSource) {
	defer func() {
		// Always update last_polled_at so we don't spin on a failing source.
		if err := queries.TouchLogSourcePolledAt(ctx, source.ID); err != nil {
			slog.Debug("log source poller: touch polled_at failed",
				"source_id", util.UUIDToString(source.ID), "error", err)
		}
	}()

	var entries []errorEntry
	var pollErr error

	switch source.Provider {
	case "s3":
		entries, pollErr = pollS3(ctx, source)
	case "cloudwatch":
		entries, pollErr = pollCloudWatch(ctx, source)
	default:
		slog.Warn("log source poller: unknown provider",
			"provider", source.Provider, "source_id", util.UUIDToString(source.ID))
		return
	}

	if pollErr != nil {
		slog.Warn("log source poller: poll failed",
			"provider", source.Provider,
			"source_id", util.UUIDToString(source.ID),
			"error", pollErr,
		)
		return
	}

	wsUUID := parseUUID(util.UUIDToString(source.WorkspaceID))

	for _, entry := range entries {
		fp := logFingerprint(entry.message)
		title := entry.message
		if len(title) > 200 {
			title = title[:200]
		}
		sample := entry.message
		if len(sample) > 2000 {
			sample = sample[:2000]
		}

		upserted, err := queries.UpsertLogErrorPattern(ctx, db.UpsertLogErrorPatternParams{
			WorkspaceID:     source.WorkspaceID,
			LogSourceID:     source.ID,
			Fingerprint:     fp,
			Title:           title,
			Sample:          sample,
			OccurrenceCount: 1,
		})
		if err != nil {
			slog.Debug("log source poller: upsert pattern failed",
				"source_id", util.UUIDToString(source.ID), "error", err)
			continue
		}

		// Auto-create an issue once the pattern has crossed the threshold.
		if source.AutoCreateIssues && upserted.OccurrenceCount >= 3 && !upserted.IssueID.Valid && source.CreatedByID.Valid {
			h.AutoCreateIssueFromPattern(ctx, wsUUID, upserted, source.CreatedByID)
		}
	}
}

// pollS3 downloads objects modified since last_polled_at (or the past hour for
// the first poll) and extracts lines that look like errors.
func pollS3(ctx context.Context, source db.LogSource) ([]errorEntry, error) {
	var cfg s3LogConfig
	if err := json.Unmarshal(source.Config, &cfg); err != nil {
		return nil, fmt.Errorf("invalid s3 config: %w", err)
	}
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("s3 config missing bucket")
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}

	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.Region),
	}
	if cfg.AccessKeyID != "" && cfg.SecretAccessKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	client := s3.NewFromConfig(awsCfg)

	// Determine the since-time: last poll or 1 hour ago for first run.
	var since time.Time
	if source.LastPolledAt.Valid {
		since = source.LastPolledAt.Time
	} else {
		since = time.Now().Add(-1 * time.Hour)
	}

	listInput := &s3.ListObjectsV2Input{
		Bucket: aws.String(cfg.Bucket),
	}
	if cfg.Prefix != "" {
		listInput.Prefix = aws.String(cfg.Prefix)
	}

	var entries []errorEntry
	paginator := s3.NewListObjectsV2Paginator(client, listInput)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list s3 objects: %w", err)
		}

		for _, obj := range page.Contents {
			if obj.LastModified == nil || obj.LastModified.Before(since) {
				continue
			}
			if obj.Key == nil {
				continue
			}

			objEntries, err := readS3ObjectErrors(ctx, client, cfg.Bucket, *obj.Key)
			if err != nil {
				slog.Debug("log source poller: read s3 object failed", "key", *obj.Key, "error", err)
				continue
			}
			entries = append(entries, objEntries...)
		}
	}

	return entries, nil
}

func readS3ObjectErrors(ctx context.Context, client *s3.Client, bucket, key string) ([]errorEntry, error) {
	out, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}
	defer out.Body.Close()

	var entries []errorEntry
	scanner := bufio.NewScanner(io.LimitReader(out.Body, 10*1024*1024)) // 10 MB cap
	for scanner.Scan() {
		line := scanner.Text()
		if looksLikeError(line) {
			entries = append(entries, errorEntry{message: line})
		}
	}
	return entries, scanner.Err()
}

// pollCloudWatch fetches log events from a CloudWatch Logs group using
// FilterLogEvents. It collects events from the window since last_polled_at
// (or the past hour on first run), then returns lines that look like errors.
func pollCloudWatch(ctx context.Context, source db.LogSource) ([]errorEntry, error) {
	var cfg cloudWatchConfig
	if err := json.Unmarshal(source.Config, &cfg); err != nil {
		return nil, fmt.Errorf("invalid cloudwatch config: %w", err)
	}
	if cfg.LogGroupName == "" {
		return nil, fmt.Errorf("cloudwatch config missing log_group_name")
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	if cfg.FilterPattern == "" {
		cfg.FilterPattern = "ERROR"
	}

	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.Region),
	}
	if cfg.AccessKeyID != "" && cfg.SecretAccessKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	client := cloudwatchlogs.NewFromConfig(awsCfg)

	var since time.Time
	if source.LastPolledAt.Valid {
		since = source.LastPolledAt.Time
	} else {
		since = time.Now().Add(-1 * time.Hour)
	}

	input := &cloudwatchlogs.FilterLogEventsInput{
		LogGroupName:  aws.String(cfg.LogGroupName),
		FilterPattern: aws.String(cfg.FilterPattern),
		StartTime:     aws.Int64(since.UnixMilli()),
		EndTime:       aws.Int64(time.Now().UnixMilli()),
		Limit:         aws.Int32(200),
	}

	var entries []errorEntry
	paginator := cloudwatchlogs.NewFilterLogEventsPaginator(client, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return entries, fmt.Errorf("filter log events: %w", err)
		}
		for _, event := range page.Events {
			if event.Message == nil {
				continue
			}
			msg := strings.TrimSpace(*event.Message)
			if msg != "" {
				entries = append(entries, errorEntry{message: msg})
			}
		}
	}
	return entries, nil
}

// Ensure the cloudwatchlogs/types import is used (ThrottlingException etc. are
// available when callers need to inspect error kinds).
var _ = types.ThrottlingException{}
