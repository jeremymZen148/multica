-- Slack workspace integration and per-user account linking.

CREATE TABLE slack_integration (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id     UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    team_id          TEXT NOT NULL,
    team_name        TEXT NOT NULL,
    bot_user_id      TEXT NOT NULL,
    bot_token        TEXT NOT NULL,
    default_channel_id   TEXT,
    default_channel_name TEXT,
    installed_by_id  UUID REFERENCES "user"(id) ON DELETE SET NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id),
    UNIQUE (team_id)
);

-- Links a Multica member to their Slack user account so DMs work.
CREATE TABLE slack_user_link (
    workspace_id  UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    user_id       UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    slack_user_id TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, user_id)
);

CREATE INDEX idx_slack_user_link_slack_user ON slack_user_link(workspace_id, slack_user_id);
