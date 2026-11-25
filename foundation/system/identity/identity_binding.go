package identity

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/eventhub"
	"horizon/foundation/kernel/plugin"
	"horizon/internal/repository/dbgen"
)

const (
	bindingClaim   = "identity.binding.claim"
	bindingRecord  = "identity.binding.record"
	bindingCreated = "identity.binding.created"
	bindingRevoked = "identity.binding.revoked"
)

type bindingProviders struct {
	types  map[string]plugin.IdentityType
	invoke func(context.Context, string, string, contracts.Input, string) (*contracts.Result, error)
}

func (s *Service) AttachBindingProviders(types []plugin.IdentityType, invoke func(context.Context, string, string, contracts.Input, string) (*contracts.Result, error)) {
	s.bindings = bindingProviders{types: make(map[string]plugin.IdentityType, len(types)), invoke: invoke}
	for _, identityType := range types {
		s.bindings.types[identityType.Code+"@"+identityType.Version] = identityType
	}
}

type bindingClaimInput struct {
	IdentityType string `json:"identityType"`
	Version      string `json:"version"`
	EntityID     int64  `json:"entityId"`
}

func bindingDefinition(name string, events []string) contracts.Definition {
	definition := identityWrite(name, name, "Register or revoke a verified plugin identity binding.",
		"identity_binding", "", events, contracts.Object(map[string]*contracts.Schema{
			"identityType": contracts.Str(1, 200), "version": contracts.Str(1, 40), "entityId": contracts.Int(1, 1<<53-1),
		}, "identityType", "version", "entityId"), "identityType", "version", "entityId")
	definition.Output = contracts.Object(map[string]*contracts.Schema{"id": {Type: "string"}}, "id")
	definition.Risk = contracts.RiskLow
	definition.Approval = contracts.ApprovalAuto
	definition.AllowedOrigins = []contracts.Origin{contracts.OriginHumanUI}
	definition.Tags = []string{"identity", "manual-only", contracts.AuthenticatedActionTag}
	return definition
}

func identityBindingVerifyDefinition() contracts.Definition {
	definition := bindingDefinition(bindingClaim, []string{bindingClaim + ".completed"})
	definition.Layer = contracts.LayerOrchestration
	return definition
}

func identityBindingCreateDefinition() contracts.Definition {
	definition := bindingDefinition(bindingRecord, []string{bindingCreated})
	definition.AllowedOrigins = []contracts.Origin{contracts.OriginCapability}
	definition.Tags = []string{"internal-only", "parent:" + bindingClaim + "@" + identityVersion}
	return definition
}

func identityBindingRevokeDefinition() contracts.Definition {
	definition := bindingDefinition("identity.binding.revoke", []string{bindingRevoked})
	definition.Input = contracts.Object(map[string]*contracts.Schema{"id": contracts.Str(36, 36)}, "id")
	definition.Loggable = []string{"id"}
	return definition
}

func (s *Service) verifyIdentityBinding(ctx context.Context, input contracts.Input) (contracts.Output, error) {
	var claim bindingClaimInput
	if err := contracts.Bind(input, &claim); err != nil {
		return contracts.Output{}, err
	}
	present, err := Current(ctx)
	if err != nil || present.TokenHash == "" {
		return contracts.Output{}, contracts.Problem("unauthenticated")
	}
	provider, exists := s.bindings.types[claim.IdentityType+"@"+claim.Version]
	if !exists || s.bindings.invoke == nil {
		return contracts.Output{}, contracts.Problem("forbidden")
	}
	verified, err := s.bindings.invoke(ctx, provider.Verify, provider.VerifyVersion, contracts.Input{"id": claim.EntityID}, "")
	if err != nil || verified == nil || verified.Status != contracts.StatusSucceeded {
		return contracts.Output{}, contracts.Problem("forbidden")
	}
	data, ok := verified.Data.(map[string]any)
	if !ok || data["ready"] != true {
		return contracts.Output{}, contracts.Problem("forbidden")
	}
	if provider.OrganizationContext {
		candidate := &Principal{TenantID: present.TenantID, SubjectID: present.SubjectID,
			IdentityType: claim.IdentityType, IdentityEntityID: claim.EntityID}
		if err := s.resolveOrganizationContext(ctx, candidate, claim.Version); err != nil {
			return contracts.Output{}, contracts.Problem("forbidden")
		}
	}
	key := contracts.Digest(contracts.Input{"subject": present.SubjectID, "tenant": present.TenantID, "binding": input})
	result, err := s.bindings.invoke(ctx, bindingRecord, identityVersion, input, key)
	if err != nil {
		return contracts.Output{}, err
	}
	if result == nil {
		return contracts.Output{}, contracts.Problem("unavailable")
	}
	if result.Status != contracts.StatusSucceeded {
		return contracts.Output{}, contracts.Problem(result.ErrorCode)
	}
	binding, ok := result.Data.(map[string]any)
	if !ok {
		return contracts.Output{}, contracts.Problem("internal")
	}
	id, ok := binding["id"].(string)
	if !ok {
		return contracts.Output{}, contracts.Problem("internal")
	}
	return contracts.Output{Data: binding, Events: []contracts.Event{{Type: bindingClaim + ".completed", AggregateType: "identity_binding", AggregateID: id}}}, nil
}

