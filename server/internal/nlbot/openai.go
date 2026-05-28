package nlbot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// runOpenAI drives a full tool-calling conversation with the OpenAI API.
func runOpenAI(ctx context.Context, apiKey, model, systemPrompt, userMessage string, tools []toolDef, exec toolExecutorFn) (string, error) {
	type functionDef struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Parameters  map[string]any `json:"parameters"`
	}
	type toolSpec struct {
		Type     string      `json:"type"`
		Function functionDef `json:"function"`
	}
	type toolCallFunction struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}
	type toolCall struct {
		ID       string           `json:"id"`
		Type     string           `json:"type"`
		Function toolCallFunction `json:"function"`
	}
	type message struct {
		Role       string     `json:"role"`
		Content    string     `json:"content,omitempty"`
		ToolCallID string     `json:"tool_call_id,omitempty"`
		ToolCalls  []toolCall `json:"tool_calls,omitempty"`
	}
	type request struct {
		Model    string     `json:"model"`
		Messages []message  `json:"messages"`
		Tools    []toolSpec `json:"tools"`
	}
	type choice struct {
		Message message `json:"message"`
	}
	type response struct {
		Choices []choice `json:"choices"`
	}

	// Build tools array.
	oaiTools := make([]toolSpec, len(tools))
	for i, t := range tools {
		oaiTools[i] = toolSpec{
			Type: "function",
			Function: functionDef{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		}
	}

	messages := []message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userMessage},
	}

	call := func(msgs []message) (*response, error) {
		body, err := json.Marshal(request{
			Model:    model,
			Messages: msgs,
			Tools:    oaiTools,
		})
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/chat/completions", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("content-type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("openai request: %w", err)
		}
		defer resp.Body.Close()

		var r response
		if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
			return nil, fmt.Errorf("openai decode: %w", err)
		}
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("openai API error %d", resp.StatusCode)
		}
		return &r, nil
	}

	for i := 0; i < 5; i++ {
		resp, err := call(messages)
		if err != nil {
			return "", err
		}
		if len(resp.Choices) == 0 {
			return "", fmt.Errorf("openai: empty response")
		}

		msg := resp.Choices[0].Message
		messages = append(messages, msg)

		if len(msg.ToolCalls) == 0 {
			return msg.Content, nil
		}

		// Execute each tool call and append results.
		for _, tc := range msg.ToolCalls {
			var args map[string]any
			_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)

			result, toolErr := exec(tc.Function.Name, args)
			content := result
			if toolErr != nil {
				content = fmt.Sprintf("error: %s", toolErr.Error())
			}
			messages = append(messages, message{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    content,
			})
		}
	}

	return "I've reached the maximum number of tool calls. Please try a more specific request.", nil
}
