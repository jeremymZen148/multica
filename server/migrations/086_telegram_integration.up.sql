-- Telegram bot integration and per-user account linking.

CREATE TABLE telegram_integration (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id     UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    bot_token        TEXT NOT NULL,
    bot_username     TEXT NOT NULL,
    installed_by_id  UUID REFERENCES "user"(id) ON DELETE SET NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id)
);

-- Links a Multica member to their Telegram chat so DMs and inline keyboards work.
CREATE TABLE telegram_user_link (
    workspace_id      UUID    NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    user_id           UUID    NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    telegram_chat_id  BIGINT  NOT NULL,
    telegram_username TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, user_id)
);

CREATE INDEX idx_telegram_user_link_chat ON telegram_user_link(workspace_id, telegram_chat_id);
