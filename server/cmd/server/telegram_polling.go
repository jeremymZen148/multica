package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// startTelegramPolling polls all registered Telegram bots for new updates.
// This replaces webhook delivery for local / non-HTTPS deployments where
// Telegram cannot reach the server directly.
func startTelegramPolling(ctx context.Context, queries *db.Queries, h *handler.Handler) {
	if h == nil {
		return
	}
	offsets := map[string]int64{} // bot_token → next offset
	clearedWebhooks := map[string]bool{}

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}

		integrations, err := queries.ListAllTelegramIntegrations(ctx)
		if err != nil {
			continue
		}

		for _, integration := range integrations {
			token := integration.BotToken

			// Remove any registered webhook on first encounter so getUpdates works.
			if !clearedWebhooks[token] {
				deleteWebhookIfSet(ctx, token)
				clearedWebhooks[token] = true
			}

			offset := offsets[token]
			updates, err := telegramGetUpdates(ctx, token, offset)
			if err != nil {
				slog.Debug("telegram polling: getUpdates failed", "error", err)
				continue
			}

			wsID := util.UUIDToString(integration.WorkspaceID)
			wsUUID := parseUUID(wsID)

			for _, update := range updates {
				h.ProcessTelegramUpdate(ctx, wsID, wsUUID, integration, update)
				if update.UpdateID+1 > offsets[token] {
					offsets[token] = update.UpdateID + 1
				}
			}
		}
	}
}

type telegramGetUpdatesResult struct {
	OK     bool                     `json:"ok"`
	Result []handler.TelegramUpdate `json:"result"`
}

func telegramGetUpdates(ctx context.Context, botToken string, offset int64) ([]handler.TelegramUpdate, error) {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates?timeout=1&offset=%d", botToken, offset)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result telegramGetUpdatesResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if !result.OK {
		return nil, fmt.Errorf("getUpdates: not ok")
	}
	return result.Result, nil
}

// deleteWebhookIfSet removes the Telegram webhook for a bot so that polling
// works. Telegram won't deliver updates to both a webhook and getUpdates.
func deleteWebhookIfSet(ctx context.Context, botToken string) {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/deleteWebhook", botToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url,
		strings.NewReader(`{"drop_pending_updates":false}`))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}
