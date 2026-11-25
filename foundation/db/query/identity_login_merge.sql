-- name: IAMMergeLookupLogin :one
SELECT login.id, login.principal_id
FROM iam_login_accounts login
JOIN iam_active_account_subjects subject ON subject.login_account_id = login.id AND subject.tenant_id = sqlc.arg(tenant_id)::text
WHERE login.username = sqlc.arg(username) AND login.realm = sqlc.arg(realm);

-- name: IAMMergeProofDevice :one
SELECT session.public_id FROM iam_sessions session
JOIN iam_active_account_subjects subject ON subject.tenant_id = session.tenant_id AND subject.account_id = session.account_id
WHERE session.token_hash = sqlc.arg(token_hash) AND session.csrf_token = sqlc.arg(csrf_token)
    AND subject.login_account_id = sqlc.arg(login_id) AND session.identity_binding_id IS NULL
    AND session.expires_at > clock_timestamp();

-- name: IAMMergeCreate :one
INSERT INTO iam_login_merge_reviews(tenant_id, primary_login_id, secondary_login_id, primary_principal_id, secondary_principal_id, primary_device_id, reason)
VALUES ($1,$2,$3,$4,$5,$6,$7)
RETURNING id;

-- name: IAMMergeReviewLock :one
SELECT * FROM iam_login_merge_reviews WHERE id = $1 AND tenant_id = $2 FOR UPDATE;

-- name: IAMMergeConfirm :exec
UPDATE iam_login_merge_reviews SET status = 'confirmed', secondary_device_id = $2 WHERE id = $1;

-- name: IAMMergeComplete :exec
UPDATE iam_login_merge_reviews SET status = $2, association_id = sqlc.narg(association_id), decision_reason = sqlc.arg(decision_reason) WHERE id = $1;

-- name: IAMMergeList :many
SELECT review.id, primary_login.username AS primary_username, second.username AS secondary_username,
    review.status, review.created_at, review.expires_at, review.reason, COALESCE(review.decision_reason, '')::text AS decision_reason,
    (review.secondary_login_id = sqlc.arg(login_id))::boolean AS can_confirm
FROM iam_login_merge_reviews review
JOIN iam_login_accounts primary_login ON primary_login.id = review.primary_login_id
JOIN iam_login_accounts second ON second.id = review.secondary_login_id
WHERE review.tenant_id = sqlc.arg(tenant_id)
    AND (sqlc.arg(reviewer)::boolean OR sqlc.arg(login_id) IN (review.primary_login_id, review.secondary_login_id))
    AND (review.status NOT IN ('pending','confirmed') OR review.expires_at > now())
    AND (sqlc.narg(before_id)::uuid IS NULL OR review.id < sqlc.narg(before_id)::uuid)
ORDER BY review.id DESC LIMIT 51;

-- name: IAMMergeLockPrincipals :many
SELECT id FROM iam_principals WHERE id IN (sqlc.arg(primary_id), sqlc.arg(secondary_id))
    AND kind = 'human' AND status = 'active' ORDER BY id FOR UPDATE;

-- name: IAMMergeLockLogins :many
SELECT id FROM iam_login_accounts WHERE id IN (sqlc.arg(primary_id), sqlc.arg(secondary_id))
    AND status = 'active' ORDER BY id FOR UPDATE;

-- name: IAMMergeLockMemberships :many
SELECT id FROM iam_tenant_memberships WHERE principal_id IN (sqlc.arg(primary_id), sqlc.arg(secondary_id))
    ORDER BY tenant_id, principal_id FOR UPDATE;

