package identity

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	genidentity "horizon/foundation/gen/identity"
	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/capability/registry"
	"horizon/internal/repository/dbgen"
)

const agentDelegationCreated = "identity.agentDelegation.created"
const agentDelegationRevoked = "identity.agentDelegation.revoked"

type agentDelegation struct {
	row       dbgen.IAMActiveAgentDelegationRow
	delegator *Principal
	actions   []string
}

func (p *Principal) AgentDelegationID() string {
	if p.machine == nil || p.machine.delegation == nil {
		return ""
	}
	return p.machine.delegation.row.ID.String()
}

func (p *Principal) DelegatorAccountID() int64 {
	if p.machine == nil || p.machine.delegation == nil {
		return 0
	}
	return p.machine.delegation.row.DelegatorAccountID
}

func agentDelegationEntity() contracts.Entity {
	return contracts.Entity{Name: "agent_delegation", Domain: "identity", Table: "iam_agent_delegations", Key: "id",
		Description: "A human's revocable and time-limited authorization for an agent to access one resource.",
		Attributes: []contracts.Attribute{
			{Name: "id", Kind: contracts.AttributeSystem, Type: contracts.TypeUUID, Required: true},
			{Name: "tenantId", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true},
			{Name: "agentAccountId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "delegatorAccountId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "delegatorBindingId", Kind: contracts.AttributeSystem, Type: contracts.TypeUUID, Nullable: true},
			{Name: "resourceId", Kind: contracts.AttributeSystem, Type: contracts.TypeInteger, Required: true},
			{Name: "capability", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true},
			{Name: "additionalCapabilities", Kind: contracts.AttributeSystem, Type: contracts.TypeJSON, Shape: "array", Required: true},
			{Name: "maxRisk", Kind: contracts.AttributeSystem, Type: contracts.TypeEnum, Required: true, Enum: []string{"low", "medium", "high"}},
			{Name: "remainingUses", Kind: contracts.AttributeSystem, Type: contracts.TypeInteger, Required: true},
			{Name: "createdAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Required: true},
			{Name: "expiresAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Required: true},
			{Name: "revokedAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Nullable: true},
		}, Relations: []contracts.Relation{
			{Name: "agent", Target: "account", Kind: "one", Via: "agent_account_id", Required: true},
			{Name: "delegator", Target: "account", Kind: "one", Via: "delegator_account_id", Required: true},
		},
	}
}

func (s *Service) agentDelegationEntries() []registry.Entry {
	create := identityWrite("identity.agentDelegation.create", "Grant an agent delegation", "Authorize one agent to act on one resource using versioned actions, expiry, risk and a shared usage limit.",
		"agent_delegation", "assistant:use", []string{agentDelegationCreated}, contracts.Object(map[string]*contracts.Schema{
			"agentAccountId": contracts.Int(1, 1<<53-1), "resourceId": contracts.Int(1, 1<<53-1), "capability": contracts.Str(7, 97),
			"maxRisk": contracts.Enum("low", "medium", "high"), "uses": contracts.Int(1, 100), "minutes": contracts.Int(1, 1440),
			"password":               contracts.Str(1, 256),
			"additionalCapabilities": contracts.Array(contracts.Str(7, 97), 0, 19),
		}, "agentAccountId", "resourceId", "capability", "maxRisk", "uses", "minutes", "password"),
		"agentAccountId", "resourceId", "capability", "maxRisk", "uses", "minutes")
	create.Output = contracts.Object(map[string]*contracts.Schema{"id": {Type: "string"}, "expiresAt": {Type: "string"}}, "id", "expiresAt")
	revoke := identityWrite("identity.agentDelegation.revoke", "Revoke an agent delegation", "Withdraw a delegation previously granted by the signed-in account.",
		"agent_delegation", "", []string{agentDelegationRevoked}, contracts.Object(map[string]*contracts.Schema{
			"id": contracts.Str(36, 36), "password": contracts.Str(1, 256),
		}, "id", "password"), "id")
	revoke.Output = contracts.Object(map[string]*contracts.Schema{"id": {Type: "string"}}, "id")
	for _, definition := range []*contracts.Definition{&create, &revoke} {
		definition.AllowedOrigins = []contracts.Origin{contracts.OriginHumanUI}
		definition.Tags = append(definition.Tags, "manual-only")
	}
	return []registry.Entry{
		{Definition: create, Handler: contracts.Handler{ExecuteTx: s.createAgentDelegation}},
		{Definition: revoke, Handler: contracts.Handler{ExecuteTx: s.revokeAgentDelegation}},
	}
}

func (s *Service) delegationOwner(ctx context.Context, queries *dbgen.Queries, password string) (*Principal, error) {
	current, err := Current(ctx)
	if err != nil || current.TokenHash == "" {
		return nil, problem("forbidden")
	}
	actor, err := s.WithReadQueries(queries).PrincipalForSessionInvocation(ctx, current, false)
	if err != nil {
		return nil, err
	}
	if err = s.VerifyCurrentPassword(ctx, actor, password); err != nil {
		return nil, err
	}
	return actor, nil
}

func (s *Service) createAgentDelegation(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
	var request struct {
		AgentAccountID         int64    `json:"agentAccountId"`
		Capability             string   `json:"capability"`
		ResourceID             int64    `json:"resourceId"`
		MaxRisk                string   `json:"maxRisk"`
		Uses                   int32    `json:"uses"`
		Minutes                int32    `json:"minutes"`
		Password               string   `json:"password"`
		AdditionalCapabilities []string `json:"additionalCapabilities"`
	}
	if err := contracts.Bind(input, &request); err != nil || !machineCapabilityPattern.MatchString(request.Capability) {
		return contracts.Output{}, problem("invalid")
	}
	queries := dbgen.New(tx)
	actor, err := s.delegationOwner(ctx, queries, request.Password)
	if err != nil {
		return contracts.Output{}, err
	}
	if !actor.CanPermanent("assistant:use") {
		return contracts.Output{}, problem("forbidden")
	}
	actions := append([]string{request.Capability}, request.AdditionalCapabilities...)
	if !validMachineCapabilities(actions) {
		return contracts.Output{}, problem("invalid")
	}
	var resource string
	for _, key := range actions {
		name, version, _ := strings.Cut(key, "@")
		entry, ok := s.authorizationCatalog.Resolve(name, version)
		d := entry.Definition
		if !ok || !d.MachineCallable || d.DelegationResourceID == "" ||
			(resource != "" && resource != d.Entity) || !actor.CanPermanent(d.Permission) {
			return contracts.Output{}, problem("forbidden")
		}
		if actor.IdentityBindingID != "" && !s.delegatedBindingAllows(ctx, queries, actor, d, request.ResourceID) {
			return contracts.Output{}, problem("forbidden")
		}
		resource = d.Entity
	}
	additional, err := json.Marshal(append([]string{}, request.AdditionalCapabilities...))
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	subject, err := queries.IAMMachineSubject(ctx, dbgen.IAMMachineSubjectParams{TenantID: actor.TenantID, AccountID: request.AgentAccountID})
	if err != nil || subject.Kind != "agent" {
		return contracts.Output{}, problem("forbidden")
	}
	row, err := queries.IAMIssueAgentDelegation(ctx, dbgen.IAMIssueAgentDelegationParams{
		TenantID: actor.TenantID, AgentAccountID: request.AgentAccountID, DelegatorAccountID: actor.Account.ID,
		BindingID: actor.IdentityBindingID, Capability: request.Capability,
		ResourceID: request.ResourceID, MaxRisk: request.MaxRisk, RemainingUses: request.Uses, Minutes: request.Minutes,
		AdditionalCapabilities: additional,
	})
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	return machineResult(agentDelegationCreated, "agent_delegation", row.ID.String(), request.AgentAccountID,
		map[string]any{"id": row.ID.String(), "expiresAt": row.ExpiresAt.Time.UTC().Format(time.RFC3339)}), nil
}

func (s *Service) revokeAgentDelegation(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
	var request struct {
		ID       string `json:"id"`
		Password string `json:"password"`
	}
	var id pgtype.UUID
	if err := contracts.Bind(input, &request); err != nil || id.Scan(request.ID) != nil || !id.Valid {
		return contracts.Output{}, problem("invalid")
	}
	queries := dbgen.New(tx)
	actor, err := s.delegationOwner(ctx, queries, request.Password)
	if err != nil {
		return contracts.Output{}, err
	}
	count, err := queries.IAMRevokeAgentDelegation(ctx, dbgen.IAMRevokeAgentDelegationParams{
		TenantID: actor.TenantID, ID: id, DelegatorAccountID: actor.Account.ID,
		BindingID: actor.IdentityBindingID,
	})
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	if count != 1 {
		return contracts.Output{}, problem("not_found")
	}
	return contracts.Output{Data: map[string]any{"id": id.String()}, Events: []contracts.Event{{
		Type: agentDelegationRevoked, AggregateType: "agent_delegation", AggregateID: id.String(), Payload: map[string]any{},
	}}}, nil
}

func (s *Service) attachAgentDelegation(ctx context.Context, principal *Principal, delegationID string) (*Principal, error) {
	var id pgtype.UUID
	if principal.machine.kind != "agent" || id.Scan(delegationID) != nil || !id.Valid {
		return nil, problem("forbidden")
	}
	row, err := s.repo.q.IAMActiveAgentDelegation(ctx, dbgen.IAMActiveAgentDelegationParams{
		TenantID: principal.TenantID, AgentAccountID: principal.Account.ID, ID: id,
	})
	if err != nil {
		return nil, problem("forbidden")
	}
	var additional []string
	if json.Unmarshal(row.AdditionalCapabilities, &additional) != nil {
		return nil, problem("forbidden")
	}
	actions := append([]string{row.Capability}, additional...)
	if !validMachineCapabilities(actions) {
		return nil, problem("forbidden")
	}
	delegator, err := s.PrincipalForBoundTask(ctx, principal.TenantID, row.DelegatorAccountID, row.BindingID)
	if err != nil || !delegator.CanPermanent("assistant:use") {
		return nil, problem("forbidden")
	}
	var grants []*genidentity.PermissionGrant
	for _, grant := range principal.PermanentGrants {
		scope, err := s.scope(ctx, principal, grant.Permission)
		if err == nil && s.withinApprovalScope(ctx, delegator, grant.Permission, scope) == nil {
			grants = append(grants, grant)
		}
	}
	restricted, err := newPrincipal(principal.Account, principal.TenantID, principal.SubjectID, grants, s.permissionCodes())
	if err != nil {
		return nil, safe(err)
	}
	restricted.MembershipID = principal.MembershipID
	machine := *principal.machine
	machine.delegation = &agentDelegation{row: row, delegator: delegator, actions: actions}
	restricted.machine = &machine
	return restricted, nil
}

func (s *Service) delegatedBindingAllows(ctx context.Context, q *dbgen.Queries, actor *Principal, definition contracts.Definition, resourceID int64) bool {
	if definition.DelegationBindingType != actor.IdentityType || definition.DelegationBindingAttribute == "" || actor.IdentityEntityID <= 0 {
		return false
	}
	values := s.resourceAttributeValues(ctx, q, actor.TenantID, definition.Entity, resourceID)
	return values[definition.DelegationBindingAttribute] == strconv.FormatInt(actor.IdentityEntityID, 10)
}

func (s *Service) AuthorizeDelegatedBinding(ctx context.Context, q *dbgen.Queries, p *Principal, definition contracts.Definition, resourceID int64, lock bool) bool {
	if p.machine == nil || p.machine.delegation == nil {
		return true
	}
	actor := p.machine.delegation.delegator
	if actor.IdentityBindingID == "" {
		return true
	}
	if lock {
		var id pgtype.UUID
		if id.Scan(actor.IdentityBindingID) != nil || !id.Valid {
			return false
		}
		if _, err := q.IAMShareActiveDelegatorBinding(ctx, dbgen.IAMShareActiveDelegatorBindingParams{TenantID: actor.TenantID, ID: id}); err != nil {
			return false
		}
	}
	return s.delegatedBindingAllows(ctx, q, actor, definition, resourceID)
}

func (s *Service) ConsumeAgentDelegation(ctx context.Context, queries *dbgen.Queries, principal *Principal) error {
	if principal.AgentDelegationID() == "" {
		return nil
	}
	count, err := queries.IAMConsumeAgentDelegation(ctx, dbgen.IAMConsumeAgentDelegationParams{
		TenantID: principal.TenantID, AgentAccountID: principal.Account.ID, ID: principal.machine.delegation.row.ID,
	})
	if err != nil {
		return safe(err)
	}
	if count != 1 {
		return problem("forbidden")
	}
	return nil
}

func (s *Service) AuditMachineExecution(ctx context.Context, queries *dbgen.Queries, principal *Principal, action, trace string) error {
	if principal.MachineCredentialID() == "" {
		return nil
	}
	record := dbgen.SecurityAuditAppendParams{
		TenantID: principal.TenantID, AccountID: principal.Account.ID,
		Action: action, Outcome: "allowed", CorrelationID: trace, ResourceType: "machine_credential", ResourceID: principal.MachineCredentialID(),
	}
	if err := queries.SecurityAuditAppend(ctx, record); err != nil {
		return err
	}
	if principal.AgentDelegationID() == "" {
		return nil
	}
	record.ResourceType, record.ResourceID = "agent_delegation", principal.AgentDelegationID()
	record.Reason = "delegator_account:" + strconv.FormatInt(principal.DelegatorAccountID(), 10)
	return queries.SecurityAuditAppend(ctx, record)
}
