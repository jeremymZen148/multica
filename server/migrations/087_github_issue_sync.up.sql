-- Bidirectional sync mapping between Multica issues and GitHub Issues.

CREATE TABLE github_issue_sync (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id        UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    installation_id     BIGINT NOT NULL,
    multica_issue_id    UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    github_repo_owner   TEXT NOT NULL,
    github_repo_name    TEXT NOT NULL,
    github_issue_number INTEGER NOT NULL,
    sync_direction      TEXT NOT NULL DEFAULT 'both'
        CHECK (sync_direction IN ('multica_to_github', 'github_to_multica', 'both')),
    last_synced_at      TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, github_repo_owner, github_repo_name, github_issue_number),
    UNIQUE (multica_issue_id)
);

CREATE INDEX idx_github_issue_sync_workspace ON github_issue_sync(workspace_id);
CREATE INDEX idx_github_issue_sync_issue ON github_issue_sync(multica_issue_id);
