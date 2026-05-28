-- name: ListCommentsForIssue :many
-- All comments for an issue in chronological order, capped at $3 (DB safety
-- net). Issue p99 is ~30 comments, max ever observed in prod is ~1.1k, so
-- the handler-side cap of 2000 is purely defensive.
SELECT * FROM comment
WHERE issue_id = $1 AND workspace_id = $2
ORDER BY created_at ASC, id ASC
LIMIT $3;

-- name: ListCommentsSinceForIssue :many
-- Comments created strictly after $3 in chronological order, capped at $4.
-- Powers the CLI's `--since` agent-polling flow.
SELECT * FROM comment
WHERE issue_id = $1 AND workspace_id = $2 AND created_at > $3
ORDER BY created_at ASC, id ASC
LIMIT $4;

-- name: CountComments :one
SELECT count(*) FROM comment
WHERE issue_id = $1 AND workspace_id = $2;

-- name: GetComment :one
SELECT * FROM comment
WHERE id = $1;

-- name: GetCommentInWorkspace :one
SELECT * FROM comment
WHERE id = $1 AND workspace_id = $2;

-- name: CreateComment :one
INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, parent_id)
VALUES ($1, $2, $3, $4, $5, $6, sqlc.narg(parent_id))
RETURNING *;

-- name: UpdateComment :one
UPDATE comment SET
    content = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: GetLatestAgentComment :one
-- Returns the most recent agent comment on an issue at any thread depth.
-- Used to display the agent's last output in external channel notifications.
SELECT * FROM comment
WHERE issue_id = @issue_id
  AND author_type = 'agent'
ORDER BY created_at DESC
LIMIT 1;

-- name: GetLatestAgentRootComment :one
-- Returns the most recent root-level (no parent) agent comment on an issue.
-- Used to thread member replies from external channels (Telegram, Slack) under
-- the agent's output comment rather than posting a separate top-level comment.
SELECT * FROM comment
WHERE issue_id = @issue_id
  AND author_type = 'agent'
  AND parent_id IS NULL
ORDER BY created_at DESC
LIMIT 1;

-- name: HasAgentCommentedSince :one
SELECT EXISTS (
    SELECT 1 FROM comment
    WHERE issue_id = @issue_id
      AND author_type = 'agent'
      AND author_id = @author_id
      AND created_at >= @since
) AS commented;

-- name: HasAgentRepliedInThread :one
-- Returns true if the given agent has posted a reply in the thread rooted at
-- the specified parent comment. Used to detect agent participation in a
-- member-started thread so that follow-up member replies still trigger the agent.
SELECT count(*) > 0 AS has_replied FROM comment
WHERE parent_id = @parent_id AND author_type = 'agent' AND author_id = @agent_id;

-- name: DeleteComment :exec
DELETE FROM comment WHERE id = $1;

-- name: ResolveComment :one
-- Idempotent: re-resolving keeps the original resolved_at + resolver. Always
-- returns the row so the handler can surface the canonical state.
UPDATE comment SET
    resolved_at = COALESCE(resolved_at, now()),
    resolved_by_type = COALESCE(resolved_by_type, $2),
    resolved_by_id = COALESCE(resolved_by_id, $3),
    updated_at = CASE WHEN resolved_at IS NULL THEN now() ELSE updated_at END
WHERE id = $1
RETURNING *;

-- name: UnresolveComment :one
-- Idempotent: a no-op clear (already unresolved) just returns the row.
UPDATE comment SET
    resolved_at = NULL,
    resolved_by_type = NULL,
    resolved_by_id = NULL,
    updated_at = CASE WHEN resolved_at IS NOT NULL THEN now() ELSE updated_at END
WHERE id = $1
RETURNING *;
