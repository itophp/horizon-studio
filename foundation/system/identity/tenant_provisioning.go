package identity

import (
	"context"
	"slices"

	"github.com/jackc/pgx/v5"
)

type tenantProvisioning struct {
	entities  []string
	provision func(context.Context, pgx.Tx, string, int64) error
}

// ConfigureTenantProvisioning installs a deployment-owned transactional extension
// to workspace creation.
func (s *Service) ConfigureTenantProvisioning(entities []string, provision func(context.Context, pgx.Tx, string, int64) error) {
	s.tenantProvisioning = tenantProvisioning{entities: slices.Clone(entities), provision: provision}
}
