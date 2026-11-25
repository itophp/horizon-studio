-- name: IAMDisabledResources :many
SELECT resource FROM iam_disabled_resources WHERE tenant_id = $1 ORDER BY resource;

-- name: IAMResourceEnabled :one
SELECT NOT EXISTS (SELECT 1 FROM iam_disabled_resources WHERE tenant_id = $1 AND resource = $2)::boolean;

-- name: IAMResourceReadLock :exec
SELECT pg_advisory_xact_lock_shared(hashtextextended('iam-resource:' || sqlc.arg(tenant_id)::text || ':' || sqlc.arg(resource)::text, 0));

-- name: IAMResourceWriteLock :exec
SELECT pg_advisory_xact_lock(hashtextextended('iam-resource:' || sqlc.arg(tenant_id)::text || ':' || sqlc.arg(resource)::text, 0));

-- name: IAMDisableResource :execrows
INSERT INTO iam_disabled_resources(tenant_id, resource) VALUES ($1, $2) ON CONFLICT DO NOTHING;

-- name: IAMEnableResource :execrows
DELETE FROM iam_disabled_resources WHERE tenant_id = $1 AND resource = $2;

-- name: IAMAdvanceResourceRevision :exec
UPDATE iam_tenants SET resource_revision = resource_revision + 1 WHERE id = $1;

-- name: IAMAuthorizationVersions :one
SELECT COALESCE(head.version_id, 0)::bigint AS policy_version, tenant.resource_revision
FROM iam_tenants tenant LEFT JOIN iam_policy_heads head ON head.tenant_id = tenant.id WHERE tenant.id = $1;
