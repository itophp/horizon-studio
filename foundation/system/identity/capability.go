package identity

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	genidentity "horizon/foundation/gen/identity"
	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/capability/registry"
	"horizon/internal/repository/dbgen"
)

// Identity administration is a capability domain like any other: the console
// button, an agent tool and an external adapter all pass through the gateway, so
// a grant change is authorized, audited and announced exactly once.
const identityVersion = "1.0.0"

// Entries is the identity administration catalog.
func (s *Service) Entries() []registry.Entry {
	entries := []registry.Entry{
		{Definition: analyticsDefinition(), Handler: contracts.Handler{Execute: projected(s.analyticsRead)}},
		{Definition: rolesReadDefinition(), Handler: contracts.Handler{Execute: projected(s.rolesRead)}},
		{Definition: accountsReadDefinition(), Handler: contracts.Handler{Execute: projected(s.accountsRead)}},
		{Definition: membershipInvitationDefinition(), Handler: contracts.Handler{ExecuteTx: s.createMembershipInvitation}},
		{Definition: membershipAcceptanceDefinition(), Handler: contracts.Handler{ExecuteTx: s.acceptMembershipInvitation}},
		{Definition: tenantMembershipRevocationDefinition(), Handler: contracts.Handler{ExecuteTx: s.revokeTenantMembership}},
		{Definition: tenantMembershipListDefinition(), Handler: contracts.Handler{Execute: s.tenantMembers}},
		{Definition: riskReportDefinition(), Handler: contracts.Handler{Execute: s.riskReport}},
		{Definition: identityBindingVerifyDefinition(), Handler: contracts.Handler{Execute: s.verifyIdentityBinding}},
		{Definition: identityBindingCreateDefinition(), Handler: contracts.Handler{ExecuteTx: s.createIdentityBinding}},
		{Definition: identityBindingRevokeDefinition(), Handler: contracts.Handler{ExecuteTx: s.revokeIdentityBinding}},
		{Definition: deviceSessionsDefinition(), Handler: contracts.Handler{Execute: s.deviceSessions}},
		{Definition: deviceSessionReauthenticationDefinition(), Handler: contracts.Handler{Execute: s.reauthenticateDeviceSession}},
		{Definition: deviceSessionRevocationDefinition(), Handler: contracts.Handler{ExecuteTx: s.revokeDeviceSession}},
		{Definition: loginStatusProofDefinition(), Handler: contracts.Handler{Execute: s.loginStatusProof}},
		{Definition: loginStatusDefinition("freeze", "frozen"), Handler: contracts.Handler{ExecuteTx: s.changeLoginStatus("freeze", "active", "frozen")}},
		{Definition: loginStatusDefinition("restore", "active"), Handler: contracts.Handler{ExecuteTx: s.changeLoginStatus("restore", "frozen", "active")}},
		{Definition: mfaEnrollDefinition(), Handler: contracts.Handler{Execute: s.enrollTOTP}},
		{Definition: mfaStatusDefinition(), Handler: contracts.Handler{Execute: s.mfaStatus}},
		{Definition: mfaActivateDefinition(), Handler: contracts.Handler{ExecuteTx: s.activateTOTP}},
		{Definition: roleCreateDefinition(), Handler: contracts.Handler{ExecuteTx: s.grantWrite((*Service).createRole)}},
		{Definition: roleUpdateDefinition(), Handler: contracts.Handler{ExecuteTx: s.grantWrite((*Service).updateRole)}},
		{Definition: roleDeleteDefinition(), Handler: contracts.Handler{Execute: projected(s.deleteRole)}},
		{Definition: contextRoleGrantDefinition(), Handler: contracts.Handler{ExecuteTx: s.grantWrite((*Service).assignContextRole)}},
		{Definition: contextRoleRevokeDefinition(), Handler: contracts.Handler{ExecuteTx: s.grantWrite((*Service).revokeContextRole)}},
		{Definition: contextRolesListDefinition(), Handler: contracts.Handler{Execute: s.contextRoles}},
	}
	entries = append(entries, s.policyEntries()...)
	entries = append(entries, s.principalEntries()...)
	entries = append(entries, s.tenantEntries()...)
	entries = append(entries, s.registrationPolicyEntry(), s.registrationPolicyReadEntry())
	entries = append(entries, s.mfaManagementEntries()...)
	entries = append(entries, s.mfaReplacementEntries()...)
	entries = append(entries, s.contactEntries()...)
	entries = append(entries, s.bindingManagementEntries()...)
	entries = append(entries, s.accessReviewEntries()...)
	entries = append(entries, s.membershipManagementEntries()...)
	entries = append(entries, s.securityAuditEntries()...)
	entries = append(entries, s.resourceEntries()...)
	entries = append(entries, s.roleReviewEntries()...)
	entries = append(entries, s.agentDelegationEntries()...)
	entries = append(entries, s.machineDirectoryEntries()...)
	entries = append(entries, s.externalIdentityEntries()...)
	entries = append(entries, s.externalReviewEntries()...)
	entries = append(entries, s.loginAssociationEntries()...)
	entries = append(entries, s.loginMergeEntries()...)
	return append(entries, s.machineEntries()...)
}

