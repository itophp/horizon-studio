-- name: IAMActiveAccountSubject :one
SELECT principal_id, login_account_id, membership_id FROM iam_active_account_subjects
WHERE tenant_id = $1 AND account_id = $2;

-- name: IAMLockManagedLogin :one
SELECT login.id FROM iam_login_accounts login
JOIN iam_accounts placement ON placement.login_account_id = login.id
WHERE placement.tenant_id = sqlc.arg(tenant_id) AND placement.id = sqlc.arg(account_id)
AND NOT placement.is_admin AND login.id <> sqlc.arg(actor_login_id)::uuid
FOR UPDATE OF login;

-- name: IAMChangeManagedLoginStatus :one
UPDATE iam_login_accounts login SET status = sqlc.arg(new_status)
WHERE login.id = sqlc.arg(login_id) AND login.status = sqlc.arg(old_status)
AND EXISTS (SELECT 1 FROM iam_accounts placement WHERE placement.login_account_id = login.id
    AND placement.tenant_id = sqlc.arg(tenant_id) AND placement.id = sqlc.arg(account_id) AND NOT placement.is_admin)
AND NOT EXISTS (SELECT 1 FROM iam_accounts placement WHERE placement.login_account_id = login.id
    AND placement.tenant_id <> sqlc.arg(tenant_id))
RETURNING login.id;

-- name: IAMGetInteractiveAccount :one
SELECT account_id, membership_id
FROM iam_active_account_subjects
WHERE login_account_id = $1 AND principal_id = $2 AND tenant_id = $3;

-- name: IAMSelectBrowserIdentity :one
UPDATE iam_sessions session SET csrf_token = sqlc.arg(new_csrf_token),
    identity_binding_id = sqlc.narg(identity_binding_id),
    expires_at = LEAST(session.expires_at, COALESCE(session.refreshed_at, session.created_at) + make_interval(mins => sqlc.arg(max_minutes)::int))
FROM iam_active_account_subjects target
WHERE session.token_hash = sqlc.arg(token_hash) AND session.csrf_token = sqlc.arg(expected_csrf_token)
    AND session.expires_at > now() AND target.tenant_id = sqlc.arg(tenant_id)
    AND session.tenant_id = target.tenant_id AND session.account_id = target.account_id
    AND target.membership_id = sqlc.arg(membership_id)
    AND (sqlc.narg(identity_binding_id)::uuid IS NOT NULL OR EXISTS (
        SELECT 1 FROM iam_tenant_account_authority access
        WHERE access.tenant_id = target.tenant_id AND access.account_id = target.account_id
    ))
    AND (sqlc.narg(identity_binding_id)::uuid IS NULL OR EXISTS (
        SELECT 1 FROM iam_active_identity_bindings binding WHERE binding.id = sqlc.narg(identity_binding_id)
            AND binding.tenant_id = target.tenant_id AND binding.principal_id = target.principal_id
    ))
RETURNING session.token_hash;

-- name: IAMCreateMembershipInvitation :one
INSERT INTO iam_membership_invitations (tenant_id, inviter_account_id, username, token_hash, expires_at, recipient_realm)
VALUES ($1, $2, $3, $4, now() + interval '24 hours', $5) RETURNING id, expires_at;

-- name: IAMLockMembershipInvitation :one
SELECT invite.id, invite.tenant_id, invite.inviter_account_id
FROM iam_membership_invitations invite
JOIN iam_login_accounts login ON login.username = invite.username AND login.realm = invite.recipient_realm AND login.id = sqlc.arg(login_account_id) AND login.status = 'active'
JOIN iam_principals principal ON principal.id = login.principal_id AND principal.kind = 'human' AND principal.status = 'active'
WHERE invite.token_hash = sqlc.arg(token_hash) AND invite.expires_at > now() AND invite.accepted_at IS NULL
FOR UPDATE OF invite;

