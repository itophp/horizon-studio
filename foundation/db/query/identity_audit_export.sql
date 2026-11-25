-- name: IAMAuditExportApproval :one
SELECT audit_export_approval FROM iam_tenants WHERE id = $1 AND status = 'active' FOR SHARE;

-- name: IAMConfigureAuditExport :exec
UPDATE iam_tenants SET audit_export_approval = $2 WHERE id = $1 AND status = 'active';

-- name: IAMCreateAuditExportPermit :exec
INSERT INTO iam_audit_export_permits(tenant_id, account_id, membership_id, filter_digest, request_id, binding_id, filters)
VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING;

-- name: IAMUseAuditExportPermit :one
DELETE FROM iam_audit_export_permits permit
USING iam_tenant_memberships membership
WHERE permit.tenant_id = sqlc.arg(tenant_id) AND permit.account_id = sqlc.arg(account_id)
    AND permit.membership_id = sqlc.arg(membership_id)
    AND permit.binding_id = sqlc.arg(binding_id)
    AND permit.request_id = sqlc.arg(request_id) AND (sqlc.arg(filter_digest)::text = '' OR permit.filter_digest = sqlc.arg(filter_digest))
    AND permit.expires_at > clock_timestamp()
    AND membership.tenant_id = permit.tenant_id AND membership.id = permit.membership_id
    AND membership.status = 'active' AND (membership.expires_at IS NULL OR membership.expires_at > clock_timestamp())
    AND (membership.access_revoked_at IS NULL OR membership.access_revoked_at < permit.expires_at - interval '15 minutes')
RETURNING permit.filters;

-- name: IAMPruneAuditExportPermits :exec
DELETE FROM iam_audit_export_permits WHERE tenant_id = $1 AND account_id = $2 AND expires_at <= now();

-- name: IAMListAuditExportPermits :many
SELECT permit.request_id, permit.expires_at,
    COALESCE((permit.filters->>'workspace')::boolean, false)::boolean AS workspace,
    COALESCE((permit.filters->>'limit')::integer, 30)::integer AS max_rows
FROM iam_audit_export_permits permit
JOIN iam_tenant_memberships membership ON membership.tenant_id = permit.tenant_id AND membership.id = permit.membership_id
WHERE permit.tenant_id = $1 AND permit.account_id = $2 AND permit.binding_id = $3
    AND permit.membership_id = $4 AND permit.expires_at > now()
    AND membership.status = 'active' AND (membership.expires_at IS NULL OR membership.expires_at > now())
    AND (membership.access_revoked_at IS NULL OR membership.access_revoked_at < permit.expires_at - interval '15 minutes')
ORDER BY permit.expires_at DESC, permit.request_id LIMIT 50;
