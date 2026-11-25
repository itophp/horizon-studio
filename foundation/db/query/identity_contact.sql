-- name: IAMListVerifiedContacts :many
SELECT channel, destination, verified_at FROM iam_verified_contacts WHERE login_account_id = $1 ORDER BY channel;

-- name: IAMPasswordRecoveryLock :exec
SELECT pg_advisory_xact_lock(hashtextextended('iam-password-recovery:' || sqlc.arg(realm)::text || ':' || sqlc.arg(username)::text, 0));

-- name: IAMActiveLoginTenants :many
SELECT member.tenant_id FROM iam_tenant_memberships member
JOIN iam_login_accounts login ON login.principal_id = member.principal_id
JOIN iam_tenants tenant ON tenant.id = member.tenant_id AND tenant.status = 'active'
WHERE login.id = $1 AND member.status = 'active'
    AND (member.expires_at IS NULL OR member.expires_at > statement_timestamp())
ORDER BY member.tenant_id;

-- name: IAMIssueContactChallenge :execrows
INSERT INTO iam_contact_challenges(id, login_account_id, channel, destination, purpose, token_hash, session_hash,
    context_hash, sealed_token, expires_at)
SELECT sqlc.arg(id)::uuid, login.id, sqlc.arg(channel), sqlc.arg(destination), 'verify', sqlc.arg(token_hash),
    sqlc.arg(session_hash), sqlc.arg(context_hash), sqlc.arg(sealed_token), statement_timestamp() + interval '15 minutes'
FROM iam_login_accounts login WHERE login.id = sqlc.arg(login_account_id) AND login.status = 'active'
AND NOT EXISTS (SELECT 1 FROM iam_contact_challenges recent
    WHERE recent.login_account_id = login.id AND recent.purpose = 'verify'
      AND recent.created_at > statement_timestamp() - interval '1 minute');

-- name: IAMIssuePasswordReset :execrows
INSERT INTO iam_contact_challenges(id, login_account_id, channel, destination, purpose, token_hash, sealed_token, expires_at)
SELECT sqlc.arg(id)::uuid, login.id, contact.channel, contact.destination, 'password_reset', sqlc.arg(token_hash),
    sqlc.arg(sealed_token), statement_timestamp() + interval '15 minutes'
FROM iam_login_accounts login
JOIN iam_principals principal ON principal.id = login.principal_id AND principal.kind = 'human' AND principal.status = 'active'
JOIN iam_verified_contacts contact ON contact.login_account_id = login.id AND contact.channel = sqlc.arg(channel)
JOIN iam_password_credentials credential ON credential.login_account_id = login.id
WHERE login.username = sqlc.arg(username) AND login.realm = sqlc.arg(realm) AND login.status = 'active'
AND NOT EXISTS (SELECT 1 FROM iam_contact_challenges recent
    WHERE recent.login_account_id = login.id AND recent.purpose = 'password_reset'
      AND recent.created_at > statement_timestamp() - interval '1 minute');

-- name: IAMVerifyLoginContact :one
WITH proof AS (
    UPDATE iam_contact_challenges challenge SET consumed_at = statement_timestamp(), sealed_token = ''
    WHERE challenge.token_hash = sqlc.arg(token_hash) AND challenge.login_account_id = sqlc.arg(login_account_id)
      AND challenge.session_hash = sqlc.arg(session_hash) AND challenge.context_hash = sqlc.arg(context_hash)
      AND challenge.purpose = 'verify' AND challenge.consumed_at IS NULL AND challenge.expires_at > statement_timestamp()
    RETURNING challenge.login_account_id, challenge.channel, challenge.destination
)
INSERT INTO iam_verified_contacts(login_account_id, channel, destination)
SELECT proof.login_account_id, proof.channel, proof.destination FROM proof
ON CONFLICT (login_account_id, channel) DO UPDATE SET destination = excluded.destination, verified_at = statement_timestamp()
RETURNING channel;

-- name: IAMFindPasswordResetLogin :one
SELECT challenge.login_account_id FROM iam_contact_challenges challenge
JOIN iam_login_accounts login ON login.id = challenge.login_account_id AND login.status = 'active'
JOIN iam_principals principal ON principal.id = login.principal_id AND principal.status = 'active' AND principal.kind = 'human'
JOIN iam_verified_contacts contact ON contact.login_account_id = challenge.login_account_id AND contact.channel = challenge.channel
    AND contact.destination = challenge.destination AND contact.verified_at <= challenge.created_at
WHERE challenge.token_hash = sqlc.arg(token_hash) AND login.realm = sqlc.arg(realm) AND challenge.purpose = 'password_reset' AND challenge.consumed_at IS NULL
    AND challenge.expires_at > statement_timestamp();

-- name: IAMConsumePasswordReset :one
UPDATE iam_contact_challenges SET consumed_at = statement_timestamp(), sealed_token = ''
WHERE token_hash = $1 AND login_account_id = $2 AND purpose = 'password_reset' AND consumed_at IS NULL
    AND expires_at > statement_timestamp()
RETURNING id;

-- name: IAMSetRecoveredPassword :one
UPDATE iam_password_credentials SET password_hash = sqlc.arg(hash), updated_at = statement_timestamp()
WHERE login_account_id = sqlc.arg(login_id) RETURNING login_account_id;

-- name: IAMCancelPasswordResets :exec
UPDATE iam_contact_challenges SET consumed_at = statement_timestamp(), sealed_token = ''
WHERE login_account_id = $1 AND purpose = 'password_reset' AND consumed_at IS NULL;

-- name: IAMClaimContactDelivery :one
WITH item AS (
    SELECT challenge.id FROM iam_contact_challenges challenge
    JOIN iam_login_accounts login ON login.id = challenge.login_account_id AND login.status = 'active'
    JOIN iam_principals principal ON principal.id = login.principal_id
      AND (principal.status = 'active' OR (principal.status = 'pending' AND challenge.purpose = 'registration'))
    WHERE challenge.expires_at > statement_timestamp() AND challenge.consumed_at IS NULL
      AND challenge.delivered_at IS NULL AND challenge.delivery_attempts < 3 AND challenge.delivery_due_at <= statement_timestamp()
    ORDER BY challenge.delivery_due_at, challenge.id LIMIT 1 FOR UPDATE OF challenge SKIP LOCKED
)
UPDATE iam_contact_challenges challenge
SET delivery_due_at = statement_timestamp() + interval '1 minute', delivery_attempts = delivery_attempts + 1
FROM item WHERE challenge.id = item.id
RETURNING challenge.id, challenge.channel, challenge.destination, challenge.purpose, challenge.sealed_token;

-- name: IAMCompleteContactDelivery :exec
UPDATE iam_contact_challenges SET delivered_at = statement_timestamp(), sealed_token = ''
WHERE id = $1 AND delivered_at IS NULL;

-- name: IAMPruneContactChallenges :exec
DELETE FROM iam_contact_challenges WHERE id IN (
    SELECT id FROM iam_contact_challenges WHERE expires_at < statement_timestamp() - interval '1 day' LIMIT 500 FOR UPDATE SKIP LOCKED
);
