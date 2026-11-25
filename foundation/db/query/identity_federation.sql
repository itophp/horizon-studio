-- name: IAMStartFederation :exec
INSERT INTO iam_federation_flows(state_hash, browser_hash, provider_id, issuer, purpose, nonce_hash, sealed_verifier)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: IAMConsumeFederation :one
UPDATE iam_federation_flows SET consumed_at = clock_timestamp()
WHERE state_hash = $1 AND browser_hash = $2 AND consumed_at IS NULL AND expires_at > now()
RETURNING provider_id, issuer, purpose, nonce_hash, sealed_verifier;

-- name: IAMStartExternalProof :exec
INSERT INTO iam_external_identity_proofs(proof_hash, provider_id, issuer, subject) VALUES ($1, $2, $3, $4);

-- name: IAMConsumeExternalProof :one
UPDATE iam_external_identity_proofs SET consumed_at = now()
WHERE proof_hash = $1 AND consumed_at IS NULL AND expires_at > now() AND NOT completion AND purpose = 'link'
RETURNING provider_id, issuer, subject;

-- name: IAMStartSAMLCompletion :exec
INSERT INTO iam_external_identity_proofs(proof_hash, provider_id, issuer, subject, purpose, completion, expires_at)
VALUES ($1, $2, $3, $4, $5, true, now() + interval '2 minutes');

-- name: IAMConsumeSAMLCompletion :one
UPDATE iam_external_identity_proofs SET consumed_at = now()
WHERE proof_hash = $1 AND completion AND consumed_at IS NULL AND expires_at > now()
RETURNING provider_id, issuer, subject, purpose;

-- name: IAMConsumeFederationAssertion :one
INSERT INTO iam_federation_assertion_uses(provider_id, assertion_hash, expires_at)
VALUES ($1, $2, $3) ON CONFLICT DO NOTHING RETURNING assertion_hash;

-- name: IAMLinkExternalIdentity :one
INSERT INTO iam_external_identities(provider_id, issuer, subject, login_account_id)
VALUES ($1, $2, $3, $4)
ON CONFLICT (provider_id, issuer, subject) DO UPDATE SET revoked_at = NULL,
    linked_at = CASE WHEN iam_external_identities.revoked_at IS NOT NULL THEN statement_timestamp() ELSE iam_external_identities.linked_at END
WHERE iam_external_identities.login_account_id = EXCLUDED.login_account_id
RETURNING id;

-- name: IAMUnlinkExternalIdentity :one
UPDATE iam_external_identities SET revoked_at = now()
WHERE iam_external_identities.id = $1 AND iam_external_identities.login_account_id = $2 AND revoked_at IS NULL
AND EXISTS (SELECT 1 FROM iam_password_credentials credential WHERE credential.login_account_id = $2)
RETURNING id;

-- name: IAMRevokeExternalSessions :exec
WITH unscoped AS (DELETE FROM iam_login_sessions WHERE external_identity_id = $1)
DELETE FROM iam_sessions WHERE iam_sessions.external_identity_id = $1;

-- name: IAMListExternalIdentities :many
SELECT id, provider_id, linked_at, revoked_at FROM iam_external_identities WHERE login_account_id = $1 ORDER BY linked_at;

-- name: IAMFindUnscopedExternalLogin :one
SELECT login.id FROM iam_external_identities external
JOIN iam_login_accounts login ON login.id = external.login_account_id AND login.status = 'active'
JOIN iam_principals principal ON principal.id = login.principal_id AND principal.status = 'active' AND principal.kind = 'human'
WHERE external.provider_id = $1 AND external.issuer = $2 AND external.subject = $3 AND external.revoked_at IS NULL
    AND login.realm = sqlc.arg(realm);

-- name: IAMCreateUnscopedExternalSession :one
INSERT INTO iam_login_sessions(token_hash, login_account_id, csrf_token, device_label, expires_at, external_identity_id)
SELECT sqlc.arg(session_hash), login.id, sqlc.arg(csrf_token), sqlc.arg(device_label), clock_timestamp() + interval '15 minutes', external.id
FROM iam_external_identities external
JOIN iam_login_accounts login ON login.id = external.login_account_id AND login.status = 'active'
JOIN iam_principals principal ON principal.id = login.principal_id AND principal.status = 'active' AND principal.kind = 'human'
WHERE external.provider_id = sqlc.arg(provider_id) AND external.issuer = sqlc.arg(issuer)
    AND external.subject = sqlc.arg(subject) AND external.revoked_at IS NULL AND login.id = sqlc.arg(login_id)
FOR SHARE OF external, principal
RETURNING token_hash;

-- name: IAMRemovePasswordForExternalLogin :one
DELETE FROM iam_password_credentials credential
WHERE credential.login_account_id = sqlc.arg(login_id)
AND EXISTS (
    SELECT 1 FROM iam_sessions session JOIN iam_external_identities external ON external.id = session.external_identity_id
    WHERE session.token_hash = sqlc.arg(session_hash) AND session.csrf_token = sqlc.arg(csrf_token)
       AND session.created_at > clock_timestamp() - interval '5 minutes'
       AND session.expires_at > clock_timestamp() AND external.revoked_at IS NULL
       AND external.login_account_id = credential.login_account_id
)
RETURNING login_account_id;
