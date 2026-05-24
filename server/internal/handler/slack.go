package handler

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/nlbot"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ── Env helpers ──────────────────────────────────────────────────────────────

func slackClientID() string     { return strings.TrimSpace(os.Getenv("SLACK_CLIENT_ID")) }
func slackClientSecret() string { return strings.TrimSpace(os.Getenv("SLACK_CLIENT_SECRET")) }
func slackSigningSecret() string {
	return strings.TrimSpace(os.Getenv("SLACK_SIGNING_SECRET"))
}
func slackRedirectURI() string {
	base := strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN"))
	if base == "" {
		base = "http://localhost:8080"
	}
	return base + "/api/slack/callback"
}

// ── Response shapes ───────────────────────────────────────────────────────────

type SlackIntegrationResponse struct {
	ID                 string  `json:"id"`
	WorkspaceID        string  `json:"workspace_id"`
	TeamID             string  `json:"team_id"`
	TeamName           string  `json:"team_name"`
	DefaultChannelID   *string `json:"default_channel_id"`
	DefaultChannelName *string `json:"default_channel_name"`
	CreatedAt          string  `json:"created_at"`
}

type SlackConnectResponse struct {
	URL        string `json:"url"`
	Configured bool   `json:"configured"`
}

func slackIntegrationToResponse(i db.SlackIntegration) SlackIntegrationResponse {
	return SlackIntegrationResponse{
		ID:                 uuidToString(i.ID),
		WorkspaceID:        uuidToString(i.WorkspaceID),
		TeamID:             i.TeamID,
		TeamName:           i.TeamName,
		DefaultChannelID:   textToPtr(i.DefaultChannelID),
		DefaultChannelName: textToPtr(i.DefaultChannelName),
		CreatedAt:          timestampToString(i.CreatedAt),
	}
}

// ── Signature verification ────────────────────────────────────────────────────

// verifySlackSignature validates the X-Slack-Signature header.
// Slack signs every inbound request with HMAC-SHA256 over "v0:{timestamp}:{body}".
// Requests older than 5 minutes are rejected to prevent replay attacks.
func verifySlackSignature(signingSecret, sigHeader, tsHeader string, body []byte) bool {
	if signingSecret == "" || sigHeader == "" || tsHeader == "" {
		return false
	}
	ts, err := strconv.ParseInt(tsHeader, 10, 64)
	if err != nil {
		return false
	}
	if time.Since(time.Unix(ts, 0)) > 5*time.Minute {
		return false
	}
	baseStr := fmt.Sprintf("v0:%s:%s", tsHeader, string(body))
	mac := hmac.New(sha256.New, []byte(signingSecret))
	mac.Write([]byte(baseStr))
	expected := "v0=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(sigHeader))
}

// ── OAuth install ─────────────────────────────────────────────────────────────

// SlackConnect returns the OAuth install URL for the workspace.
// GET /api/workspaces/{id}/slack/connect
func (h *Handler) SlackConnect(w http.ResponseWriter, r *http.Request) {
	if slackClientID() == "" {
		writeJSON(w, http.StatusOK, SlackConnectResponse{Configured: false})
		return
	}
	wsID := h.resolveWorkspaceID(r)
	authURL := fmt.Sprintf(
		"https://slack.com/oauth/v2/authorize?client_id=%s&scope=chat:write,commands,users:read&redirect_uri=%s&state=%s",
		url.QueryEscape(slackClientID()),
		url.QueryEscape(slackRedirectURI()),
		url.QueryEscape(wsID),
	)
	writeJSON(w, http.StatusOK, SlackConnectResponse{URL: authURL, Configured: true})
}

// SlackCallback handles the OAuth redirect after the user installs the Slack app.
// GET /api/slack/callback
func (h *Handler) SlackCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	code := r.URL.Query().Get("code")
	wsID := r.URL.Query().Get("state")
	if code == "" || wsID == "" {
		writeError(w, http.StatusBadRequest, "missing code or state")
		return
	}

	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace_id")
	if !ok {
		return
	}

	token, err := slackExchangeCode(ctx, code)
	if err != nil {
		slog.Error("slack oauth exchange failed", "error", err)
		writeError(w, http.StatusBadGateway, "slack oauth exchange failed")
		return
	}

	actorID := r.Header.Get("X-User-ID")
	integration, err := h.Queries.UpsertSlackIntegration(ctx, db.UpsertSlackIntegrationParams{
		WorkspaceID:   wsUUID,
		TeamID:        token.TeamID,
		TeamName:      token.TeamName,
		BotUserID:     token.BotUserID,
		BotToken:      token.BotToken,
		InstalledByID: optionalUUID(actorID),
	})
	if err != nil {
		slog.Error("slack integration upsert failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to save slack integration")
		return
	}

	h.publish(protocol.EventSlackIntegrationConnected, wsID, "member", actorID,
		map[string]any{"integration": slackIntegrationToResponse(integration)})

	frontend := strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN"))
	if frontend == "" {
		frontend = "http://localhost:3000"
	}
	http.Redirect(w, r, frontend+"/settings/integrations?slack=connected", http.StatusFound)
}

