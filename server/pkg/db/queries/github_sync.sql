-- =====================
-- GitHub Issue Sync
-- =====================

-- name: GetGitHubIssueSyncByMulticaIssue :one
SELECT * FROM github_issue_sync WHERE multica_issue_id = $1;

-- name: GetGitHubIssueSyncByGitHubIssue :one
SELECT * FROM github_issue_sync
WHERE workspace_id = $1
  AND github_repo_owner = $2
  AND github_repo_name = $3
  AND github_issue_number = $4;

-- name: UpsertGitHubIssueSync :one
INSERT INTO github_issue_sync (
    workspace_id, installation_id, multica_issue_id,
    github_repo_owner, github_repo_name, github_issue_number,
    sync_direction
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
)
ON CONFLICT (multica_issue_id) DO UPDATE SET
    installation_id     = EXCLUDED.installation_id,
    github_repo_owner   = EXCLUDED.github_repo_owner,
    github_repo_name    = EXCLUDED.github_repo_name,
    github_issue_number = EXCLUDED.github_issue_number,
    sync_direction      = EXCLUDED.sync_direction,
    updated_at          = now()
RETURNING *;

-- name: TouchGitHubIssueSyncTimestamp :exec
UPDATE github_issue_sync SET last_synced_at = now(), updated_at = now()
WHERE multica_issue_id = $1;

-- name: DeleteGitHubIssueSync :exec
DELETE FROM github_issue_sync WHERE multica_issue_id = $1 AND workspace_id = $2;

-- name: ListGitHubIssueSyncsByWorkspace :many
SELECT * FROM github_issue_sync WHERE workspace_id = $1 ORDER BY created_at DESC;
