package identity

import (
	"context"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	genidentity "horizon/foundation/gen/identity"
	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/capability/registry"
	"horizon/internal/repository/dbgen"
)

func (s *Service) roleReviewEntries() []registry.Entry {
	impact := map[string]*contracts.Schema{
		"principals": {Type: "integer"}, "sessions": {Type: "integer"}, "tasks": {Type: "integer"},
	}
	preview := readDefinition("identity.role.preview", "Preview role changes", "Compare role grants and count principals, sessions and enabled schedules potentially affected.",
		"role", "roles:update", contracts.Object(map[string]*contracts.Schema{
			"added": contracts.Array(grantSchema(), 0, 100), "removed": contracts.Array(grantSchema(), 0, 100),
			"statusBefore": contracts.Enum("active", "inactive"), "statusAfter": contracts.Enum("active", "inactive"),
			"revision": contracts.Str(64, 64), "impact": contracts.Object(impact, "principals", "sessions", "tasks"),
		}, "added", "removed", "statusBefore", "statusAfter", "revision", "impact"))
	preview.Input = roleSchema(true)
	copy := identityWrite("identity.role.copy", "Copy a role", "Copy a role's permission definitions into a new tenant role.",
		"role", "roles:create", []string{"identity.role.created"}, contracts.Object(map[string]*contracts.Schema{
			"sourceId": contracts.Int(1, 1<<40), "name": contracts.Str(1, 100), "code": contracts.Str(2, 40),
		}, "sourceId", "name", "code"), "sourceId", "name", "code")
	revoke := readDefinition("identity.roleGrant.previewRevoke", "Preview contextual role revocation", "Count principals, sessions and enabled schedules within the grant's context.",
		"context_role_grant", "access:manage", contracts.Object(impact, "principals", "sessions", "tasks"))
	revoke.Input = contracts.Object(map[string]*contracts.Schema{"id": contracts.Str(36, 36)}, "id")
	for _, definition := range []*contracts.Definition{&preview, &copy, &revoke} {
		definition.Tags = []string{"identity", "manual-only"}
		definition.AllowedOrigins = []contracts.Origin{contracts.OriginHumanUI}
	}
	return []registry.Entry{
		{Definition: revoke, Handler: contracts.Handler{Execute: s.previewContextRevocation}},
		{Definition: preview, Handler: contracts.Handler{Execute: s.previewRole}},
		{Definition: copy, Handler: contracts.Handler{ExecuteTx: s.grantWrite((*Service).copyRole)}},
	}
}

func roleRevision(old *genidentity.RoleRecord, proposed *RoleInput) string {
	// Include the whole proposed change so changing the confirmed input also
	// requires a fresh preview.
	return contracts.Digest(contracts.Input{
		"current":  []any{old.ID, old.Name, old.Code, old.Description, old.Status, old.Grants},
		"proposed": []any{proposed.ID, proposed.Name, proposed.Code, proposed.Description, proposed.Status, proposed.Grants},
	})
}

func grantDifference(source, target []*genidentity.PermissionGrant) []*genidentity.PermissionGrant {
	result := make([]*genidentity.PermissionGrant, 0)
	for _, grant := range source {
		if !slices.ContainsFunc(target, func(other *genidentity.PermissionGrant) bool { return sameJSON(grant, other) }) {
			result = append(result, grant)
		}
	}
	return result
}

func (s *Service) previewRole(ctx context.Context, input contracts.Input) (contracts.Output, error) {
	actor, err := s.roleAuthority(ctx, "roles:update")
	if err != nil {
		return contracts.Output{}, err
	}
	var proposed RoleInput
	if contracts.Bind(input, &proposed) != nil {
		return contracts.Output{}, problem("invalid")
	}
	if err = s.validateTenantGrants(proposed.Grants); err != nil {
		return contracts.Output{}, err
	}
	if err = s.validateRoleGrantCeiling(ctx, actor, proposed.Grants); err != nil {
		return contracts.Output{}, err
	}
	old, err := s.repo.role(ctx, tenantScope(actor), proposed.ID)
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	impact, err := s.repo.q.IAMRoleImpact(ctx, dbgen.IAMRoleImpactParams{TenantID: actor.TenantID, RoleID: old.ID})
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	return contracts.Output{Data: map[string]any{
		"added":        CapabilityData(grantDifference(proposed.Grants, old.Grants)),
		"removed":      CapabilityData(grantDifference(old.Grants, proposed.Grants)),
		"statusBefore": old.Status, "statusAfter": proposed.Status, "revision": roleRevision(old, &proposed),
		"impact": map[string]int64{"principals": impact.Principals, "sessions": impact.Sessions, "tasks": impact.Tasks},
	}}, nil
}

func (s *Service) copyRole(ctx context.Context, input contracts.Input) (contracts.Output, error) {
	var request struct {
		SourceID   int64
		Name, Code string
	}
	if contracts.Bind(input, &request) != nil {
		return contracts.Output{}, problem("invalid")
	}
	actor, err := s.roleAuthority(ctx, "roles:create")
	if err != nil {
		return contracts.Output{}, err
	}
	old, err := s.repo.role(ctx, tenantScope(actor), request.SourceID)
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	record, err := s.CreateRole(ctx, &RoleInput{
		Name: request.Name, Code: request.Code, Description: old.Description, Status: "inactive", Grants: old.Grants,
	})
	if err != nil {
		return contracts.Output{}, err
	}
	return roleCreatedOutput(record), nil
}

func (s *Service) previewContextRevocation(ctx context.Context, input contracts.Input) (contracts.Output, error) {
	actor, err := s.contextRoleAuthority(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	var id pgtype.UUID
	if id.Scan(input["id"]) != nil || !id.Valid {
		return contracts.Output{}, problem("invalid")
	}
	impact, err := s.repo.q.IAMContextRoleImpact(ctx, dbgen.IAMContextRoleImpactParams{TenantID: actor.TenantID, GrantID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, problem("not_found")
	}
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	return contracts.Output{Data: map[string]int64{"principals": impact.Principals, "sessions": impact.Sessions, "tasks": impact.Tasks}}, nil
}
