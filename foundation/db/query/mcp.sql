-- name: MCPCreateToken :one
INSERT INTO mcp_tokens (tenant_id, account_id, name, token_hash, token_prefix, expires_at, identity_binding_id)
SELECT subject.tenant_id, subject.account_id, sqlc.arg(name), sqlc.arg(token_hash), sqlc.arg(token_prefix),
       COALESCE(sqlc.narg(preserved_expiry)::timestamptz, now() + make_interval(days => sqlc.arg(valid_days)::int)),
       NULLIF(sqlc.arg(identity_binding_id)::text, '')::uuid
FROM iam_active_account_subjects subject
WHERE subject.tenant_id = sqlc.arg(tenant_id) AND subject.account_id = sqlc.arg(account_id)
AND (sqlc.arg(identity_binding_id)::text = '' OR EXISTS (
    SELECT 1 FROM iam_active_identity_bindings binding
    WHERE binding.tenant_id = subject.tenant_id AND binding.principal_id = subject.principal_id
      AND binding.id = NULLIF(sqlc.arg(identity_binding_id)::text, '')::uuid))
RETURNING id, name, token_prefix, created_at, expires_at;

-- name: MCPListTokens :many
SELECT id, name, token_prefix, created_at, expires_at, revoked_at, last_used_at, COALESCE(revoked_by,0)::bigint AS revoked_by
FROM mcp_tokens WHERE tenant_id = $1 AND account_id = $2
AND identity_binding_id IS NOT DISTINCT FROM NULLIF(sqlc.arg(identity_binding_id)::text, '')::uuid
ORDER BY (revoked_at IS NULL AND expires_at>now()) DESC, id DESC LIMIT 100;

-- name: MCPRevokeToken :one
UPDATE mcp_tokens SET revoked_at = now(), revoked_by = account_id
WHERE tenant_id = $1 AND account_id = $2 AND id = $3 AND revoked_at IS NULL
AND identity_binding_id IS NOT DISTINCT FROM NULLIF(sqlc.arg(identity_binding_id)::text, '')::uuid
RETURNING id;

-- name: MCPAuthenticateToken :one
SELECT id, tenant_id, account_id, token_hash, COALESCE(identity_binding_id::text, '')::text AS identity_binding_id
FROM mcp_tokens WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > now();

-- name: MCPTouchToken :exec
UPDATE mcp_tokens SET last_used_at = now() WHERE id = $1 AND revoked_at IS NULL AND expires_at > now();

-- name: MCPInsertAudit :exec
INSERT INTO mcp_call_audits (tenant_id, account_id, token_id, tool, result_code, duration_ms, request_id)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: MCPListAudits :many
SELECT audit.id, audit.token_id, audit.tool, audit.result_code, audit.duration_ms, audit.request_id, audit.created_at
FROM mcp_call_audits audit JOIN mcp_tokens token ON token.tenant_id = audit.tenant_id AND token.id = audit.token_id
WHERE audit.tenant_id = $1 AND audit.account_id = $2
AND token.identity_binding_id IS NOT DISTINCT FROM NULLIF(sqlc.arg(identity_binding_id)::text, '')::uuid
ORDER BY audit.id DESC LIMIT 100;

-- name: MCPTokenForRotation :one
SELECT name, expires_at FROM mcp_tokens
WHERE tenant_id = $1 AND account_id = $2 AND id = $3 AND revoked_at IS NULL AND expires_at > now()
AND identity_binding_id IS NOT DISTINCT FROM NULLIF(sqlc.arg(identity_binding_id)::text, '')::uuid
FOR UPDATE;

-- name: MCPLockOwner :one
SELECT placement.id, credential.password_hash
FROM iam_accounts placement
JOIN iam_active_account_subjects subject ON subject.tenant_id = placement.tenant_id AND subject.account_id = placement.id
JOIN iam_password_credentials credential ON credential.login_account_id = subject.login_account_id
WHERE placement.tenant_id = $1 AND placement.id = $2 FOR UPDATE OF placement, credential;

-- name: MCPActiveCount :one
SELECT count(*) FROM mcp_tokens WHERE tenant_id=$1 AND account_id=$2 AND revoked_at IS NULL AND expires_at>now();

-- name: MCPPruneAuditBatch :exec
DELETE FROM mcp_call_audits WHERE id IN (
 SELECT id FROM mcp_call_audits WHERE created_at<now()-interval '90 days' ORDER BY id LIMIT 1000
);

-- name: MCPPruneTokenBatch :exec
DELETE FROM mcp_tokens WHERE id IN (
 SELECT id FROM mcp_tokens WHERE expires_at<now()-interval '90 days' OR revoked_at<now()-interval '90 days' ORDER BY id LIMIT 1000
);

-- name: MCPAdminRevokeToken :one
UPDATE mcp_tokens SET revoked_at=now(),revoked_by=sqlc.arg(actor_id)
WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(id) AND revoked_at IS NULL RETURNING id;
