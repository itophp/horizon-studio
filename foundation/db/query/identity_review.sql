-- name: IAMReviewsDueTenants :many
SELECT id, next_review_at FROM iam_tenants WHERE status = 'active' AND next_review_at <= statement_timestamp()
ORDER BY next_review_at, id LIMIT 20;

-- name: IAMProvisionReviewService :exec
INSERT INTO iam_principals(id, kind, display_name, creation_source, created_in_tenant)
SELECT md5('iam-access-review:' || tenant.id)::uuid, 'service', 'Access review scheduler', 'iam-access-review', tenant.id
FROM iam_tenants tenant WHERE tenant.id = $1 AND tenant.status = 'active'
ON CONFLICT (id) DO NOTHING;

-- name: IAMReviewService :one
SELECT principal.id
FROM iam_tenants tenant
JOIN iam_principals principal ON principal.id = md5('iam-access-review:' || tenant.id)::uuid
    AND principal.created_in_tenant = tenant.id AND principal.creation_source = 'iam-access-review'
WHERE tenant.id = $1 AND tenant.status = 'active' AND principal.kind = 'service' AND principal.status = 'active';

-- name: IAMReviewConfigure :one
UPDATE iam_tenants
SET review_interval_days = sqlc.arg(interval_days),
    next_review_at = statement_timestamp() + make_interval(days => sqlc.arg(interval_days))
WHERE id = sqlc.arg(tenant_id) AND status = 'active'
RETURNING next_review_at;

-- name: IAMReviewSnapshot :one
WITH schedule AS (
    UPDATE iam_tenants SET next_review_at = statement_timestamp() + make_interval(days => review_interval_days)
    WHERE iam_tenants.id = sqlc.arg(tenant_id) AND iam_tenants.status = 'active'
        AND (sqlc.narg(expected_due_at)::timestamptz IS NULL OR
            (next_review_at = sqlc.narg(expected_due_at)::timestamptz AND next_review_at <= statement_timestamp()))
    RETURNING iam_tenants.id
), assignments AS (
    SELECT member.tenant_id, member.principal_id, 'membership'::text AS kind, member.id AS target_id,
           NULL::bigint AS role_id, member.expires_at, member.access_revoked_at
    FROM iam_tenant_memberships member JOIN schedule ON schedule.id = member.tenant_id
    JOIN iam_principals principal ON principal.id = member.principal_id AND principal.status = 'active'
    WHERE member.status = 'active' AND (member.expires_at IS NULL OR member.expires_at > statement_timestamp())
    UNION ALL
    SELECT grant_row.tenant_id, grant_row.principal_id, 'context_role', grant_row.id, grant_row.role_id, grant_row.expires_at, NULL::timestamptz
    FROM iam_context_role_grants grant_row JOIN schedule ON schedule.id = grant_row.tenant_id
    JOIN iam_tenant_memberships member ON member.tenant_id = grant_row.tenant_id AND member.principal_id = grant_row.principal_id
    JOIN iam_principals principal ON principal.id = member.principal_id AND principal.status = 'active'
    JOIN iam_roles role ON role.tenant_id = grant_row.tenant_id AND role.id = grant_row.role_id AND role.status = 'active'
    WHERE grant_row.revoked_at IS NULL AND grant_row.expires_at > statement_timestamp()
      AND member.status = 'active' AND (member.expires_at IS NULL OR member.expires_at > statement_timestamp())
      AND (grant_row.identity_binding_id IS NULL OR EXISTS (
          SELECT 1 FROM iam_active_identity_bindings binding WHERE binding.id = grant_row.identity_binding_id
      ))
), created AS (
    INSERT INTO iam_access_reviews(tenant_id, principal_id, kind, target_id, role_id, target_expires_at, target_revoked_at)
    SELECT tenant_id, principal_id, kind, target_id, role_id, expires_at, access_revoked_at FROM assignments
    ON CONFLICT (tenant_id, kind, target_id) WHERE status = 'pending' DO NOTHING
    RETURNING iam_access_reviews.id
)
SELECT count(*) FROM created;

