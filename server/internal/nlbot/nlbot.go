package nlbot

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const systemPrompt = `You are a Multica assistant. Multica is an AI-native task management platform where AI agents are first-class citizens alongside human team members.

Help the user manage their workspace: list issues, update statuses, assign work, add comments, and create new items. When the user asks you to do something, use the available tools and confirm briefly what was done. When listing issues, be concise — show title, status, and ID.

Always use the UUID returned by list_issues or get_issue when calling other tools that require an issue_id.`

const (
	defaultAnthropicModel = "claude-sonnet-4-6"
	defaultOpenAIModel    = "gpt-4o"
	defaultGeminiModel    = "gemini-1.5-pro"
)

// toolExecutorFn executes a named tool with the given arguments and returns a result string.
type toolExecutorFn func(name string, args map[string]any) (string, error)

// Process takes a user message and returns a natural-language reply.
// It loads the workspace's provider config from the DB (falls back to env vars),
// sends the message to the configured LLM with tool definitions, executes any
// tool calls the LLM requests, and returns the final text response.
func Process(ctx context.Context, queries *db.Queries, wsID, userID pgtype.UUID, message string) (string, error) {
	provider, model, apiKey := loadConfig(ctx, queries, wsID)
	if apiKey == "" {
		return "", fmt.Errorf("no API key configured for AI provider %q — set it in Settings → Integrations → AI Provider or configure the server environment", provider)
	}

	tools := buildToolDefs()
	exec := func(name string, args map[string]any) (string, error) {
		return executeTool(ctx, queries, wsID, userID, name, args)
	}

	switch provider {
	case "openai":
		return runOpenAI(ctx, apiKey, model, systemPrompt, message, tools, exec)
	case "gemini":
		return runGemini(ctx, apiKey, model, systemPrompt, message, tools, exec)
	default:
		return runAnthropic(ctx, apiKey, model, systemPrompt, message, tools, exec)
	}
}

func loadConfig(ctx context.Context, queries *db.Queries, wsID pgtype.UUID) (provider, model, apiKey string) {
	provider = "anthropic"

	cfg, err := queries.GetAIProviderConfig(ctx, wsID)
	if err == nil {
		provider = cfg.Provider
		if cfg.Model.Valid && cfg.Model.String != "" {
			model = cfg.Model.String
		}
		if cfg.ApiKey.Valid && cfg.ApiKey.String != "" {
			apiKey = cfg.ApiKey.String
		}
	}

	// Fall back to server-level env var when no workspace key is set.
	if apiKey == "" {
		switch provider {
		case "openai":
			apiKey = os.Getenv("OPENAI_API_KEY")
		case "gemini":
			apiKey = os.Getenv("GEMINI_API_KEY")
		default:
			apiKey = os.Getenv("ANTHROPIC_API_KEY")
		}
	}

	// Default model per provider.
	if model == "" {
		switch provider {
		case "openai":
			model = defaultOpenAIModel
		case "gemini":
			model = defaultGeminiModel
		default:
			model = defaultAnthropicModel
		}
	}

	return provider, model, apiKey
}
