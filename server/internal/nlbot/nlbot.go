package nlbot

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ErrNoAPIKey is returned by Process when no AI provider API key is configured
// for the workspace (neither in the DB nor via environment variables). Callers
// can detect this and fall back to the daemon runtime path.
var ErrNoAPIKey = errors.New("no AI provider API key configured")

// TaskEnqueuer is satisfied by *service.TaskService. Defined here so nlbot
// stays independent of the service package.
type TaskEnqueuer interface {
	EnqueueTaskForIssue(ctx context.Context, issue db.Issue, triggerCommentID ...pgtype.UUID) (db.AgentTaskQueue, error)
}

const systemPrompt = `You are a Multica assistant. Multica is an AI-native task management platform where AI agents are first-class citizens alongside human team members.

Help the user manage their workspace: list issues, update statuses and priorities, assign work to members or agents, add comments, create new items, and trigger agent runs. When the user asks you to do something, use the available tools and confirm briefly what was done. When listing issues, be concise — show title, status, priority, and ID.

Always use the UUID returned by list_issues or get_issue when calling other tools that require an issue_id. When assigning an issue, call list_members or list_agents first to resolve the name to a UUID, then call assign_issue. To start an agent on an issue, the issue must have an agent assigned — use assign_issue first if needed, then trigger_agent.`

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
// enqueuer may be nil — trigger_agent will return an error if called without one.
func Process(ctx context.Context, queries *db.Queries, enqueuer TaskEnqueuer, wsID, userID pgtype.UUID, message string) (string, error) {
	provider, model, apiKey := loadConfig(ctx, queries, wsID)
	if apiKey == "" {
		return "", fmt.Errorf("%w for provider %q — set it in Settings → Integrations → AI Provider or configure the server environment", ErrNoAPIKey, provider)
	}

	tools := buildToolDefs()
	exec := func(name string, args map[string]any) (string, error) {
		return executeTool(ctx, queries, enqueuer, wsID, userID, name, args)
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
