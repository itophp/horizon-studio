package identity

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	genidentity "horizon/foundation/gen/identity"
	"horizon/foundation/kernel/capability/contracts"
	"horizon/internal/repository/dbgen"
)

func (s *Service) newWorkPrincipal(ctx context.Context, account *genidentity.AccountRecord, tenant string, subjectID, loginID, membershipID, bindingID pgtype.UUID) (*Principal, error) {
	candidate := &Principal{TenantID: tenant, SubjectID: subjectID.String()}
	if err := s.applyIdentityBinding(ctx, candidate, bindingID); err != nil {
		return nil, err
	}
	var grants []*genidentity.PermissionGrant
	if candidate.IdentityBindingID == "" {
		admin, err := s.repo.q.IAMTenantAccountAuthority(ctx, dbgen.IAMTenantAccountAuthorityParams{TenantID: tenant, AccountID: account.ID})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, problem("forbidden")
		}
		if err != nil {
			return nil, safe(err)
		}
		account.IsAdmin = admin
		if admin {
			for _, permission := range s.permissionCodes() {
				grants = append(grants, &genidentity.PermissionGrant{Permission: permission, Effect: "allow", Scope: "ALL"})
			}
		}
	} else {
		account.IsAdmin = false
		account.RoleIds = []int64{}
	}
	account.Overrides = []*genidentity.PermissionGrant{}
	delegable := grants
	rows, err := s.repo.q.IAMCurrentContextRoleGrants(ctx, dbgen.IAMCurrentContextRoleGrantsParams{
		TenantID: tenant, PrincipalID: subjectID, IdentityBindingID: bindingID,
	})
	if err != nil {
		return nil, safe(err)
	}
	if !account.IsAdmin && len(rows) == 0 {
		return nil, problem("forbidden")
	}
	for _, raw := range rows {
		additional, err := decodeGrants(raw)
		if err != nil {
			return nil, safe(err)
		}
		for _, grant := range additional {
			if grant == nil || (grant.Scope != "ALL" && grant.Scope != "OWN") {
				return nil, problem("forbidden")
			}
			grants = append(grants, grant)
		}
	}
	principal, err := newPrincipal(account, tenant, subjectID.String(), grants, s.permissionCodes())
	if err != nil {
		return nil, safe(err)
	}
	principal.delegableGrants = delegable
	principal.LoginAccountID = loginID.String()
	principal.MembershipID = membershipID.String()
	principal.IdentityBindingID = candidate.IdentityBindingID
	principal.IdentityType = candidate.IdentityType
	principal.IdentityEntityID = candidate.IdentityEntityID
	principal.ActingOrganization = candidate.ActingOrganization
	return principal, nil
}

const (
	contextRoleGranted = "identity.roleGrant.created"
	contextRoleRevoked = "identity.roleGrant.revoked"
)

func contextRoleGrantDefinition() contracts.Definition {
	definition := identityWrite("identity.roleGrant.create", "Grant a contextual role", "Assign a role to a tenant member with a required expiry and reason.",
		"context_role_grant", "access:manage", []string{contextRoleGranted}, contracts.Object(map[string]*contracts.Schema{
			"accountId": contracts.Int(1, 1<<53-1), "roleId": contracts.Int(1, 1<<53-1),
			"bindingId": contracts.Str(36, 36), "minutes": contracts.Int(1, 365*24*60), "reason": contracts.Str(5, 500),
		}, "accountId", "roleId", "minutes", "reason"), "accountId", "roleId", "bindingId", "minutes")
	return contextRoleMutation(definition)
}

func contextRoleRevokeDefinition() contracts.Definition {
	definition := identityWrite("identity.roleGrant.revoke", "Revoke a contextual role", "Immediately remove an unexpired contextual role assignment.",
		"context_role_grant", "access:manage", []string{contextRoleRevoked},
		contracts.Object(map[string]*contracts.Schema{"id": contracts.Str(36, 36)}, "id"), "id")
	return contextRoleMutation(definition)
}

func contextRoleMutation(definition contracts.Definition) contracts.Definition {
	definition.AllowedOrigins = []contracts.Origin{contracts.OriginHumanUI}
	definition.Tags = append(definition.Tags, "manual-only")
	definition.Output = contracts.Object(map[string]*contracts.Schema{"id": {Type: "string"}}, "id")
	return definition
}

func contextRolesListDefinition() contracts.Definition {
	definition := readDefinition("identity.roleGrant.list", "List contextual role assignments", "Inspect role assignments and their expiry in the active tenant.",
		"context_role_grant", "access:manage", contracts.Object(map[string]*contracts.Schema{"items": {
			Type: "array", Items: contracts.Object(map[string]*contracts.Schema{
				"id": {Type: "string"}, "principalId": {Type: "string"}, "bindingId": {Type: "string"},
				"roleId": {Type: "integer"}, "grantedBy": {Type: "integer"}, "reason": {Type: "string"},
				"createdAt": {Type: "string"}, "expiresAt": {Type: "string"}, "revokedAt": {Type: "string"},
			}, "id", "principalId", "roleId", "grantedBy", "createdAt", "expiresAt"),
		}}, "items"))
	definition.Tags = append(definition.Tags, "manual-only")
	definition.AllowedOrigins = []contracts.Origin{contracts.OriginHumanUI}
	return definition
}

