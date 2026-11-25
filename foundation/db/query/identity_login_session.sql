-- name: IAMUnscopedPasswordLogin :one
SELECT login.id, credential.password_hash
FROM iam_login_accounts login
JOIN iam_principals principal ON principal.id = login.principal_id AND principal.kind = 'human' AND principal.status = 'active'
JOIN iam_password_credentials credential ON credential.login_account_id = login.id
WHERE login.username = sqlc.arg(username) AND login.realm = sqlc.arg(realm) AND login.status = 'active';

-- name: IAMCreateUnscopedLoginSession :one
INSERT INTO iam_login_sessions(token_hash, login_account_id, csrf_token, device_label, expires_at)
SELECT sqlc.arg(token_hash), login.id, sqlc.arg(csrf_token), sqlc.arg(device_label), clock_timestamp() + interval '15 minutes'
FROM iam_login_accounts login
JOIN iam_principals principal ON principal.id = login.principal_id AND principal.kind = 'human' AND principal.status = 'active'
JOIN iam_password_credentials credential ON credential.login_account_id = login.id
WHERE login.id = sqlc.arg(login_account_id) AND login.status = 'active' AND credential.password_hash = sqlc.arg(password_hash)
FOR SHARE OF principal, credential
RETURNING token_hash;

-- name: IAMGetUnscopedLoginSession :one
SELECT session.csrf_token, session.login_account_id, login.principal_id
FROM iam_login_sessions session
JOIN iam_login_accounts login ON login.id = session.login_account_id AND login.status = 'active'
JOIN iam_principals principal ON principal.id = login.principal_id AND principal.kind = 'human' AND principal.status = 'active'
WHERE session.token_hash = $1 AND session.expires_at > clock_timestamp()
AND (session.external_identity_id IS NULL OR EXISTS (
  SELECT 1 FROM iam_external_identities external WHERE external.id = session.external_identity_id
    AND external.login_account_id = session.login_account_id AND external.revoked_at IS NULL
));

-- name: IAMLockUnscopedLoginSession :one
SELECT session.login_account_id
FROM iam_login_sessions session
JOIN iam_login_accounts login ON login.id = session.login_account_id AND login.status = 'active'
JOIN iam_principals principal ON principal.id = login.principal_id AND principal.kind = 'human' AND principal.status = 'active'
WHERE session.token_hash = sqlc.arg(token_hash) AND session.csrf_token = sqlc.arg(csrf_token)
  AND session.expires_at > clock_timestamp()
AND (session.external_identity_id IS NULL OR EXISTS (
  SELECT 1 FROM iam_external_identities external WHERE external.id = session.external_identity_id
    AND external.login_account_id = session.login_account_id AND external.revoked_at IS NULL
))
FOR UPDATE OF session;

-- name: IAMDeleteUnscopedLoginSession :exec
DELETE FROM iam_login_sessions WHERE token_hash = $1;

-- name: IAMPromoteLoginSession :one
WITH accepted AS (
    DELETE FROM iam_login_sessions session
    USING iam_active_account_subjects target
    WHERE session.token_hash = sqlc.arg(token_hash) AND session.csrf_token = sqlc.arg(expected_csrf)
        AND session.expires_at > clock_timestamp()
        AND (session.external_identity_id IS NULL OR EXISTS (
  SELECT 1 FROM iam_external_identities external WHERE external.id = session.external_identity_id
    AND external.login_account_id = session.login_account_id AND external.revoked_at IS NULL
))
        AND target.login_account_id = session.login_account_id AND target.tenant_id = sqlc.arg(tenant_id)
        AND target.membership_id = sqlc.arg(membership_id)
        AND (sqlc.narg(binding_id)::uuid IS NOT NULL OR EXISTS (
            SELECT 1 FROM iam_tenant_account_authority access
            WHERE access.tenant_id = target.tenant_id AND access.account_id = target.account_id
        ))
        AND (sqlc.narg(binding_id)::uuid IS NULL OR EXISTS (
            SELECT 1 FROM iam_active_identity_bindings binding WHERE binding.id = sqlc.narg(binding_id)
              AND binding.tenant_id = target.tenant_id AND binding.principal_id = target.principal_id
        ))
    RETURNING session.token_hash, session.device_label, session.login_account_id, session.external_identity_id
)
INSERT INTO iam_sessions(token_hash, tenant_id, account_id, csrf_token, device_label, expires_at, identity_binding_id, external_identity_id)
SELECT accepted.token_hash, target.tenant_id, target.account_id, sqlc.arg(new_csrf),
    accepted.device_label, clock_timestamp() + make_interval(mins => sqlc.arg(session_minutes)::int), sqlc.narg(binding_id), accepted.external_identity_id
FROM accepted JOIN iam_active_account_subjects target ON target.login_account_id = accepted.login_account_id
    AND target.tenant_id = sqlc.arg(tenant_id)
    AND target.membership_id = sqlc.arg(membership_id)
RETURNING token_hash;

-- name: IAMPruneUnscopedLoginSessions :exec
DELETE FROM iam_login_sessions WHERE token_hash IN (
    SELECT token_hash FROM iam_login_sessions WHERE expires_at <= clock_timestamp()
    ORDER BY expires_at LIMIT 500 FOR UPDATE SKIP LOCKED
);
