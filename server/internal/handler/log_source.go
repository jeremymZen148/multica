package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ── Response shapes ───────────────────────────────────────────────────────────

type LogSourceResponse struct {
	ID                  string          `json:"id"`
	WorkspaceID         string          `json:"workspace_id"`
	Name                string          `json:"name"`
	Provider            string          `json:"provider"`
	Config              json.RawMessage `json:"config"`
	Enabled             bool            `json:"enabled"`
	PollIntervalMinutes int32           `json:"poll_interval_minutes"`
	AutoCreateIssues    bool            `json:"auto_create_issues"`
	LastPolledAt        *string         `json:"last_polled_at,omitempty"`
	CreatedAt           string          `json:"created_at"`
}

type LogErrorPatternResponse struct {
	ID              string  `json:"id"`
	WorkspaceID     string  `json:"workspace_id"`
	LogSourceID     string  `json:"log_source_id"`
	Fingerprint     string  `json:"fingerprint"`
	Title           string  `json:"title"`
	Sample          string  `json:"sample"`
	OccurrenceCount int32   `json:"occurrence_count"`
	FirstSeenAt     string  `json:"first_seen_at"`
	LastSeenAt      string  `json:"last_seen_at"`
	IssueID         *string `json:"issue_id,omitempty"`
}

func logSourceToResponse(s db.LogSource) LogSourceResponse {
	cfg := json.RawMessage(s.Config)
	if len(cfg) == 0 {
		cfg = json.RawMessage("{}")
	}
	return LogSourceResponse{
		ID:                  uuidToString(s.ID),
		WorkspaceID:         uuidToString(s.WorkspaceID),
		Name:                s.Name,
		Provider:            s.Provider,
		Config:              cfg,
		Enabled:             s.Enabled,
		PollIntervalMinutes: s.PollIntervalMinutes,
		AutoCreateIssues:    s.AutoCreateIssues,
		LastPolledAt:        timestampToPtr(s.LastPolledAt),
		CreatedAt:           timestampToString(s.CreatedAt),
	}
}

func logErrorPatternToResponse(p db.LogErrorPattern) LogErrorPatternResponse {
	return LogErrorPatternResponse{
		ID:              uuidToString(p.ID),
		WorkspaceID:     uuidToString(p.WorkspaceID),
		LogSourceID:     uuidToString(p.LogSourceID),
		Fingerprint:     p.Fingerprint,
		Title:           p.Title,
		Sample:          p.Sample,
		OccurrenceCount: p.OccurrenceCount,
		FirstSeenAt:     timestampToString(p.FirstSeenAt),
		LastSeenAt:      timestampToString(p.LastSeenAt),
		IssueID:         uuidToPtr(p.IssueID),
	}
}

// ── Log Source CRUD ───────────────────────────────────────────────────────────

// ListLogSources lists all log sources for the workspace.
// GET /api/workspaces/{id}/log-sources
func (h *Handler) ListLogSources(w http.ResponseWriter, r *http.Request) {
	wsID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace_id")
	if !ok {
		return
	}

	sources, err := h.Queries.ListLogSources(r.Context(), wsUUID)
	if err != nil {
		slog.Warn("list log sources failed", "error", err, "workspace_id", wsID)
		writeError(w, http.StatusInternalServerError, "failed to list log sources")
		return
	}

	resp := make([]LogSourceResponse, len(sources))
	for i, s := range sources {
		resp[i] = logSourceToResponse(s)
	}
	writeJSON(w, http.StatusOK, resp)
}

type createLogSourceRequest struct {
	Name                string          `json:"name"`
	Provider            string          `json:"provider"`
	Config              json.RawMessage `json:"config"`
	Enabled             *bool           `json:"enabled"`
	PollIntervalMinutes *int32          `json:"poll_interval_minutes"`
	AutoCreateIssues    *bool           `json:"auto_create_issues"`
}

