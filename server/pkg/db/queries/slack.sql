-- =====================
-- Slack Integration
-- =====================

-- name: GetSlackIntegration :one
SELECT * FROM slack_integration WHERE workspace_id = $1;

-- name: UpsertSlackIntegration :one
INSERT INTO slack_integration (
    workspace_id, team_id, team_name, bot_user_id, bot_token,
    default_channel_id, default_channel_name, installed_by_id
) VALUES (
    $1, $2, $3, $4, $5,
    sqlc.narg('default_channel_id'), sqlc.narg('default_channel_name'), sqlc.narg('installed_by_id')
)
ON CONFLICT (workspace_id) DO UPDATE SET
    team_id              = EXCLUDED.team_id,
    team_name            = EXCLUDED.team_name,
    bot_user_id          = EXCLUDED.bot_user_id,
    bot_token            = EXCLUDED.bot_token,
    default_channel_id   = EXCLUDED.default_channel_id,
    default_channel_name = EXCLUDED.default_channel_name,
    updated_at           = now()
RETURNING *;

-- name: DeleteSlackIntegration :exec
DELETE FROM slack_integration WHERE workspace_id = $1;

-- name: GetSlackIntegrationByTeamID :one
SELECT * FROM slack_integration WHERE team_id = $1;

-- =====================
-- Slack User Link
-- =====================

-- name: GetSlackUserLink :one
SELECT * FROM slack_user_link WHERE workspace_id = $1 AND user_id = $2;

-- name: GetSlackUserLinkBySlackUserID :one
SELECT * FROM slack_user_link WHERE workspace_id = $1 AND slack_user_id = $2;

-- name: UpsertSlackUserLink :one
INSERT INTO slack_user_link (workspace_id, user_id, slack_user_id)
VALUES ($1, $2, $3)
ON CONFLICT (workspace_id, user_id) DO UPDATE SET
    slack_user_id = EXCLUDED.slack_user_id
RETURNING *;

-- name: DeleteSlackUserLink :exec
DELETE FROM slack_user_link WHERE workspace_id = $1 AND user_id = $2;
