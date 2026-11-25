-- name: SecurityAuditAppend :exec
INSERT INTO iam_security_events(tenant_id, account_id, binding_id, action, outcome, reason, correlation_id, resource_type, resource_id, authority)
VALUES (sqlc.arg(tenant_id), sqlc.arg(account_id), NULLIF(sqlc.arg(binding)::text, '')::uuid,
    sqlc.arg(action), sqlc.arg(outcome), sqlc.arg(reason), sqlc.arg(correlation_id), sqlc.arg(resource_type), sqlc.arg(resource_id), sqlc.narg(authority));

-- name: SecurityAuditSetCorrelation :exec
SELECT set_config('horizon.request_id', sqlc.arg(request_id)::text, true);

-- name: SecurityAuditLock :exec
LOCK TABLE iam_security_events IN SHARE ROW EXCLUSIVE MODE;

-- name: SecurityAuditLastCheckpoint :one
SELECT last_id, previous_id, previous_digest, digest, signature, created_at, key_id FROM iam_security_checkpoints ORDER BY last_id DESC LIMIT 1;

-- name: SecurityAuditCheckpoint :one
SELECT last_id, previous_id, previous_digest, digest, signature, created_at, key_id FROM iam_security_checkpoints WHERE last_id = $1;

-- name: SecurityAuditSaveCheckpoint :exec
INSERT INTO iam_security_checkpoints(last_id, previous_id, previous_digest, digest, signature, key_id) VALUES ($1, $2, $3, $4, $5, $6);

-- name: SecurityAuditBatch :many
SELECT id, tenant_id, account_id, binding_id, action, outcome, reason, correlation_id, resource_type, resource_id, created_at, authority
FROM iam_security_event_history WHERE id > sqlc.arg(first_id) AND id <= sqlc.arg(last_id)
ORDER BY id LIMIT sqlc.arg(max_rows);

-- name: SecurityAuditList :many
SELECT id, account_id, action, outcome, reason, correlation_id, resource_type, resource_id, created_at,
    coalesce(binding_id::text, '')::text AS actual_binding_id,
    coalesce(authority->>'principalId', '')::text AS principal_id,
    coalesce(authority->>'loginAccountId', '')::text AS login_account_id,
    coalesce(authority->>'origin', '')::text AS origin,
    coalesce(authority->>'capabilityVersion', '')::text AS capability_version,
    coalesce(authority->>'machineCredentialId', '')::text AS machine_credential_id,
    coalesce(authority->>'delegationId', '')::text AS delegation_id
FROM iam_security_event_history WHERE tenant_id = sqlc.arg(tenant_id)
AND (sqlc.arg(all_events)::boolean OR (account_id = sqlc.arg(account_id)
  AND binding_id IS NOT DISTINCT FROM NULLIF(sqlc.arg(binding)::text, '')::uuid))
AND id < sqlc.arg(before_id) AND id > sqlc.arg(after_id)
AND (sqlc.narg(from_at)::timestamptz IS NULL OR created_at >= sqlc.narg(from_at)::timestamptz)
AND (sqlc.narg(until_at)::timestamptz IS NULL OR created_at < sqlc.narg(until_at)::timestamptz)
AND (sqlc.narg(subject_account_id)::bigint IS NULL OR account_id = sqlc.narg(subject_account_id)::bigint)
AND (sqlc.arg(filter_action)::text = '' OR action = sqlc.arg(filter_action))
AND (sqlc.arg(filter_outcome)::text = '' OR outcome = sqlc.arg(filter_outcome))
AND (sqlc.arg(filter_resource_id)::text = '' OR resource_id = sqlc.arg(filter_resource_id))
AND (sqlc.arg(filter_resource_type)::text = '' OR resource_type = sqlc.arg(filter_resource_type))
AND (sqlc.arg(filter_correlation_id)::text = '' OR correlation_id = sqlc.arg(filter_correlation_id))
AND (sqlc.arg(filter_principal_id)::text = '' OR authority->>'principalId' = sqlc.arg(filter_principal_id))
AND (sqlc.arg(filter_login_account_id)::text = '' OR authority->>'loginAccountId' = sqlc.arg(filter_login_account_id))
AND (sqlc.arg(filter_binding_id)::text = '' OR binding_id::text = sqlc.arg(filter_binding_id))
AND (sqlc.arg(filter_origin)::text = '' OR authority->>'origin' = sqlc.arg(filter_origin))
AND (sqlc.arg(filter_delegation_id)::text = '' OR authority->>'delegationId' = sqlc.arg(filter_delegation_id))
ORDER BY id DESC LIMIT sqlc.arg(max_rows);

-- name: SecurityAuditArchivableCheckpoint :one
SELECT checkpoint.last_id FROM iam_security_checkpoints checkpoint
WHERE checkpoint.created_at < sqlc.arg(cutoff)::timestamptz
AND EXISTS (SELECT 1 FROM iam_security_events event WHERE event.id > checkpoint.previous_id AND event.id <= checkpoint.last_id)
ORDER BY checkpoint.last_id LIMIT 1;

-- name: SecurityAuditCopyCheckpointToArchive :exec
INSERT INTO iam_security_event_archive (id, tenant_id, account_id, binding_id, action, outcome, reason,
    correlation_id, resource_type, resource_id, created_at, authority)
SELECT event.id, event.tenant_id, event.account_id, event.binding_id, event.action, event.outcome, event.reason,
    event.correlation_id, event.resource_type, event.resource_id, event.created_at, event.authority
FROM iam_security_events event WHERE event.id > sqlc.arg(first_id) AND event.id <= sqlc.arg(last_id);

-- name: SecurityAuditMoveArchivedCheckpoint :execrows
DELETE FROM iam_security_events WHERE id > sqlc.arg(first_id) AND id <= sqlc.arg(last_id);

-- name: SecurityAuditExpiredArchiveCheckpoint :one
SELECT checkpoint.* FROM iam_security_checkpoints checkpoint
WHERE checkpoint.created_at < sqlc.arg(cutoff)::timestamptz
AND EXISTS (SELECT 1 FROM iam_security_event_archive a WHERE a.id = checkpoint.last_id)
AND NOT EXISTS (SELECT 1 FROM iam_security_events e WHERE e.id > checkpoint.previous_id AND e.id <= checkpoint.last_id)
AND NOT EXISTS (SELECT 1 FROM iam_security_event_archive a WHERE a.id > checkpoint.previous_id AND a.id <= checkpoint.last_id
    AND a.created_at >= sqlc.arg(cutoff))
ORDER BY checkpoint.last_id LIMIT 1;

-- name: SecurityAuditRetentionReceipt :one
SELECT checkpoint_id, cutoff, signature, key_id FROM iam_security_retention_receipts WHERE checkpoint_id = $1;

-- name: SecurityAuditSaveRetentionReceipt :exec
INSERT INTO iam_security_retention_receipts(checkpoint_id, cutoff, signature, key_id) VALUES ($1, $2, $3, $4);

-- name: SecurityAuditDeleteExpiredArchive :exec
DELETE FROM iam_security_event_archive WHERE id > sqlc.arg(first_id) AND id <= sqlc.arg(last_id);