-- name: IAMReviewList :many
SELECT review.id, review.principal_id, review.kind, review.target_id, review.role_id, review.target_expires_at,
       review.status, review.created_at, review.reviewer_id, review.reviewed_at, review.reason,
       coalesce(nullif(principal.display_name, ''), member.username, '')::text AS principal_name,
       coalesce(member.username, '')::text AS username, coalesce(role.name, '')::text AS role_name
FROM iam_access_reviews review
LEFT JOIN iam_principals principal ON principal.id = review.principal_id
LEFT JOIN LATERAL (
    SELECT placement.username FROM iam_accounts placement
    JOIN iam_login_accounts login ON login.id = placement.login_account_id AND login.principal_id = review.principal_id
    WHERE placement.tenant_id = review.tenant_id
    ORDER BY placement.id LIMIT 1
) member ON true
LEFT JOIN iam_roles role ON role.tenant_id = review.tenant_id AND role.id = review.role_id
WHERE review.tenant_id = sqlc.arg(tenant_id)
AND review.id > sqlc.arg(after_id)::bigint AND (sqlc.arg(status)::text = '' OR review.status = sqlc.arg(status))
ORDER BY review.id LIMIT 51;

-- name: IAMReviewLock :one
SELECT id, kind, target_id, target_expires_at, target_revoked_at
FROM iam_access_reviews WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id)
AND status = 'pending' AND principal_id <> sqlc.arg(reviewer_principal)::uuid
FOR UPDATE;

-- name: IAMReviewCheckMembership :one
SELECT member.id, EXISTS (
    SELECT 1 FROM iam_accounts placement JOIN iam_login_accounts login ON login.id = placement.login_account_id
    WHERE placement.tenant_id = member.tenant_id AND login.principal_id = member.principal_id AND placement.is_admin
) AS protected
FROM iam_tenant_memberships member
JOIN iam_principals principal ON principal.id = member.principal_id AND principal.status = 'active'
WHERE member.tenant_id = sqlc.arg(tenant_id) AND member.id = sqlc.arg(id) AND member.status = 'active'
    AND (member.expires_at IS NULL OR member.expires_at > statement_timestamp())
    AND member.expires_at IS NOT DISTINCT FROM sqlc.narg(expires_at)::timestamptz
    AND member.access_revoked_at IS NOT DISTINCT FROM sqlc.narg(revoked_at)::timestamptz
FOR UPDATE OF member;

-- name: IAMReviewSuspendMembership :exec
UPDATE iam_tenant_memberships SET status = 'suspended' WHERE tenant_id = $1 AND id = $2 AND status = 'active';

-- name: IAMReviewCheckContextRole :one
SELECT grant_row.id FROM iam_context_role_grants grant_row
JOIN iam_roles role ON role.tenant_id = grant_row.tenant_id AND role.id = grant_row.role_id AND role.status = 'active'
JOIN iam_tenant_memberships member ON member.tenant_id = grant_row.tenant_id AND member.principal_id = grant_row.principal_id
JOIN iam_principals principal ON principal.id = member.principal_id AND principal.status = 'active'
WHERE grant_row.tenant_id = sqlc.arg(tenant_id) AND grant_row.id = sqlc.arg(id)
AND grant_row.revoked_at IS NULL AND grant_row.expires_at > statement_timestamp()
AND member.status = 'active' AND (member.expires_at IS NULL OR member.expires_at > statement_timestamp())
AND (grant_row.identity_binding_id IS NULL OR EXISTS (
    SELECT 1 FROM iam_active_identity_bindings binding WHERE binding.id = grant_row.identity_binding_id
))
FOR UPDATE OF grant_row;

-- name: IAMReviewDecide :one
UPDATE iam_access_reviews SET status = sqlc.arg(next_status), reviewer_id = sqlc.arg(reviewer_id)::bigint,
    reviewed_at = statement_timestamp(), reason = sqlc.arg(reason)::text
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id) AND status = 'pending'
RETURNING id;
