-- name: IAMListTenantMembers :many
SELECT placement.id, placement.username, member.status AS membership_status,
       placement.status AS account_status, COALESCE(login.status, '')::text AS login_status,
       principal.kind AS principal_kind,
       member.expires_at, placement.is_admin AS protected
FROM iam_tenant_memberships member
JOIN iam_principals principal ON principal.id = member.principal_id
JOIN iam_accounts placement ON placement.tenant_id = member.tenant_id
LEFT JOIN iam_login_accounts login ON login.id = placement.login_account_id
WHERE member.tenant_id = sqlc.arg(tenant_id) AND placement.id > sqlc.arg(after_id)::bigint
AND member.principal_id = COALESCE(login.principal_id, placement.machine_principal_id)
ORDER BY placement.id
LIMIT 51;

-- name: IAMChangeTenantMembership :one
UPDATE iam_tenant_memberships member
SET status = sqlc.arg(new_status),
    expires_at = CASE WHEN sqlc.arg(replace_expiry)::boolean THEN sqlc.narg(expires_at)::timestamptz ELSE member.expires_at END
FROM iam_accounts placement LEFT JOIN iam_login_accounts login ON login.id = placement.login_account_id
WHERE member.tenant_id = sqlc.arg(tenant_id) AND placement.tenant_id = member.tenant_id
AND placement.id = sqlc.arg(account_id) AND placement.id <> sqlc.arg(actor_id)
AND member.principal_id = COALESCE(login.principal_id, placement.machine_principal_id)
AND member.status = sqlc.arg(old_status)
AND (sqlc.narg(expires_at)::timestamptz IS NULL OR sqlc.narg(expires_at)::timestamptz > statement_timestamp())
AND (NOT sqlc.arg(require_unexpired)::boolean OR member.expires_at IS NULL OR member.expires_at > statement_timestamp())
AND NOT EXISTS (
    SELECT 1 FROM iam_accounts protected JOIN iam_login_accounts protected_login ON protected_login.id = protected.login_account_id
    WHERE protected.tenant_id = member.tenant_id AND protected.is_admin AND protected_login.principal_id = member.principal_id
)
RETURNING member.id, member.status, member.expires_at;

-- name: IAMRevokeMembershipInvitation :one
UPDATE iam_membership_invitations
SET expires_at = statement_timestamp()
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id)::uuid
AND accepted_at IS NULL AND expires_at > statement_timestamp()
RETURNING id;
