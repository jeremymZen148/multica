package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// registerSlackListeners subscribes to the event bus and forwards relevant events
// as Slack DMs or channel messages using the workspace's installed bot token.
//
// Delivered events:
//   - task:failed    → DM assignee: agent blocked, action buttons
//   - task:completed → DM assignee: agent done, review buttons
//   - issue:updated  → DM new assignee when assignee changes
func registerSlackListeners(bus *events.Bus, queries *db.Queries) {
	ctx := context.Background()
	frontend := strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN"))

	// task:failed — agent needs human input.
	bus.Subscribe(protocol.EventTaskFailed, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		issueID, _ := payload["issue_id"].(string)
		if issueID == "" {
			return
		}
		issue, err := queries.GetIssue(ctx, parseUUID(issueID))
		if err != nil || !issue.AssigneeID.Valid {
			return
		}
		integration, err := queries.GetSlackIntegration(ctx, issue.WorkspaceID)
		if err != nil {
			return
		}
		blocks := handler.BuildIssueActionBlocks(issueID, issue.Title, issue.Status, frontend,
			[]string{"approve", "reject", "view"})
		fallback := fmt.Sprintf("⚠️ Agent blocked on: %s", issue.Title)

		slackUserID, err := slackResolveUser(ctx, queries, integration.WorkspaceID, issue.AssigneeID)
		if err == nil {
			if err := handler.SendSlackDM(ctx, integration.BotToken, slackUserID, blocks, fallback); err != nil {
				slog.Warn("slack: DM failed for task:failed", "error", err)
			}
			return
		}
		// Fall back to workspace default channel.
		if integration.DefaultChannelID.Valid {
			if err := handler.SendSlackMessage(ctx, integration.BotToken, integration.DefaultChannelID.String, blocks, fallback); err != nil {
				slog.Warn("slack: channel message failed for task:failed", "error", err)
			}
		}
	})

	// task:completed — agent finished, time to review.
	bus.Subscribe(protocol.EventTaskCompleted, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		issueID, _ := payload["issue_id"].(string)
		if issueID == "" {
			return
		}
		issue, err := queries.GetIssue(ctx, parseUUID(issueID))
		if err != nil || !issue.AssigneeID.Valid {
			return
		}
		integration, err := queries.GetSlackIntegration(ctx, issue.WorkspaceID)
		if err != nil {
			return
		}
		blocks := handler.BuildIssueActionBlocks(issueID, issue.Title, issue.Status, frontend,
			[]string{"approve", "reject", "view"})
		fallback := fmt.Sprintf("✅ Agent completed: %s — ready for review.", issue.Title)

		slackUserID, err := slackResolveUser(ctx, queries, integration.WorkspaceID, issue.AssigneeID)
		if err == nil {
			if err := handler.SendSlackDM(ctx, integration.BotToken, slackUserID, blocks, fallback); err != nil {
				slog.Warn("slack: DM failed for task:completed", "error", err)
			}
			return
		}
		if integration.DefaultChannelID.Valid {
			if err := handler.SendSlackMessage(ctx, integration.BotToken, integration.DefaultChannelID.String, blocks, fallback); err != nil {
				slog.Warn("slack: channel message failed for task:completed", "error", err)
			}
		}
	})

	// issue:updated — DM new assignee when assignee changes.
	bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		if assigneeChanged, _ := payload["assignee_changed"].(bool); !assigneeChanged {
			return
		}
		issue, ok := payload["issue"].(handler.IssueResponse)
		if !ok || issue.AssigneeID == nil || issue.AssigneeType == nil || *issue.AssigneeType != "member" {
			return
		}

		wsUUID := parseUUID(e.WorkspaceID)
		integration, err := queries.GetSlackIntegration(ctx, wsUUID)
		if err != nil {
			return
		}
		assigneeUUID := parseUUID(*issue.AssigneeID)
		slackUserID, err := slackResolveUser(ctx, queries, integration.WorkspaceID, assigneeUUID)
		if err != nil {
			return
		}
		blocks := handler.BuildIssueActionBlocks(issue.ID, issue.Title, issue.Status, frontend, []string{"view"})
		if err := handler.SendSlackDM(ctx, integration.BotToken, slackUserID, blocks,
			fmt.Sprintf("📋 You were assigned: %s", issue.Title)); err != nil {
			slog.Warn("slack: DM failed for issue:assigned", "error", err)
		}
	})
}

// slackResolveUser looks up the Slack user ID for a Multica user UUID.
func slackResolveUser(ctx context.Context, queries *db.Queries, wsUUID, userUUID pgtype.UUID) (string, error) {
	link, err := queries.GetSlackUserLink(ctx, db.GetSlackUserLinkParams{
		WorkspaceID: wsUUID,
		UserID:      userUUID,
	})
	if err != nil {
		return "", err
	}
	return link.SlackUserID, nil
}


