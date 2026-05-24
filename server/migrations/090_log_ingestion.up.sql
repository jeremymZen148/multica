-- Log source configurations and error pattern detection for automated ticket creation.

CREATE TABLE log_source (
    id                    UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id          UUID        NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name                  TEXT        NOT NULL,
    provider              TEXT        NOT NULL CHECK (provider IN ('cloudwatch', 's3')),
    config                JSONB       NOT NULL DEFAULT '{}',
    enabled               BOOLEAN     NOT NULL DEFAULT true,
    poll_interval_minutes INTEGER     NOT NULL DEFAULT 60,
    auto_create_issues    BOOLEAN     NOT NULL DEFAULT true,
    last_polled_at        TIMESTAMPTZ,
    created_by_id         UUID        REFERENCES "user"(id) ON DELETE SET NULL,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_log_source_workspace ON log_source(workspace_id);

CREATE TABLE log_error_pattern (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id     UUID        NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    log_source_id    UUID        NOT NULL REFERENCES log_source(id) ON DELETE CASCADE,
    fingerprint      TEXT        NOT NULL,
    title            TEXT        NOT NULL,
    sample           TEXT        NOT NULL,
    occurrence_count INTEGER     NOT NULL DEFAULT 1,
    first_seen_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    issue_id         UUID        REFERENCES issue(id) ON DELETE SET NULL,
    UNIQUE (workspace_id, log_source_id, fingerprint)
);

CREATE INDEX idx_log_error_pattern_workspace ON log_error_pattern(workspace_id, occurrence_count DESC);
CREATE INDEX idx_log_error_pattern_source    ON log_error_pattern(log_source_id);
