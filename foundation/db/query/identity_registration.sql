-- name: IAMRegistrationPolicy :one
SELECT tenant.registration_channel FROM iam_tenants tenant
WHERE tenant.id = $1 AND tenant.status = 'active'
FOR SHARE OF tenant;

-- name: IAMSetRegistrationPolicy :one
UPDATE iam_tenants SET registration_channel = sqlc.arg(channel)
WHERE id = sqlc.arg(tenant_id) AND status = 'active'
RETURNING id;

-- name: IAMCreateRegistrationLogin :one
WITH principal AS (
    INSERT INTO iam_principals(kind, status, display_name, creation_source, created_in_tenant)
    VALUES ('human', sqlc.arg(status)::text, sqlc.arg(display_name)::text, 'registration', sqlc.arg(tenant_id)::text)
    RETURNING id
), login AS (
    INSERT INTO iam_login_accounts(principal_id, username, realm)
    SELECT id, sqlc.arg(username)::text, sqlc.arg(realm)::text FROM principal RETURNING id
), credential AS (
    INSERT INTO iam_password_credentials(login_account_id, password_hash) SELECT id, sqlc.arg(password_hash)::text FROM login
)
SELECT id FROM login;

-- name: IAMRegisterContactChallenge :exec
WITH pending AS (
    INSERT INTO iam_pending_registrations(login_account_id, tenant_id, channel, expires_at)
    VALUES (sqlc.arg(login_id), sqlc.arg(tenant_id), sqlc.arg(channel), statement_timestamp() + interval '15 minutes')
    RETURNING login_account_id, expires_at
)
INSERT INTO iam_contact_challenges(id, login_account_id, channel, destination, purpose, token_hash, sealed_token, expires_at)
SELECT sqlc.arg(id)::uuid, pending.login_account_id, sqlc.arg(channel), sqlc.arg(destination), 'registration',
    sqlc.arg(token_hash), sqlc.arg(sealed_token), pending.expires_at FROM pending;

-- name: IAMReadPendingRegistration :one
SELECT pending.login_account_id, pending.tenant_id, pending.channel,
    challenge.destination, login.principal_id, login.username, principal.display_name, credential.password_hash
FROM iam_contact_challenges challenge
JOIN iam_pending_registrations pending ON pending.login_account_id = challenge.login_account_id
JOIN iam_login_accounts login ON login.id = pending.login_account_id AND login.status = 'active'
JOIN iam_password_credentials credential ON credential.login_account_id = login.id
JOIN iam_principals principal ON principal.id = login.principal_id AND principal.status = 'pending'
WHERE challenge.token_hash = sqlc.arg(token_hash) AND login.realm = sqlc.arg(realm) AND challenge.purpose = 'registration'
    AND challenge.consumed_at IS NULL AND challenge.expires_at > statement_timestamp()
    AND pending.expires_at > statement_timestamp()
FOR UPDATE OF pending, login;

-- name: IAMConsumeRegistrationChallenge :exec
UPDATE iam_contact_challenges SET consumed_at = statement_timestamp(), sealed_token = '' WHERE token_hash = $1;

-- name: IAMActivateRegistrationPrincipal :one
UPDATE iam_principals SET status = 'active' WHERE id = $1 AND status = 'pending' RETURNING id;

-- name: IAMInstallRegistrationPlacement :one
WITH member AS (
    INSERT INTO iam_tenant_memberships(tenant_id, principal_id)
    VALUES (sqlc.arg(tenant_id), sqlc.arg(principal_id))
    RETURNING tenant_id
)
INSERT INTO iam_accounts(tenant_id, username, login_account_id, status)
SELECT member.tenant_id, sqlc.arg(username),
    sqlc.arg(login_account_id), 'active' FROM member RETURNING id;

-- name: IAMRecordRegistrationContact :exec
INSERT INTO iam_verified_contacts(login_account_id, channel, destination)
VALUES ($1, $2, $3);

-- name: IAMDeletePendingRegistration :exec
DELETE FROM iam_pending_registrations WHERE login_account_id = $1;

-- name: IAMPrunePendingRegistrations :exec
WITH stale AS (
    SELECT pending.login_account_id, login.principal_id FROM iam_pending_registrations pending
    JOIN iam_login_accounts login ON login.id = pending.login_account_id
    JOIN iam_principals principal ON principal.id = login.principal_id AND principal.status = 'pending'
    WHERE pending.expires_at <= statement_timestamp() ORDER BY pending.expires_at LIMIT 500 FOR UPDATE OF pending SKIP LOCKED
), removed AS (
    DELETE FROM iam_login_accounts login USING stale WHERE login.id = stale.login_account_id RETURNING login.principal_id
)
DELETE FROM iam_principals principal USING removed WHERE principal.id = removed.principal_id;
