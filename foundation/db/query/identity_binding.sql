-- name: IAMManagedBindings :many
SELECT binding.id, binding.principal_id, binding.identity_type, binding.identity_version, binding.entity_id,
       CASE WHEN binding.status <> 'revoked' AND binding.valid_until <= statement_timestamp() THEN 'expired'
            ELSE binding.status END::text AS status,
       binding.source, binding.verification_level, binding.valid_until, binding.created_at
FROM iam_identity_bindings binding
WHERE binding.tenant_id = sqlc.arg(tenant_id)
    AND (sqlc.narg(after_id)::uuid IS NULL OR binding.id > sqlc.narg(after_id)::uuid)
ORDER BY binding.id
LIMIT 51;

-- name: IAMManageBinding :one
UPDATE iam_identity_bindings binding
SET status = sqlc.arg(next_status),
    revoked_at = CASE WHEN sqlc.arg(next_status) = 'revoked' THEN statement_timestamp() ELSE revoked_at END
WHERE binding.tenant_id = sqlc.arg(tenant_id) AND binding.id = sqlc.arg(binding_id)
    AND binding.status = ANY(sqlc.arg(previous_states)::text[])
    AND (sqlc.arg(next_status) <> 'active' OR (
        binding.principal_id <> sqlc.arg(actor_id)::uuid
        AND (binding.valid_until IS NULL OR binding.valid_until > statement_timestamp())
        AND EXISTS (
            SELECT 1 FROM iam_tenant_memberships member
            JOIN iam_principals principal ON principal.id = member.principal_id AND principal.status = 'active'
            WHERE member.tenant_id = binding.tenant_id AND member.principal_id = binding.principal_id
              AND member.status = 'active' AND (member.expires_at IS NULL OR member.expires_at > statement_timestamp())
        )
        AND NOT EXISTS (
            SELECT 1 FROM cap_outbox event WHERE event.tenant_id = binding.tenant_id
              AND event.event_type = binding.revocation_event AND event.aggregate_id = binding.entity_id::text
        )
    ))
RETURNING binding.id;

-- name: IAMExpireIdentityBindings :execrows
WITH due AS (
    SELECT binding.id FROM iam_identity_bindings binding
    WHERE binding.status IN ('active', 'pending', 'suspended') AND binding.valid_until <= statement_timestamp()
    ORDER BY binding.valid_until, binding.id LIMIT 500 FOR UPDATE SKIP LOCKED
)
UPDATE iam_identity_bindings binding SET status = 'expired' FROM due WHERE binding.id = due.id;
