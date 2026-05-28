package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/nlbot"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ── Response shapes ───────────────────────────────────────────────────────────

type TelegramIntegrationResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	BotUsername string  `json:"bot_username"`
	CreatedAt   string  `json:"created_at"`
}

type TelegramUserLinkResponse struct {
	UserID           string  `json:"user_id"`
	TelegramChatID   int64   `json:"telegram_chat_id"`
	TelegramUsername *string `json:"telegram_username"`
}

func telegramIntegrationToResponse(i db.TelegramIntegration) TelegramIntegrationResponse {
	return TelegramIntegrationResponse{
		ID:          uuidToString(i.ID),
		WorkspaceID: uuidToString(i.WorkspaceID),
		BotUsername: i.BotUsername,
		CreatedAt:   timestampToString(i.CreatedAt),
	}
}

// ── CRUD ─────────────────────────────────────────────────────────────────────

type upsertTelegramRequest struct {
	BotToken string `json:"bot_token"`
}

// UpsertTelegramIntegration creates or updates the Telegram bot for the workspace.
// POST /api/integrations/telegram
func (h *Handler) UpsertTelegramIntegration(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	wsID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace_id")
	if !ok {
		return
	}

	var req upsertTelegramRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.BotToken) == "" {
		writeError(w, http.StatusBadRequest, "bot_token is required")
		return
	}

	// Validate the token by calling Telegram's getMe API.
	botUser, err := telegramGetMe(ctx, req.BotToken)
	if err != nil {
		slog.Warn("telegram getMe failed", "error", err)
		writeError(w, http.StatusBadRequest, "invalid bot token: "+err.Error())
		return
	}

	actorID := r.Header.Get("X-User-ID")
	integration, err := h.Queries.UpsertTelegramIntegration(ctx, db.UpsertTelegramIntegrationParams{
		WorkspaceID:   wsUUID,
		BotToken:      req.BotToken,
		BotUsername:   botUser.Username,
		InstalledByID: optionalUUID(actorID),
	})
	if err != nil {
		slog.Error("telegram integration upsert failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to save telegram integration")
		return
	}

	// Register the webhook with Telegram so it POSTs updates to our server.
	webhookURL := telegramWebhookURL(wsID)
	if err := telegramSetWebhook(ctx, req.BotToken, webhookURL); err != nil {
		slog.Warn("telegram set webhook failed", "error", err, "url", webhookURL)
	}

	h.publish(protocol.EventTelegramIntegrationConnected, wsID, "member", actorID,
		map[string]any{"integration": telegramIntegrationToResponse(integration)})

	writeJSON(w, http.StatusOK, telegramIntegrationToResponse(integration))
}

// GetTelegramIntegration returns the workspace's Telegram integration (token hidden).
// GET /api/integrations/telegram
func (h *Handler) GetTelegramIntegration(w http.ResponseWriter, r *http.Request) {
	wsID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace_id")
	if !ok {
		return
	}
	integration, err := h.Queries.GetTelegramIntegration(r.Context(), wsUUID)
	if err == pgx.ErrNoRows {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load telegram integration")
		return
	}
	writeJSON(w, http.StatusOK, telegramIntegrationToResponse(integration))
}

// DeleteTelegramIntegration removes the Telegram integration for the workspace.
// DELETE /api/integrations/telegram
func (h *Handler) DeleteTelegramIntegration(w http.ResponseWriter, r *http.Request) {
	wsID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace_id")
	if !ok {
		return
	}

	// Try to unregister the webhook before deleting.
	if integration, err := h.Queries.GetTelegramIntegration(r.Context(), wsUUID); err == nil {
		if err := telegramDeleteWebhook(r.Context(), integration.BotToken); err != nil {
			slog.Warn("telegram delete webhook failed", "error", err)
		}
	}

	if err := h.Queries.DeleteTelegramIntegration(r.Context(), wsUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete telegram integration")
		return
	}
	actorID := r.Header.Get("X-User-ID")
	h.publish(protocol.EventTelegramIntegrationDisconnected, wsID, "member", actorID, nil)
	w.WriteHeader(http.StatusNoContent)
}