-- name: IAMAcceptMembershipInvitation :one
WITH recipient AS (
    SELECT login.id, login.principal_id, login.username, login.realm
    FROM iam_login_accounts login
    JOIN iam_principals principal ON principal.id = login.principal_id AND principal.kind = 'human' AND principal.status = 'active'
    WHERE login.id = sqlc.arg(login_account_id) AND login.status = 'active'
    FOR UPDATE OF login
), accepted AS (
    UPDATE iam_membership_invitations invite SET accepted_at = now()
    FROM recipient
    WHERE invite.id = sqlc.arg(invitation_id) AND invite.accepted_at IS NULL AND invite.expires_at > now()
        AND invite.username = recipient.username AND invite.recipient_realm = recipient.realm
    RETURNING invite.tenant_id
), membership AS (
    INSERT INTO iam_tenant_memberships (tenant_id, principal_id)
    SELECT accepted.tenant_id, recipient.principal_id FROM accepted CROSS JOIN recipient
    ON CONFLICT (tenant_id, principal_id) DO NOTHING
    RETURNING tenant_id, principal_id
)
INSERT INTO iam_accounts (tenant_id, username, status, login_account_id)
SELECT accepted.tenant_id, recipient.username, 'active', recipient.id
FROM accepted CROSS JOIN recipient
WHERE EXISTS (SELECT 1 FROM membership)
    OR EXISTS (SELECT 1 FROM iam_tenant_memberships existing WHERE existing.tenant_id = accepted.tenant_id
        AND existing.principal_id = recipient.principal_id AND existing.status = 'active'
        AND (existing.expires_at IS NULL OR existing.expires_at > now()))
RETURNING id;

-- name: IAMRevokeTenantMembership :one
UPDATE iam_tenant_memberships member SET status = 'revoked'
FROM iam_accounts placement LEFT JOIN iam_login_accounts login ON login.id = placement.login_account_id
WHERE placement.id = sqlc.arg(account_id) AND placement.tenant_id = sqlc.arg(tenant_id)
    AND member.tenant_id = placement.tenant_id AND member.principal_id = COALESCE(login.principal_id, placement.machine_principal_id)
    AND member.status <> 'revoked'
    AND NOT EXISTS (
        SELECT 1 FROM iam_accounts protected JOIN iam_login_accounts protected_login ON protected_login.id = protected.login_account_id
        WHERE protected.tenant_id = member.tenant_id AND protected.is_admin AND protected_login.principal_id = member.principal_id
    )
RETURNING member.id;

-- name: IAMLoginThrottle :one
SELECT failures,locked_until FROM iam_login_attempts WHERE attempt_key=$1;

-- name: IAMLoginFailure :exec
INSERT INTO iam_login_attempts(attempt_key,failures,locked_until) VALUES($1,1,now()) ON CONFLICT(attempt_key) DO UPDATE SET failures=CASE WHEN iam_login_attempts.locked_until < now()-interval '15 minutes' THEN 1 ELSE iam_login_attempts.failures+1 END,locked_until=CASE WHEN iam_login_attempts.failures>=4 THEN now()+interval '15 minutes' ELSE now() END;

-- name: IAMLoginClear :exec
DELETE FROM iam_login_attempts WHERE attempt_key=$1;

-- name: IAMSession :one
SELECT s.tenant_id, s.account_id, s.csrf_token,
       subject.principal_id, subject.login_account_id, subject.membership_id, s.identity_binding_id
FROM iam_sessions s
JOIN iam_active_account_subjects subject ON subject.tenant_id = s.tenant_id AND subject.account_id = s.account_id
WHERE s.token_hash = $1 AND s.expires_at > now()
AND (s.external_identity_id IS NULL OR EXISTS (
    SELECT 1 FROM iam_external_identities external WHERE external.id = s.external_identity_id
        AND external.login_account_id = subject.login_account_id AND external.revoked_at IS NULL
))
AND (s.identity_binding_id IS NULL OR EXISTS (
    SELECT 1 FROM iam_active_identity_bindings binding WHERE binding.id = s.identity_binding_id
        AND binding.tenant_id = s.tenant_id AND binding.principal_id = subject.principal_id
));

-- name: IAMCreateIdentityBinding :one
INSERT INTO iam_identity_bindings(tenant_id, principal_id, identity_type, identity_version, revocation_event, entity_id, status, valid_until)
SELECT subject.tenant_id, subject.principal_id, sqlc.arg(identity_type)::text, sqlc.arg(identity_version)::text,
       sqlc.arg(revocation_event)::text, sqlc.arg(entity_id)::bigint, sqlc.arg(status)::text,
       CASE WHEN sqlc.arg(valid_minutes)::int > 0 THEN statement_timestamp() + make_interval(mins => sqlc.arg(valid_minutes)::int) END
