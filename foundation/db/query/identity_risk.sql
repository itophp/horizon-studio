-- name: IAMSecurityRiskSummary :one
SELECT now()::timestamptz AS as_of,
       count(*) FILTER (WHERE action='identity.login' AND outcome='denied')::bigint AS authentication_failures,
       count(*) FILTER (WHERE action<>'identity.login' AND
         (outcome='denied' OR (outcome='failed' AND reason IN ('forbidden','unauthenticated'))))::bigint AS authorization_denials
FROM iam_security_event_history
WHERE tenant_id=sqlc.arg(tenant_id)
  AND created_at >= now() - make_interval(hours => sqlc.arg(window_hours)::integer);

-- name: IAMRepeatedAuthorizationDenials :many
SELECT action, reason, count(*)::bigint AS occurrences,
       count(DISTINCT nullif(account_id, 0))::bigint AS affected_accounts
FROM iam_security_event_history
WHERE tenant_id=sqlc.arg(tenant_id) AND action<>'identity.login'
  AND created_at >= now() - make_interval(hours => sqlc.arg(window_hours)::integer)
  AND (outcome='denied' OR (outcome='failed' AND reason IN ('forbidden','unauthenticated')))
GROUP BY action, reason
HAVING count(*) >= 5
ORDER BY occurrences DESC, action, reason
LIMIT 50;

-- name: IAMRepeatedAuthenticationFailures :many
SELECT account.id AS account_id, account.username,
       count(*)::bigint AS occurrences, max(event.created_at)::timestamptz AS last_at
FROM iam_security_event_history event
JOIN iam_accounts account ON account.tenant_id = event.tenant_id AND account.id = event.account_id
WHERE event.tenant_id = sqlc.arg(tenant_id) AND event.action = 'identity.login' AND event.outcome = 'denied'
  AND event.created_at >= now() - make_interval(hours => sqlc.arg(window_hours)::integer)
GROUP BY account.id, account.username
HAVING count(*) >= 5
ORDER BY occurrences DESC, last_at DESC, account.id
LIMIT 50;

-- name: IAMPrivilegedRoleGrants :many
WITH assignments AS (
    SELECT grant_row.id, grant_row.role_id, actor.account_id, grant_row.expires_at
    FROM iam_context_role_grants grant_row
    JOIN LATERAL (
        SELECT subject.account_id FROM iam_active_account_subjects subject
        WHERE subject.tenant_id = grant_row.tenant_id AND subject.principal_id = grant_row.principal_id
        ORDER BY subject.account_id LIMIT 1
    ) actor ON true
    WHERE grant_row.tenant_id = sqlc.arg(tenant_id) AND grant_row.revoked_at IS NULL AND grant_row.expires_at > now()
      AND (grant_row.identity_binding_id IS NULL OR EXISTS (
          SELECT 1 FROM iam_active_identity_bindings binding WHERE binding.tenant_id = grant_row.tenant_id
          AND binding.id = grant_row.identity_binding_id AND binding.principal_id = grant_row.principal_id
      ))
)
SELECT assignment.id, role.name AS role_name, account.username,
       assignment.expires_at, count(*) OVER ()::bigint AS total
FROM assignments assignment
JOIN iam_roles role ON role.tenant_id = sqlc.arg(tenant_id) AND role.id = assignment.role_id
    AND role.status = 'active' AND role.template_key = 'custom'
JOIN iam_accounts account ON account.tenant_id = role.tenant_id AND account.id = assignment.account_id
WHERE EXISTS (
    SELECT 1 FROM jsonb_array_elements(role.grants) privilege
    WHERE coalesce(privilege->>'Effect', privilege->>'effect') = 'allow'
      AND coalesce(privilege->>'Permission', privilege->>'permission') = ANY(sqlc.arg(permissions)::text[])
)
ORDER BY assignment.expires_at NULLS FIRST, assignment.id
LIMIT 50;