// ── User account linking ──────────────────────────────────────────────────────

type linkTelegramUserRequest struct {
	TelegramChatID   int64   `json:"telegram_chat_id"`
	TelegramUsername *string `json:"telegram_username"`
}

// LinkTelegramUser links the authenticated user to their Telegram chat ID.
// POST /api/integrations/telegram/link
func (h *Handler) LinkTelegramUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	wsID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace_id")
	if !ok {
		return
	}

	var req linkTelegramUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TelegramChatID == 0 {
		writeError(w, http.StatusBadRequest, "telegram_chat_id is required")
		return
	}

	userID := r.Header.Get("X-User-ID")
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user_id")
	if !ok {
		return
	}

	username := util.StrToText("")
	if req.TelegramUsername != nil {
		username = util.StrToText(*req.TelegramUsername)
	}

	link, err := h.Queries.UpsertTelegramUserLink(ctx, db.UpsertTelegramUserLinkParams{
		WorkspaceID:      wsUUID,
		UserID:           userUUID,
		TelegramChatID:   req.TelegramChatID,
		TelegramUsername: username,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to link telegram user")
		return
	}

	writeJSON(w, http.StatusOK, TelegramUserLinkResponse{
		UserID:           util.UUIDToString(link.UserID),
		TelegramChatID:   link.TelegramChatID,
		TelegramUsername: textToPtr(link.TelegramUsername),
	})
}