FROM iam_active_account_subjects subject WHERE subject.tenant_id = sqlc.arg(tenant_id) AND subject.account_id = sqlc.arg(account_id)
AND NOT EXISTS (SELECT 1 FROM cap_outbox event WHERE event.tenant_id = subject.tenant_id
    AND event.event_type = sqlc.arg(revocation_event) AND event.aggregate_id = sqlc.arg(entity_id)::bigint::text)
RETURNING iam_identity_bindings.id;

-- name: IAMListIdentityBindings :many
SELECT id, tenant_id, identity_type, identity_version FROM iam_active_identity_bindings
WHERE principal_id = $1 ORDER BY tenant_id, identity_type, id;

-- name: IAMReadIdentityBinding :one
SELECT identity_type, identity_version, entity_id FROM iam_active_identity_bindings
WHERE tenant_id = $1 AND principal_id = $2 AND id = $3;

-- name: IAMRevokeIdentityBindingsForEntity :exec
UPDATE iam_identity_bindings SET status = 'revoked', revoked_at = now()
WHERE tenant_id = $1 AND identity_type = $2 AND identity_version = $3 AND entity_id = $4 AND status IN ('active', 'pending', 'suspended', 'expired');

-- name: IAMRevokeOwnIdentityBinding :one
UPDATE iam_identity_bindings SET status = 'revoked', revoked_at = now()
WHERE tenant_id = $1 AND principal_id = $2 AND id = $3 AND status IN ('active', 'pending', 'suspended', 'expired') RETURNING id;

-- name: IAMReconcileIdentityBindingRevocations :execrows
WITH stale AS (
    SELECT binding.id FROM iam_identity_bindings binding
    WHERE binding.status IN ('active', 'pending', 'suspended', 'expired') AND EXISTS (
        SELECT 1 FROM cap_outbox event WHERE event.tenant_id = binding.tenant_id
            AND event.event_type = binding.revocation_event AND event.aggregate_id = binding.entity_id::text
    )
    ORDER BY binding.id LIMIT 500 FOR UPDATE OF binding SKIP LOCKED
)
UPDATE iam_identity_bindings binding SET status = 'revoked', revoked_at = now()
FROM stale WHERE binding.id = stale.id AND binding.status IN ('active', 'pending', 'suspended', 'expired');

-- name: IAMDeleteSession :exec
DELETE FROM iam_sessions WHERE token_hash=$1;

-- name: IAMLockRefreshSession :one
SELECT session.public_id, COALESCE(session.refreshed_at, session.created_at)::timestamptz AS authenticated_at,
    session.expires_at, session.last_seen_at
FROM iam_sessions session
JOIN iam_active_account_subjects subject ON subject.tenant_id = session.tenant_id AND subject.account_id = session.account_id
WHERE session.token_hash = sqlc.arg(token_hash) AND session.csrf_token = sqlc.arg(expected_csrf)
    AND subject.login_account_id = sqlc.arg(login_id) AND session.expires_at > clock_timestamp()
FOR UPDATE OF session;

-- name: IAMRotateBrowserSession :one
WITH removed AS (
    DELETE FROM iam_sessions old_session WHERE old_session.token_hash = sqlc.arg(old_token) AND old_session.csrf_token = sqlc.arg(expected_csrf)
        AND old_session.expires_at > clock_timestamp()
    RETURNING old_session.*
)
INSERT INTO iam_sessions(token_hash, tenant_id, account_id, csrf_token,
    created_at, expires_at, public_id, device_label, last_seen_at, identity_binding_id, external_identity_id, refreshed_at)
SELECT sqlc.arg(new_token), tenant_id, account_id, sqlc.arg(new_csrf), created_at,
    statement_timestamp() + make_interval(mins => sqlc.arg(minutes)::int), public_id, device_label,
    statement_timestamp(), identity_binding_id, external_identity_id, statement_timestamp()
FROM removed RETURNING public_id;

-- name: IAMListDeviceSessions :many
SELECT session.public_id, session.device_label, session.tenant_id, session.created_at, session.last_seen_at, session.expires_at,
       (session.token_hash = sqlc.arg(current_token)::text)::boolean AS current_device
