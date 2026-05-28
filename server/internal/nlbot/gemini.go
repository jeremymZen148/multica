package nlbot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type geminiPart struct {
	Text         string              `json:"text,omitempty"`
	FunctionCall *geminiFunctionCall `json:"functionCall,omitempty"`
	FunctionResp *geminiFunctionResp `json:"functionResponse,omitempty"`
}

type geminiFunctionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

type geminiFunctionResp struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

type geminiContent struct {
	Role  string       `json:"role"`
	Parts []geminiPart `json:"parts"`
}

// runGemini drives a full function-calling conversation with the Gemini API.
func runGemini(ctx context.Context, apiKey, model, systemPrompt, userMessage string, tools []toolDef, exec toolExecutorFn) (string, error) {
	type functionDecl struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Parameters  map[string]any `json:"parameters"`
	}
	type toolSpec struct {
		FunctionDeclarations []functionDecl `json:"function_declarations"`
	}
	type sysInstruction struct {
		Parts []geminiPart `json:"parts"`
	}
	type request struct {
		SystemInstruction sysInstruction  `json:"system_instruction"`
		Contents          []geminiContent `json:"contents"`
		Tools             []toolSpec      `json:"tools"`
	}
	type candidate struct {
		Content geminiContent `json:"content"`
	}
	type response struct {
		Candidates []candidate `json:"candidates"`
	}

	decls := make([]functionDecl, len(tools))
	for i, t := range tools {
		decls[i] = functionDecl{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  geminiSchema(t.Parameters),
		}
	}

	toolSpec_ := toolSpec{FunctionDeclarations: decls}
	contents := []geminiContent{{Role: "user", Parts: []geminiPart{{Text: userMessage}}}}

	call := func(cts []geminiContent) (*response, error) {
		url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", model, apiKey)
		body, err := json.Marshal(request{
			SystemInstruction: sysInstruction{Parts: []geminiPart{{Text: systemPrompt}}},
			Contents:          cts,
			Tools:             []toolSpec{toolSpec_},
		})
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("content-type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("gemini request: %w", err)
		}
		defer resp.Body.Close()

		var r response
		if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
			return nil, fmt.Errorf("gemini decode: %w", err)
		}
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("gemini API error %d", resp.StatusCode)
		}
		return &r, nil
	}

	for i := 0; i < 5; i++ {
		resp, err := call(contents)
		if err != nil {
			return "", err
		}
		if len(resp.Candidates) == 0 {
			return "", fmt.Errorf("gemini: empty response")
		}

		candidate := resp.Candidates[0]
		contents = append(contents, candidate.Content)

		var functionCalls []geminiPart
		var textPart string
		for _, p := range candidate.Content.Parts {
			if p.FunctionCall != nil {
				functionCalls = append(functionCalls, p)
			}
			if p.Text != "" {
				textPart = p.Text
			}
		}

		if len(functionCalls) == 0 {
			return textPart, nil
		}

		var resultParts []geminiPart
		for _, fc := range functionCalls {
			result, toolErr := exec(fc.FunctionCall.Name, fc.FunctionCall.Args)
			respContent := result
			if toolErr != nil {
				respContent = fmt.Sprintf("error: %s", toolErr.Error())
			}
			resultParts = append(resultParts, geminiPart{
				FunctionResp: &geminiFunctionResp{
					Name:     fc.FunctionCall.Name,
					Response: map[string]any{"content": respContent},
				},
			})
		}
		contents = append(contents, geminiContent{Role: "function", Parts: resultParts})
	}

	return "I've reached the maximum number of tool calls. Please try a more specific request.", nil
}

// geminiSchema recursively converts a JSON Schema map to Gemini's format
// (type values must be uppercase: "object" → "OBJECT", "string" → "STRING", etc.).
func geminiSchema(schema map[string]any) map[string]any {
	out := make(map[string]any, len(schema))
	for k, v := range schema {
		switch k {
		case "type":
			if s, ok := v.(string); ok {
				out[k] = strings.ToUpper(s)
			} else {
				out[k] = v
			}
		case "properties":
			if props, ok := v.(map[string]any); ok {
				converted := make(map[string]any, len(props))
				for pk, pv := range props {
					if pm, ok := pv.(map[string]any); ok {
						converted[pk] = geminiSchema(pm)
					} else {
						converted[pk] = pv
					}
				}
				out[k] = converted
			} else {
				out[k] = v
			}
		default:
			out[k] = v
		}
	}
	return out
}
