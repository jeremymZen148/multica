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
//   - task:completed → DM issue creator: agent done, review buttons
//   - task:failed    → DM issue creator: agent blocked, action buttons
//   - inbox:new      → DM member recipients for action_required / attention items
//   - issue:updated  → DM new assignee when assignee changes
func registerSlackListeners(bus *events.Bus, queries *db.Queries) {
	ctx := context.Background()
	frontend := strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN"))

	// task:failed — agent needs human input, notify issue creator.
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
		if err != nil {
			return
		}
		integration, err := queries.GetSlackIntegration(ctx, issue.WorkspaceID)
		if err != nil {
			return
		}
		if issue.CreatorType != "member" || !issue.CreatorID.Valid {
			return
		}

		agentMsg := ""
		if latest, err := queries.GetLatestAgentComment(ctx, issue.ID); err == nil && latest.Content != "" {
			body := latest.Content
			if len(body) > 3000 {
				body = body[:3000] + "…"
			}
			agentMsg = "\n\n" + body
		}

		blocks := handler.BuildIssueActionBlocks(issueID, issue.Title, issue.Status, frontend,
			[]string{"approve", "reject", "view"})
		fallback := fmt.Sprintf("⚠️ Agent needs help on: %s%s", issue.Title, agentMsg)

		slackUserID, err := slackResolveUser(ctx, queries, integration.WorkspaceID, issue.CreatorID)
		if err == nil {
			if err := handler.SendSlackDM(ctx, integration.BotToken, slackUserID, blocks, fallback); err != nil {
				slog.Warn("slack: DM failed for task:failed", "error", err)
			}
			return
		}
		if integration.DefaultChannelID.Valid {
			if err := handler.SendSlackMessage(ctx, integration.BotToken, integration.DefaultChannelID.String, blocks, fallback); err != nil {
				slog.Warn("slack: channel message failed for task:failed", "error", err)
			}
		}
	})

	// task:completed — agent finished, notify issue creator.
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
		if err != nil {
			return
		}
		integration, err := queries.GetSlackIntegration(ctx, issue.WorkspaceID)
		if err != nil {
			return
		}
		if issue.CreatorType != "member" || !issue.CreatorID.Valid {
			return
		}

		agentMsg := ""
		if latest, err := queries.GetLatestAgentComment(ctx, issue.ID); err == nil && latest.Content != "" {
			body := latest.Content
			if len(body) > 3000 {
				body = body[:3000] + "…"
			}
			agentMsg = "\n\n" + body
		}

		blocks := handler.BuildIssueActionBlocks(issueID, issue.Title, issue.Status, frontend,
			[]string{"approve", "reject", "done", "view"})
		fallback := fmt.Sprintf("✅ Agent finished: %s — ready for review.%s", issue.Title, agentMsg)

		slackUserID, err := slackResolveUser(ctx, queries, integration.WorkspaceID, issue.CreatorID)
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

	// inbox:new — forward inbox items to Slack for member recipients.
	bus.Subscribe(protocol.EventInboxNew, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		item, ok := payload["item"].(map[string]any)
		if !ok {
			return
		}

		recipientType, _ := item["recipient_type"].(string)
		if recipientType != "member" {
			return
		}
		severity, _ := item["severity"].(string)
		notifTypeCheck, _ := item["type"].(string)
		issueStatusCheck, _ := item["issue_status"].(string)
		isImportant := severity == "action_required" || severity == "attention" ||
			notifTypeCheck == "task_failed" ||
			(notifTypeCheck == "status_changed" && issueStatusCheck == "in_review")
		if !isImportant {
			return
		}

		recipientID, _ := item["recipient_id"].(string)
		if recipientID == "" {
			return
		}

		wsUUID := parseUUID(e.WorkspaceID)
		integration, err := queries.GetSlackIntegration(ctx, wsUUID)
		if err != nil {
			return
		}

		slackUserID, err := slackResolveUser(ctx, queries, integration.WorkspaceID, parseUUID(recipientID))
		if err != nil {
			return
		}

		title, _ := item["title"].(string)
		body, _ := item["body"].(string)
		issueID, _ := item["issue_id"].(string)
		notifType, _ := item["type"].(string)

		emoji := slackInboxEmoji(notifType)
		fallback := emoji + " " + title
		if body != "" {
			fallback += "\n" + body
		}

		var actions []string
		switch notifType {
		case "task_failed":
			actions = []string{"approve", "reject", "view"}
		case "task_completed", "quick_create_done":
			actions = []string{"approve", "reject", "done", "view"}
		default:
			if issueID != "" {
				actions = []string{"view"}
			}
		}

		var blocks []map[string]any
		if issueID != "" {
			blocks = handler.BuildIssueActionBlocks(issueID, title, "", frontend, actions)
		} else {
			blocks = handler.BuildIssueActionBlocks("", title, "", frontend, nil)
		}

		if err := handler.SendSlackDM(ctx, integration.BotToken, slackUserID, blocks, fallback); err != nil {
			slog.Warn("slack: DM failed for inbox:new", "type", notifType, "error", err)
		}
	})

	// issue:updated — DM new assignee when assignee changes.
	// For member assignees: DM the assignee. For agent assignees: DM the issue creator.
	bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		if assigneeChanged, _ := payload["assignee_changed"].(bool); !assigneeChanged {
			return
		}
		issue, ok := payload["issue"].(handler.IssueResponse)
		if !ok || issue.AssigneeID == nil || issue.AssigneeType == nil {
			return
		}

		wsUUID := parseUUID(e.WorkspaceID)
		integration, err := queries.GetSlackIntegration(ctx, wsUUID)
		if err != nil {
			return
		}

		if *issue.AssigneeType == "member" {
			assigneeUUID := parseUUID(*issue.AssigneeID)
			slackUserID, err := slackResolveUser(ctx, queries, integration.WorkspaceID, assigneeUUID)
			if err != nil {
				return
			}
			blocks := handler.BuildIssueActionBlocks(issue.ID, issue.Title, issue.Status, frontend, []string{"view"})
			if err := handler.SendSlackDM(ctx, integration.BotToken, slackUserID, blocks,
				fmt.Sprintf("📋 You were assigned: %s", issue.Title)); err != nil {
				slog.Warn("slack: DM failed for issue:assigned (member)", "error", err)
			}
		} else if *issue.AssigneeType == "agent" && issue.CreatorType == "member" && issue.CreatorID != "" {
			creatorUUID := parseUUID(issue.CreatorID)
			slackUserID, err := slackResolveUser(ctx, queries, integration.WorkspaceID, creatorUUID)
			if err != nil {
				return
			}
			blocks := handler.BuildIssueActionBlocks(issue.ID, issue.Title, issue.Status, frontend, []string{"view"})
			if err := handler.SendSlackDM(ctx, integration.BotToken, slackUserID, blocks,
				fmt.Sprintf("🤖 Agent assigned to: %s", issue.Title)); err != nil {
				slog.Warn("slack: DM failed for issue:assigned (agent)", "error", err)
			}
		}
	})
}

// slackInboxEmoji returns a leading emoji string for a given inbox notification type.
func slackInboxEmoji(notifType string) string {
	switch notifType {
	case "task_failed":
		return "⚠️ Agent needs help:"
	case "task_completed", "quick_create_done":
		return "✅ Agent finished — ready for review:"
	case "quick_create_failed":
		return "❌ Agent task failed:"
	case "issue_assigned":
		return "📋 You were assigned:"
	case "new_comment":
		return "💬 New comment:"
	case "mentioned":
		return "🔔 You were mentioned:"
	case "status_changed":
		return "🔄 Status changed:"
	case "autopilot_paused":
		return "⏸ Autopilot paused:"
	default:
		return "🔔"
	}
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