// AdministrationEntries supplies IAM-backed account placement mutations to a deployment's administration application.
func (s *Service) AdministrationEntries() []registry.Entry {
	return []registry.Entry{
		{Definition: accountCreateDefinition(), Handler: contracts.Handler{ExecuteTx: s.grantWrite((*Service).createAccount)}},
		{Definition: accountUpdateDefinition(), Handler: contracts.Handler{ExecuteTx: s.grantWrite((*Service).updateAccount)}},
		{Definition: accountDeleteDefinition(), Handler: contracts.Handler{Execute: projected(s.deleteAccount)}},
	}
}

// AuthorizedWrite serializes security-sensitive mutations and supplies freshly
// verified authority bound to the transaction used by the caller.
func (s *Service) AuthorizedWrite(execute func(*Service, context.Context, pgx.Tx, contracts.Input) (contracts.Output, error)) contracts.TxExecutor {
	return func(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
		actor, err := Current(ctx)
		if err != nil {
			return contracts.Output{}, err
		}
		queries := dbgen.New(tx)
		if err = queries.IAMGrantWriteLock(ctx, actor.TenantID); err != nil {
			return contracts.Output{}, safe(err)
		}
		bound := *s
		bound.repo = &Repository{q: queries}
		var fresh *Principal
		if actor.TokenHash != "" {
			fresh, err = bound.PrincipalForSessionInvocation(ctx, actor, len(actor.TemporaryScopes) > 0)
		} else {
			fresh, err = bound.RefreshTaskPrincipal(ctx, actor)
		}
		if err != nil {
			return contracts.Output{}, err
		}
		out, err := execute(&bound, WithPrincipal(ctx, fresh), tx, input)
		if err == nil {
			out.Data = CapabilityData(out.Data)
		}
		return out, err
	}
}

func (s *Service) grantWrite(execute func(*Service, context.Context, contracts.Input) (contracts.Output, error)) contracts.TxExecutor {
	return s.AuthorizedWrite(func(bound *Service, ctx context.Context, _ pgx.Tx, input contracts.Input) (contracts.Output, error) {
		return execute(bound, ctx, input)
	})
}

// projected rewrites the Go field names of a snapshot record to the camelCase
// JSON the console and the old REST projection used. The identity record types
// come from the session snapshot contract, which has no JSON tags.
func projected(execute contracts.Executor) contracts.Executor {
	return func(ctx context.Context, in contracts.Input) (contracts.Output, error) {
		out, err := execute(ctx, in)
		if err != nil {
			return out, err
		}
		out.Data = CapabilityData(out.Data)
		return out, nil
	}
}

// CapabilityData projects service records to their public camel-case property names.
func CapabilityData(value any) any {
	raw, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var decoded any
	if err = json.Unmarshal(raw, &decoded); err != nil {
		return value
	}
	return camelize(decoded)
}

func camelize(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[lowerCamel(key)] = camelize(item)
		}
		return out
	case []any:
		for index := range typed {
			typed[index] = camelize(typed[index])
		}
		return typed
	}
	return value
}

// lowerCamel turns a Go field name into the JSON name Goa used: ID becomes id,
// OrganizationID becomes organizationId, RoleIds stays roleIds.
func lowerCamel(name string) string {
	parts := splitWords(name)
	if len(parts) == 0 {
		return name
	}
	out := strings.ToLower(parts[0])
	for _, part := range parts[1:] {
		if part == strings.ToUpper(part) {
			out += part[:1] + strings.ToLower(part[1:])
			continue
		}
		out += strings.ToUpper(part[:1]) + part[1:]
	}
	return out
}

