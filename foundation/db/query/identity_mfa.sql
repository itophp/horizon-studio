-- name: IAMLockActiveLogin :one
SELECT id FROM iam_login_accounts WHERE id = $1 AND status = 'active' FOR UPDATE;

-- name: IAMStartTOTPEnrollment :one
INSERT INTO iam_totp_credentials(login_account_id, sealed_secret, enrollment_context_hash, enroll_expires_at)
VALUES ($1, $2, $3, now() + interval '10 minutes')
ON CONFLICT (login_account_id) DO UPDATE SET sealed_secret = EXCLUDED.sealed_secret,
enrollment_context_hash = EXCLUDED.enrollment_context_hash, enroll_expires_at = EXCLUDED.enroll_expires_at
WHERE iam_totp_credentials.activated_at IS NULL
RETURNING enroll_expires_at;

-- name: IAMReadTOTP :one
SELECT sealed_secret, enrollment_context_hash, activated_at, (enroll_expires_at > now())::boolean AS pending_valid,
       floor(extract(epoch FROM statement_timestamp()) / 30)::bigint AS counter
FROM iam_totp_credentials WHERE login_account_id = $1;

-- name: IAMMFAStatus :one
SELECT EXISTS(SELECT 1 FROM iam_totp_credentials credential WHERE credential.login_account_id = $1 AND credential.activated_at IS NOT NULL)::boolean AS enabled,
       EXISTS(SELECT 1 FROM iam_totp_credentials credential WHERE credential.login_account_id = $1 AND credential.activated_at IS NULL AND credential.enroll_expires_at > now())::boolean AS pending,
       (SELECT count(*) FROM iam_recovery_codes recovery WHERE recovery.login_account_id = $1 AND recovery.used_at IS NULL)::bigint AS recovery_remaining;

-- name: IAMActivateTOTP :one
UPDATE iam_totp_credentials SET activated_at = now(), last_counter = sqlc.arg(counter)
WHERE login_account_id = $1 AND activated_at IS NULL AND enroll_expires_at > now()
AND sealed_secret = sqlc.arg(sealed_secret) AND enrollment_context_hash = sqlc.arg(enrollment_context_hash)
RETURNING login_account_id;

-- name: IAMUseTOTP :one
UPDATE iam_totp_credentials SET last_counter = sqlc.arg(counter)
WHERE login_account_id = $1 AND activated_at IS NOT NULL AND last_counter < sqlc.arg(counter) RETURNING login_account_id;

-- name: IAMCreateRecoveryCode :exec
INSERT INTO iam_recovery_codes(login_account_id, code_hash) VALUES ($1, $2);

-- name: IAMUseRecoveryCode :one
UPDATE iam_recovery_codes SET used_at = now() WHERE login_account_id = $1 AND code_hash = $2 AND used_at IS NULL RETURNING login_account_id;

-- name: IAMRevokeAlternativesAfterMFA :exec
WITH placements AS (
    SELECT tenant_id, id FROM iam_accounts WHERE login_account_id = sqlc.arg(login_account_id)
), sessions AS (
    DELETE FROM iam_sessions session USING placements
    WHERE session.tenant_id = placements.tenant_id AND session.account_id = placements.id AND session.token_hash <> sqlc.arg(current_token)::text
)
UPDATE mcp_tokens token SET revoked_at = now() FROM placements
WHERE token.tenant_id = placements.tenant_id AND token.account_id = placements.id AND token.revoked_at IS NULL;

-- name: IAMDeleteRecoveryCodes :exec
DELETE FROM iam_recovery_codes WHERE login_account_id = $1;

-- name: IAMDeactivateTOTP :one
DELETE FROM iam_totp_credentials
WHERE login_account_id = $1 AND activated_at IS NOT NULL
RETURNING login_account_id;

-- name: IAMStageTOTPReplacement :one
UPDATE iam_totp_credentials
SET replacement_secret = sqlc.arg(secret)::text,
    replacement_context_hash = sqlc.arg(context_hash)::text,
    replacement_expires_at = statement_timestamp() + interval '10 minutes'
WHERE login_account_id = sqlc.arg(login_id) AND activated_at IS NOT NULL
RETURNING replacement_expires_at;

-- name: IAMReadTOTPReplacement :one
SELECT replacement_secret, replacement_context_hash,
       floor(extract(epoch FROM statement_timestamp()) / 30)::bigint AS counter
FROM iam_totp_credentials
WHERE login_account_id = $1 AND activated_at IS NOT NULL AND replacement_expires_at > statement_timestamp();

-- name: IAMActivateTOTPReplacement :one
UPDATE iam_totp_credentials
SET sealed_secret = replacement_secret, last_counter = sqlc.arg(counter)::bigint, activated_at = statement_timestamp(),
    replacement_secret = NULL, replacement_expires_at = NULL, replacement_context_hash = NULL
WHERE login_account_id = sqlc.arg(login_id) AND activated_at IS NOT NULL
    AND replacement_expires_at > statement_timestamp() AND replacement_secret = sqlc.arg(secret)::text
    AND replacement_context_hash = sqlc.arg(context_hash)::text
RETURNING login_account_id;
