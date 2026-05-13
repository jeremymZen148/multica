-- =====================
-- GitHub Repo Sync
-- =====================

-- name: GetGitHubRepoSync :one
SELECT * FROM github_repo_sync
WHERE workspace_id = $1 AND repo_owner = $2 AND repo_name = $3;

-- name: ListGitHubRepoSyncs :many
SELECT * FROM github_repo_sync WHERE workspace_id = $1 ORDER BY created_at DESC;

-- name: UpsertGitHubRepoSync :one
INSERT INTO github_repo_sync (workspace_id, installation_id, repo_owner, repo_name, sync_direction)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (workspace_id, repo_owner, repo_name) DO UPDATE SET
    installation_id = EXCLUDED.installation_id,
    sync_direction  = EXCLUDED.sync_direction
RETURNING *;

-- name: DeleteGitHubRepoSync :exec
DELETE FROM github_repo_sync
WHERE workspace_id = $1 AND repo_owner = $2 AND repo_name = $3;

-- name: GetWorkspacesByGitHubRepo :many
SELECT * FROM github_repo_sync
WHERE repo_owner = $1 AND repo_name = $2;