// splitWords splits a Go identifier into words, keeping acronyms together.
func splitWords(name string) []string {
	words := []string{}
	current := []rune{}
	flush := func() {
		if len(current) > 0 {
			words = append(words, string(current))
			current = nil
		}
	}
	runes := []rune(name)
	for index, r := range runes {
		if r < 'A' || r > 'Z' {
			current = append(current, r)
			continue
		}
		previousLower := index > 0 && runes[index-1] >= 'a' && runes[index-1] <= 'z'
		nextLower := index+1 < len(runes) && runes[index+1] >= 'a' && runes[index+1] <= 'z'
		if len(current) > 0 && (previousLower || nextLower) {
			flush()
		}
		current = append(current, r)
	}
	flush()
	return words
}

func identityWrite(name, title, description, entity, permission string, events []string, input *contracts.Schema, loggable ...string) contracts.Definition {
	risk := contracts.RiskHigh
	approval := contracts.ApprovalConfirm
	return contracts.Definition{Layer: contracts.LayerAtomic,
		Name: name, Version: identityVersion, Title: title, Description: description,
		Domain: "identity", Entity: entity, Permission: permission,
		Risk: risk, Approval: approval,
		Idempotency:   contracts.Idempotency{Required: true, Scope: "tenant+operation+key"},
		SuccessEvents: events, Executor: "go:" + name, Tags: []string{"identity", "write"},
		Input: input, Output: contracts.Object(map[string]*contracts.Schema{"id": {Type: "integer"}}),
		Loggable: loggable,
	}
}

func rolesReadDefinition() contracts.Definition {
	return readDefinition("identity.roles.read", "Read roles",
		"Read authorized IAM roles and the permission catalog.",
		"role", "roles:read", contracts.Object(map[string]*contracts.Schema{
			"items": {Type: "array", Items: contracts.Object(map[string]*contracts.Schema{
				"id": {Type: "integer"}, "name": {Type: "string"}, "code": {Type: "string"},
				"description": {Type: "string"}, "status": {Type: "string"}, "memberCount": {Type: "integer"},
				"createdAt": {Type: "string"}, "grants": {Type: "array", Items: contracts.FreeObject()},
			}, "id", "name", "code", "status", "grants")},
			"total":   {Type: "integer"},
			"catalog": {Type: "array", Items: contracts.FreeObject()},
		}, "items", "total", "catalog"))
}

func readDefinition(name, title, description, entity, permission string, output *contracts.Schema) contracts.Definition {
	return contracts.Definition{Layer: contracts.LayerAtomic,
		Name: name, Version: identityVersion, Title: title, Description: description,
		Domain: "identity", Entity: entity, Entities: []string{entity},
		Permission: permission, Risk: contracts.RiskLow, Approval: contracts.ApprovalAuto,
		ReadOnly: true, TransientResult: true,
		Executor: "go:" + name, Tags: []string{"identity", "read"},
		Input:  contracts.Object(map[string]*contracts.Schema{}),
		Output: output,
	}
}

func projectFields(item any, fields []string) any {
	record, ok := item.(map[string]any)
	if !ok {
		return nil
	}
	row := map[string]any{}
	for _, field := range fields {
		if value, exists := record[field]; exists {
			row[field] = value
		}
	}
	return row
}

func accountsReadDefinition() contracts.Definition {
	definition := readDefinition("identity.accounts.read", "Read accounts",
		"Read account identifiers and security status within the active tenant or the current organization context.",
		"account", "accounts:read", contracts.Object(map[string]*contracts.Schema{
			"items": {Type: "array", Items: contracts.Object(map[string]*contracts.Schema{
				"id": {Type: "integer"}, "username": {Type: "string"}, "status": {Type: "string"},
			}, "id", "username", "status")},
			"total": {Type: "integer"},
		}, "items", "total"))
	definition.Tags = append(definition.Tags, contracts.OrganizationScopedTag)
	return definition
}

