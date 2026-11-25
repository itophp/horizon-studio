-- name: IAMListPrincipals :many
SELECT principal.id, principal.kind, principal.status, principal.display_name, principal.creation_source, principal.created_at, principal.risk_level,
    (SELECT count(*) FROM iam_tenant_memberships m WHERE m.principal_id = principal.id) AS workspace_count,
    (SELECT count(*) FROM iam_login_accounts login WHERE login.principal_id = principal.id AND login.status = 'active') AS login_count
FROM iam_principals principal
WHERE (principal.created_in_tenant = sqlc.arg(tenant_id) OR EXISTS (
    SELECT 1 FROM iam_tenant_memberships member WHERE member.tenant_id = sqlc.arg(tenant_id) AND member.principal_id = principal.id
))
AND (sqlc.narg(after_id)::uuid IS NULL OR principal.id > sqlc.narg(after_id)::uuid)
AND (sqlc.narg(principal_id)::uuid IS NULL OR principal.id = sqlc.narg(principal_id)::uuid)
ORDER BY principal.id
LIMIT 51;

-- name: IAMPrincipalDetail :one
SELECT principal.id, principal.kind, principal.status, principal.display_name, principal.creation_source,
    principal.created_at, principal.risk_level, principal.security_notes,
    COALESCE((SELECT jsonb_agg(to_jsonb(item)) FROM (
        SELECT login.id::text AS id, login.username AS label, login.status, login.realm AS context,
            COALESCE(to_char(placement.last_login AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), '') AS "at"
        FROM iam_login_accounts login
        LEFT JOIN iam_accounts placement ON placement.login_account_id = login.id AND placement.tenant_id = sqlc.arg(tenant_id)::text
        WHERE login.principal_id = principal.id AND (placement.id IS NOT NULL OR
            (principal.created_in_tenant = sqlc.arg(tenant_id) AND NOT EXISTS (
                SELECT 1 FROM iam_accounts elsewhere WHERE elsewhere.login_account_id = login.id)))
        ORDER BY login.username, login.id LIMIT 50
    ) item), '[]'::jsonb)::jsonb AS logins,
    COALESCE((SELECT jsonb_agg(to_jsonb(item)) FROM (
        SELECT member.id::text AS id, member.tenant_id AS label, member.status, member.tenant_id AS context,
            COALESCE(to_char(member.expires_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), '') AS "at"
        FROM iam_tenant_memberships member WHERE member.principal_id = principal.id
        ORDER BY member.tenant_id LIMIT 50
    ) item), '[]'::jsonb)::jsonb AS memberships,
    COALESCE((SELECT jsonb_agg(to_jsonb(item)) FROM (
        SELECT binding.id::text AS id, binding.identity_type AS label, binding.status, binding.entity_id::text AS context,
            COALESCE(to_char(binding.valid_until AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), '') AS "at"
        FROM iam_identity_bindings binding WHERE binding.principal_id = principal.id AND binding.tenant_id = sqlc.arg(tenant_id)
        ORDER BY binding.created_at DESC, binding.id LIMIT 50
    ) item), '[]'::jsonb)::jsonb AS bindings,
    COALESCE((SELECT jsonb_agg(to_jsonb(item)) FROM (
        SELECT entry.id, role.name AS label, role.status, entry.context, entry."at"
        FROM (
            SELECT g.id::text AS id, g.role_id, coalesce(g.identity_binding_id::text, '') AS context, to_char(g.expires_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"') AS "at"
            FROM iam_context_role_grants g
            WHERE g.principal_id = principal.id AND g.tenant_id = sqlc.arg(tenant_id) AND g.revoked_at IS NULL AND g.expires_at > now()
            UNION ALL
            SELECT assignment.id::text, assignment.role_id, account.id::text, '' FROM iam_account_roles assignment
            JOIN iam_accounts account ON account.id = assignment.account_id AND account.tenant_id = assignment.tenant_id
            LEFT JOIN iam_login_accounts login ON login.id = account.login_account_id
            WHERE assignment.tenant_id = sqlc.arg(tenant_id) AND login.principal_id = principal.id
              AND EXISTS (SELECT 1 FROM iam_roles template
                  WHERE template.tenant_id = assignment.tenant_id AND template.id = assignment.role_id AND template.template_key = 'tenant-admin')
        ) entry JOIN iam_roles role ON role.tenant_id = sqlc.arg(tenant_id) AND role.id = entry.role_id
        ORDER BY role.name, entry.id LIMIT 50
    ) item), '[]'::jsonb)::jsonb AS roles,
    COALESCE((SELECT jsonb_agg(to_jsonb(item)) FROM (
        SELECT session.public_id::text AS id, session.device_label AS label, 'active'::text AS status,
            session.account_id::text AS context, to_char(session.expires_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"') AS "at"
        FROM iam_sessions session
        JOIN iam_active_account_subjects active ON active.tenant_id = session.tenant_id AND active.account_id = session.account_id
        WHERE session.tenant_id = sqlc.arg(tenant_id) AND active.principal_id = principal.id AND session.expires_at > now()
            AND (EXISTS (SELECT 1 FROM iam_tenant_account_authority access
                WHERE session.identity_binding_id IS NULL AND access.tenant_id = session.tenant_id AND access.account_id = session.account_id)
                OR EXISTS (
                    SELECT 1 FROM iam_active_identity_bindings binding
                    JOIN iam_context_role_grants grant_row ON grant_row.tenant_id = binding.tenant_id
                        AND grant_row.principal_id = binding.principal_id AND grant_row.identity_binding_id = binding.id
                    JOIN iam_roles role ON role.tenant_id = grant_row.tenant_id AND role.id = grant_row.role_id AND role.status = 'active'
                    WHERE binding.id = session.identity_binding_id AND binding.tenant_id = session.tenant_id
                        AND binding.principal_id = active.principal_id AND grant_row.revoked_at IS NULL AND grant_row.expires_at > now()))
            AND (session.external_identity_id IS NULL OR EXISTS (
                SELECT 1 FROM iam_external_identities external WHERE external.id = session.external_identity_id
                    AND external.login_account_id = active.login_account_id AND external.revoked_at IS NULL))
        ORDER BY session.last_seen_at DESC, session.public_id LIMIT 50
    ) item), '[]'::jsonb)::jsonb AS sessions,
    COALESCE((SELECT jsonb_agg(to_jsonb(item)) FROM (
        SELECT event.id::text AS id, event.action AS label, event.outcome AS status, event.correlation_id AS context,
            to_char(event.created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"') AS "at"
        FROM iam_security_event_history event
        WHERE event.tenant_id = sqlc.arg(tenant_id)
            AND (event.authority->>'principalId' = principal.id::text OR
                (event.resource_type = 'security_principal' AND event.resource_id = principal.id::text))
        ORDER BY event.id DESC LIMIT 50
    ) item), '[]'::jsonb)::jsonb AS audit_events
FROM iam_principals principal
WHERE principal.id = sqlc.arg(principal_id) AND (principal.created_in_tenant = sqlc.arg(tenant_id) OR EXISTS (
    SELECT 1 FROM iam_tenant_memberships member WHERE member.tenant_id = sqlc.arg(tenant_id) AND member.principal_id = principal.id
));

-- name: IAMUpdatePrincipalSecurity :one
UPDATE iam_principals principal SET security_notes = sqlc.arg(security_notes), risk_level = sqlc.arg(risk_level)
WHERE principal.id = sqlc.arg(principal_id) AND (principal.created_in_tenant = sqlc.arg(tenant_id) OR EXISTS (
    SELECT 1 FROM iam_tenant_memberships member WHERE member.tenant_id = sqlc.arg(tenant_id) AND member.principal_id = principal.id
)) RETURNING principal.id;

-- name: IAMProvisionHumanPrincipal :one
WITH principal AS (
    INSERT INTO iam_principals(kind, display_name, creation_source, created_in_tenant)
    SELECT 'human', sqlc.arg(display_name)::text, 'platform', tenant.id FROM iam_tenants tenant
    WHERE tenant.id = sqlc.arg(tenant_id) AND tenant.status = 'active'
    RETURNING id
), login AS (
    INSERT INTO iam_login_accounts(principal_id, username, realm)
    SELECT id, sqlc.arg(username)::text, sqlc.arg(realm)::text FROM principal RETURNING id
), credential AS (
    INSERT INTO iam_password_credentials(login_account_id, password_hash)
    SELECT id, sqlc.arg(password_hash)::text FROM login
)
SELECT principal.id FROM principal;

-- name: IAMTransitionPrincipal :one
UPDATE iam_principals principal SET status = sqlc.arg(next_status)
WHERE principal.id = sqlc.arg(principal_id) AND principal.status = ANY(sqlc.arg(previous_statuses)::text[])
AND (principal.created_in_tenant = sqlc.arg(tenant_id) OR EXISTS (
    SELECT 1 FROM iam_tenant_memberships member WHERE member.tenant_id = sqlc.arg(tenant_id) AND member.principal_id = principal.id
))
AND NOT EXISTS (
    SELECT 1 FROM iam_accounts placement JOIN iam_login_accounts login ON login.id = placement.login_account_id
    WHERE placement.is_admin AND login.principal_id = principal.id
)
RETURNING principal.id;

-- name: IAMChangeTenantStatus :one
UPDATE iam_tenants SET status = sqlc.arg(next_status)
WHERE id = sqlc.arg(tenant_id) AND status = sqlc.arg(previous_status)
RETURNING id;

-- name: IAMPlatformListTenants :many
SELECT id, status, created_at FROM iam_tenants WHERE id > sqlc.arg(after_id)::text ORDER BY id LIMIT 51;