FROM iam_sessions session
JOIN iam_accounts placement ON placement.tenant_id = session.tenant_id AND placement.id = session.account_id
WHERE placement.login_account_id = sqlc.arg(login_account_id) AND session.expires_at > now()
ORDER BY current_device DESC, session.last_seen_at DESC, session.public_id
LIMIT 100;

-- name: IAMRevokeDeviceSession :one
DELETE FROM iam_sessions target USING iam_accounts placement
WHERE target.tenant_id = placement.tenant_id AND target.account_id = placement.id
AND placement.login_account_id = sqlc.arg(login_account_id) AND target.public_id = sqlc.arg(public_id)
AND EXISTS (
    SELECT 1 FROM iam_sessions present
    JOIN iam_active_account_subjects subject ON subject.tenant_id = present.tenant_id AND subject.account_id = present.account_id
    WHERE present.token_hash = sqlc.arg(current_token) AND present.csrf_token = sqlc.arg(current_context)
    AND present.expires_at > now() AND subject.login_account_id = sqlc.arg(login_account_id)
    AND (present.identity_binding_id IS NULL OR EXISTS (
        SELECT 1 FROM iam_active_identity_bindings binding WHERE binding.id = present.identity_binding_id
            AND binding.tenant_id = present.tenant_id AND binding.principal_id = subject.principal_id
    ))
)
RETURNING target.public_id;

-- name: IAMRevokeSessions :exec
DELETE FROM iam_sessions WHERE tenant_id=$1 AND account_id=$2;

-- name: IAMTouchLogin :exec
UPDATE iam_accounts SET last_login=now() WHERE tenant_id=$1 AND id=$2;

-- name: IAMProtectedRole :one
SELECT (template_key <> 'custom')::boolean AS protected FROM iam_roles WHERE tenant_id = $1 AND id = $2;

-- name: IAMCurrentContextRoleGrants :many
SELECT role.grants FROM iam_context_role_grants grant_record
JOIN iam_roles role ON role.tenant_id = grant_record.tenant_id AND role.id = grant_record.role_id
WHERE grant_record.tenant_id = sqlc.arg(tenant_id) AND grant_record.principal_id = sqlc.arg(principal_id)
AND grant_record.expires_at > now() AND grant_record.revoked_at IS NULL
AND grant_record.identity_binding_id IS NOT DISTINCT FROM sqlc.narg(identity_binding_id)::uuid
AND role.status = 'active' AND role.template_key = 'custom';

-- name: IAMCreateContextRoleGrant :one
INSERT INTO iam_context_role_grants(tenant_id, principal_id, identity_binding_id, role_id, granted_by, reason, expires_at)
SELECT subject.tenant_id, subject.principal_id, sqlc.narg(identity_binding_id)::uuid,
       role.id, sqlc.arg(granted_by), sqlc.arg(reason), now() + make_interval(mins => sqlc.arg(valid_minutes)::int)
FROM iam_active_account_subjects subject
JOIN iam_roles role ON role.tenant_id = subject.tenant_id AND role.id = sqlc.arg(role_id) AND role.status = 'active' AND role.template_key = 'custom'
WHERE subject.tenant_id = sqlc.arg(tenant_id) AND subject.account_id = sqlc.arg(account_id)
AND (sqlc.narg(identity_binding_id)::uuid IS NULL OR EXISTS (
    SELECT 1 FROM iam_active_identity_bindings binding WHERE binding.tenant_id = subject.tenant_id
    AND binding.principal_id = subject.principal_id AND binding.id = sqlc.narg(identity_binding_id)::uuid
))
RETURNING id;

-- name: IAMRevokeContextRoleGrant :one
UPDATE iam_context_role_grants SET revoked_at = now()
WHERE tenant_id = $1 AND id = $2 AND revoked_at IS NULL AND expires_at > now() RETURNING id;

-- name: IAMRevokeChangedContextRoles :exec
UPDATE iam_context_role_grants SET revoked_at = now()
WHERE tenant_id = $1 AND role_id = $2 AND revoked_at IS NULL AND expires_at > now();

-- name: IAMListContextRoleGrants :many
SELECT id, principal_id, identity_binding_id, role_id, granted_by, reason, created_at, expires_at, revoked_at
FROM iam_context_role_grants WHERE tenant_id = $1 ORDER BY created_at DESC, id LIMIT 100;

