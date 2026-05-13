package handler

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type aiProviderConfigResponse struct {
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	HasAPIKey bool   `json:"has_api_key"`
}

type aiProviderConfigRequest struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	APIKey   string `json:"api_key"`
}

// GetAIProviderConfig returns the workspace's AI provider configuration.
// The API key value is never returned — only whether one is set.
// Access is gated by the RequireWorkspaceRoleFromURL middleware in router.go.
func (h *Handler) GetAIProviderConfig(w http.ResponseWriter, r *http.Request) {
	wsID := r.Header.Get("X-Workspace-ID")
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace_id")
	if !ok {
		return
	}

	cfg, err := h.Queries.GetAIProviderConfig(r.Context(), wsUUID)
	if err == pgx.ErrNoRows {
		writeJSON(w, http.StatusOK, aiProviderConfigResponse{Provider: "anthropic"})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load AI provider config")
		return
	}

	resp := aiProviderConfigResponse{
		Provider:  cfg.Provider,
		HasAPIKey: cfg.ApiKey.Valid && cfg.ApiKey.String != "",
	}
	if cfg.Model.Valid {
		resp.Model = cfg.Model.String
	}
	writeJSON(w, http.StatusOK, resp)
}

// UpsertAIProviderConfig saves provider/model/api_key for the workspace.
func (h *Handler) UpsertAIProviderConfig(w http.ResponseWriter, r *http.Request) {
	wsID := r.Header.Get("X-Workspace-ID")
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace_id")
	if !ok {
		return
	}
	userIDStr := r.Header.Get("X-User-ID")

	var req aiProviderConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	switch req.Provider {
	case "anthropic", "openai", "gemini":
	default:
		writeError(w, http.StatusBadRequest, "provider must be anthropic, openai, or gemini")
		return
	}

	userUUID := parseUUID(userIDStr)

	params := db.UpsertAIProviderConfigParams{
		WorkspaceID: wsUUID,
		Provider:    req.Provider,
		UpdatedByID: userUUID,
	}
	if req.Model != "" {
		params.Model = pgtype.Text{String: req.Model, Valid: true}
	}
	if req.APIKey != "" {
		params.ApiKey = pgtype.Text{String: req.APIKey, Valid: true}
	}

	cfg, err := h.Queries.UpsertAIProviderConfig(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save AI provider config")
		return
	}

	resp := aiProviderConfigResponse{
		Provider:  cfg.Provider,
		HasAPIKey: cfg.ApiKey.Valid && cfg.ApiKey.String != "",
	}
	if cfg.Model.Valid {
		resp.Model = cfg.Model.String
	}
	writeJSON(w, http.StatusOK, resp)
}

// DeleteAIProviderConfig removes the workspace's AI provider config.
func (h *Handler) DeleteAIProviderConfig(w http.ResponseWriter, r *http.Request) {
	wsID := r.Header.Get("X-Workspace-ID")
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace_id")
	if !ok {
		return
	}

	if err := h.Queries.DeleteAIProviderConfig(r.Context(), wsUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete AI provider config")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