// UnlinkTelegramUser removes the Telegram link for the authenticated user.
// DELETE /api/integrations/telegram/link
func (h *Handler) UnlinkTelegramUser(w http.ResponseWriter, r *http.Request) {
	wsID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace_id")
	if !ok {
		return
	}
	userID := r.Header.Get("X-User-ID")
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user_id")
	if !ok {
		return
	}
	if err := h.Queries.DeleteTelegramUserLink(r.Context(), db.DeleteTelegramUserLinkParams{
		WorkspaceID: wsUUID,
		UserID:      userUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unlink telegram user")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── Inbound webhook ───────────────────────────────────────────────────────────

// HandleTelegramWebhook receives updates from the Telegram Bot API.
// The URL is workspace-scoped so Telegram knows which bot to route to.
// POST /api/webhooks/telegram/{workspaceId}
func (h *Handler) HandleTelegramWebhook(w http.ResponseWriter, r *http.Request) {
	wsID := chi.URLParam(r, "workspaceId")
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspaceId")
	if !ok {
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		w.WriteHeader(http.StatusOK) // always 200 to Telegram
		return
	}

	var update telegramUpdate
	if err := json.Unmarshal(body, &update); err != nil {
		w.WriteHeader(http.StatusOK)
		return
	}

	ctx := r.Context()
	integration, err := h.Queries.GetTelegramIntegration(ctx, wsUUID)
	if err != nil {
		w.WriteHeader(http.StatusOK)
		return
	}

	h.ProcessTelegramUpdate(ctx, wsID, wsUUID, integration, update)
	w.WriteHeader(http.StatusOK)
}

// ProcessTelegramUpdate handles a parsed Telegram update — called by both the
// webhook HTTP handler and the polling loop.
func (h *Handler) ProcessTelegramUpdate(ctx context.Context, wsID string, wsUUID pgtype.UUID, integration db.TelegramIntegration, update telegramUpdate) {
	// Handle callback_query (inline keyboard button press).
	if update.CallbackQuery != nil {
		h.handleTelegramCallback(ctx, wsID, wsUUID, integration, update.CallbackQuery)
		return
	}

	// Handle text messages.
	if update.Message == nil || update.Message.Text == "" {
		return
	}

	chatID := update.Message.Chat.ID
	text := strings.TrimSpace(update.Message.Text)

	// Resolve linked Multica user.
	link, err := h.Queries.GetTelegramUserLinkByChatID(ctx, db.GetTelegramUserLinkByChatIDParams{
		WorkspaceID:    wsUUID,
		TelegramChatID: chatID,
	})
	if err == pgx.ErrNoRows {
		_ = TelegramSendMessage(ctx, integration.BotToken, chatID,
			"⚠️ Your Telegram account is not linked. Visit Multica → Settings → Integrations → Telegram.", nil)
		return
	}
	if err != nil {
		return
	}
	actorID := util.UUIDToString(link.UserID)

	// Reply-to-message → post as comment on the issue from the original notification.
	if update.Message.ReplyToMessage != nil && !strings.HasPrefix(text, "/") {
		issueID := extractIssueIDFromText(update.Message.ReplyToMessage.Text)
		if issueID != "" {
			reply := h.telegramComment(ctx, wsID, actorID, []string{issueID, text})
			if reply != "" {
				_ = TelegramSendMessage(ctx, integration.BotToken, chatID, reply, nil)
			}
			return
		}
	}

	parts := strings.Fields(text)
	if len(parts) == 0 {
		return
	}

	var reply string
	if !strings.HasPrefix(parts[0], "/") {
		// Free-text message — route to NL bot.
		nlReply, err := nlbot.Process(ctx, h.Queries, wsUUID, link.UserID, text)
		if err != nil {
			reply = "⚠️ " + err.Error()
		} else {
			reply = nlReply
		}
	} else {
		cmd := strings.ToLower(strings.TrimPrefix(parts[0], "/"))
		args := parts[1:]
		switch cmd {
		case "approve":
			reply = h.telegramApprove(ctx, wsID, actorID, args)
		case "reject":
			reply = h.telegramReject(ctx, wsID, actorID, args)
		case "done":
			reply = h.telegramMarkDone(ctx, wsID, actorID, args)
		case "comment":
			reply = h.telegramComment(ctx, wsID, actorID, args)
		case "assign":
			reply = h.telegramAssign(ctx, wsID, actorID, args)
		case "start", "help":
			reply = "Welcome to Multica bot! Available commands:\n" +
				"/approve <issue-id> — move to In Review\n" +
				"/reject <issue-id> — send back to Todo\n" +
				"/done <issue-id> — mark as Done\n" +
				"/comment <issue-id> <text...> — post a comment\n" +
				"/assign <issue-id> <username-or-agent> — reassign\n\n" +
				"💡 *Tip:* Reply directly to a notification message to post a comment on that issue."
		default:
			// Unknown slash command — also route to NL bot.
			nlReply, err := nlbot.Process(ctx, h.Queries, wsUUID, link.UserID, text)
			if err != nil {
				reply = fmt.Sprintf("Unknown command /%s. Try /help.", cmd)
			} else {
				reply = nlReply
			}
		}
	}

	if reply != "" {
		_ = TelegramSendMessage(ctx, integration.BotToken, chatID, reply, nil)
	}
}

func (h *Handler) handleTelegramCallback(
	ctx context.Context,
	wsID string,
	wsUUID pgtype.UUID,
	integration db.TelegramIntegration,
	cb *telegramCallbackQuery,
) {
	wsUUIDParsed := wsUUID
	link, err := h.Queries.GetTelegramUserLinkByChatID(ctx, db.GetTelegramUserLinkByChatIDParams{
		WorkspaceID:    wsUUIDParsed,
		TelegramChatID: cb.From.ID,
	})
	if err != nil {
		return
	}
	actorID := util.UUIDToString(link.UserID)

	data := strings.SplitN(cb.Data, ":", 2)
	if len(data) != 2 {
		return
	}
	action, issueID := data[0], data[1]

	var reply string
	switch action {
	case "approve":
		reply = h.telegramApprove(ctx, wsID, actorID, []string{issueID})
	case "reject":
		reply = h.telegramReject(ctx, wsID, actorID, []string{issueID})
	case "done":
		reply = h.telegramMarkDone(ctx, wsID, actorID, []string{issueID})
	}

	if reply != "" {
		_ = TelegramSendMessage(ctx, integration.BotToken, cb.From.ID, reply, nil)
	}
}

func (h *Handler) telegramApprove(ctx context.Context, wsID, actorID string, args []string) string {
	if len(args) == 0 {
		return "Usage: /approve <issue-id>"
	}
	issue, err := h.getIssueInWorkspace(ctx, wsID, args[0])
	if err != nil {
		return fmt.Sprintf("⚠️ Issue %q not found.", args[0])
	}
	prev := issue.Status
	updated, err := h.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID:     issue.ID,
		Status: "in_review",
	})
	if err != nil {
		return "⚠️ Failed to update issue."
	}
	h.publish(protocol.EventIssueUpdated, wsID, "member", actorID, map[string]any{
		"issue":          updated,
		"status_changed": true,
		"prev_status":    prev,
	})
	return fmt.Sprintf("✅ *%s* moved to In Review.", updated.Title)
}

func (h *Handler) telegramReject(ctx context.Context, wsID, actorID string, args []string) string {
	if len(args) == 0 {
		return "Usage: /reject <issue-id>"
	}
	issue, err := h.getIssueInWorkspace(ctx, wsID, args[0])
	if err != nil {
		return fmt.Sprintf("⚠️ Issue %q not found.", args[0])
	}
	prev := issue.Status
	updated, err := h.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID:     issue.ID,
		Status: "todo",
	})
	if err != nil {
		return "⚠️ Failed to update issue."
	}
	h.publish(protocol.EventIssueUpdated, wsID, "member", actorID, map[string]any{
		"issue":          updated,
		"status_changed": true,
		"prev_status":    prev,
	})
	return fmt.Sprintf("↩️ *%s* sent back to Todo.", updated.Title)
}

