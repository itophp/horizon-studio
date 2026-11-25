-- name: IAMLockUnplacedLoginForAssociation :one
SELECT login.id, login.principal_id, credential.password_hash
FROM iam_login_accounts login
JOIN iam_password_credentials credential ON credential.login_account_id = login.id
JOIN iam_principals principal ON principal.id = login.principal_id
WHERE login.username = sqlc.arg(username) AND login.realm = sqlc.arg(realm) AND login.status = 'active'
AND principal.kind = 'human' AND principal.status = 'active'
AND NOT EXISTS (SELECT 1 FROM iam_tenant_memberships member WHERE member.principal_id = principal.id)
AND NOT EXISTS (SELECT 1 FROM iam_accounts placement WHERE placement.login_account_id = login.id)
AND NOT EXISTS (SELECT 1 FROM iam_login_accounts other WHERE other.principal_id = principal.id AND other.id <> login.id)
AND NOT EXISTS (SELECT 1 FROM iam_pending_registrations pending WHERE pending.login_account_id = login.id)
FOR UPDATE OF login, principal, credential;

-- name: IAMAttachUnplacedLogin :one
UPDATE iam_login_accounts login SET principal_id = sqlc.arg(principal_id)::uuid
WHERE login.id = sqlc.arg(login_id) AND login.principal_id = sqlc.arg(original_principal_id)::uuid
AND login.status = 'active'
AND NOT EXISTS (SELECT 1 FROM iam_tenant_memberships member WHERE member.principal_id = login.principal_id)
AND NOT EXISTS (SELECT 1 FROM iam_accounts placement WHERE placement.login_account_id = login.id)
RETURNING login.id;

-- name: IAMRecordLoginAssociation :one
INSERT INTO iam_login_associations(principal_id, original_principal_id, primary_login_id, associated_login_id)
VALUES ($1, $2, $3, $4)
RETURNING id;

-- name: IAMOwnLoginAssociations :many
SELECT association.id, login.username, login.realm, association.created_at, association.revoked_at,
    EXISTS (SELECT 1 FROM iam_accounts placement WHERE placement.login_account_id = login.id) AS placed
FROM iam_login_associations association
JOIN iam_login_accounts login ON login.id = association.associated_login_id
WHERE association.primary_login_id = sqlc.arg(login_id) AND association.principal_id = sqlc.arg(principal_id)
AND (sqlc.narg(after_id)::uuid IS NULL OR association.id > sqlc.narg(after_id)::uuid)
ORDER BY association.id LIMIT 51;

-- name: IAMLockOwnLoginAssociation :one
SELECT association.associated_login_id, association.original_principal_id
FROM iam_login_associations association
WHERE association.id = sqlc.arg(id) AND association.primary_login_id = sqlc.arg(login_id)
AND association.principal_id = sqlc.arg(principal_id) AND association.revoked_at IS NULL
FOR UPDATE;

-- name: IAMDetachUnplacedLogin :one
UPDATE iam_login_accounts login SET principal_id = sqlc.arg(original_principal_id)::uuid
WHERE login.id = sqlc.arg(login_id) AND login.principal_id = sqlc.arg(principal_id)
AND EXISTS (SELECT 1 FROM iam_principals original WHERE original.id = sqlc.arg(original_principal_id) AND original.status = 'active')
AND NOT EXISTS (SELECT 1 FROM iam_accounts placement WHERE placement.login_account_id = login.id)
RETURNING login.id;

-- name: IAMMarkLoginAssociationRevoked :exec
UPDATE iam_login_associations SET revoked_at = statement_timestamp() WHERE id = $1 AND revoked_at IS NULL;

-- name: IAMClearAssociationSessions :exec
DELETE FROM iam_login_sessions WHERE login_account_id = $1;
