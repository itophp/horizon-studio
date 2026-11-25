package identity

import (
	"context"
	"maps"

	"horizon/internal/repository/dbgen"
)

// OrganizationContext is derived from a trusted binding provider.
type OrganizationContext struct {
	OrganizationID int64
	Name           string
	Portal         string
}

type OrganizationContextResolver func(ctx context.Context, db dbgen.DBTX, tenantID, principalID string, entityID int64) (OrganizationContext, error)

// ConfigureOrganizationContexts installs deployment-owned providers before serving
// requests. Registered binding types require valid organization context on use.
func (s *Service) ConfigureOrganizationContexts(resolvers map[string]OrganizationContextResolver) {
	s.organizationContexts = maps.Clone(resolvers)
}

func (s *Service) resolveOrganizationContext(ctx context.Context, principal *Principal, version string) error {
	resolve := s.organizationContexts[principal.IdentityType+"@"+version]
	if resolve == nil {
		return problem("unauthenticated")
	}
	resolved, err := resolve(ctx, s.repo.q.Connection(), principal.TenantID, principal.SubjectID, principal.IdentityEntityID)
	if err != nil || resolved.OrganizationID <= 0 || resolved.Portal == "" {
		return problem("unauthenticated")
	}
	principal.ActingOrganization = resolved
	return nil
}
