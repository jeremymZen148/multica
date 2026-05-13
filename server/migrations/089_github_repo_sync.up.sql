-- Workspace-level GitHub repo sync configuration.
-- Records which workspaces want issues synced to/from which repos.

CREATE TABLE github_repo_sync (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    installation_id BIGINT NOT NULL,
    repo_owner      TEXT NOT NULL,
    repo_name       TEXT NOT NULL,
    sync_direction  TEXT NOT NULL DEFAULT 'both'
        CHECK (sync_direction IN ('multica_to_github', 'github_to_multica', 'both')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, repo_owner, repo_name)
);

CREATE INDEX idx_github_repo_sync_workspace ON github_repo_sync(workspace_id);
CREATE INDEX idx_github_repo_sync_repo ON github_repo_sync(repo_owner, repo_name);