func (s *Service) contextRoleAuthority(ctx context.Context) (*Principal, error) {
	actor, err := Current(ctx)
	if err != nil {
		return nil, err
	}
	actor, err = actor.delegable()
	if err != nil {
		return nil, problem("forbidden")
	}
	scope, err := s.scope(ctx, actor, "access:manage")
	if err != nil || !scope.All || actor.Account.ID < 1 {
		return nil, problem("forbidden")
	}
	return actor, nil
}

func (s *Service) assignContextRole(ctx context.Context, input contracts.Input) (contracts.Output, error) {
	var request struct {
		AccountID int64  `json:"accountId"`
		RoleID    int64  `json:"roleId"`
		BindingID string `json:"bindingId"`
		Minutes   int32  `json:"minutes"`
		Reason    string `json:"reason"`
	}
	if err := contracts.Bind(input, &request); err != nil {
		return contracts.Output{}, err
	}
	actor, err := s.contextRoleAuthority(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	if actor.Account.ID == request.AccountID {
		return contracts.Output{}, problem("forbidden")
	}
	reason := strings.TrimSpace(request.Reason)
	if utf8.RuneCountInString(reason) < 5 {
		return contracts.Output{}, problem("invalid")
	}
	var bindingID pgtype.UUID
	if request.BindingID != "" && (bindingID.Scan(request.BindingID) != nil || !bindingID.Valid) {
		return contracts.Output{}, problem("invalid")
	}
	if bindingID.Valid {
		subject, err := s.repo.q.IAMActiveAccountSubject(ctx, dbgen.IAMActiveAccountSubjectParams{TenantID: actor.TenantID, AccountID: request.AccountID})
		if err != nil {
			return contracts.Output{}, problem("forbidden")
		}
		binding := &Principal{SubjectID: subject.PrincipalID.String(), TenantID: actor.TenantID}
		if err := s.applyIdentityBinding(ctx, binding, bindingID); err != nil {
			return contracts.Output{}, problem("forbidden")
		}
	}
	role, err := s.repo.role(ctx, tenantScope(actor), request.RoleID)
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	if err = s.validateRoleGrantCeiling(ctx, actor, role.Grants); err != nil {
		return contracts.Output{}, err
	}
	id, err := s.repo.q.IAMCreateContextRoleGrant(ctx, dbgen.IAMCreateContextRoleGrantParams{
		TenantID: actor.TenantID, AccountID: request.AccountID, RoleID: request.RoleID, IdentityBindingID: bindingID,
		Reason: reason, ValidMinutes: request.Minutes, GrantedBy: actor.Account.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, problem("forbidden")
	}
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	return contextualRoleOutput(id, contextRoleGranted), nil
}

func (s *Service) revokeContextRole(ctx context.Context, input contracts.Input) (contracts.Output, error) {
	actor, err := s.contextRoleAuthority(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	var id pgtype.UUID
	if id.Scan(input["id"]) != nil || !id.Valid {
		return contracts.Output{}, problem("invalid")
	}
	if _, err := s.repo.q.IAMRevokeContextRoleGrant(ctx, dbgen.IAMRevokeContextRoleGrantParams{TenantID: actor.TenantID, ID: id}); err != nil {
		return contracts.Output{}, safe(err)
	}
	return contextualRoleOutput(id, contextRoleRevoked), nil
}

func contextualRoleOutput(id pgtype.UUID, event string) contracts.Output {
	return contracts.Output{Data: map[string]any{"id": id.String()}, Events: []contracts.Event{{
		Type: event, AggregateType: "context_role_grant", AggregateID: id.String(),
	}}}
}

func (s *Service) contextRoles(ctx context.Context, _ contracts.Input) (contracts.Output, error) {
	actor, err := s.contextRoleAuthority(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	rows, err := s.repo.q.IAMListContextRoleGrants(ctx, actor.TenantID)
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	items := make([]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, map[string]any{
			"id": row.ID.String(), "principalId": row.PrincipalID.String(), "bindingId": row.IdentityBindingID.String(),
			"roleId": row.RoleID, "grantedBy": row.GrantedBy, "reason": row.Reason,
			"createdAt": timeValue(row.CreatedAt), "expiresAt": timeValue(row.ExpiresAt), "revokedAt": timeValue(row.RevokedAt),
		})
	}
	return contracts.Output{Data: map[string]any{"items": items}}, nil
}
