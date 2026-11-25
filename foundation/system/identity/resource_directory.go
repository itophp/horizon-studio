package identity

import (
	"context"
	"slices"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"

	"horizon/foundation/kernel/audit"
	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/capability/registry"
	"horizon/internal/repository/dbgen"
)

func resourceCanBeDisabled(definition contracts.Definition) bool {
	return definition.Domain != "identity" &&
		!slices.Contains(definition.Tags, "system-only") &&
		!slices.Contains(definition.Tags, "durable-system")
}

func (s *Service) resourceEntries() []registry.Entry {
	code := contracts.Str(1, 200)
	strings := &contracts.Schema{Type: "array", Items: &contracts.Schema{Type: "string"}}
	action := contracts.Object(map[string]*contracts.Schema{
		"name": code, "version": code, "module": code, "permission": {Type: "string"},
		"machineCallable": {Type: "boolean"}, "origins": strings, "rowAttributes": strings, "maskedFields": strings,
		"resourceSelector": {Type: "string"}, "attributeProvider": {Type: "boolean"},
	}, "name", "version", "module", "permission", "machineCallable", "origins", "rowAttributes", "maskedFields", "resourceSelector", "attributeProvider")
	item := contracts.Object(map[string]*contracts.Schema{
		"resource": code, "enabled": {Type: "boolean"}, "canDisable": {Type: "boolean"},
		"actions": {Type: "array", Items: action},
	}, "resource", "enabled", "canDisable", "actions")
	list := readDefinition("identity.resource.list", "List authorization resources", "List deployed resource and action contracts with workspace availability.",
		"authorization_resource_state", "access:manage", contracts.Object(map[string]*contracts.Schema{
			"items": contracts.Array(item, 0, 50), "nextAfter": {Type: "string"},
		}, "items", "nextAfter"))
	list.Input = contracts.Object(map[string]*contracts.Schema{"after": code, "search": code})
	change := identityWrite("identity.resource.setStatus", "Change resource availability", "Enable or disable a deployed resource for the current workspace.",
		"authorization_resource_state", "access:manage", []string{"identity.resource.status.changed"},
		contracts.Object(map[string]*contracts.Schema{
			"resource": code, "enabled": {Type: "boolean"}, "reason": contracts.Str(5, 500),
			"password": contracts.Str(1, 128), "factor": contracts.Str(6, 32),
		}, "resource", "enabled", "reason", "password"), "resource", "enabled")
	change.Output = contracts.Object(map[string]*contracts.Schema{"resource": code, "enabled": {Type: "boolean"}}, "resource", "enabled")
	for _, definition := range []*contracts.Definition{&list, &change} {
		definition.Tags = []string{"identity", "manual-only"}
		definition.AllowedOrigins = []contracts.Origin{contracts.OriginHumanUI}
	}
	return []registry.Entry{
		{Definition: list, Handler: contracts.Handler{Execute: s.listResources}},
		{Definition: change, Handler: contracts.Handler{ExecuteTx: s.changeResource}},
	}
}

