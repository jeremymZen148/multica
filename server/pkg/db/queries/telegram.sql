-- =====================
-- Telegram Integration
-- =====================

-- name: GetTelegramIntegration :one
SELECT * FROM telegram_integration WHERE workspace_id = $1;

-- name: ListAllTelegramIntegrations :many
SELECT * FROM telegram_integration;

-- name: UpsertTelegramIntegration :one
INSERT INTO telegram_integration (workspace_id, bot_token, bot_username, installed_by_id)
VALUES ($1, $2, $3, sqlc.narg('installed_by_id'))
ON CONFLICT (workspace_id) DO UPDATE SET
    bot_token    = EXCLUDED.bot_token,
    bot_username = EXCLUDED.bot_username,
    updated_at   = now()
RETURNING *;

-- name: DeleteTelegramIntegration :exec
DELETE FROM telegram_integration WHERE workspace_id = $1;

-- =====================
-- Telegram User Link
-- =====================

-- name: GetTelegramUserLink :one
SELECT * FROM telegram_user_link WHERE workspace_id = $1 AND user_id = $2;

-- name: GetTelegramUserLinkByChatID :one
SELECT * FROM telegram_user_link WHERE workspace_id = $1 AND telegram_chat_id = $2;

-- name: UpsertTelegramUserLink :one
INSERT INTO telegram_user_link (workspace_id, user_id, telegram_chat_id, telegram_username)
VALUES ($1, $2, $3, sqlc.narg('telegram_username'))
ON CONFLICT (workspace_id, user_id) DO UPDATE SET
    telegram_chat_id  = EXCLUDED.telegram_chat_id,
    telegram_username = EXCLUDED.telegram_username
RETURNING *;

-- name: DeleteTelegramUserLink :exec
DELETE FROM telegram_user_link WHERE workspace_id = $1 AND user_id = $2;

-- name: ListTelegramUserLinksByWorkspace :many
SELECT * FROM telegram_user_link WHERE workspace_id = $1;