// CreateLogSource creates a new log source for the workspace.
// POST /api/workspaces/{id}/log-sources
func (h *Handler) CreateLogSource(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	wsID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace_id")
	if !ok {
		return
	}

	var req createLogSourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	switch req.Provider {
	case "cloudwatch", "s3":
		// valid
	default:
		writeError(w, http.StatusBadRequest, "provider must be one of: cloudwatch, s3")
		return
	}

	cfg := req.Config
	if len(cfg) == 0 {
		cfg = json.RawMessage("{}")
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	pollInterval := int32(60)
	if req.PollIntervalMinutes != nil {
		pollInterval = *req.PollIntervalMinutes
	}
	autoCreate := true
	if req.AutoCreateIssues != nil {
		autoCreate = *req.AutoCreateIssues
	}

	userID := requestUserID(r)
	createdByID := optionalUUID(userID)

	source, err := h.Queries.CreateLogSource(ctx, db.CreateLogSourceParams{
		WorkspaceID:         wsUUID,
		Name:                strings.TrimSpace(req.Name),
		Provider:            req.Provider,
		Config:              []byte(cfg),
		Enabled:             enabled,
		PollIntervalMinutes: pollInterval,
		AutoCreateIssues:    autoCreate,
		CreatedByID:         createdByID,
	})
	if err != nil {
		slog.Warn("create log source failed", "error", err, "workspace_id", wsID)
		writeError(w, http.StatusInternalServerError, "failed to create log source")
		return
	}

	writeJSON(w, http.StatusCreated, logSourceToResponse(source))
}

type updateLogSourceRequest struct {
	Name                *string         `json:"name"`
	Config              json.RawMessage `json:"config"`
	Enabled             *bool           `json:"enabled"`
	PollIntervalMinutes *int32          `json:"poll_interval_minutes"`
	AutoCreateIssues    *bool           `json:"auto_create_issues"`
}

// UpdateLogSource updates a log source.
// PATCH /api/workspaces/{id}/log-sources/{logSourceId}
func (h *Handler) UpdateLogSource(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	wsID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace_id")
	if !ok {
		return
	}

	logSourceID := chi.URLParam(r, "logSourceId")
	sourceUUID, ok := parseUUIDOrBadRequest(w, logSourceID, "logSourceId")
	if !ok {
		return
	}

	existing, err := h.Queries.GetLogSource(ctx, db.GetLogSourceParams{
		ID:          sourceUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "log source not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get log source")
		return
	}

	var req updateLogSourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	name := existing.Name
	if req.Name != nil && strings.TrimSpace(*req.Name) != "" {
		name = strings.TrimSpace(*req.Name)
	}
	cfg := existing.Config
	if len(req.Config) > 0 {
		cfg = []byte(req.Config)
	}
	enabled := existing.Enabled
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	pollInterval := existing.PollIntervalMinutes
	if req.PollIntervalMinutes != nil {
		pollInterval = *req.PollIntervalMinutes
	}
	autoCreate := existing.AutoCreateIssues
	if req.AutoCreateIssues != nil {
		autoCreate = *req.AutoCreateIssues
	}

	updated, err := h.Queries.UpdateLogSource(ctx, db.UpdateLogSourceParams{
		ID:                  sourceUUID,
		WorkspaceID:         wsUUID,
		Name:                name,
		Config:              cfg,
		Enabled:             enabled,
		PollIntervalMinutes: pollInterval,
		AutoCreateIssues:    autoCreate,
	})
	if err != nil {
		slog.Warn("update log source failed", "error", err, "log_source_id", logSourceID)
		writeError(w, http.StatusInternalServerError, "failed to update log source")
		return
	}

	writeJSON(w, http.StatusOK, logSourceToResponse(updated))
}

// DeleteLogSource deletes a log source and its associated patterns.
// DELETE /api/workspaces/{id}/log-sources/{logSourceId}
func (h *Handler) DeleteLogSource(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	wsID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace_id")
	if !ok {
		return
	}

	logSourceID := chi.URLParam(r, "logSourceId")
	sourceUUID, ok := parseUUIDOrBadRequest(w, logSourceID, "logSourceId")
	if !ok {
		return
	}

	if err := h.Queries.DeleteLogSource(ctx, db.DeleteLogSourceParams{
		ID:          sourceUUID,
		WorkspaceID: wsUUID,
	}); err != nil {
		slog.Warn("delete log source failed", "error", err, "log_source_id", logSourceID)
		writeError(w, http.StatusInternalServerError, "failed to delete log source")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// TriggerLogSourcePoll accepts a manual poll trigger for a log source.
// POST /api/workspaces/{id}/log-sources/{logSourceId}/poll
func (h *Handler) TriggerLogSourcePoll(w http.ResponseWriter, r *http.Request) {
	wsID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace_id")
	if !ok {
		return
	}

	logSourceID := chi.URLParam(r, "logSourceId")
	sourceUUID, ok := parseUUIDOrBadRequest(w, logSourceID, "logSourceId")
	if !ok {
		return
	}

	// Verify the source belongs to this workspace.
	if _, err := h.Queries.GetLogSource(r.Context(), db.GetLogSourceParams{
		ID:          sourceUUID,
		WorkspaceID: wsUUID,
	}); err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "log source not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get log source")
		return
	}

	// Poll dispatch is handled by the background poller goroutine.
	writeJSON(w, http.StatusAccepted, map[string]string{"message": "poll triggered"})
}

// ── Log Error Pattern handlers ────────────────────────────────────────────────

// ListLogErrorPatterns lists error patterns for the workspace.
// GET /api/workspaces/{id}/log-error-patterns
func (h *Handler) ListLogErrorPatterns(w http.ResponseWriter, r *http.Request) {
	wsID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace_id")
	if !ok {
		return
	}

	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")
	limit := int32(50)
	offset := int32(0)
	if limitStr != "" {
		if v, err := strconv.ParseInt(limitStr, 10, 32); err == nil && v > 0 {
			limit = int32(v)
		}
	}
	if offsetStr != "" {
		if v, err := strconv.ParseInt(offsetStr, 10, 32); err == nil && v >= 0 {
			offset = int32(v)
		}
	}

	patterns, err := h.Queries.ListLogErrorPatterns(r.Context(), db.ListLogErrorPatternsParams{
		WorkspaceID: wsUUID,
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		slog.Warn("list log error patterns failed", "error", err, "workspace_id", wsID)
		writeError(w, http.StatusInternalServerError, "failed to list log error patterns")
		return
	}

	resp := make([]LogErrorPatternResponse, len(patterns))
	for i, p := range patterns {
		resp[i] = logErrorPatternToResponse(p)
	}
	writeJSON(w, http.StatusOK, resp)
}

// CreateIssueFromPattern creates a Multica issue from a log error pattern.
// POST /api/workspaces/{id}/log-error-patterns/{patternId}/create-issue
func (h *Handler) CreateIssueFromPattern(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	wsID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace_id")
	if !ok {
		return
	}

	patternID := chi.URLParam(r, "patternId")
	patternUUID, ok := parseUUIDOrBadRequest(w, patternID, "patternId")
	if !ok {
		return
	}

	pattern, err := h.Queries.GetLogErrorPattern(ctx, db.GetLogErrorPatternParams{
		ID:          patternUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "log error pattern not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get log error pattern")
		return
	}

	if pattern.IssueID.Valid {
		writeError(w, http.StatusConflict, "issue already exists for this pattern")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	creatorUUID, ok := parseUUIDOrBadRequest(w, userID, "user_id")
	if !ok {
		return
	}

	issueNumber, err := h.Queries.IncrementIssueCounter(ctx, wsUUID)
	if err != nil {
		slog.Warn("increment issue counter failed", "error", err, "workspace_id", wsID)
		writeError(w, http.StatusInternalServerError, "failed to create issue")
		return
	}

	description := pattern.Sample
	if len(description) > 2000 {
		description = description[:2000]
	}

	issue, err := h.Queries.CreateIssue(ctx, db.CreateIssueParams{
		WorkspaceID:   wsUUID,
		Title:         pattern.Title,
		Description:   pgtype.Text{String: description, Valid: true},
		Status:        "todo",
		Priority:      "high",
		AssigneeType:  pgtype.Text{},
		AssigneeID:    pgtype.UUID{},
		CreatorType:   "member",
		CreatorID:     creatorUUID,
		ParentIssueID: pgtype.UUID{},
		Position:      0,
		DueDate:       pgtype.Timestamptz{},
		Number:        issueNumber,
		ProjectID:     pgtype.UUID{},
	})
	if err != nil {
		slog.Warn("create issue from pattern failed", "error", err, "pattern_id", patternID)
		writeError(w, http.StatusInternalServerError, "failed to create issue")
		return
	}

	if _, err := h.Queries.LinkLogErrorPatternToIssue(ctx, db.LinkLogErrorPatternToIssueParams{
		ID:          patternUUID,
		IssueID:     issue.ID,
		WorkspaceID: wsUUID,
	}); err != nil {
		slog.Warn("link log error pattern to issue failed", "error", err, "pattern_id", patternID, "issue_id", uuidToString(issue.ID))
		// Issue was created; continue and return it even if the link fails.
	}

	prefix := h.getIssuePrefix(ctx, wsUUID)
	resp := issueToResponse(issue, prefix)

	slog.Info("issue created from log error pattern",
		"pattern_id", patternID,
		"issue_id", uuidToString(issue.ID),
		"workspace_id", wsID,
	)
	h.publish(protocol.EventIssueCreated, wsID, "member", userID, map[string]any{"issue": resp})

	writeJSON(w, http.StatusCreated, resp)
}

// AutoCreateIssueFromPattern creates a Multica issue automatically from a log
// error pattern when auto_create_issues is enabled and occurrence_count >= 3.
// Called from the background poller goroutine; uses the log source's creator
// as the issue creator. Returns true when an issue was successfully created.
func (h *Handler) AutoCreateIssueFromPattern(
	ctx context.Context,
	wsUUID pgtype.UUID,
	pattern db.LogErrorPattern,
	creatorID pgtype.UUID,
) bool {
	wsID := uuidToString(wsUUID)

	issueNumber, err := h.Queries.IncrementIssueCounter(ctx, wsUUID)
	if err != nil {
		slog.Warn("log source poller: increment issue counter failed", "workspace_id", wsID, "error", err)
		return false
	}

	description := pattern.Sample
	if len(description) > 2000 {
		description = description[:2000]
	}

	issue, err := h.Queries.CreateIssue(ctx, db.CreateIssueParams{
		WorkspaceID:   wsUUID,
		Title:         pattern.Title,
		Description:   pgtype.Text{String: description, Valid: true},
		Status:        "todo",
		Priority:      "high",
		AssigneeType:  pgtype.Text{},
		AssigneeID:    pgtype.UUID{},
		CreatorType:   "member",
		CreatorID:     creatorID,
		ParentIssueID: pgtype.UUID{},
		Position:      0,
		DueDate:       pgtype.Timestamptz{},
		Number:        issueNumber,
		ProjectID:     pgtype.UUID{},
	})
	if err != nil {
		slog.Warn("log source poller: create issue failed",
			"workspace_id", wsID,
			"pattern_id", uuidToString(pattern.ID),
			"error", err,
		)
		return false
	}

	if _, err := h.Queries.LinkLogErrorPatternToIssue(ctx, db.LinkLogErrorPatternToIssueParams{
		ID:          pattern.ID,
		IssueID:     issue.ID,
		WorkspaceID: wsUUID,
	}); err != nil {
		slog.Warn("log source poller: link pattern to issue failed",
			"pattern_id", uuidToString(pattern.ID),
			"issue_id", uuidToString(issue.ID),
			"error", err,
		)
	}

	prefix := h.getIssuePrefix(ctx, wsUUID)
	resp := issueToResponse(issue, prefix)
	h.publish(protocol.EventIssueCreated, wsID, "member", uuidToString(creatorID), map[string]any{"issue": resp})

	slog.Info("log source poller: auto-created issue from pattern",
		"pattern_id", uuidToString(pattern.ID),
		"issue_id", uuidToString(issue.ID),
		"workspace_id", wsID,
	)
	return true
}

// DeleteLogErrorPattern deletes a log error pattern.
// DELETE /api/workspaces/{id}/log-error-patterns/{patternId}
func (h *Handler) DeleteLogErrorPattern(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	wsID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace_id")
	if !ok {
		return
	}

	patternID := chi.URLParam(r, "patternId")
	patternUUID, ok := parseUUIDOrBadRequest(w, patternID, "patternId")
	if !ok {
		return
	}

	if err := h.Queries.DeleteLogErrorPattern(ctx, db.DeleteLogErrorPatternParams{
		ID:          patternUUID,
		WorkspaceID: wsUUID,
	}); err != nil {
		slog.Warn("delete log error pattern failed", "error", err, "pattern_id", patternID)
		writeError(w, http.StatusInternalServerError, "failed to delete log error pattern")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
