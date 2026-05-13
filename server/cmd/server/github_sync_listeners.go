package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// registerGitHubSyncListeners subscribes to the event bus and keeps GitHub
// issues in sync with their Multica counterparts.
//
// Delivered events:
//   - issue:created → logged; auto-push requires a repo target supplied via API
//   - issue:updated → PATCH the linked GitHub issue title/body if a sync record exists
//   - issue:deleted → close the linked GitHub issue and remove the sync record
func registerGitHubSyncListeners(bus *events.Bus, queries *db.Queries) {
	ctx := context.Background()

	// issue:created — auto-push on create requires knowing which repo to target.
	// That information is not present on the event bus (a workspace may have
	// many repos synced). Callers that want to push a new issue to a specific
	// repo should use the dedicated sync API endpoint instead.
	bus.Subscribe(protocol.EventIssueCreated, func(e events.Event) {
		slog.Debug("github_sync: issue:created received; auto-push requires a repo target — use the sync API endpoint to link a repo")
	})

	// issue:updated — update the GitHub issue title/body when a sync record exists.
	bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}

		// The issue:updated payload carries the full IssueResponse under "issue".
		issue, ok := payload["issue"].(handler.IssueResponse)
		if !ok {
			return
		}

		token := os.Getenv("GITHUB_TOKEN")
		if token == "" {
			slog.Warn("github_sync: GITHUB_TOKEN not set; skipping issue:updated sync")
			return
		}

		sync, err := queries.GetGitHubIssueSyncByMulticaIssue(ctx, parseUUID(issue.ID))
		if err != nil {
			// No sync record — nothing to do.
			return
		}

		if sync.SyncDirection == "github_to_multica" {
			// One-way inbound only; do not write back to GitHub.
			return
		}

		body := githubIssueBody(issue.Description, issue.ID)
		if err := githubPatchIssue(ctx, token, sync.GithubRepoOwner, sync.GithubRepoName, sync.GithubIssueNumber, issue.Title, body, ""); err != nil {
			slog.Warn("github_sync: failed to update GitHub issue",
				"multica_issue_id", issue.ID,
				"github_issue_number", sync.GithubIssueNumber,
				"error", err,
			)
			return
		}

		if err := queries.TouchGitHubIssueSyncTimestamp(ctx, sync.MulticaIssueID); err != nil {
			slog.Warn("github_sync: failed to touch sync timestamp", "error", err)
		}
	})

	// issue:deleted — close the GitHub issue and remove the sync record.
	bus.Subscribe(protocol.EventIssueDeleted, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		issueID, _ := payload["issue_id"].(string)
		if issueID == "" {
			return
		}

		token := os.Getenv("GITHUB_TOKEN")
		if token == "" {
			slog.Warn("github_sync: GITHUB_TOKEN not set; skipping issue:deleted sync")
			return
		}

		issueUUID := parseUUID(issueID)
		sync, err := queries.GetGitHubIssueSyncByMulticaIssue(ctx, issueUUID)
		if err != nil {
			// No sync record — nothing to do.
			return
		}

		if sync.SyncDirection != "github_to_multica" {
			// Close the GitHub issue for multica_to_github and bidirectional.
			if err := githubPatchIssue(ctx, token, sync.GithubRepoOwner, sync.GithubRepoName, sync.GithubIssueNumber, "", "", "closed"); err != nil {
				slog.Warn("github_sync: failed to close GitHub issue",
					"multica_issue_id", issueID,
					"github_issue_number", sync.GithubIssueNumber,
					"error", err,
				)
				// Continue to delete the local sync record even if the GitHub call failed.
			}
		}

		if err := queries.DeleteGitHubIssueSync(ctx, db.DeleteGitHubIssueSyncParams{
			MulticaIssueID: issueUUID,
			WorkspaceID:    sync.WorkspaceID,
		}); err != nil {
			slog.Warn("github_sync: failed to delete sync record", "multica_issue_id", issueID, "error", err)
		}
	})
}

// githubIssueBody constructs the GitHub issue body from the Multica description,
// appending a footer that links back to the originating Multica issue.
func githubIssueBody(description *string, issueID string) string {
	body := ""
	if description != nil {
		body = *description
	}
	return fmt.Sprintf("%s\n\n---\n*Synced from Multica issue %s*", body, issueID)
}

// githubPatchIssue sends a PATCH request to the GitHub Issues API.
// Only non-empty fields are included in the request body so that callers can
// update a subset of fields (e.g. only "state" when closing an issue).
func githubPatchIssue(ctx context.Context, token, owner, repo string, number int32, title, body, state string) error {
	payload := map[string]any{}
	if title != "" {
		payload["title"] = title
	}
	if body != "" {
		payload["body"] = body
	}
	if state != "" {
		payload["state"] = state
	}
	if len(payload) == 0 {
		return nil
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal github patch payload: %w", err)
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues/%d", owner, repo, number)
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("build github patch request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("github patch request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("github PATCH /repos/%s/%s/issues/%d returned %d", owner, repo, number, resp.StatusCode)
	}
	return nil
}
