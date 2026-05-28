-- Per-workspace AI provider configuration for the NL bot.
-- Stores which provider (anthropic, openai, gemini) and an optional
-- encrypted API key override. When key is NULL the server falls back to
-- its own environment-level key.

CREATE TABLE ai_provider_config (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    provider     TEXT NOT NULL DEFAULT 'anthropic'
        CHECK (provider IN ('anthropic', 'openai', 'gemini')),
    model        TEXT,
    api_key      TEXT,
    updated_by_id UUID REFERENCES "user"(id) ON DELETE SET NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id)
);
