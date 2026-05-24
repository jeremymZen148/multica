-- =====================
-- Log Source
-- =====================

-- name: CreateLogSource :one
INSERT INTO log_source (
    workspace_id, name, provider, config, enabled,
    poll_interval_minutes, auto_create_issues, created_by_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
) RETURNING *;

-- name: GetLogSource :one
SELECT * FROM log_source
WHERE id = $1 AND workspace_id = $2;

-- name: ListLogSources :many
SELECT * FROM log_source
WHERE workspace_id = $1
ORDER BY created_at ASC;

-- name: UpdateLogSource :one
UPDATE log_source SET
    name                  = $3,
    config                = $4,
    enabled               = $5,
    poll_interval_minutes = $6,
    auto_create_issues    = $7,
    updated_at            = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: DeleteLogSource :exec
DELETE FROM log_source
WHERE id = $1 AND workspace_id = $2;

-- name: TouchLogSourcePolledAt :exec
UPDATE log_source SET last_polled_at = now()
WHERE id = $1;

-- name: ListAllEnabledLogSources :many
SELECT * FROM log_source
WHERE enabled = true;

-- =====================
-- Log Error Pattern
-- =====================

-- name: UpsertLogErrorPattern :one
INSERT INTO log_error_pattern (
    workspace_id, log_source_id, fingerprint, title, sample,
    occurrence_count, first_seen_at, last_seen_at
) VALUES (
    $1, $2, $3, $4, $5, $6, now(), now()
)
ON CONFLICT (workspace_id, log_source_id, fingerprint) DO UPDATE SET
    title            = EXCLUDED.title,
    sample           = EXCLUDED.sample,
    occurrence_count = log_error_pattern.occurrence_count + $6,
    last_seen_at     = now()
RETURNING *;

-- name: ListLogErrorPatterns :many
SELECT * FROM log_error_pattern
WHERE workspace_id = $1
ORDER BY occurrence_count DESC
LIMIT $2 OFFSET $3;

-- name: GetLogErrorPattern :one
SELECT * FROM log_error_pattern
WHERE id = $1 AND workspace_id = $2;

-- name: LinkLogErrorPatternToIssue :one
UPDATE log_error_pattern SET
    issue_id = $2
WHERE id = $1 AND workspace_id = $3
RETURNING *;

-- name: DeleteLogErrorPattern :exec
DELETE FROM log_error_pattern
WHERE id = $1 AND workspace_id = $2;