func (s *Service) rolesRead(ctx context.Context, _ contracts.Input) (contracts.Output, error) {
	p, err := Current(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	roles, err := s.repo.roles(ctx, tenantScope(p))
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	return contracts.Output{
		Data:    map[string]any{"items": roles, "total": len(roles), "catalog": s.permissionCatalog()},
		Summary: fmt.Sprintf("roles=%d", len(roles)),
	}, nil
}

func (s *Service) accountsRead(ctx context.Context, _ contracts.Input) (contracts.Output, error) {
	p, err := Current(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	scope, err := s.scope(ctx, p, "accounts:read")
	if err != nil {
		return contracts.Output{}, err
	}
	if scope.ActingOrganizationID != 0 {
		scope.All, scope.OwnID = false, p.Account.ID
	}
	rows, err := s.repo.q.IAMListAuthorityAccounts(ctx, dbgen.IAMListAuthorityAccountsParams{
		TenantID: scope.TenantID, AllData: scope.All, OwnID: scope.OwnID,
	})
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, map[string]any{"id": row.ID, "username": row.Username, "status": row.Status})
	}
	return contracts.Output{
		Data: map[string]any{"items": items, "total": len(items)}, Summary: fmt.Sprintf("accounts=%d", len(items)),
	}, nil
}

func grantSchema() *contracts.Schema {
	return contracts.Object(map[string]*contracts.Schema{
		"permission": contracts.Str(1, 80),
		"effect":     contracts.Enum("allow"),
		"scope":      contracts.Enum("ALL", "OWN"),
	}, "permission", "effect", "scope")
}

func accountSchema(update bool) *contracts.Schema {
	fields := map[string]*contracts.Schema{
		"username": contracts.Str(3, 80), "fullName": contracts.Str(1, 100),
		"email":    contracts.Str(0, 160),
		"password": contracts.Str(0, 128),
	}
	required := []string{"username", "fullName"}
	if update {
		fields["id"] = contracts.Int(1, 1<<53-1)
		fields["status"] = contracts.Enum("active", "inactive")
		required = append(required, "id", "status")
	} else {
		fields["password"] = contracts.Str(12, 128)
		fields["initialStatus"] = contracts.Enum("active", "inactive")
		required = append(required, "password", "initialStatus")
	}
	return contracts.Object(fields, required...)
}

func roleSchema(requireID bool) *contracts.Schema {
	properties := map[string]*contracts.Schema{
		"name": contracts.Str(1, 100), "code": contracts.Str(2, 40),
		"description": contracts.Str(0, 500), "status": contracts.Enum("active", "inactive"),
		"grants": contracts.Array(grantSchema(), 0, 100),
	}
	required := []string{"name", "code", "description", "status", "grants"}
	if requireID {
		properties["id"] = contracts.Int(1, 1<<40)
		required = append(required, "id")
	}
	return contracts.Object(properties, required...)
}

func deleteSchema() *contracts.Schema {
	return contracts.Object(map[string]*contracts.Schema{"id": contracts.Int(1, 1<<40)}, "id")
}

func accountCreateDefinition() contracts.Definition {
	definition := identityWrite("identity.account.create", "Create account", "Create a login account and tenant membership. Assign authorization separately.",
		"account", "accounts:create", []string{"identity.account.created"}, accountSchema(false), "username", "fullName")
	definition.Entities = []string{"account", "security_principal", "login_account", "password_credential", "tenant_membership"}
	return definition
}

func accountUpdateDefinition() contracts.Definition {
	return identityWrite("identity.account.update", "Update account", "Update the account profile and sign-in status.",
		"account", "accounts:update", []string{"identity.account.updated"}, accountSchema(true), "id", "username", "fullName", "status")
}

func accountDeleteDefinition() contracts.Definition {
	return identityWrite("identity.account.delete", "Delete account", "Delete one account. High risk: automation must obtain human confirmation.",
		"account", "accounts:delete", []string{"identity.account.deleted"}, deleteSchema(), "id")
}

func roleCreateDefinition() contracts.Definition {
	return identityWrite("identity.role.create", "Create role", "Create one role and its permission grants.",
		"role", "roles:create", []string{"identity.role.created"}, roleSchema(false), "name", "code")
}

func roleUpdateDefinition() contracts.Definition {
	schema := roleSchema(true)
	schema.Properties["revision"] = contracts.Str(64, 64)
	return identityWrite("identity.role.update", "Update role", "Replace the grants of one role. High risk: the change moves real authority.",
		"role", "roles:update", []string{"identity.role.updated"}, schema, "id", "name", "code")
}

func roleDeleteDefinition() contracts.Definition {
	return identityWrite("identity.role.delete", "Delete role", "Delete one role that no account still holds.",
		"role", "roles:delete", []string{"identity.role.deleted"}, deleteSchema(), "id")
}

