-- name: IAMCreateMachine :one
WITH principal AS (
    INSERT INTO iam_principals(kind, display_name) VALUES (sqlc.arg(kind), sqlc.arg(display_name)) RETURNING id
), membership AS (
    INSERT INTO iam_tenant_memberships(tenant_id, principal_id)
    SELECT sqlc.arg(tenant_id)::text, id FROM principal RETURNING principal_id
)
INSERT INTO iam_accounts(tenant_id, username, status, overrides, machine_principal_id)
SELECT sqlc.arg(tenant_id), 'machine-' || membership.principal_id::text,
    'active', sqlc.arg(grants)::jsonb, membership.principal_id FROM membership
RETURNING id;

-- name: IAMLockMachineAccount :one
SELECT placement.id FROM iam_accounts placement
JOIN iam_active_machine_subjects machine ON machine.tenant_id = placement.tenant_id AND machine.account_id = placement.id
WHERE placement.tenant_id = $1 AND placement.id = $2 FOR UPDATE OF placement;

-- name: IAMMachineSubject :one
SELECT principal_id, kind FROM iam_active_machine_subjects WHERE tenant_id = $1 AND account_id = $2;

-- name: IAMRestoreMachineCredential :one
SELECT credential.id, credential.tenant_id, credential.account_id, credential.audience, credential.capabilities,
    credential.issuer_id, machine.principal_id, machine.kind, machine.membership_id
FROM iam_machine_credentials credential
JOIN iam_active_machine_subjects machine ON machine.tenant_id = credential.tenant_id AND machine.account_id = credential.account_id
WHERE credential.token_hash = sqlc.arg(token_hash) AND credential.audience = sqlc.arg(audience)
AND credential.revoked_at IS NULL AND credential.expires_at > now();

-- name: IAMShareMachineCredential :one
SELECT id FROM iam_machine_credentials
WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > now() FOR SHARE;

-- name: IAMMachineCredentialHash :one
SELECT token_hash FROM iam_machine_credentials WHERE tenant_id = $1 AND account_id = $2
AND id = sqlc.arg(credential_id)::uuid AND revoked_at IS NULL AND expires_at > now();

-- name: IAMRevokeMachineCredentials :execrows
UPDATE iam_machine_credentials SET revoked_at = now()
WHERE tenant_id = $1 AND account_id = $2 AND revoked_at IS NULL;

-- name: IAMIssueMachineCredential :one
INSERT INTO iam_machine_credentials(tenant_id, account_id, issuer_id, token_hash, audience, capabilities, expires_at)
VALUES (sqlc.arg(tenant_id), sqlc.arg(account_id), sqlc.arg(issuer_id),
        sqlc.arg(token_hash), sqlc.arg(audience), sqlc.arg(capabilities),
        now() + make_interval(mins => sqlc.arg(minutes)::int))
RETURNING id, expires_at;

-- name: IAMIssueAgentDelegation :one
INSERT INTO iam_agent_delegations (tenant_id, agent_account_id, delegator_account_id, delegator_binding_id,
    capability, resource_id, max_risk, remaining_uses, expires_at, additional_capabilities)
VALUES (sqlc.arg(tenant_id), sqlc.arg(agent_account_id), sqlc.arg(delegator_account_id), NULLIF(sqlc.arg(binding_id)::text, '')::uuid,
    sqlc.arg(capability), sqlc.arg(resource_id), sqlc.arg(max_risk), sqlc.arg(remaining_uses),
    now() + make_interval(mins => sqlc.arg(minutes)::int), sqlc.arg(additional_capabilities)::jsonb)
RETURNING id, expires_at;

-- name: IAMActiveAgentDelegation :one
SELECT id, capability, resource_id, max_risk, remaining_uses, delegator_account_id,
    COALESCE(delegator_binding_id::text, '')::text AS binding_id, additional_capabilities
FROM iam_agent_delegations WHERE tenant_id = $1 AND agent_account_id = $2 AND id = $3
AND revoked_at IS NULL AND expires_at > now();

-- name: IAMConsumeAgentDelegation :execrows
UPDATE iam_agent_delegations SET remaining_uses = remaining_uses - 1
WHERE tenant_id = $1 AND agent_account_id = $2 AND id = $3
AND revoked_at IS NULL AND expires_at > clock_timestamp() AND remaining_uses > 0;

-- name: IAMRevokeAgentDelegation :execrows
UPDATE iam_agent_delegations SET revoked_at = now()
WHERE tenant_id = $1 AND delegator_account_id = $2 AND id = $3 AND revoked_at IS NULL
AND delegator_binding_id IS NOT DISTINCT FROM NULLIF(sqlc.arg(binding_id)::text, '')::uuid;

-- name: IAMShareActiveDelegatorBinding :one
SELECT binding.id FROM iam_identity_bindings binding
JOIN iam_active_identity_bindings active ON active.id = binding.id AND active.tenant_id = binding.tenant_id
WHERE binding.tenant_id = $1 AND binding.id = $2
FOR SHARE OF binding;

-- name: IAMListMachineCredentials :many
SELECT credential.id, credential.audience, credential.capabilities, credential.created_at, credential.expires_at, credential.revoked_at
FROM iam_machine_credentials credential
JOIN iam_accounts account ON account.tenant_id = credential.tenant_id AND account.id = credential.account_id
WHERE credential.tenant_id = sqlc.arg(tenant_id) AND credential.account_id = sqlc.arg(account_id)
    AND account.machine_principal_id IS NOT NULL
    AND (sqlc.narg(after_id)::uuid IS NULL OR credential.id > sqlc.narg(after_id)::uuid)
ORDER BY credential.id LIMIT 51;

-- name: IAMListOwnAgentDelegations :many
SELECT delegation.id, delegation.agent_account_id, delegation.capability, delegation.resource_id,
    delegation.max_risk, delegation.remaining_uses, delegation.created_at, delegation.expires_at, delegation.revoked_at, delegation.additional_capabilities
FROM iam_agent_delegations delegation
WHERE delegation.tenant_id = sqlc.arg(tenant_id) AND delegation.delegator_account_id = sqlc.arg(account_id)
    AND delegation.delegator_binding_id IS NOT DISTINCT FROM sqlc.narg(binding_id)::uuid
    AND (sqlc.narg(after_id)::uuid IS NULL OR delegation.id > sqlc.narg(after_id)::uuid)
ORDER BY delegation.id LIMIT 51;