-- name: IAMAccountRoles :many
SELECT role_id FROM iam_account_roles WHERE tenant_id=$1 AND account_id=$2 ORDER BY role_id;

-- name: IAMClearAccountRoles :exec
DELETE FROM iam_account_roles WHERE tenant_id=$1 AND account_id=$2;

-- name: IAMAssignRole :exec
INSERT INTO iam_account_roles(tenant_id,account_id,role_id) VALUES($1,$2,$3);

-- name: IAMCountAccounts :one
SELECT count(*) FROM iam_accounts WHERE iam_accounts.tenant_id=$1;

-- name: IAMListOrganizations :many
SELECT id,name,code,kind,COALESCE(parent_id,0)::bigint AS parent_id,status,description,created_at FROM identity_organizations WHERE tenant_id=sqlc.arg(tenant_id) AND (sqlc.arg(all_data)::boolean OR id=ANY(sqlc.arg(org_ids)::bigint[])) ORDER BY id;

-- name: IAMGetOrganization :one
SELECT id,name,code,kind,COALESCE(parent_id,0)::bigint AS parent_id,status,description,created_at FROM identity_organizations WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(id) AND (sqlc.arg(all_data)::boolean OR id=ANY(sqlc.arg(org_ids)::bigint[]));

-- name: IAMUpdateOrganization :one
UPDATE identity_organizations SET name=sqlc.arg(name),code=sqlc.arg(code),description=sqlc.arg(description) WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(id) AND (sqlc.arg(all_data)::boolean OR id=ANY(sqlc.arg(org_ids)::bigint[])) RETURNING id;

-- name: IAMListDepartments :many
SELECT id,name,code,organization_id,COALESCE(parent_id,0)::bigint AS parent_id,manager,status,description,created_at FROM identity_department_directory WHERE tenant_id=sqlc.arg(tenant_id) AND (sqlc.arg(all_data)::boolean OR organization_id=ANY(sqlc.arg(org_ids)::bigint[]) OR id=ANY(sqlc.arg(dept_ids)::bigint[])) ORDER BY id;

-- name: IAMGetDepartment :one
SELECT id,name,code,organization_id,COALESCE(parent_id,0)::bigint AS parent_id,manager,status,description,created_at FROM identity_department_directory WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(id) AND (sqlc.arg(all_data)::boolean OR organization_id=ANY(sqlc.arg(org_ids)::bigint[]) OR id=ANY(sqlc.arg(dept_ids)::bigint[]));

-- name: IAMCreateDepartment :one
INSERT INTO identity_departments (tenant_id,name,code,organization_id,parent_id,manager,status,description) VALUES (sqlc.arg(tenant_id),sqlc.arg(name),sqlc.arg(code),sqlc.arg(organization_id),NULLIF(sqlc.arg(parent_id)::bigint,0),sqlc.arg(manager),sqlc.arg(status),sqlc.arg(description)) RETURNING id;

-- name: IAMUpdateDepartment :one
UPDATE identity_departments SET name=sqlc.arg(name),code=sqlc.arg(code),organization_id=sqlc.arg(organization_id),parent_id=NULLIF(sqlc.arg(parent_id)::bigint,0),manager=sqlc.arg(manager),status=sqlc.arg(status),description=sqlc.arg(description) WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(id) AND (sqlc.arg(all_data)::boolean OR organization_id=ANY(sqlc.arg(org_ids)::bigint[]) OR id=ANY(sqlc.arg(dept_ids)::bigint[])) RETURNING id;

-- name: IAMDeleteDepartment :one
DELETE FROM identity_departments WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(id) AND (sqlc.arg(all_data)::boolean OR organization_id=ANY(sqlc.arg(org_ids)::bigint[]) OR id=ANY(sqlc.arg(dept_ids)::bigint[])) RETURNING id;

-- name: IAMListRoles :many
SELECT id,name,code,description,status,grants,created_at FROM iam_roles WHERE tenant_id=sqlc.arg(tenant_id) AND sqlc.arg(all_data)::boolean ORDER BY id;

-- name: IAMGetRole :one
SELECT id,name,code,description,status,grants,created_at FROM iam_roles WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(id) AND sqlc.arg(all_data)::boolean;

