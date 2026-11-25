package identity

import (
	"context"

	"github.com/jackc/pgx/v5"

	"horizon/foundation/kernel/capability/contracts"
	"horizon/internal/repository/dbgen"
)

func riskReportDefinition() contracts.Definition {
	definition := readDefinition("identity.governance.report", "Review workspace access risks",
		"Summarize recorded authentication failures, repeated authorization denials and active custom roles with sensitive management grants.",
		"security_audit_event", "access:manage", contracts.Object(map[string]*contracts.Schema{
			"asOf": {Type: "string"}, "windowHours": contracts.Int(1, 168),
			"authenticationFailures": {Type: "integer"}, "authorizationDenials": {Type: "integer"}, "privilegedGrantTotal": {Type: "integer"},
			"repeatedDenials": contracts.Array(contracts.Object(map[string]*contracts.Schema{
				"action": {Type: "string"}, "reason": {Type: "string"}, "occurrences": {Type: "integer"}, "affectedAccounts": {Type: "integer"},
			}, "action", "reason", "occurrences", "affectedAccounts"), 0, 50),
			"privilegedGrants": contracts.Array(contracts.Object(map[string]*contracts.Schema{
				"id": {Type: "string"}, "roleName": {Type: "string"}, "username": {Type: "string"}, "expiresAt": {Type: "string"},
			}, "id", "roleName", "username", "expiresAt"), 0, 50),
			"repeatedAuthentication": contracts.Array(contracts.Object(map[string]*contracts.Schema{
				"username": {Type: "string"}, "occurrences": {Type: "integer"}, "lastAt": {Type: "string"},
			}, "username", "occurrences", "lastAt"), 0, 50),
		}, "asOf", "windowHours", "authenticationFailures", "authorizationDenials", "privilegedGrantTotal", "repeatedDenials", "privilegedGrants"))
	definition.Input = contracts.Object(map[string]*contracts.Schema{"windowHours": contracts.Int(1, 168)})
	definition.Entities = []string{"security_audit_event", "context_role_grant", "role", "tenant_membership", "identity_binding", "security_principal", "login_account", "account", "tenant"}
	definition.AllowedOrigins = []contracts.Origin{contracts.OriginHumanUI}
	definition.Tags = []string{"identity", "manual-only"}
	return definition
}

// These explicit criteria identify management assignments worth reviewing.
// They do not classify every plugin permission or override effective deny rules.
var sensitiveManagementPermissions = []string{
	"access:manage", "accounts:security", "accounts:create", "accounts:update", "accounts:delete",
	"roles:create", "roles:update", "roles:delete", "capability:approve", "applications:manage", "settings:publish",
}

func (s *Service) riskReport(ctx context.Context, input contracts.Input) (contracts.Output, error) {
	request := struct {
		WindowHours int32 `json:"windowHours"`
	}{WindowHours: 24}
	if err := contracts.Bind(input, &request); err != nil {
		return contracts.Output{}, contracts.Problem("invalid")
	}
	// All sections, the clock and authority use the same database snapshot.
	tx, err := s.repo.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	defer tx.Rollback(ctx)
	queries := dbgen.New(tx)
	actor, err := s.workspaceIAMAdmin(ctx, queries)
	if err != nil {
		return contracts.Output{}, err
	}
	summary, err := queries.IAMSecurityRiskSummary(ctx, dbgen.IAMSecurityRiskSummaryParams{TenantID: actor.TenantID, WindowHours: request.WindowHours})
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	denials, err := queries.IAMRepeatedAuthorizationDenials(ctx, dbgen.IAMRepeatedAuthorizationDenialsParams{TenantID: actor.TenantID, WindowHours: request.WindowHours})
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	grants, err := queries.IAMPrivilegedRoleGrants(ctx, dbgen.IAMPrivilegedRoleGrantsParams{TenantID: actor.TenantID, Permissions: sensitiveManagementPermissions})
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	authentication, err := queries.IAMRepeatedAuthenticationFailures(ctx, dbgen.IAMRepeatedAuthenticationFailuresParams{TenantID: actor.TenantID, WindowHours: request.WindowHours})
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	repeated := make([]map[string]any, 0, len(denials))
	for _, row := range denials {
		repeated = append(repeated, map[string]any{"action": row.Action, "reason": row.Reason, "occurrences": row.Occurrences, "affectedAccounts": row.AffectedAccounts})
	}
	privileged := make([]map[string]any, 0, len(grants))
	total := int64(0)
	for _, row := range grants {
		total = row.Total
		privileged = append(privileged, map[string]any{"id": row.ID.String(), "roleName": row.RoleName, "username": row.Username, "expiresAt": timeValue(row.ExpiresAt)})
	}
	failedLogins := make([]map[string]any, 0, len(authentication))
	for _, row := range authentication {
		failedLogins = append(failedLogins, map[string]any{"username": row.Username, "occurrences": row.Occurrences, "lastAt": timeValue(row.LastAt)})
	}
	if err := tx.Commit(ctx); err != nil {
		return contracts.Output{}, safe(err)
	}
	return contracts.Output{Data: map[string]any{
		"asOf": timeValue(summary.AsOf), "windowHours": request.WindowHours,
		"authenticationFailures": summary.AuthenticationFailures, "authorizationDenials": summary.AuthorizationDenials,
		"privilegedGrantTotal": total, "repeatedDenials": repeated, "privilegedGrants": privileged,
		"repeatedAuthentication": failedLogins,
	}}, nil
}