func (s *Service) createAccount(ctx context.Context, in contracts.Input) (contracts.Output, error) {
	var payload struct {
		AccountInput
		InitialStatus string `json:"initialStatus"`
	}
	if err := contracts.Bind(in, &payload); err != nil {
		return contracts.Output{}, err
	}
	payload.Status = payload.InitialStatus
	out, err := s.CreateAccount(ctx, &payload.AccountInput)
	if err != nil {
		return contracts.Output{}, err
	}
	return contracts.Output{Data: out, Summary: fmt.Sprintf("account=%d", out.ID), Events: []contracts.Event{{
		Type: "identity.account.created", AggregateType: "account", AggregateID: fmt.Sprintf("%d", out.ID),
		Payload: map[string]any{"accountId": out.ID, "username": out.Username},
	}}}, nil
}

func (s *Service) updateAccount(ctx context.Context, in contracts.Input) (contracts.Output, error) {
	payload := AccountInput{}
	if err := contracts.Bind(in, &payload); err != nil {
		return contracts.Output{}, err
	}
	out, err := s.UpdateAccount(ctx, &payload)
	if err != nil {
		return contracts.Output{}, err
	}
	return contracts.Output{Data: out, Summary: fmt.Sprintf("account=%d", out.ID), Events: []contracts.Event{{
		Type: "identity.account.updated", AggregateType: "account", AggregateID: fmt.Sprintf("%d", out.ID),
		Payload: map[string]any{"accountId": out.ID, "username": out.Username, "status": out.Status},
	}}}, nil
}

func (s *Service) deleteAccount(ctx context.Context, in contracts.Input) (contracts.Output, error) {
	payload := DeleteInput{}
	if err := contracts.Bind(in, &payload); err != nil {
		return contracts.Output{}, err
	}
	if err := s.DeleteAccount(ctx, &payload); err != nil {
		return contracts.Output{}, err
	}
	return contracts.Output{Data: map[string]any{"id": payload.ID}, Summary: fmt.Sprintf("account=%d", payload.ID), Events: []contracts.Event{{
		Type: "identity.account.deleted", AggregateType: "account", AggregateID: fmt.Sprintf("%d", payload.ID),
		Payload: map[string]any{"accountId": payload.ID},
	}}}, nil
}

func (s *Service) createRole(ctx context.Context, in contracts.Input) (contracts.Output, error) {
	payload := RoleInput{}
	if err := contracts.Bind(in, &payload); err != nil {
		return contracts.Output{}, err
	}
	out, err := s.CreateRole(ctx, &payload)
	if err != nil {
		return contracts.Output{}, err
	}
	return roleCreatedOutput(out), nil
}

func roleCreatedOutput(out *genidentity.RoleRecord) contracts.Output {
	return contracts.Output{Data: out, Summary: fmt.Sprintf("role=%d", out.ID), Events: []contracts.Event{{
		Type: "identity.role.created", AggregateType: "role", AggregateID: fmt.Sprintf("%d", out.ID),
		Payload: map[string]any{"roleId": out.ID, "name": out.Name, "grants": len(out.Grants)},
	}}}
}

func (s *Service) updateRole(ctx context.Context, in contracts.Input) (contracts.Output, error) {
	payload := RoleInput{}
	if err := contracts.Bind(in, &payload); err != nil {
		return contracts.Output{}, err
	}
	out, err := s.UpdateRole(ctx, &payload)
	if err != nil {
		return contracts.Output{}, err
	}
	return contracts.Output{Data: out, Summary: fmt.Sprintf("role=%d", out.ID), Events: []contracts.Event{{
		Type: "identity.role.updated", AggregateType: "role", AggregateID: fmt.Sprintf("%d", out.ID),
		Payload: map[string]any{"roleId": out.ID, "name": out.Name, "grants": len(out.Grants)},
	}}}, nil
}

func (s *Service) deleteRole(ctx context.Context, in contracts.Input) (contracts.Output, error) {
	payload := DeleteInput{}
	if err := contracts.Bind(in, &payload); err != nil {
		return contracts.Output{}, err
	}
	if err := s.DeleteRole(ctx, &payload); err != nil {
		return contracts.Output{}, err
	}
	return contracts.Output{Data: map[string]any{"id": payload.ID}, Summary: fmt.Sprintf("role=%d", payload.ID), Events: []contracts.Event{{
		Type: "identity.role.deleted", AggregateType: "role", AggregateID: fmt.Sprintf("%d", payload.ID),
		Payload: map[string]any{"roleId": payload.ID},
	}}}, nil
}
