package channelreply

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// PostSlackResponseURL sends an ephemeral text reply to a Slack slash command
// via its response_url. This is used for async replies where the initial 200
// ACK has already been written.
func PostSlackResponseURL(ctx context.Context, responseURL, text string) error {
	body, _ := json.Marshal(map[string]any{
		"response_type": "ephemeral",
		"text":          text,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, responseURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("slack response_url returned %d", resp.StatusCode)
	}
	return nil
}

// PostTelegramMessage sends a plain text message to a Telegram chat.
func PostTelegramMessage(ctx context.Context, botToken string, chatID int64, text string) error {
	body, _ := json.Marshal(map[string]any{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "Markdown",
	})
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
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
		return fmt.Errorf("telegram sendMessage: %s", result.Description)
	}
	return nil
}
