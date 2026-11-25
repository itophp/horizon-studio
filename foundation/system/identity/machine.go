package identity

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	genidentity "horizon/foundation/gen/identity"
	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/capability/registry"
	"horizon/internal/repository/dbgen"
)

const (
	CapabilityAudience = "horizon-capability"
	AgentAudience      = "horizon-agent"
	machineTokenPrefix = "hznm_"
	machineCreated     = "identity.machine.created"
	machineRotated     = "identity.machine.credential.rotated"
	machineRevoked     = "identity.machine.credential.revoked"
)

var machineCapabilityPattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9._-]{0,79}@[0-9]+\.[0-9]+\.[0-9]+$`)

type machineContext struct {
	credentialID string
	tokenHash    string
	audience     string
	kind         string
	capabilities []string
	issuer       *Principal
	delegation   *agentDelegation
}

func (p *Principal) MachineCredentialID() string {
	if p.machine == nil {
		return ""
	}
	return p.machine.credentialID
}

func (p *Principal) HasTenantWideGrant(permission string) bool {
	return p.ActingOrganization.OrganizationID == 0 && p.Can(permission) && slices.ContainsFunc(p.PermanentGrants, func(grant *genidentity.PermissionGrant) bool {
		return grant.Permission == permission && grant.Effect == "allow" && grant.Scope == "ALL"
	})
}

func (p *Principal) MachineAllowsCapability(definition contracts.Definition, origin contracts.Origin) bool {
	if p.machine == nil || !slices.Contains(p.machine.capabilities, definition.Key()) {
		return false
	}
	if p.machine.audience == CapabilityAudience {
		if origin == contracts.OriginAutomation {
			return p.machine.kind == "service"
		}
		return p.machine.kind != "agent" && (origin == contracts.OriginAPI || origin == contracts.OriginWorkflow || origin == contracts.OriginAgent)
	}
	if (origin != contracts.OriginAgent && origin != contracts.OriginWorkflow) || p.machine.delegation == nil || definition.DelegationResourceID == "" {
		return false
	}
	grant := p.machine.delegation.row
	return slices.Contains(p.machine.delegation.actions, definition.Key()) && machineRisk(definition.Risk) <= machineRisk(contracts.Risk(grant.MaxRisk))
}

func (p *Principal) MachineAllows(definition contracts.Definition, input contracts.Input, origin contracts.Origin) bool {
	if !p.MachineAllowsCapability(definition, origin) {
		return false
	}
	if p.machine.delegation == nil {
		return true
	}
	var resource struct{ ID int64 }
	return contracts.Bind(contracts.Input{"ID": input[definition.DelegationResourceID]}, &resource) == nil && resource.ID == p.machine.delegation.row.ResourceID
}

func machineRisk(risk contracts.Risk) int {
	switch risk {
	case contracts.RiskLow:
		return 1
	case contracts.RiskMedium:
		return 2
	case contracts.RiskHigh:
		return 3
	default:
		return 4
	}
}

func (s *Service) machineEntries() []registry.Entry {
	create := identityWrite("identity.machine.create", "Create a machine principal", "Create an Agent, Service or Application principal with explicitly scoped grants.",
		"security_principal", "access:manage", []string{machineCreated}, contracts.Object(map[string]*contracts.Schema{
			"kind": contracts.Enum("agent", "service", "application"), "name": contracts.Str(1, 120),
			"grants":   contracts.Array(grantSchema(), 0, 50),
			"password": contracts.Str(1, 256),
		}, "kind", "name", "grants", "password"), "kind", "name")
	create.Output = contracts.Object(map[string]*contracts.Schema{"id": {Type: "integer"}, "principalId": {Type: "string"}}, "id", "principalId")

	rotate := identityWrite("identity.machine.credential.rotate", "Rotate machine credentials", "Revoke the current credential and issue a time-limited replacement whose secret is displayed once.",
		"machine_credential", "access:manage", []string{machineRotated}, contracts.Object(map[string]*contracts.Schema{
			"accountId": contracts.Int(1, 1<<53-1), "audience": contracts.Enum(CapabilityAudience, AgentAudience),
			"capabilities": contracts.Array(contracts.Str(7, 97), 1, 50), "minutes": contracts.Int(1, 1440), "password": contracts.Str(1, 256),
		}, "accountId", "audience", "capabilities", "minutes", "password"), "accountId", "audience", "capabilities", "minutes")
	rotate.TransientResult = true
	rotate.Output = contracts.Object(map[string]*contracts.Schema{
		"id": {Type: "string"}, "secret": {Type: "string"}, "expiresAt": {Type: "string"},
	}, "id", "secret", "expiresAt")

	revoke := identityWrite("identity.machine.credential.revoke", "Revoke machine credentials", "Revoke all currently issued credentials for a machine principal.",
		"machine_credential", "access:manage", []string{machineRevoked}, contracts.Object(map[string]*contracts.Schema{
			"accountId": contracts.Int(1, 1<<53-1), "password": contracts.Str(1, 256),
		}, "accountId", "password"), "accountId")
	for _, definition := range []*contracts.Definition{&create, &rotate, &revoke} {
		definition.AllowedOrigins = []contracts.Origin{contracts.OriginHumanUI}
		definition.Tags = append(definition.Tags, "manual-only")
	}
	return []registry.Entry{
		{Definition: create, Handler: contracts.Handler{ExecuteTx: s.createMachine}},
		{Definition: rotate, Handler: contracts.Handler{ExecuteTx: s.changeMachineCredential(true)}},
		{Definition: revoke, Handler: contracts.Handler{ExecuteTx: s.changeMachineCredential(false)}},
	}
}

func (s *Service) machineAdministrator(ctx context.Context, queries *dbgen.Queries, password string) (*Principal, error) {
	current, err := Current(ctx)
	if err != nil || current.TokenHash == "" || current.IdentityBindingID != "" {
		return nil, problem("forbidden")
	}
	actor, err := s.WithReadQueries(queries).PrincipalForSessionInvocation(ctx, current, false)
	if err != nil {
		return nil, err
	}
	base, err := actor.delegable()
	if err != nil || !base.Can("access:manage") {
		return nil, problem("forbidden")
	}
	if err = s.VerifyCurrentPassword(ctx, actor, password); err != nil {
		return nil, err
	}
	return base, nil
}

func (s *Service) createMachine(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
	var request struct {
		Kind     string                         `json:"kind"`
		Name     string                         `json:"name"`
		Grants   []*genidentity.PermissionGrant `json:"grants"`
		Password string                         `json:"password"`
	}
	if err := contracts.Bind(input, &request); err != nil || strings.TrimSpace(request.Name) == "" {
		return contracts.Output{}, problem("invalid")
	}
	queries := dbgen.New(tx)
	bound := s.WithReadQueries(queries)
	actor, err := s.machineAdministrator(ctx, queries, request.Password)
	if err != nil {
		return contracts.Output{}, err
	}
	if err = bound.validateTenantGrants(request.Grants); err != nil {
		return contracts.Output{}, err
	}
	if err = bound.validateRoleGrantCeiling(ctx, actor, request.Grants); err != nil {
		return contracts.Output{}, err
	}
	grants, err := encodeTenantGrants(request.Grants)
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	accountID, err := queries.IAMCreateMachine(ctx, dbgen.IAMCreateMachineParams{
		TenantID: actor.TenantID,
		Kind:     request.Kind, DisplayName: strings.TrimSpace(request.Name), Grants: grants,
	})
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	subject, err := queries.IAMMachineSubject(ctx, dbgen.IAMMachineSubjectParams{TenantID: actor.TenantID, AccountID: accountID})
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	return machineResult(machineCreated, "security_principal", subject.PrincipalID.String(), accountID,
		map[string]any{"id": accountID, "principalId": subject.PrincipalID.String()}), nil
}

func (s *Service) changeMachineCredential(rotate bool) contracts.TxExecutor {
	return func(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
		var request struct {
			AccountID    int64    `json:"accountId"`
			Audience     string   `json:"audience"`
			Capabilities []string `json:"capabilities"`
			Minutes      int32    `json:"minutes"`
			Password     string   `json:"password"`
		}
		if err := contracts.Bind(input, &request); err != nil {
			return contracts.Output{}, problem("invalid")
		}
		queries := dbgen.New(tx)
		actor, err := s.machineAdministrator(ctx, queries, request.Password)
		if err != nil {
			return contracts.Output{}, err
		}
		if _, err = queries.IAMLockMachineAccount(ctx, dbgen.IAMLockMachineAccountParams{TenantID: actor.TenantID, ID: request.AccountID}); err != nil {
			return contracts.Output{}, safe(err)
		}
		subject, err := queries.IAMMachineSubject(ctx, dbgen.IAMMachineSubjectParams{TenantID: actor.TenantID, AccountID: request.AccountID})
		if err != nil {
			return contracts.Output{}, safe(err)
		}
		if _, err = queries.IAMRevokeMachineCredentials(ctx, dbgen.IAMRevokeMachineCredentialsParams{TenantID: actor.TenantID, AccountID: request.AccountID}); err != nil {
			return contracts.Output{}, safe(err)
		}
		if !rotate {
			return machineResult(machineRevoked, "security_principal", subject.PrincipalID.String(), request.AccountID, map[string]any{"id": request.AccountID}), nil
		}
		if (subject.Kind == "agent") != (request.Audience == AgentAudience) || !validMachineCapabilities(request.Capabilities) {
			return contracts.Output{}, problem("invalid")
		}
		capabilities, err := json.Marshal(request.Capabilities)
		if err != nil {
			return contracts.Output{}, safe(err)
		}
		secret, err := randomToken()
		if err != nil {
			return contracts.Output{}, safe(err)
		}
		secret = machineTokenPrefix + secret
		credential, err := queries.IAMIssueMachineCredential(ctx, dbgen.IAMIssueMachineCredentialParams{
			TenantID: actor.TenantID, AccountID: request.AccountID, IssuerID: actor.Account.ID,
			Audience: request.Audience, Capabilities: capabilities,
			Minutes: request.Minutes, TokenHash: tokenHash(secret),
		})
		if err != nil {
			return contracts.Output{}, safe(err)
		}
		return machineResult(machineRotated, "machine_credential", credential.ID.String(), request.AccountID, map[string]any{
			"id": credential.ID.String(), "secret": secret, "expiresAt": credential.ExpiresAt.Time.UTC().Format(time.RFC3339),
		}), nil
	}
}

func validMachineCapabilities(capabilities []string) bool {
	seen := make(map[string]bool, len(capabilities))
	for _, name := range capabilities {
		if !machineCapabilityPattern.MatchString(name) || seen[name] {
			return false
		}
		seen[name] = true
	}
	return len(seen) > 0
}

func machineResult(event, entity, id string, accountID int64, data map[string]any) contracts.Output {
	return contracts.Output{Data: data, Events: []contracts.Event{{
		Type: event, AggregateType: entity, AggregateID: id, Payload: map[string]any{"accountId": accountID},
	}}}
}

func (s *Service) AuthenticateMachine(ctx context.Context, secret, delegationID string) (*Principal, error) {
	encoded, ok := strings.CutPrefix(secret, machineTokenPrefix)
	if !ok || len(encoded) != 64 {
		return nil, problem("unauthenticated")
	}
	if _, err := hex.DecodeString(encoded); err != nil {
		return nil, problem("unauthenticated")
	}
	audience := CapabilityAudience
	if delegationID != "" {
		audience = AgentAudience
	}
	principal, err := s.machinePrincipal(ctx, tokenHash(secret), audience)
	if err == nil && delegationID != "" {
		return s.attachAgentDelegation(ctx, principal, delegationID)
	}
	return principal, err
}

func (s *Service) machinePrincipal(ctx context.Context, hash, audience string) (*Principal, error) {
	credential, err := s.repo.q.IAMRestoreMachineCredential(ctx, dbgen.IAMRestoreMachineCredentialParams{TokenHash: hash, Audience: audience})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, problem("unauthenticated")
	}
	if err != nil {
		return nil, safe(err)
	}
	var capabilities []string
	if json.Unmarshal(credential.Capabilities, &capabilities) != nil || !validMachineCapabilities(capabilities) {
		return nil, problem("unauthenticated")
	}
	issuer, err := s.PrincipalForTask(ctx, credential.TenantID, credential.IssuerID)
	if err != nil {
		return nil, problem("unauthenticated")
	}
	base, err := issuer.delegable()
	if err != nil || !base.Can("access:manage") {
		return nil, problem("unauthenticated")
	}
	account, err := s.repo.authorityAccount(ctx, base.TenantID, credential.AccountID)
	if err != nil || account.IsAdmin || account.Status != "active" {
		return nil, problem("unauthenticated")
	}
	account = accountForTenantContext(account)
	machine, err := newPrincipal(account, credential.TenantID, credential.PrincipalID.String(), account.Overrides, s.permissionCodes())
	if err != nil {
		return nil, safe(err)
	}
	var grants []*genidentity.PermissionGrant
	for _, grant := range machine.PermanentGrants {
		scope, err := s.scope(ctx, machine, grant.Permission)
		if err == nil && s.withinApprovalScope(ctx, base, grant.Permission, scope) == nil {
			grants = append(grants, grant)
		}
	}
	machine, err = newPrincipal(account, credential.TenantID, credential.PrincipalID.String(), grants, s.permissionCodes())
	if err != nil {
		return nil, safe(err)
	}
	machine.MembershipID = credential.MembershipID.String()
	machine.machine = &machineContext{credentialID: credential.ID.String(), tokenHash: hash, audience: audience,
		kind: credential.Kind, capabilities: capabilities, issuer: issuer}
	return machine, nil
}

func (s *Service) RefreshMachine(ctx context.Context, current *Principal, lock bool) (*Principal, error) {
	if current.machine == nil {
		return nil, problem("unauthenticated")
	}
	if lock {
		if _, err := s.repo.q.IAMShareMachineCredential(ctx, current.machine.tokenHash); err != nil {
			return nil, problem("unauthenticated")
		}
	}
	principal, err := s.machinePrincipal(ctx, current.machine.tokenHash, current.machine.audience)
	if err != nil || principal.MachineCredentialID() != current.MachineCredentialID() || principal.TenantID != current.TenantID ||
		principal.Account.ID != current.Account.ID ||
		principal.SubjectID != current.SubjectID || principal.MembershipID != current.MembershipID {
		return nil, problem("unauthenticated")
	}
	if delegationID := current.AgentDelegationID(); delegationID != "" {
		return s.attachAgentDelegation(ctx, principal, delegationID)
	}
	return principal, nil
}

func (s *Service) PrincipalForMachineInvocation(ctx context.Context, tenant string, accountID int64, credentialID, delegationID string) (*Principal, error) {
	return s.principalForMachineCredential(ctx, tenant, accountID, credentialID, delegationID)
}

// PrincipalForScheduledService restores a service from its stored credential.
func (s *Service) PrincipalForScheduledService(ctx context.Context, tenant string, accountID int64, credentialID string) (*Principal, error) {
	principal, err := s.principalForMachineCredential(ctx, tenant, accountID, credentialID, "")
	if err != nil || principal.machine.kind != "service" {
		return nil, problem("forbidden")
	}
	return principal, nil
}

func (s *Service) principalForMachineCredential(ctx context.Context, tenant string, accountID int64, credentialID, delegationID string) (*Principal, error) {
	var credential pgtype.UUID
	if credential.Scan(credentialID) != nil || !credential.Valid {
		return nil, problem("forbidden")
	}
	hash, err := s.repo.q.IAMMachineCredentialHash(ctx, dbgen.IAMMachineCredentialHashParams{
		TenantID: tenant, AccountID: accountID, CredentialID: credential,
	})
	if err != nil {
		return nil, problem("forbidden")
	}
	audience := CapabilityAudience
	if delegationID != "" {
		audience = AgentAudience
	}
	principal, err := s.machinePrincipal(ctx, hash, audience)
	if err != nil {
		return nil, problem("forbidden")
	}
	if delegationID != "" {
		return s.attachAgentDelegation(ctx, principal, delegationID)
	}
	return principal, nil
}

func (s *Service) AuthorizeMachineIssuer(ctx context.Context, queries *dbgen.Queries, input AuthorizationInput) bool {
	if input.Principal.machine == nil {
		return false
	}
	machine := input.Principal.machine
	input.Principal = machine.issuer
	decision, err := s.AuthorizeAction(ctx, queries, input)
	if err != nil || !decision.Allowed {
		return false
	}
	if machine.delegation != nil {
		input.Principal = machine.delegation.delegator
		decision, err = s.AuthorizeAction(ctx, queries, input)
	}
	return err == nil && decision.Allowed
}

// ServiceSchedulePrincipal checks that the administrator owns the credential
// issuance context before it can be bound to a new schedule.
func (s *Service) ServiceSchedulePrincipal(ctx context.Context, queries *dbgen.Queries, accountID int64, credentialID, password string) (*Principal, error) {
	actor, err := s.machineAdministrator(ctx, queries, password)
	if err != nil {
		return nil, err
	}
	bound := s.WithReadQueries(queries)
	service, err := bound.PrincipalForScheduledService(ctx, actor.TenantID, accountID, credentialID)
	if err != nil || service.machine.issuer.SubjectID != actor.SubjectID ||
		service.machine.issuer.Account.ID != actor.Account.ID {
		return nil, problem("forbidden")
	}
	return bound.RefreshMachine(ctx, service, true)
}