// ── Integration CRUD ──────────────────────────────────────────────────────────

// GetSlackIntegration returns the current workspace's Slack integration.
// GET /api/integrations/slack
func (h *Handler) GetSlackIntegration(w http.ResponseWriter, r *http.Request) {
	wsID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace_id")
	if !ok {
		return
	}
	integration, err := h.Queries.GetSlackIntegration(r.Context(), wsUUID)
	if err == pgx.ErrNoRows {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load slack integration")
		return
	}
	writeJSON(w, http.StatusOK, slackIntegrationToResponse(integration))
}

// DeleteSlackIntegration removes the Slack integration for the workspace.
// DELETE /api/integrations/slack
func (h *Handler) DeleteSlackIntegration(w http.ResponseWriter, r *http.Request) {
	wsID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace_id")
	if !ok {
		return
	}
	if err := h.Queries.DeleteSlackIntegration(r.Context(), wsUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete slack integration")
		return
	}
	actorID := r.Header.Get("X-User-ID")
	h.publish(protocol.EventSlackIntegrationDisconnected, wsID, "member", actorID, nil)
	w.WriteHeader(http.StatusNoContent)
}

// ── User account linking ──────────────────────────────────────────────────────

type linkSlackUserRequest struct {
	SlackUserID string `json:"slack_user_id"`
}

// LinkSlackUser links the authenticated user's account to their Slack user ID.
// POST /api/integrations/slack/link
func (h *Handler) LinkSlackUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	wsID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace_id")
	if !ok {
		return
	}

	var req linkSlackUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SlackUserID == "" {
		writeError(w, http.StatusBadRequest, "slack_user_id is required")
		return
	}

	userID := r.Header.Get("X-User-ID")
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user_id")
	if !ok {
		return
	}

	link, err := h.Queries.UpsertSlackUserLink(ctx, db.UpsertSlackUserLinkParams{
		WorkspaceID: wsUUID,
		UserID:      userUUID,
		SlackUserID: req.SlackUserID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to link slack user")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"slack_user_id": link.SlackUserID,
		"user_id":       util.UUIDToString(link.UserID),
	})
}