-- name: IAMCreateRole :one
INSERT INTO iam_roles (tenant_id,name,code,description,status,grants,template_key) VALUES (sqlc.arg(tenant_id),sqlc.arg(name),sqlc.arg(code),sqlc.arg(description),sqlc.arg(status),sqlc.arg(grants),sqlc.arg(template_key)) RETURNING id;

-- name: IAMUpdateRole :one
UPDATE iam_roles SET name=sqlc.arg(name),code=sqlc.arg(code),description=sqlc.arg(description),status=sqlc.arg(status),grants=sqlc.arg(grants) WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(id) AND sqlc.arg(all_data)::boolean AND template_key = 'custom' RETURNING id;

-- name: IAMDeleteRole :one
DELETE FROM iam_roles WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(id) AND sqlc.arg(all_data)::boolean AND template_key = 'custom' RETURNING id;

-- name: IAMListAuthorityAccounts :many
SELECT id, username, status
FROM iam_accounts
WHERE tenant_id = sqlc.arg(tenant_id) AND (sqlc.arg(all_data)::boolean OR id = sqlc.arg(own_id)::bigint)
ORDER BY id;

-- name: IAMGetAuthorityAccount :one
SELECT id, username, status, is_admin, created_at, last_login,
       CASE WHEN machine_principal_id IS NOT NULL THEN overrides ELSE '[]'::jsonb END AS machine_grants
FROM iam_accounts
WHERE tenant_id = $1 AND id = $2;

-- name: IAMListAccounts :many
SELECT iam_accounts.id,username,full_name,email,status,overrides,is_admin,created_at,last_login FROM account_profile_directory iam_accounts WHERE iam_accounts.tenant_id=sqlc.arg(tenant_id) AND (sqlc.arg(all_data)::boolean OR iam_accounts.id=sqlc.arg(own_id)::bigint) ORDER BY iam_accounts.id;

-- name: IAMGetAccount :one
SELECT iam_accounts.id,username,full_name,email,status,overrides,is_admin,created_at,last_login FROM account_profile_directory iam_accounts WHERE iam_accounts.tenant_id=sqlc.arg(tenant_id) AND iam_accounts.id=sqlc.arg(id) AND (sqlc.arg(all_data)::boolean OR iam_accounts.id=sqlc.arg(own_id)::bigint);

-- name: IAMCreateAccount :one
WITH ensure_tenant AS (
    INSERT INTO iam_tenants(id) VALUES (sqlc.arg(tenant_id)::text)
    ON CONFLICT (id) DO NOTHING
), subject AS (
    INSERT INTO iam_principals(kind) VALUES ('human') RETURNING id
), login AS (
    INSERT INTO iam_login_accounts(principal_id, username) SELECT id, sqlc.arg(username)::text FROM subject RETURNING id, principal_id
), credential AS (
    INSERT INTO iam_password_credentials(login_account_id, password_hash)
    SELECT id, sqlc.arg(password_hash)::text FROM login
), membership AS (
    INSERT INTO iam_tenant_memberships(tenant_id, principal_id)
    SELECT sqlc.arg(tenant_id)::text, principal_id FROM login
)
INSERT INTO iam_accounts (tenant_id, username, status, login_account_id)
SELECT sqlc.arg(tenant_id), sqlc.arg(username), sqlc.arg(status), login.id
FROM login RETURNING iam_accounts.id;

-- name: IAMUpdateAccountLogin :one
WITH placement AS (
    UPDATE iam_accounts SET username = sqlc.arg(username), status = sqlc.arg(status)
    WHERE iam_accounts.tenant_id = sqlc.arg(tenant_id) AND iam_accounts.id = sqlc.arg(id)
        AND iam_accounts.status = sqlc.arg(expected_status)
        AND machine_principal_id IS NULL
        AND (iam_accounts.username = sqlc.arg(username) OR NOT EXISTS (
            SELECT 1 FROM iam_accounts other WHERE other.login_account_id = iam_accounts.login_account_id AND other.id <> iam_accounts.id
        ))
    RETURNING id, login_account_id, username
), renamed AS (
    UPDATE iam_login_accounts login SET username = placement.username FROM placement
    WHERE login.id = placement.login_account_id AND login.username <> placement.username
)
SELECT id FROM placement;

