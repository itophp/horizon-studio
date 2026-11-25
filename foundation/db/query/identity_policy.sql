-- name: IAMPolicyCurrent :one
SELECT version.id, version.rules FROM iam_policy_heads head
JOIN iam_policy_versions version ON version.tenant_id = head.tenant_id AND version.id = head.version_id
WHERE head.tenant_id = $1;

-- name: IAMPolicyCurrentID :one
SELECT COALESCE((SELECT version_id FROM iam_policy_heads WHERE tenant_id = $1), 0)::bigint;

-- name: IAMPolicyLock :exec
SELECT pg_advisory_xact_lock(hashtextextended('iam-policy:' || sqlc.arg(tenant_id)::text, 0));

-- name: IAMPolicyReadLock :exec
SELECT pg_advisory_xact_lock_shared(hashtextextended('iam-policy:' || sqlc.arg(tenant_id)::text, 0));

-- name: IAMPolicyDraft :one
INSERT INTO iam_policy_versions (tenant_id, author_id, author_principal_id, base_version, rules, reason)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING id;

-- name: IAMPolicyVersion :one
SELECT version.id, version.author_id, version.base_version, version.rules, version.reason, version.created_at,
       version.published_at, version.reviewer_id,
       COALESCE(NULLIF(author.display_name, ''), placement.username, '')::text AS author_name,
       COALESCE(NULLIF(reviewer.display_name, ''), reviewer_placement.username, '')::text AS reviewer_name,
       version.author_principal_id, login.principal_id AS current_author_principal_id
FROM iam_policy_versions version
LEFT JOIN iam_accounts placement ON placement.tenant_id = version.tenant_id AND placement.id = version.author_id
LEFT JOIN iam_login_accounts login ON login.id = placement.login_account_id
LEFT JOIN iam_principals author ON author.id = COALESCE(version.author_principal_id, login.principal_id)
LEFT JOIN iam_principals reviewer ON reviewer.id = version.reviewer_principal_id
LEFT JOIN iam_accounts reviewer_placement ON reviewer_placement.tenant_id = version.tenant_id AND reviewer_placement.id = version.reviewer_id
WHERE version.tenant_id = $1 AND version.id = $2;

-- name: IAMPolicyPublish :one
UPDATE iam_policy_versions version SET published_at = now(), reviewer_id = sqlc.arg(reviewer_id)::bigint,
    reviewer_principal_id = sqlc.arg(reviewer_principal_id)::uuid
WHERE version.tenant_id = sqlc.arg(tenant_id) AND version.id = sqlc.arg(id) AND published_at IS NULL
AND author_id <> sqlc.arg(reviewer_id) AND base_version = sqlc.arg(base_version)
AND author_principal_id IS NOT NULL AND author_principal_id <> sqlc.arg(reviewer_principal_id)
AND NOT EXISTS (
    SELECT 1 FROM iam_accounts placement JOIN iam_login_accounts login ON login.id = placement.login_account_id
    WHERE placement.tenant_id = version.tenant_id AND placement.id = version.author_id
        AND login.principal_id = sqlc.arg(reviewer_principal_id)
)
RETURNING id;

-- name: IAMPolicyAdvance :exec
INSERT INTO iam_policy_heads (tenant_id, version_id) VALUES ($1, $2)
ON CONFLICT (tenant_id) DO UPDATE SET version_id = EXCLUDED.version_id;

-- name: IAMPolicyHistory :many
SELECT id, author_id, base_version, reason, published_at, reviewer_id FROM iam_policy_versions
WHERE tenant_id = $1 ORDER BY id DESC LIMIT 50;

-- name: IAMPolicySearchContexts :many
SELECT access.account_id, placement.username AS full_name, placement.username
FROM iam_tenant_account_authority access
JOIN iam_accounts placement ON placement.tenant_id = access.tenant_id AND placement.id = access.account_id
WHERE access.tenant_id = sqlc.arg(tenant_id)
  AND strpos(lower(placement.username), lower(sqlc.arg(keyword)::text)) > 0
ORDER BY placement.username, access.account_id
LIMIT 20;