// UnlinkSlackUser removes the Slack account link for the authenticated user.
// DELETE /api/integrations/slack/link
func (h *Handler) UnlinkSlackUser(w http.ResponseWriter, r *http.Request) {
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
	if err := h.Queries.DeleteSlackUserLink(r.Context(), db.DeleteSlackUserLinkParams{
		WorkspaceID: wsUUID,
		UserID:      userUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unlink slack user")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── Inbound webhook: slash commands ──────────────────────────────────────────

// HandleSlackCommand handles Slack slash commands (e.g. /multica approve MUL-42).
// POST /api/webhooks/slack/commands
func (h *Handler) HandleSlackCommand(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read body failed")
		return
	}
	secret := slackSigningSecret()
	if secret == "" {
		writeError(w, http.StatusServiceUnavailable, "slack not configured")
		return
	}
	if !verifySlackSignature(secret,
		r.Header.Get("X-Slack-Signature"),
		r.Header.Get("X-Slack-Request-Timestamp"),
		body,
	) {
		writeError(w, http.StatusUnauthorized, "invalid signature")
		return
	}

	vals, err := url.ParseQuery(string(body))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid form body")
		return
	}

	text := strings.TrimSpace(vals.Get("text"))
	slackUserID := vals.Get("user_id")
	teamID := vals.Get("team_id")
	ctx := r.Context()

	integration, err := h.Queries.GetSlackIntegrationByTeamID(ctx, teamID)
	if err != nil {
		writeJSON(w, http.StatusOK, slackEphemeral("⚠️ This Slack workspace is not connected to Multica."))
		return
	}
	wsID := uuidToString(integration.WorkspaceID)

	link, err := h.Queries.GetSlackUserLinkBySlackUserID(ctx, db.GetSlackUserLinkBySlackUserIDParams{
		WorkspaceID: integration.WorkspaceID,
		SlackUserID: slackUserID,
	})
	if err == pgx.ErrNoRows {
		writeJSON(w, http.StatusOK, slackEphemeral("⚠️ Link your Slack account in Multica → Settings → Integrations → Slack."))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusOK, slackEphemeral("⚠️ Failed to resolve your Multica account."))
		return
	}
	actorID := util.UUIDToString(link.UserID)

	parts := strings.Fields(text)
	if len(parts) == 0 {
		writeJSON(w, http.StatusOK, slackEphemeral("Usage: /multica [approve|reject|done|comment] <issue-id> [text...]"))
		return
	}

	switch strings.ToLower(parts[0]) {
	case "approve":
		h.slackApprove(w, ctx, wsID, actorID, parts[1:])
	case "reject":
		h.slackReject(w, ctx, wsID, actorID, parts[1:])
	case "done":
		h.slackMarkDone(w, ctx, wsID, actorID, parts[1:])
	case "comment":
		h.slackComment(w, ctx, wsID, actorID, parts[1:])
	default:
		// Route everything else to the NL bot.
		reply, err := nlbot.Process(ctx, h.Queries, h.TaskService, integration.WorkspaceID, link.UserID, text)
		if err != nil {
			writeJSON(w, http.StatusOK, slackEphemeral("⚠️ "+err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, slackEphemeral(reply))
	}
}

func (h *Handler) slackApprove(w http.ResponseWriter, ctx context.Context, wsID, actorID string, args []string) {
	if len(args) == 0 {
		writeJSON(w, http.StatusOK, slackEphemeral("Usage: /multica approve <issue-id>"))
		return
	}
	issue, err := h.getIssueInWorkspace(ctx, wsID, args[0])
	if err != nil {
		writeJSON(w, http.StatusOK, slackEphemeral(fmt.Sprintf("⚠️ Issue %q not found.", args[0])))
		return
	}
	prev := issue.Status
	updated, err := h.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID:     issue.ID,
		Status: "in_review",
	})
	if err != nil {
		writeJSON(w, http.StatusOK, slackEphemeral("⚠️ Failed to update issue."))
		return
	}
	h.publish(protocol.EventIssueUpdated, wsID, "member", actorID, map[string]any{
		"issue":          updated,
		"status_changed": true,
		"prev_status":    prev,
	})
	writeJSON(w, http.StatusOK, slackEphemeral(fmt.Sprintf("✅ *%s* moved to In Review.", updated.Title)))
}

func (h *Handler) slackReject(w http.ResponseWriter, ctx context.Context, wsID, actorID string, args []string) {
	if len(args) == 0 {
		writeJSON(w, http.StatusOK, slackEphemeral("Usage: /multica reject <issue-id>"))
		return
	}
	issue, err := h.getIssueInWorkspace(ctx, wsID, args[0])
	if err != nil {
		writeJSON(w, http.StatusOK, slackEphemeral(fmt.Sprintf("⚠️ Issue %q not found.", args[0])))
		return
	}
	prev := issue.Status
	updated, err := h.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID:     issue.ID,
		Status: "todo",
	})
	if err != nil {
		writeJSON(w, http.StatusOK, slackEphemeral("⚠️ Failed to update issue."))
		return
	}
	h.publish(protocol.EventIssueUpdated, wsID, "member", actorID, map[string]any{
		"issue":          updated,
		"status_changed": true,
		"prev_status":    prev,
	})
	writeJSON(w, http.StatusOK, slackEphemeral(fmt.Sprintf("↩️ *%s* sent back to Todo.", updated.Title)))
}

