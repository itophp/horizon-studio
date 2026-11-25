package identity

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgtype"

	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/capability/registry"
	"horizon/internal/repository/dbgen"
)

func (s *Service) machineDirectoryEntries() []registry.Entry {
	uuid := contracts.Str(36, 36)
	text := &contracts.Schema{Type: "string"}
	number := &contracts.Schema{Type: "integer"}
	credential := contracts.Object(map[string]*contracts.Schema{
		"id": uuid, "audience": text, "capabilities": contracts.Array(text, 0, 50),
		"createdAt": text, "expiresAt": text, "revokedAt": text,
	}, "id", "audience", "capabilities", "createdAt", "expiresAt", "revokedAt")
	delegation := contracts.Object(map[string]*contracts.Schema{
		"id": uuid, "agentAccountId": number, "capability": text, "resourceId": number,
		"additionalCapabilities": contracts.Array(text, 0, 19),
		"maxRisk":                text, "remainingUses": number, "createdAt": text, "expiresAt": text, "revokedAt": text,
	}, "id", "agentAccountId", "capability", "additionalCapabilities", "resourceId", "maxRisk", "remainingUses", "createdAt", "expiresAt", "revokedAt")
	page := func(item *contracts.Schema) *contracts.Schema {
		return contracts.Object(map[string]*contracts.Schema{"items": contracts.Array(item, 0, 50), "nextAfter": contracts.Str(0, 36)}, "items", "nextAfter")
	}
	machine := readDefinition("identity.machine.credential.list", "List machine credentials", "Read credential metadata for a machine in the current workspace.", "machine_credential", "access:manage", page(credential))
	machine.AllowedOrigins, machine.Tags = []contracts.Origin{contracts.OriginHumanUI}, []string{"identity", "manual-only"}
	machine.Input = contracts.Object(map[string]*contracts.Schema{"accountId": contracts.Int(1, 1<<53-1), "after": uuid}, "accountId")
	agent := readDefinition("identity.agentDelegation.list", "List agent delegations", "Read delegations issued from the current login context.", "agent_delegation", "", page(delegation))
	agent.AllowedOrigins, agent.Tags = []contracts.Origin{contracts.OriginHumanUI}, []string{"identity", "manual-only", contracts.AuthenticatedActionTag}
	agent.Input = contracts.Object(map[string]*contracts.Schema{"after": uuid})
	return []registry.Entry{
		{Definition: machine, Handler: contracts.Handler{Execute: s.listMachineCredentials}},
		{Definition: agent, Handler: contracts.Handler{Execute: s.listAgentDelegations}},
	}
}

func (s *Service) listMachineCredentials(ctx context.Context, input contracts.Input) (contracts.Output, error) {
	actor, err := s.workspaceIAMAdmin(ctx, s.repo.q)
	if err != nil {
		return contracts.Output{}, err
	}
	var request struct {
		AccountID int64
		After     string
	}
	var after pgtype.UUID
	if err = contracts.Bind(input, &request); err != nil || request.After != "" && after.Scan(request.After) != nil {
		return contracts.Output{}, problem("invalid")
	}
	rows, err := s.repo.q.IAMListMachineCredentials(ctx, dbgen.IAMListMachineCredentialsParams{TenantID: actor.TenantID, AccountID: request.AccountID, AfterID: after})
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	next := ""
	if len(rows) > 50 {
		rows = rows[:50]
		next = rows[49].ID.String()
	}
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		var keys []string
		if err = json.Unmarshal(row.Capabilities, &keys); err != nil || !validMachineCapabilities(keys) {
			return contracts.Output{}, problem("unavailable")
		}
		items = append(items, map[string]any{
			"id": row.ID.String(), "audience": row.Audience, "capabilities": keys, "createdAt": timeValue(row.CreatedAt),
			"expiresAt": timeValue(row.ExpiresAt), "revokedAt": timeValue(row.RevokedAt),
		})
	}
	return contracts.Output{Data: map[string]any{"items": items, "nextAfter": next}}, nil
}

func (s *Service) listAgentDelegations(ctx context.Context, input contracts.Input) (contracts.Output, error) {
	p, err := Current(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	actor, err := s.PrincipalForSessionInvocation(ctx, p, false)
	if err != nil {
		return contracts.Output{}, err
	}
	var request struct{ After string }
	var after, binding pgtype.UUID
	if err = contracts.Bind(input, &request); err != nil || request.After != "" && after.Scan(request.After) != nil ||
		actor.IdentityBindingID != "" && binding.Scan(actor.IdentityBindingID) != nil {
		return contracts.Output{}, problem("invalid")
	}
	rows, err := s.repo.q.IAMListOwnAgentDelegations(ctx, dbgen.IAMListOwnAgentDelegationsParams{
		TenantID: actor.TenantID, AccountID: actor.Account.ID, BindingID: binding, AfterID: after,
	})
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	next := ""
	if len(rows) > 50 {
		rows = rows[:50]
		next = rows[49].ID.String()
	}
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		additional := []string{}
		if err := json.Unmarshal(row.AdditionalCapabilities, &additional); err != nil {
			return contracts.Output{}, problem("unavailable")
		}
		items = append(items, map[string]any{"id": row.ID.String(), "agentAccountId": row.AgentAccountID,
			"capability": row.Capability, "additionalCapabilities": additional, "resourceId": row.ResourceID, "maxRisk": row.MaxRisk, "remainingUses": row.RemainingUses,
			"createdAt": timeValue(row.CreatedAt), "expiresAt": timeValue(row.ExpiresAt), "revokedAt": timeValue(row.RevokedAt)})
	}
	return contracts.Output{Data: map[string]any{"items": items, "nextAfter": next}}, nil
}
