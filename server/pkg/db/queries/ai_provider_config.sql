-- =====================
-- AI Provider Config
-- =====================

-- name: GetAIProviderConfig :one
SELECT * FROM ai_provider_config WHERE workspace_id = $1;

-- name: UpsertAIProviderConfig :one
INSERT INTO ai_provider_config (
    workspace_id, provider, model, api_key, updated_by_id
) VALUES (
    $1, $2, $3, $4, $5
)
ON CONFLICT (workspace_id) DO UPDATE SET
    provider      = EXCLUDED.provider,
    model         = EXCLUDED.model,
    api_key       = EXCLUDED.api_key,
    updated_by_id = EXCLUDED.updated_by_id,
    updated_at    = now()
RETURNING *;

-- name: DeleteAIProviderConfig :exec
DELETE FROM ai_provider_config WHERE workspace_id = $1;