func (h *Handler) slackComment(w http.ResponseWriter, ctx context.Context, wsID, actorID string, args []string) {
	if len(args) < 2 {
		writeJSON(w, http.StatusOK, slackEphemeral("Usage: /multica comment <issue-id> <text...>"))
		return
	}
	issue, err := h.getIssueInWorkspace(ctx, wsID, args[0])
	if err != nil {
		writeJSON(w, http.StatusOK, slackEphemeral(fmt.Sprintf("⚠️ Issue %q not found.", args[0])))
		return
	}
	content := strings.Join(args[1:], " ")

	// Thread under the agent's latest root comment so the reply appears in context.
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
		writeJSON(w, http.StatusOK, slackEphemeral("⚠️ Failed to post comment."))
		return
	}
	h.publish(protocol.EventCommentCreated, wsID, "member", actorID, map[string]any{
		"comment":      comment,
		"issue_title":  issue.Title,
		"issue_status": issue.Status,
	})
	if h.shouldEnqueueOnComment(ctx, issue) {
		h.TaskService.EnqueueTaskForIssue(ctx, issue, comment.ID)
	}
	writeJSON(w, http.StatusOK, slackEphemeral(fmt.Sprintf("💬 Comment posted on *%s*.", issue.Title)))
}

func (h *Handler) slackMarkDone(w http.ResponseWriter, ctx context.Context, wsID, actorID string, args []string) {
	if len(args) == 0 {
		writeJSON(w, http.StatusOK, slackEphemeral("Usage: /multica done <issue-id>"))
		return
	}
	issue, err := h.getIssueInWorkspace(ctx, wsID, args[0])
	if err != nil {
		writeJSON(w, http.StatusOK, slackEphemeral(fmt.Sprintf("⚠️ Issue %q not found.", args[0])))
		return
	}
	prev := issue.Status
	updated, err := h.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID:     issue.ID,
		Status: "done",
	})
	if err != nil {
		writeJSON(w, http.StatusOK, slackEphemeral("⚠️ Failed to update issue."))
		return
	}
	h.publish(protocol.EventIssueUpdated, wsID, "member", actorID, map[string]any{
		"issue":          updated,
		"status_changed": true,
		"prev_status":    prev,
	})
	writeJSON(w, http.StatusOK, slackEphemeral(fmt.Sprintf("✅ *%s* marked as Done.", updated.Title)))
}

// ── Inbound webhook: interactive components ───────────────────────────────────

// HandleSlackInteractive handles Slack button clicks and other interactive payloads.
// POST /api/webhooks/slack/interactive
func (h *Handler) HandleSlackInteractive(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read body failed")
		return
	}
	secret := slackSigningSecret()
	if secret == "" {
		writeError(w, http.StatusServiceUnavailable, "slack not configured")
		return
	}
	if !verifySlackSignature(secret,
		r.Header.Get("X-Slack-Signature"),
		r.Header.Get("X-Slack-Request-Timestamp"),
		body,
	) {
		writeError(w, http.StatusUnauthorized, "invalid signature")
		return
	}

	vals, err := url.ParseQuery(string(body))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid form body")
		return
	}

	var payload slackInteractivePayload
	if err := json.Unmarshal([]byte(vals.Get("payload")), &payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid payload")
		return
	}

	ctx := r.Context()
	integration, err := h.Queries.GetSlackIntegrationByTeamID(ctx, payload.Team.ID)
	if err != nil {
		w.WriteHeader(http.StatusOK)
		return
	}
	wsID := uuidToString(integration.WorkspaceID)

	link, err := h.Queries.GetSlackUserLinkBySlackUserID(ctx, db.GetSlackUserLinkBySlackUserIDParams{
		WorkspaceID: integration.WorkspaceID,
		SlackUserID: payload.User.ID,
	})
	if err != nil {
		writeJSON(w, http.StatusOK, slackEphemeral("⚠️ Link your Slack account in Multica → Settings → Integrations → Slack."))
		return
	}
	actorID := util.UUIDToString(link.UserID)

	for _, action := range payload.Actions {
		switch action.ActionID {
		case "multica_approve":
			h.slackApprove(w, ctx, wsID, actorID, []string{action.Value})
			return
		case "multica_reject":
			h.slackReject(w, ctx, wsID, actorID, []string{action.Value})
			return
		case "multica_done":
			h.slackMarkDone(w, ctx, wsID, actorID, []string{action.Value})
			return
		}
	}
	w.WriteHeader(http.StatusOK)
}

// ── Slack API helpers ─────────────────────────────────────────────────────────