func (s *Service) createIdentityBinding(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
	var claim bindingClaimInput
	if err := contracts.Bind(input, &claim); err != nil {
		return contracts.Output{}, err
	}
	present, err := Current(ctx)
	if err != nil || present.TokenHash == "" {
		return contracts.Output{}, contracts.Problem("unauthenticated")
	}
	provider, exists := s.bindings.types[claim.IdentityType+"@"+claim.Version]
	if !exists {
		return contracts.Output{}, contracts.Problem("forbidden")
	}
	if provider.OrganizationContext {
		candidate := &Principal{TenantID: present.TenantID, SubjectID: present.SubjectID,
			IdentityType: claim.IdentityType, IdentityEntityID: claim.EntityID}
		if err := s.WithReadQueries(dbgen.New(tx)).resolveOrganizationContext(ctx, candidate, claim.Version); err != nil {
			return contracts.Output{}, contracts.Problem("forbidden")
		}
	}
	status := "active"
	if provider.ReviewRequired {
		status = "pending"
	}
	id, err := dbgen.New(tx).IAMCreateIdentityBinding(ctx, dbgen.IAMCreateIdentityBindingParams{
		TenantID: present.TenantID, AccountID: present.Account.ID, IdentityType: claim.IdentityType,
		IdentityVersion: claim.Version, EntityID: claim.EntityID, RevocationEvent: provider.RevocationEvent,
		Status: status, ValidMinutes: int32(provider.ValidForMinutes),
	})
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	return bindingOutput(id.String(), bindingCreated), nil
}

func (s *Service) revokeIdentityBinding(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
	present, err := Current(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	_, subjectID, err := sessionLoginSubject(present)
	if err != nil {
		return contracts.Output{}, err
	}
	var bindingID pgtype.UUID
	if err = bindingID.Scan(input["id"]); err != nil || !bindingID.Valid {
		return contracts.Output{}, contracts.Problem("invalid")
	}
	id, err := dbgen.New(tx).IAMRevokeOwnIdentityBinding(ctx, dbgen.IAMRevokeOwnIdentityBindingParams{
		TenantID: present.TenantID, PrincipalID: subjectID, ID: bindingID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, contracts.Problem("forbidden")
	}
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	return bindingOutput(id.String(), bindingRevoked), nil
}

func bindingOutput(id, event string) contracts.Output {
	return contracts.Output{
		Data:   map[string]any{"id": id},
		Events: []contracts.Event{{Type: event, AggregateType: "identity_binding", AggregateID: id}},
	}
}

func (s *Service) applyIdentityBinding(ctx context.Context, principal *Principal, id pgtype.UUID) error {
	if !id.Valid {
		return nil
	}
	var subjectID pgtype.UUID
	if err := subjectID.Scan(principal.SubjectID); err != nil || !subjectID.Valid {
		return problem("unauthenticated")
	}
	binding, err := s.repo.q.IAMReadIdentityBinding(ctx, dbgen.IAMReadIdentityBindingParams{
		TenantID: principal.TenantID, PrincipalID: subjectID, ID: id,
	})
	if err != nil {
		return problem("unauthenticated")
	}
	provider, exists := s.bindings.types[binding.IdentityType+"@"+binding.IdentityVersion]
	if !exists {
		return problem("unauthenticated")
	}
	principal.IdentityBindingID = id.String()
	principal.IdentityType = binding.IdentityType
	principal.IdentityEntityID = binding.EntityID
	if provider.OrganizationContext {
		return s.resolveOrganizationContext(ctx, principal, binding.IdentityVersion)
	}
	return nil
}

func (s *Service) RevokeBindingsForEvent(ctx context.Context, event eventhub.Event) {
	if s.repo == nil || event.TenantID == "" {
		return
	}
	entityID, err := strconv.ParseInt(event.AggregateID, 10, 64)
	if err != nil || entityID < 1 {
		return
	}
	for _, provider := range s.bindings.types {
		if provider.RevocationEvent != event.Type {
			continue
		}
		if err = s.repo.q.IAMRevokeIdentityBindingsForEntity(ctx, dbgen.IAMRevokeIdentityBindingsForEntityParams{
			TenantID: event.TenantID, IdentityType: provider.Code, IdentityVersion: provider.Version, EntityID: entityID,
		}); err != nil {
			slog.Error("identity binding revocation failed", "event_id", event.EventID, "provider", provider.Code)
		}
	}
}

func (s *Service) ReconcileBindingRevocations(ctx context.Context) error {
	if s.repo == nil || len(s.bindings.types) == 0 {
		return nil
	}
	if _, err := s.repo.q.IAMExpireIdentityBindings(ctx); err != nil {
		return err
	}
	_, err := s.repo.q.IAMReconcileIdentityBindingRevocations(ctx)
	return err
}

func (s *Service) RunBindingReconciliation(ctx context.Context) {
	if s.repo == nil || len(s.bindings.types) == 0 {
		return
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if err := s.ReconcileBindingRevocations(ctx); err != nil && ctx.Err() == nil {
			slog.Error("identity binding reconciliation failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