func (s *Service) listResources(ctx context.Context, input contracts.Input) (contracts.Output, error) {
	actor, err := s.workspaceIAMAdmin(ctx, s.repo.q)
	if err != nil {
		return contracts.Output{}, err
	}
	if s.authorizationCatalog == nil {
		return contracts.Output{}, problem("unavailable")
	}
	disabled, err := s.repo.q.IAMDisabledResources(ctx, actor.TenantID)
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	var request struct{ After, Search string }
	if contracts.Bind(input, &request) != nil {
		return contracts.Output{}, problem("invalid")
	}
	grouped := make(map[string][]contracts.Definition)
	for _, definition := range s.authorizationCatalog.All() {
		grouped[definition.Entity] = append(grouped[definition.Entity], definition)
	}
	names := make([]string, 0, len(grouped))
	search := strings.ToLower(strings.TrimSpace(request.Search))
	for name, definitions := range grouped {
		if name > request.After && (strings.Contains(strings.ToLower(name), search) ||
			slices.ContainsFunc(definitions, func(d contracts.Definition) bool {
				return strings.Contains(strings.ToLower(d.Name), search) || strings.Contains(strings.ToLower(d.Permission), search)
			})) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	next := ""
	if len(names) > 50 {
		names, next = names[:50], names[49]
	}
	items := make([]map[string]any, 0, len(names))
	for _, name := range names {
		definitions := grouped[name]
		actions := make([]map[string]any, 0, len(definitions))
		for _, d := range definitions {
			fields := make([]string, 0, len(d.OutputAccess))
			for _, field := range d.OutputAccess {
				fields = append(fields, field.Path)
			}
			actions = append(actions, map[string]any{
				"name": d.Name, "version": d.Version, "module": d.Domain, "permission": d.Permission,
				"machineCallable": d.MachineCallable, "origins": append([]contracts.Origin{}, d.AllowedOrigins...),
				"rowAttributes": append([]string{}, d.RowFilterAttributes...), "maskedFields": fields,
				"resourceSelector": d.ResourceAttributeID, "attributeProvider": s.resourceAttributes[name] != nil,
			})
		}
		items = append(items, map[string]any{
			"resource": name, "enabled": !slices.Contains(disabled, name), "actions": actions,
			"canDisable": !slices.ContainsFunc(definitions, func(d contracts.Definition) bool { return !resourceCanBeDisabled(d) }),
		})
	}
	return contracts.Output{Data: map[string]any{"items": items, "nextAfter": next}}, nil
}

func (s *Service) changeResource(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
	var request struct {
		Resource, Reason, Password string
		Enabled                    bool
		Factor                     *string
	}
	if contracts.Bind(input, &request) != nil || len(strings.TrimSpace(request.Reason)) < 5 {
		return contracts.Output{}, problem("invalid")
	}
	q := dbgen.New(tx)
	if _, err := s.workspaceIAMAdmin(ctx, q); err != nil {
		return contracts.Output{}, err
	}
	actor, _, _, err := s.associationOwner(ctx, q, request.Password, request.Factor)
	if err != nil {
		return contracts.Output{}, err
	}
	if s.authorizationCatalog == nil {
		return contracts.Output{}, problem("unavailable")
	}
	matches := false
	for _, definition := range s.authorizationCatalog.All() {
		if definition.Entity != request.Resource {
			continue
		}
		matches = true
		if !resourceCanBeDisabled(definition) {
			return contracts.Output{}, problem("forbidden")
		}
	}
	if !matches {
		return contracts.Output{}, problem("not_found")
	}
	if err = q.IAMResourceWriteLock(ctx, dbgen.IAMResourceWriteLockParams{TenantID: actor.TenantID, Resource: request.Resource}); err != nil {
		return contracts.Output{}, safe(err)
	}
	var affected int64
	if request.Enabled {
		affected, err = q.IAMEnableResource(ctx, dbgen.IAMEnableResourceParams{TenantID: actor.TenantID, Resource: request.Resource})
	} else {
		affected, err = q.IAMDisableResource(ctx, dbgen.IAMDisableResourceParams{TenantID: actor.TenantID, Resource: request.Resource})
	}
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	if affected > 0 {
		if err = q.IAMAdvanceResourceRevision(ctx, actor.TenantID); err != nil {
			return contracts.Output{}, safe(err)
		}
	}
	if err = q.SecurityAuditAppend(ctx, dbgen.SecurityAuditAppendParams{
		TenantID: actor.TenantID, AccountID: actor.Account.ID,
		Action: "identity.resource.setStatus", Outcome: "allowed", Reason: strings.TrimSpace(request.Reason),
		CorrelationID: audit.Correlation(ctx), ResourceType: "authorization_resource_state", ResourceID: request.Resource,
	}); err != nil {
		return contracts.Output{}, safe(err)
	}
	data := map[string]any{"resource": request.Resource, "enabled": request.Enabled}
	return contracts.Output{Data: data, Events: []contracts.Event{{
		Type: "identity.resource.status.changed", AggregateType: "authorization_resource_state", AggregateID: request.Resource, Payload: data,
	}}}, nil
}