// SendSlackMessage posts a Block Kit message to a Slack channel using a bot token.
func SendSlackMessage(ctx context.Context, botToken, channel string, blocks []map[string]any, fallback string) error {
	payload, _ := json.Marshal(map[string]any{
		"channel": channel,
		"text":    fallback,
		"blocks":  blocks,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://slack.com/api/chat.postMessage", strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+botToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var result struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	if !result.OK {
		return fmt.Errorf("slack API error: %s", result.Error)
	}
	return nil
}

// SendSlackDM sends a DM to a Slack user. Opens a conversation if needed.
func SendSlackDM(ctx context.Context, botToken, slackUserID string, blocks []map[string]any, fallback string) error {
	openPayload, _ := json.Marshal(map[string]any{"users": slackUserID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://slack.com/api/conversations.open", strings.NewReader(string(openPayload)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+botToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var openResult struct {
		OK      bool   `json:"ok"`
		Error   string `json:"error"`
		Channel struct {
			ID string `json:"id"`
		} `json:"channel"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&openResult); err != nil {
		return err
	}
	if !openResult.OK {
		return fmt.Errorf("slack conversations.open error: %s", openResult.Error)
	}
	return SendSlackMessage(ctx, botToken, openResult.Channel.ID, blocks, fallback)
}

// BuildIssueActionBlocks builds a Slack Block Kit message for an issue with action buttons.
func BuildIssueActionBlocks(issueID, issueTitle, status, frontendOrigin string, actions []string) []map[string]any {
	header := map[string]any{
		"type": "section",
		"text": map[string]any{
			"type": "mrkdwn",
			"text": fmt.Sprintf("*%s*\nStatus: _%s_", issueTitle, status),
		},
	}
	buttons := make([]map[string]any, 0, len(actions))
	for _, action := range actions {
		switch action {
		case "approve":
			buttons = append(buttons, map[string]any{
				"type":      "button",
				"text":      map[string]any{"type": "plain_text", "text": "✅ Approve"},
				"action_id": "multica_approve",
				"value":     issueID,
				"style":     "primary",
			})
		case "reject":
			buttons = append(buttons, map[string]any{
				"type":      "button",
				"text":      map[string]any{"type": "plain_text", "text": "↩️ Reject"},
				"action_id": "multica_reject",
				"value":     issueID,
			})
		case "done":
			buttons = append(buttons, map[string]any{
				"type":      "button",
				"text":      map[string]any{"type": "plain_text", "text": "✅ Mark Done"},
				"action_id": "multica_done",
				"value":     issueID,
			})
		case "view":
			if issueID != "" && strings.HasPrefix(frontendOrigin, "https://") {
				buttons = append(buttons, map[string]any{
					"type":      "button",
					"text":      map[string]any{"type": "plain_text", "text": "🔗 View"},
					"action_id": "multica_view",
					"value":     issueID,
					"url":       frontendOrigin + "/issues/" + issueID,
				})
			}
		}
	}
	return []map[string]any{
		header,
		{"type": "actions", "elements": buttons},
	}
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// getIssueInWorkspace looks up an issue by UUID in the given workspace.
func (h *Handler) getIssueInWorkspace(ctx context.Context, wsID, issueID string) (db.Issue, error) {
	issueUUID, err := util.ParseUUID(issueID)
	if err != nil {
		return db.Issue{}, err
	}
	return h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID:          issueUUID,
		WorkspaceID: parseUUID(wsID),
	})
}

func slackEphemeral(text string) map[string]any {
	return map[string]any{"response_type": "ephemeral", "text": text}
}

// ── Slack payload types ───────────────────────────────────────────────────────

type slackInteractivePayload struct {
	Type string `json:"type"`
	Team struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"team"`
	User struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"user"`
	Actions []struct {
		ActionID string `json:"action_id"`
		Value    string `json:"value"`
	} `json:"actions"`
}

type slackOAuthToken struct {
	TeamID    string
	TeamName  string
	BotUserID string
	BotToken  string
}

func slackExchangeCode(ctx context.Context, code string) (*slackOAuthToken, error) {
	form := url.Values{}
	form.Set("code", code)
	form.Set("client_id", slackClientID())
	form.Set("client_secret", slackClientSecret())
	form.Set("redirect_uri", slackRedirectURI())

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://slack.com/api/oauth.v2.access", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		OK          bool   `json:"ok"`
		Error       string `json:"error"`
		AccessToken string `json:"access_token"`
		BotUserID   string `json:"bot_user_id"`
		Team        struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"team"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if !result.OK {
		return nil, fmt.Errorf("slack oauth error: %s", result.Error)
	}
	return &slackOAuthToken{
		TeamID:    result.Team.ID,
		TeamName:  result.Team.Name,
		BotUserID: result.BotUserID,
		BotToken:  result.AccessToken,
	}, nil
}