-- name: IAMDeleteAccount :one
DELETE FROM iam_accounts WHERE iam_accounts.tenant_id=sqlc.arg(tenant_id) AND iam_accounts.id=sqlc.arg(id) AND (sqlc.arg(all_data)::boolean OR iam_accounts.id=sqlc.arg(own_id)::bigint) RETURNING iam_accounts.id;

-- name: IAMSetPassword :one
INSERT INTO iam_password_credentials(login_account_id, password_hash)
SELECT placement.login_account_id, sqlc.arg(password_hash)::text FROM iam_accounts placement
WHERE placement.tenant_id = sqlc.arg(tenant_id) AND placement.id = sqlc.arg(id)
AND placement.login_account_id IS NOT NULL
AND NOT EXISTS (SELECT 1 FROM iam_accounts other WHERE other.login_account_id = placement.login_account_id AND other.id <> placement.id)
ON CONFLICT (login_account_id) DO UPDATE SET password_hash = EXCLUDED.password_hash, updated_at = now()
RETURNING login_account_id;

-- name: IAMMarkAdmin :exec
UPDATE iam_accounts SET is_admin=true WHERE tenant_id=$1 AND id=$2;

-- name: IAMDepartmentTree :many
WITH RECURSIVE tree AS (SELECT root.id FROM identity_department_directory root WHERE root.tenant_id=$1 AND root.id=$2 UNION SELECT d.id FROM identity_department_directory d JOIN tree t ON d.parent_id=t.id WHERE d.tenant_id=$1) SELECT id FROM tree;

-- name: IAMRoleMemberCount :one
SELECT count(DISTINCT member.principal_id) FROM (
    SELECT grant_row.principal_id
    FROM iam_context_role_grants grant_row
    JOIN iam_active_account_subjects subject ON subject.tenant_id = grant_row.tenant_id
        AND subject.principal_id = grant_row.principal_id
    WHERE grant_row.tenant_id = $1 AND grant_row.role_id = $2
      AND grant_row.revoked_at IS NULL AND grant_row.expires_at > now()
      AND (grant_row.identity_binding_id IS NULL OR EXISTS (
          SELECT 1 FROM iam_active_identity_bindings binding
          WHERE binding.tenant_id = grant_row.tenant_id AND binding.id = grant_row.identity_binding_id
            AND binding.principal_id = grant_row.principal_id
      ))
    UNION ALL
    SELECT subject.principal_id FROM iam_account_roles grant_row
    JOIN iam_roles role ON role.tenant_id = grant_row.tenant_id AND role.id = grant_row.role_id
        AND role.template_key = 'tenant-admin' AND role.status = 'active'
    JOIN iam_tenant_account_authority access ON access.tenant_id = grant_row.tenant_id
        AND access.account_id = grant_row.account_id AND access.is_admin
    JOIN iam_active_account_subjects subject ON subject.tenant_id = access.tenant_id
        AND subject.account_id = access.account_id
    WHERE grant_row.tenant_id = $1 AND grant_row.role_id = $2
) member;

-- name: IAMRootAnchors :many
SELECT member.organization_id, member.department_id
FROM identity_department_memberships member
JOIN iam_accounts account ON account.tenant_id = member.tenant_id AND account.id = member.account_id
WHERE account.tenant_id = $1 AND account.is_admin;

-- name: IAMBootstrapLock :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(tenant_id)::text,0));

-- name: IAMEnsureBootstrapTenant :exec
INSERT INTO iam_tenants (id) VALUES (sqlc.arg(tenant_id)) ON CONFLICT (id) DO NOTHING;

-- name: IAMGrantWriteLock :exec
SELECT pg_advisory_xact_lock(hashtextextended('iam:grants:' || sqlc.arg(tenant_id)::text, 0));

-- name: IAMHasTenantAccountAuthority :one
SELECT EXISTS (SELECT 1 FROM iam_tenant_account_authority access
    WHERE access.tenant_id = sqlc.arg(tenant_id) AND access.account_id = sqlc.arg(account_id))::boolean;


-- name: IAMTenantAccountAuthority :one
SELECT COALESCE(access.is_admin, false)::boolean AS is_admin
FROM iam_tenant_account_authority access
WHERE access.tenant_id = sqlc.arg(tenant_id) AND access.account_id = sqlc.arg(account_id);
