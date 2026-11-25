package identity

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"horizon/foundation/kernel/capability/contracts"
	"horizon/internal/repository/dbgen"
)

const membershipRevoked = "identity.membership.revoked"

func tenantMembershipRevocationDefinition() contracts.Definition {
	definition := identityWrite("identity.membership.revoke", "Revoke tenant membership",
		"Remove a subject's access to the current tenant and expire its sessions and machine tokens.",
		"tenant_membership", "access:manage", []string{membershipRevoked},
		contracts.Object(map[string]*contracts.Schema{"accountId": contracts.Int(1, 1<<53-1)}, "accountId"), "accountId")
	definition.AllowedOrigins = []contracts.Origin{contracts.OriginHumanUI}
	definition.Tags = append(definition.Tags, "manual-only")
	return definition
}

func (s *Service) revokeTenantMembership(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
	var request struct {
		AccountID int64 `json:"accountId"`
	}
	if err := contracts.Bind(input, &request); err != nil {
		return contracts.Output{}, err
	}
	queries := dbgen.New(tx)
	actor, err := s.workspaceIAMAdmin(ctx, queries)
	if err != nil {
		return contracts.Output{}, err
	}
	membership, err := queries.IAMRevokeTenantMembership(ctx, dbgen.IAMRevokeTenantMembershipParams{TenantID: actor.TenantID, AccountID: request.AccountID})
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, contracts.Problem("forbidden")
	}
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	return contracts.Output{
		Data:   map[string]any{"id": request.AccountID},
		Events: []contracts.Event{{Type: membershipRevoked, AggregateType: "tenant_membership", AggregateID: membership.String()}},
	}, nil
}
