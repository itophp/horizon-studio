package identity

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/capability/registry"
	"horizon/internal/repository/dbgen"
)

func (s *Service) bindingManagementEntries() []registry.Entry {
	const entity = "identity_binding"
	tags := []string{"identity", "manual-only"}
	origins := []contracts.Origin{contracts.OriginHumanUI}
	item := contracts.Object(map[string]*contracts.Schema{
		"id": contracts.Str(36, 36), "principalId": contracts.Str(36, 36), "identityType": {Type: "string"},
		"identityVersion": {Type: "string"}, "entityId": {Type: "integer"}, "status": {Type: "string"},
		"source": {Type: "string"}, "verificationLevel": {Type: "string"}, "validUntil": {Type: "string"}, "createdAt": {Type: "string"},
	}, "id", "principalId", "identityType", "identityVersion", "entityId", "status", "source", "verificationLevel", "validUntil", "createdAt")
	list := readDefinition("identity.binding.list", "List identity bindings", "Read verified and inactive bindings for the current workspace.",
		entity, "access:manage", contracts.Object(map[string]*contracts.Schema{
			"items": contracts.Array(item, 0, 50), "nextAfter": contracts.Str(0, 36),
		}, "items", "nextAfter"))
	list.Input = contracts.Object(map[string]*contracts.Schema{"after": contracts.Str(36, 36)})
	list.TransientResult, list.Tags, list.AllowedOrigins = true, tags, origins
	change := func(operation, next string, previous ...string) registry.Entry {
		name := "identity.binding." + operation
		event := name + ".completed"
		d := identityWrite(name, "Manage identity binding", "Change the state of a binding verified by a registered identity provider.",
			entity, "access:manage", []string{event}, contracts.Object(map[string]*contracts.Schema{
				"id": contracts.Str(36, 36), "reason": contracts.Str(5, 500),
			}, "id", "reason"), "id", "reason")
		d.Output = contracts.Object(map[string]*contracts.Schema{"id": contracts.Str(36, 36)}, "id")
		d.Tags, d.AllowedOrigins = tags, origins
		return registry.Entry{Definition: d, Handler: contracts.Handler{ExecuteTx: s.changeBinding(event, next, previous)}}
	}
	return []registry.Entry{
		{Definition: list, Handler: contracts.Handler{Execute: s.managedBindings}},
		change("approve", "active", "pending"),
		change("suspend", "suspended", "active"),
		change("remove", "revoked", "pending", "active", "suspended", "expired"),
	}
}

func (s *Service) managedBindings(ctx context.Context, input contracts.Input) (contracts.Output, error) {
	actor, err := s.workspaceIAMAdmin(ctx, s.repo.q)
	if err != nil {
		return contracts.Output{}, err
	}
	var request struct {
		After string `json:"after"`
	}
	var after pgtype.UUID
	if err = contracts.Bind(input, &request); err != nil || request.After != "" && after.Scan(request.After) != nil {
		return contracts.Output{}, problem("invalid")
	}
	rows, err := s.repo.q.IAMManagedBindings(ctx, dbgen.IAMManagedBindingsParams{TenantID: actor.TenantID, AfterID: after})
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
		items = append(items, map[string]any{
			"id": row.ID.String(), "principalId": row.PrincipalID.String(), "identityType": row.IdentityType, "identityVersion": row.IdentityVersion,
			"entityId": row.EntityID, "status": row.Status, "source": row.Source, "verificationLevel": row.VerificationLevel,
			"validUntil": timeValue(row.ValidUntil), "createdAt": timeValue(row.CreatedAt),
		})
	}
	return contracts.Output{Data: map[string]any{"items": items, "nextAfter": next}}, nil
}

func (s *Service) changeBinding(event, next string, previous []string) contracts.TxExecutor {
	return func(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
		var request struct {
			ID     string `json:"id"`
			Reason string `json:"reason"`
		}
		var id pgtype.UUID
		if err := contracts.Bind(input, &request); err != nil || id.Scan(request.ID) != nil || !id.Valid || len(strings.TrimSpace(request.Reason)) < 5 {
			return contracts.Output{}, problem("invalid")
		}
		q := dbgen.New(tx)
		actor, err := s.workspaceIAMAdmin(ctx, q)
		if err != nil {
			return contracts.Output{}, err
		}
		_, subject, err := sessionLoginSubject(actor)
		if err != nil {
			return contracts.Output{}, err
		}
		id, err = q.IAMManageBinding(ctx, dbgen.IAMManageBindingParams{
			TenantID: actor.TenantID, BindingID: id, ActorID: subject, NextStatus: next, PreviousStates: previous,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return contracts.Output{}, problem("forbidden")
		}
		if err != nil {
			return contracts.Output{}, safe(err)
		}
		out := bindingOutput(id.String(), event)
		out.Events[0].Payload = map[string]any{"reason": strings.TrimSpace(request.Reason), "status": next}
		return out, nil
	}
}
