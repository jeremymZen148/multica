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

// registerTelegramListeners subscribes to the event bus and forwards relevant events
// as Telegram messages with inline keyboard buttons.
//
// Delivered events:
//   - task:failed    → DM assignee: agent blocked, inline approve/reject
//   - task:completed → DM assignee: agent done, inline approve/reject
//   - issue:updated  → DM new assignee when assignee changes
func registerTelegramListeners(bus *events.Bus, queries *db.Queries) {
	ctx := context.Background()
	frontend := strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN"))

	// task:failed — agent is blocked, prompt the assignee.
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

		integration, err := queries.GetTelegramIntegration(ctx, issue.WorkspaceID)
		if err != nil {
			return
		}

		chatID, err := telegramResolveUser(ctx, queries, integration.WorkspaceID, issue.AssigneeID)
		if err != nil {
			return
		}

		keyboard := handler.BuildIssueInlineKeyboard(issueID, []string{"approve", "reject"})
		text := fmt.Sprintf("⚠️ Agent blocked on: *%s*\nStatus: _%s_\n\nView: %s/issues/%s",
			issue.Title, issue.Status, frontend, issueID)
		if err := handler.TelegramSendMessage(ctx, integration.BotToken, chatID, text, keyboard); err != nil {
			slog.Warn("telegram: send failed for task:failed", "error", err)
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

		integration, err := queries.GetTelegramIntegration(ctx, issue.WorkspaceID)
		if err != nil {
			return
		}

		chatID, err := telegramResolveUser(ctx, queries, integration.WorkspaceID, issue.AssigneeID)
		if err != nil {
			return
		}

		keyboard := handler.BuildIssueInlineKeyboard(issueID, []string{"approve", "reject"})
		text := fmt.Sprintf("✅ Agent completed: *%s*\nReady for review.\n\nView: %s/issues/%s",
			issue.Title, frontend, issueID)
		if err := handler.TelegramSendMessage(ctx, integration.BotToken, chatID, text, keyboard); err != nil {
			slog.Warn("telegram: send failed for task:completed", "error", err)
		}
	})

	// issue:updated — notify new assignee.
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
		integration, err := queries.GetTelegramIntegration(ctx, wsUUID)
		if err != nil {
			return
		}
		assigneeUUID := parseUUID(*issue.AssigneeID)
		chatID, err := telegramResolveUser(ctx, queries, integration.WorkspaceID, assigneeUUID)
		if err != nil {
			return
		}

		text := fmt.Sprintf("📋 You were assigned: *%s*\n\nView: %s/issues/%s",
			issue.Title, frontend, issue.ID)
		if err := handler.TelegramSendMessage(ctx, integration.BotToken, chatID, text, nil); err != nil {
			slog.Warn("telegram: send failed for issue:assigned", "error", err)
		}
	})
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

