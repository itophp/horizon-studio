package identity

import (
	"context"

	genidentity "horizon/foundation/gen/identity"
)

func (s *Service) validateRoleGrantCeiling(ctx context.Context, actor *Principal, grants []*genidentity.PermissionGrant) error {
	base, err := actor.delegable()
	if err != nil {
		return problem("forbidden")
	}
	for _, grant := range grants {
		if grant == nil || (grant.Scope != "ALL" && grant.Scope != "OWN") {
			return problem("forbidden")
		}
		scope := Scope{TenantID: actor.TenantID, All: true}
		if err := s.withinApprovalScope(ctx, base, grant.Permission, scope); err != nil {
			return err
		}
	}
	return nil
}