func (h *Handler) telegramComment(ctx context.Context, wsID, actorID string, args []string) string {
	if len(args) < 2 {
		return "Usage: /comment <issue-id> <text...>"
	}
	issue, err := h.getIssueInWorkspace(ctx, wsID, args[0])
	if err != nil {
		return fmt.Sprintf("⚠️ Issue %q not found.", args[0])
	}
	content := strings.Join(args[1:], " ")

	// Thread the reply under the latest agent root comment so it appears
	// inside the agent's output thread rather than as a new top-level comment.
	parentID := pgtype.UUID{}
	if agentRoot, err := h.Queries.GetLatestAgentRootComment(ctx, issue.ID); err == nil {
		parentID = agentRoot.ID
	}

	comment, err := h.Queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     issue.ID,
		WorkspaceID: parseUUID(wsID),
		AuthorType:  "member",
		AuthorID:    parseUUID(actorID),
		Content:     content,
		Type:        "comment",
		ParentID:    parentID,
	})
	if err != nil {
		return "⚠️ Failed to post comment."
	}
	h.publish(protocol.EventCommentCreated, wsID, "member", actorID, map[string]any{
		"comment":      comment,
		"issue_title":  issue.Title,
		"issue_status": issue.Status,
	})

	// Mirror the HTTP comment handler: if the issue is assigned to an agent
	// with on_comment trigger, enqueue a task so the agent picks up the reply.
	if h.shouldEnqueueOnComment(ctx, issue, "member", actorID) {
		if _, err := h.TaskService.EnqueueTaskForIssue(ctx, issue, comment.ID); err != nil {
			slog.Warn("telegram: enqueue agent task on comment failed", "issue_id", issue.ID, "error", err)
		}
	}

	return fmt.Sprintf("💬 Comment posted on *%s*.", issue.Title)
}

func (h *Handler) telegramMarkDone(ctx context.Context, wsID, actorID string, args []string) string {
	if len(args) == 0 {
		return "Usage: /done <issue-id>"
	}
	issue, err := h.getIssueInWorkspace(ctx, wsID, args[0])
	if err != nil {
		return fmt.Sprintf("⚠️ Issue %q not found.", args[0])
	}
	prev := issue.Status
	updated, err := h.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID:     issue.ID,
		Status: "done",
	})
	if err != nil {
		return "⚠️ Failed to update issue."
	}
	h.publish(protocol.EventIssueUpdated, wsID, "member", actorID, map[string]any{
		"issue":          updated,
		"status_changed": true,
		"prev_status":    prev,
	})
	return fmt.Sprintf("✔️ *%s* marked as Done.", updated.Title)
}

