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

// registerTelegramListeners subscribes to the event bus and forwards relevant
// events as Telegram DMs.
//
// Delivered events:
//   - task:completed → DM issue creator: agent done, review buttons
//   - task:failed    → DM issue creator: agent blocked, approve/reject buttons
//   - inbox:new      → DM member recipients for action_required / attention items
//   - issue:updated  → DM new assignee when assignee changes
func registerTelegramListeners(bus *events.Bus, queries *db.Queries) {
	ctx := context.Background()
	frontend := strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN"))

	// task:completed — agent finished, notify the issue creator.
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
		integration, err := queries.GetTelegramIntegration(ctx, issue.WorkspaceID)
		if err != nil {
			return
		}
		// Notify the issue creator (the human who submitted the task).
		if issue.CreatorType != "member" || !issue.CreatorID.Valid {
			return
		}
		chatID, err := telegramResolveUser(ctx, queries, integration.WorkspaceID, issue.CreatorID)
		if err != nil {
			return
		}
		keyboard := handler.BuildIssueInlineKeyboard(issueID, []string{"approve", "reject", "done", "open"})

		// Include the agent's latest message if available.
		agentMsg := ""
		if latest, err := queries.GetLatestAgentComment(ctx, issue.ID); err == nil && latest.Content != "" {
			body := latest.Content
			if len(body) > 3000 {
				body = body[:3000] + "…"
			}
			agentMsg = "\n\n" + body
		}

		text := fmt.Sprintf("✅ Agent finished: *%s*%s\n\n_Reply to this message to add a comment._\nView: %s/issues/%s",
			issue.Title, agentMsg, frontend, issueID)
		if err := handler.TelegramSendMessage(ctx, integration.BotToken, chatID, text, keyboard); err != nil {
			slog.Warn("telegram: send failed for task:completed", "error", err)
		}
	})

	// task:failed — agent blocked, notify the issue creator.
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
		integration, err := queries.GetTelegramIntegration(ctx, issue.WorkspaceID)
		if err != nil {
			return
		}
		if issue.CreatorType != "member" || !issue.CreatorID.Valid {
			return
		}
		chatID, err := telegramResolveUser(ctx, queries, integration.WorkspaceID, issue.CreatorID)
		if err != nil {
			return
		}
		keyboard := handler.BuildIssueInlineKeyboard(issueID, []string{"approve", "reject", "open"})

		agentMsg := ""
		if latest, err := queries.GetLatestAgentComment(ctx, issue.ID); err == nil && latest.Content != "" {
			body := latest.Content
			if len(body) > 3000 {
				body = body[:3000] + "…"
			}
			agentMsg = "\n\n" + body
		}

		text := fmt.Sprintf("⚠️ Agent needs help on: *%s*%s\n\n_Reply to this message to add a comment._\nView: %s/issues/%s",
			issue.Title, agentMsg, frontend, issueID)
		if err := handler.TelegramSendMessage(ctx, integration.BotToken, chatID, text, keyboard); err != nil {
			slog.Warn("telegram: send failed for task:failed", "error", err)
		}
	})

	// inbox:new — forward inbox items to Telegram for member recipients.
	// Covers task:failed, task:completed, issue_assigned, new_comment, mentions, etc.
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
		// Forward action_required / attention always; also forward task_failed and
		// status_changed-to-in_review for belt-and-suspenders coverage.
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
		integration, err := queries.GetTelegramIntegration(ctx, wsUUID)
		if err != nil {
			return
		}

		chatID, err := telegramResolveUser(ctx, queries, integration.WorkspaceID, parseUUID(recipientID))
		if err != nil {
			return
		}

		title, _ := item["title"].(string)
		body, _ := item["body"].(string)
		issueID, _ := item["issue_id"].(string)
		notifType, _ := item["type"].(string)

		emoji := telegramInboxEmoji(notifType)
		var text string
		if issueID != "" {
			text = fmt.Sprintf("%s *%s*", emoji, title)
			if body != "" {
				text += "\n" + body
			}
			text += fmt.Sprintf("\n\n_Reply to this message to post a comment._\nView: %s/issues/%s", frontend, issueID)
		} else {
			text = fmt.Sprintf("%s *%s*", emoji, title)
			if body != "" {
				text += "\n" + body
			}
		}

		// Choose buttons based on notification type.
		var actions []string
		switch notifType {
		case "task_failed":
			actions = []string{"approve", "reject", "open"}
		case "task_completed", "quick_create_done":
			actions = []string{"approve", "reject", "done", "open"}
		default:
			if issueID != "" {
				actions = []string{"open"}
			}
		}
		keyboard := handler.BuildIssueInlineKeyboard(issueID, actions)

		if err := handler.TelegramSendMessage(ctx, integration.BotToken, chatID, text, keyboard); err != nil {
			slog.Warn("telegram: send failed for inbox:new", "type", notifType, "error", err)
		}
	})

	// issue:updated — notify new assignee when the assignee field changes.
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
		integration, err := queries.GetTelegramIntegration(ctx, wsUUID)
		if err != nil {
			return
		}

		if *issue.AssigneeType == "member" {
			assigneeUUID := parseUUID(*issue.AssigneeID)
			chatID, err := telegramResolveUser(ctx, queries, integration.WorkspaceID, assigneeUUID)
			if err != nil {
				return
			}
			text := fmt.Sprintf("📋 You were assigned: *%s*\n\nView: %s/issues/%s",
				issue.Title, frontend, issue.ID)
			if err := handler.TelegramSendMessage(ctx, integration.BotToken, chatID, text, nil); err != nil {
				slog.Warn("telegram: send failed for issue:assigned (member)", "error", err)
			}
		} else if *issue.AssigneeType == "agent" && issue.CreatorType == "member" && issue.CreatorID != "" {
			creatorUUID := parseUUID(issue.CreatorID)
			chatID, err := telegramResolveUser(ctx, queries, integration.WorkspaceID, creatorUUID)
			if err != nil {
				return
			}
			text := fmt.Sprintf("🤖 Agent assigned to: *%s*\n\nView: %s/issues/%s",
				issue.Title, frontend, issue.ID)
			if err := handler.TelegramSendMessage(ctx, integration.BotToken, chatID, text, nil); err != nil {
				slog.Warn("telegram: send failed for issue:assigned (agent)", "error", err)
			}
		}
	})
}

// telegramInboxEmoji returns a leading emoji for a given inbox notification type.
func telegramInboxEmoji(notifType string) string {
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

// telegramResolveUser looks up the Telegram chat ID for a Multica user UUID.
func telegramResolveUser(ctx context.Context, queries *db.Queries, wsUUID, userUUID pgtype.UUID) (int64, error) {
	link, err := queries.GetTelegramUserLink(ctx, db.GetTelegramUserLinkParams{
		WorkspaceID: wsUUID,
		UserID:      userUUID,
	})
	if err != nil {
		return 0, err
	}
	return link.TelegramChatID, nil
}
