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

	// Load the integration to get the bot token (needed for sending replies).
	integration, err := h.Queries.GetTelegramIntegration(ctx, wsUUID)
	if err != nil {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Handle callback_query (inline keyboard button press).
	if update.CallbackQuery != nil {
		h.handleTelegramCallback(ctx, wsID, wsUUID, integration, update.CallbackQuery)
		w.WriteHeader(http.StatusOK)
		return
	}

	// Handle text messages.
	if update.Message == nil || update.Message.Text == "" {
		w.WriteHeader(http.StatusOK)
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
		w.WriteHeader(http.StatusOK)
		return
	}
	if err != nil {
		w.WriteHeader(http.StatusOK)
		return
	}
	actorID := util.UUIDToString(link.UserID)

	parts := strings.Fields(text)
	if len(parts) == 0 {
		w.WriteHeader(http.StatusOK)
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
		case "comment":
			reply = h.telegramComment(ctx, wsID, actorID, args)
		case "start", "help":
			reply = "Welcome to Multica bot! Available commands:\n" +
				"/approve <issue-id> — move to In Review\n" +
				"/reject <issue-id> — send back to Todo\n" +
				"/comment <issue-id> <text...> — post a comment\n\n" +
				"Or just send a message in plain text and I'll understand it."
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
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) handleTelegramCallback(
	ctx context.Context,
	wsID string,
	wsUUID interface{ String() string },
	integration db.TelegramIntegration,
	cb *telegramCallbackQuery,
) {
	wsUUIDParsed := parseUUID(wsID)
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
	comment, err := h.Queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     issue.ID,
		WorkspaceID: parseUUID(wsID),
		AuthorType:  "member",
		AuthorID:    parseUUID(actorID),
		Content:     content,
		Type:        "comment",
	})
	if err != nil {
		return "⚠️ Failed to post comment."
	}
	h.publish(protocol.EventCommentCreated, wsID, "member", actorID, map[string]any{
		"comment":      comment,
		"issue_title":  issue.Title,
		"issue_status": issue.Status,
	})
	return fmt.Sprintf("💬 Comment posted on *%s*.", issue.Title)
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
func BuildIssueInlineKeyboard(issueID string, actions []string) *telegramInlineKeyboard {
	var buttons [][]telegramInlineButton
	var row []telegramInlineButton
	for _, action := range actions {
		switch action {
		case "approve":
			row = append(row, telegramInlineButton{Text: "✅ Approve", CallbackData: "approve:" + issueID})
		case "reject":
			row = append(row, telegramInlineButton{Text: "↩️ Reject", CallbackData: "reject:" + issueID})
		}
	}
	if len(row) > 0 {
		buttons = append(buttons, row)
	}
	return &telegramInlineKeyboard{InlineKeyboard: buttons}
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

type telegramUpdate struct {
	UpdateID      int64                  `json:"update_id"`
	Message       *telegramMessage       `json:"message"`
	CallbackQuery *telegramCallbackQuery `json:"callback_query"`
}

type telegramMessage struct {
	MessageID int64        `json:"message_id"`
	From      *telegramUser `json:"from"`
	Chat      telegramChat `json:"chat"`
	Text      string       `json:"text"`
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
	CallbackData string `json:"callback_data"`
}