func (h *Handler) telegramAssign(ctx context.Context, wsID, actorID string, args []string) string {
	if len(args) < 2 {
		return "Usage: /assign <issue-id> <member-email-or-agent-name>"
	}
	issue, err := h.getIssueInWorkspace(ctx, wsID, args[0])
	if err != nil {
		return fmt.Sprintf("⚠️ Issue %q not found.", args[0])
	}
	target := strings.Join(args[1:], " ")

	wsUUID := parseUUID(wsID)
	pos := pgtype.Float8{Float64: issue.Position, Valid: true}

	// Try to find a member by email or name.
	members, err := h.Queries.ListMembersWithUser(ctx, wsUUID)
	if err == nil {
		for _, m := range members {
			if strings.EqualFold(m.UserEmail, target) || strings.EqualFold(m.UserName, target) {
				updated, err := h.Queries.UpdateIssue(ctx, db.UpdateIssueParams{
					ID:            issue.ID,
					Title:         util.StrToText(issue.Title),
					Description:   issue.Description,
					Status:        util.StrToText(issue.Status),
					Priority:      util.StrToText(issue.Priority),
					AssigneeType:  util.StrToText("member"),
					AssigneeID:    m.UserID,
					Position:      pos,
					DueDate:       issue.DueDate,
					ParentIssueID: issue.ParentIssueID,
					ProjectID:     issue.ProjectID,
				})
				if err != nil {
					return "⚠️ Failed to reassign."
				}
				h.publish(protocol.EventIssueUpdated, wsID, "member", actorID, map[string]any{
					"issue":            issueToResponse(updated, ""),
					"assignee_changed": true,
				})
				return fmt.Sprintf("👤 *%s* reassigned to %s.", issue.Title, m.UserName)
			}
		}
	}

	// Try to find an agent by name.
	agents, err2 := h.Queries.ListAgents(ctx, wsUUID)
	if err2 == nil {
		for _, a := range agents {
			if strings.EqualFold(a.Name, target) {
				updated, err := h.Queries.UpdateIssue(ctx, db.UpdateIssueParams{
					ID:            issue.ID,
					Title:         util.StrToText(issue.Title),
					Description:   issue.Description,
					Status:        util.StrToText(issue.Status),
					Priority:      util.StrToText(issue.Priority),
					AssigneeType:  util.StrToText("agent"),
					AssigneeID:    a.ID,
					Position:      pos,
					DueDate:       issue.DueDate,
					ParentIssueID: issue.ParentIssueID,
					ProjectID:     issue.ProjectID,
				})
				if err != nil {
					return "⚠️ Failed to reassign."
				}
				h.publish(protocol.EventIssueUpdated, wsID, "member", actorID, map[string]any{
					"issue":            issueToResponse(updated, ""),
					"assignee_changed": true,
				})
				return fmt.Sprintf("🤖 *%s* assigned to agent %s.", issue.Title, a.Name)
			}
		}
	}

	return fmt.Sprintf("⚠️ Could not find member or agent %q. Use email, full name, or agent name.", target)
}

// extractIssueIDFromText parses an issue UUID or short ID from a message that
// contains a Multica issue URL (e.g. "http://localhost:3000/issues/<id>").
func extractIssueIDFromText(text string) string {
	const marker = "/issues/"
	idx := strings.LastIndex(text, marker)
	if idx < 0 {
		return ""
	}
	rest := text[idx+len(marker):]
	// Trim any trailing whitespace or punctuation.
	end := strings.IndexAny(rest, " \t\n\r)")
	if end >= 0 {
		rest = rest[:end]
	}
	return strings.TrimSpace(rest)
}

// ── Telegram Bot API helpers ──────────────────────────────────────────────────

// TelegramSendMessage sends a message to a Telegram chat with optional inline keyboard.
func TelegramSendMessage(ctx context.Context, botToken string, chatID int64, text string, keyboard *telegramInlineKeyboard) error {
	payload := map[string]any{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "Markdown",
	}
	if keyboard != nil {
		payload["reply_markup"] = keyboard
	}
	body, _ := json.Marshal(payload)
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var result struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	if !result.OK {
		return fmt.Errorf("telegram sendMessage error: %s", result.Description)
	}
	return nil
}

