package nlbot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// runAnthropic drives a full tool-calling conversation with the Anthropic API.
func runAnthropic(ctx context.Context, apiKey, model, systemPrompt, userMessage string, tools []toolDef, exec toolExecutorFn) (string, error) {
	type contentBlock struct {
		Type       string         `json:"type"`
		Text       string         `json:"text,omitempty"`
		ID         string         `json:"id,omitempty"`
		Name       string         `json:"name,omitempty"`
		Input      map[string]any `json:"input,omitempty"`
		ToolUseID  string         `json:"tool_use_id,omitempty"`
		Content    string         `json:"content,omitempty"`
	}

	type message struct {
		Role    string        `json:"role"`
		Content any           `json:"content"` // string OR []contentBlock
	}

	type anthropicTool struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		InputSchema map[string]any `json:"input_schema"`
	}

	type request struct {
		Model     string          `json:"model"`
		MaxTokens int             `json:"max_tokens"`
		System    string          `json:"system"`
		Messages  []message       `json:"messages"`
		Tools     []anthropicTool `json:"tools"`
	}

	type response struct {
		Content    []contentBlock `json:"content"`
		StopReason string         `json:"stop_reason"`
	}

	// Build tools array.
	atTools := make([]anthropicTool, len(tools))
	for i, t := range tools {
		atTools[i] = anthropicTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.Parameters,
		}
	}

	messages := []message{{Role: "user", Content: userMessage}}

	call := func(msgs []message) (*response, error) {
		body, err := json.Marshal(request{
			Model:     model,
			MaxTokens: 1024,
			System:    systemPrompt,
			Messages:  msgs,
			Tools:     atTools,
		})
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
		req.Header.Set("content-type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("anthropic request: %w", err)
		}
		defer resp.Body.Close()

		var r response
		if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
			return nil, fmt.Errorf("anthropic decode: %w", err)
		}
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("anthropic API error %d", resp.StatusCode)
		}
		return &r, nil
	}

	for i := 0; i < 5; i++ {
		resp, err := call(messages)
		if err != nil {
			return "", err
		}

		// Collect text and tool_use blocks.
		var text string
		var toolUseBlocks []contentBlock
		for _, b := range resp.Content {
			switch b.Type {
			case "text":
				text = b.Text
			case "tool_use":
				toolUseBlocks = append(toolUseBlocks, b)
			}
		}

		if resp.StopReason == "end_turn" || len(toolUseBlocks) == 0 {
			return text, nil
		}

		// Append the assistant turn (full content array) to messages.
		messages = append(messages, message{Role: "assistant", Content: resp.Content})

		// Execute tools and build the tool_result turn.
		var results []contentBlock
		for _, tu := range toolUseBlocks {
			result, toolErr := exec(tu.Name, tu.Input)
			content := result
			if toolErr != nil {
				content = fmt.Sprintf("error: %s", toolErr.Error())
			}
			results = append(results, contentBlock{
				Type:      "tool_result",
				ToolUseID: tu.ID,
				Content:   content,
			})
		}
		messages = append(messages, message{Role: "user", Content: results})
	}

	return "I've reached the maximum number of tool calls. Please try a more specific request.", nil
}