-- name: IAMMergeCompatible :one
SELECT EXISTS (
    SELECT 1 FROM iam_login_merge_reviews review
    JOIN iam_login_accounts a ON a.id = review.primary_login_id AND a.principal_id = review.primary_principal_id
    JOIN iam_login_accounts b ON b.id = review.secondary_login_id
        AND b.principal_id = CASE WHEN sqlc.arg(rollback)::boolean THEN review.primary_principal_id ELSE review.secondary_principal_id END
    JOIN iam_tenant_memberships first_member ON first_member.tenant_id = review.tenant_id AND first_member.principal_id = review.primary_principal_id
    JOIN iam_tenant_memberships second_member ON second_member.tenant_id = review.tenant_id AND second_member.principal_id = review.secondary_principal_id
    WHERE review.id = sqlc.arg(id) AND first_member.status = 'active' AND second_member.status = 'active'
    AND first_member.expires_at IS NOT DISTINCT FROM second_member.expires_at
    AND (first_member.expires_at IS NULL OR first_member.expires_at > now())
    AND (NOT sqlc.arg(rollback) OR second_member.access_revoked_at IS NOT DISTINCT FROM first_member.access_revoked_at)
    AND NOT EXISTS (
        SELECT 1 FROM iam_tenant_memberships m WHERE m.principal_id IN (review.primary_principal_id, review.secondary_principal_id)
            AND m.tenant_id <> review.tenant_id)
    AND NOT EXISTS (
        SELECT 1 FROM iam_login_accounts other WHERE other.principal_id IN (review.primary_principal_id, review.secondary_principal_id)
            AND other.id NOT IN (a.id, b.id))
    AND NOT EXISTS (
        SELECT 1 FROM iam_accounts placement WHERE placement.login_account_id IN (a.id, b.id)
            AND (placement.is_admin OR placement.tenant_id <> review.tenant_id))
    AND NOT EXISTS (
        SELECT 1 FROM iam_identity_bindings binding WHERE binding.principal_id IN (review.primary_principal_id, review.secondary_principal_id)
            AND binding.status IN ('pending','active','suspended'))
    AND NOT EXISTS (
        SELECT 1 FROM iam_context_role_grants grant_row WHERE grant_row.principal_id IN (review.primary_principal_id, review.secondary_principal_id)
            AND grant_row.revoked_at IS NULL AND grant_row.expires_at > now())
    AND (sqlc.arg(rollback) OR (
        EXISTS (SELECT 1 FROM iam_sessions session JOIN iam_active_account_subjects subject
            ON subject.tenant_id = session.tenant_id AND subject.account_id = session.account_id
            WHERE session.public_id = review.primary_device_id AND subject.login_account_id = a.id
                AND session.tenant_id = review.tenant_id AND session.identity_binding_id IS NULL AND session.expires_at > now())
        AND EXISTS (SELECT 1 FROM iam_sessions session JOIN iam_active_account_subjects subject
            ON subject.tenant_id = session.tenant_id AND subject.account_id = session.account_id
            WHERE session.public_id = review.secondary_device_id AND subject.login_account_id = b.id
                AND session.tenant_id = review.tenant_id AND session.identity_binding_id IS NULL AND session.expires_at > now())
    ))
)::boolean AS compatible;

-- name: IAMMergeMoveLogin :one
UPDATE iam_login_accounts SET principal_id = sqlc.arg(to_id)
WHERE id = sqlc.arg(login_id) AND principal_id = sqlc.arg(from_id) AND status = 'active'
RETURNING id;

-- name: IAMMergeRevokeRuntime :exec
WITH accounts AS (
    SELECT placement.tenant_id, placement.id FROM iam_accounts placement
    WHERE placement.login_account_id IN (sqlc.arg(primary_login), sqlc.arg(secondary_login))
), browser AS (
    DELETE FROM iam_sessions s USING accounts a WHERE s.tenant_id = a.tenant_id AND s.account_id = a.id
), mcp AS (
    UPDATE mcp_tokens t SET revoked_at = statement_timestamp() FROM accounts a
    WHERE t.tenant_id = a.tenant_id AND t.account_id = a.id AND t.revoked_at IS NULL
), unscoped AS (
    DELETE FROM iam_login_sessions WHERE login_account_id IN (sqlc.arg(primary_login), sqlc.arg(secondary_login))
)
UPDATE iam_tenant_memberships SET access_revoked_at = statement_timestamp()
WHERE principal_id IN (sqlc.arg(primary_principal), sqlc.arg(secondary_principal));