// BuildIssueInlineKeyboard creates a Telegram inline keyboard for an issue.
// BuildIssueInlineKeyboard builds an inline keyboard for an issue notification.
// actions: "approve", "reject", "done", "open" (URL button using appURL).
func BuildIssueInlineKeyboard(issueID string, actions []string, appURL ...string) *telegramInlineKeyboard {
	if len(actions) == 0 {
		return nil
	}
	baseURL := ""
	if len(appURL) > 0 {
		baseURL = appURL[0]
	}
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN"))
	}

	var actionRow []telegramInlineButton
	var urlRow []telegramInlineButton
	for _, action := range actions {
		switch action {
		case "approve":
			actionRow = append(actionRow, telegramInlineButton{Text: "✅ Approve", CallbackData: "approve:" + issueID})
		case "reject":
			actionRow = append(actionRow, telegramInlineButton{Text: "↩️ Reject", CallbackData: "reject:" + issueID})
		case "done":
			actionRow = append(actionRow, telegramInlineButton{Text: "✔️ Mark Done", CallbackData: "done:" + issueID})
		case "open":
			// Telegram rejects non-HTTPS URLs in inline keyboard buttons.
			if strings.HasPrefix(baseURL, "https://") {
				urlRow = append(urlRow, telegramInlineButton{Text: "🔗 Open in Multica", URL: baseURL + "/issues/" + issueID})
			}
		}
	}

	var rows [][]telegramInlineButton
	if len(actionRow) > 0 {
		rows = append(rows, actionRow)
	}
	if len(urlRow) > 0 {
		rows = append(rows, urlRow)
	}
	if len(rows) == 0 {
		return nil
	}
	return &telegramInlineKeyboard{InlineKeyboard: rows}
}

func telegramGetMe(ctx context.Context, botToken string) (*telegramUser, error) {
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/getMe", botToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result struct {
		OK     bool         `json:"ok"`
		Result *telegramUser `json:"result"`
		Error  string       `json:"description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if !result.OK || result.Result == nil {
		return nil, fmt.Errorf("getMe failed: %s", result.Error)
	}
	return result.Result, nil
}

func telegramSetWebhook(ctx context.Context, botToken, webhookURL string) error {
	payload, _ := json.Marshal(map[string]any{"url": webhookURL})
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/setWebhook", botToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var result struct {
		OK bool `json:"ok"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&result)
	return nil
}

func telegramDeleteWebhook(ctx context.Context, botToken string) error {
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/deleteWebhook", botToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func telegramWebhookURL(wsID string) string {
	base := strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN"))
	if base == "" {
		base = "http://localhost:8080"
	}
	return base + "/api/webhooks/telegram/" + wsID
}

// ── Telegram API types ────────────────────────────────────────────────────────

// TelegramUpdate is exported so the polling service can decode getUpdates responses.
type TelegramUpdate struct {
	UpdateID      int64                  `json:"update_id"`
	Message       *telegramMessage       `json:"message"`
	CallbackQuery *telegramCallbackQuery `json:"callback_query"`
}

type telegramUpdate = TelegramUpdate

type telegramMessage struct {
	MessageID        int64            `json:"message_id"`
	From             *telegramUser    `json:"from"`
	Chat             telegramChat     `json:"chat"`
	Text             string           `json:"text"`
	ReplyToMessage   *telegramMessage `json:"reply_to_message"`
}

type telegramCallbackQuery struct {
	ID   string       `json:"id"`
	From telegramUser `json:"from"`
	Data string       `json:"data"`
}

type telegramUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

type telegramChat struct {
	ID int64 `json:"id"`
}

type telegramInlineKeyboard struct {
	InlineKeyboard [][]telegramInlineButton `json:"inline_keyboard"`
}

type telegramInlineButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
	URL          string `json:"url,omitempty"`
}
