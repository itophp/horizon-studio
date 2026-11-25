package gateway

import (
	"context"
	"encoding/json"
	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/capability/registry"
	"horizon/foundation/system/identity"
	"horizon/internal/repository/dbgen"
	"slices"
	"sort"
	"strings"
)

// MachineDirectEntry checks whether a capability can execute as a bounded machine action.
func MachineDirectEntry(entry registry.Entry) bool {
	definition := entry.Definition
	return definition.MachineCallable && definition.Layer == contracts.LayerAtomic && definition.Permission != "" && definition.Domain != "identity" &&
		!slices.Contains(definition.Tags, "manual-only") && !slices.Contains(definition.Tags, "system-only") &&
		!slices.Contains(definition.Tags, "internal-only") &&
		!definition.TransientResult && entry.Handler.ExecuteTx != nil &&
		definition.Risk != contracts.RiskCritical && definition.Approval != contracts.ApprovalForbidden
}

func serviceAutomationTarget(entry registry.Entry) bool {
	definition := entry.Definition
	return MachineDirectEntry(entry) && definition.Risk == contracts.RiskLow &&
		definition.Approval == contracts.ApprovalAuto && slices.Contains(definition.AllowedOrigins, contracts.OriginAutomation)
}

// ValidateServiceAutomationTarget validates a schedule without executing it.
// The worker still repeats all Gateway checks for each occurrence.
func (g *Gateway) ValidateServiceAutomationTarget(p *identity.Principal, name, version string, input contracts.Input) error {
	entry, ok := g.registry.Resolve(name, version)
	if !ok || version == "" || !serviceAutomationTarget(entry) ||
		!p.MachineAllows(entry.Definition, input, contracts.OriginAutomation) || !p.Can(entry.Definition.Permission) {
		return contracts.Problem("forbidden")
	}
	if err := entry.Definition.Input.Validate(input); err != nil {
		return contracts.Problem("invalid")
	}
	return checkInputAccess(p, entry.Definition, input)
}

func (g *Gateway) available(ctx context.Context, q *dbgen.Queries, p *identity.Principal, d contracts.Definition, lock bool) error {
	if d.Name != "" && p.ActingOrganization.OrganizationID != 0 && !slices.Contains(d.Tags, contracts.OrganizationScopedTag) {
		return contracts.Problem("forbidden")
	}
	if d.Installation == "" {
		return nil
	}
	var status string
	if lock {
		row, err := q.RuntimeInstallationReadLock(ctx, dbgen.RuntimeInstallationReadLockParams{TenantID: p.TenantID, PluginID: d.Installation})
		if err != nil {
			return contracts.Problem("unavailable")
		}
		status = row.Status
	} else {
		row, err := q.RuntimeInstallation(ctx, dbgen.RuntimeInstallationParams{TenantID: p.TenantID, PluginID: d.Installation})
		if err != nil {
			return contracts.Problem("unavailable")
		}
		status = row.Status
	}
	if status != "enabled" {
		return contracts.Problem("unavailable")
	}
	if d.Application != "" {
		row, err := q.RuntimeApplication(ctx, dbgen.RuntimeApplicationParams{TenantID: p.TenantID, ApplicationID: d.Application})
		if err != nil || !row.Enabled {
			return contracts.Problem("unavailable")
		}
		head, e := q.RuntimeDefinitionHead(ctx, dbgen.RuntimeDefinitionHeadParams{TenantID: p.TenantID, ApplicationID: d.Application})
		if e != nil || head.Compatibility != "compatible" || head.PublishedVersion == "" {
			return contracts.Problem("unavailable")
		}
	}
	return nil
}
func checkInputAccess(p *identity.Principal, d contracts.Definition, input contracts.Input) error {
	for _, rule := range d.InputAccess {
		if !p.Can(rule.Permission) && hasPath(input, strings.Split(rule.Path, ".")) {
			return contracts.Problem("forbidden")
		}
	}
	return nil
}
func hasPath(v any, parts []string) bool {
	if len(parts) == 0 {
		return true
	}
	switch x := v.(type) {
	case contracts.Input:
		return hasPath(map[string]any(x), parts)
	case map[string]any:
		n, ok := x[parts[0]]
		return ok && hasPath(n, parts[1:])
	case []any:
		for _, n := range x {
			if hasPath(n, parts) {
				return true
			}
		}
	}
	return false
}
func projectOutput(p *identity.Principal, d contracts.Definition, value any, masks ...string) (any, error) {
	if len(d.OutputAccess) == 0 && len(masks) == 0 {
		return value, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, contracts.Problem("internal")
	}
	var out any
	if json.Unmarshal(raw, &out) != nil {
		return nil, contracts.Problem("internal")
	}
	for _, rule := range d.OutputAccess {
		if !p.Can(rule.Permission) {
			removePath(out, strings.Split(rule.Path, "."))
		}
	}
	for _, path := range masks {
		removePath(out, strings.Split(path, "."))
	}
	return out, nil
}
func removePath(v any, parts []string) {
	if len(parts) == 0 {
		return
	}
	switch x := v.(type) {
	case map[string]any:
		if len(parts) == 1 {
			delete(x, parts[0])
		} else {
			removePath(x[parts[0]], parts[1:])
		}
	case []any:
		for _, item := range x {
			removePath(item, parts)
		}
	}
}

// DefinitionsFor resolves pinned tool references, including retained versions.
// It never upgrades a requested tool to the catalog's newest version.
func (g *Gateway) DefinitionsFor(ctx context.Context, keys []string) ([]contracts.Definition, error) {
	p, err := identity.Current(ctx)
	if err != nil {
		return nil, err
	}
	disabled, err := g.disabledResources(ctx, p)
	if err != nil {
		return nil, err
	}
	out := []contracts.Definition{}
	for _, key := range keys {
		name, version, ok := strings.Cut(key, "@")
		if !ok {
			return nil, contracts.Problem("invalid")
		}
		entry, ok := g.registry.Resolve(name, version)
		if !ok {
			return nil, contracts.Problem("invalid")
		}
		d := entry.Definition
		if disabled[d.Entity] || !callerOf(p).Allowed(d.Permission) || !d.AllowsOrigin(contracts.OriginAgent) || g.available(ctx, dbgen.New(g.pool), p, d, false) != nil {
			continue
		}
		d.Input = contracts.AuthorizedSchema(d.Input, d.InputAccess, p.Can)
		if d.Input == nil {
			continue
		}
		d.Output = contracts.AuthorizedSchema(d.Output, d.OutputAccess, p.Can)
		if d.Output == nil {
			continue
		}
		out = append(out, d)
	}
	return out, nil
}

func (g *Gateway) disabledResources(ctx context.Context, p *identity.Principal) (map[string]bool, error) {
	if g.identity == nil || g.pool == nil {
		return nil, nil
	}
	rows, err := dbgen.New(g.pool).IAMDisabledResources(ctx, p.TenantID)
	if err != nil {
		return nil, contracts.Problem("unavailable")
	}
	disabled := make(map[string]bool, len(rows))
	for _, resource := range rows {
		disabled[resource] = true
	}
	return disabled, nil
}

func entrySources(entry registry.Entry, input contracts.Input) ([]contracts.Activation, error) {
	refs := []contracts.Activation{}
	if entry.Definition.Installation != "" {
		refs = append(refs, contracts.Activation{Installation: entry.Definition.Installation, Application: entry.Definition.Application})
	}
	if entry.Handler.ActivationSources != nil {
		other, err := entry.Handler.ActivationSources(input)
		if err != nil {
			return nil, err
		}
		refs = append(refs, other...)
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Installation == refs[j].Installation {
			return refs[i].Application < refs[j].Application
		}
		return refs[i].Installation < refs[j].Installation
	})
	return refs, nil
}
func (g *Gateway) checkEntry(ctx context.Context, q *dbgen.Queries, p *identity.Principal, entry registry.Entry, request Request, lock bool) error {
	definition := entry.Definition
	if p.ActingOrganization.OrganizationID != 0 && !slices.Contains(definition.Tags, contracts.OrganizationScopedTag) {
		return contracts.Problem("forbidden")
	}
	if slices.Contains(definition.Tags, identity.PlatformOperatorTag) && (g.identity == nil || !g.identity.IsPlatformOperator(p)) {
		if !lock {
			g.observeDenied(ctx, p, request, definition.Name, "platform_authority")
		}
		return contracts.Problem("forbidden")
	}
	origin := request.Origin
	if request.authorizationOrigin != "" {
		origin = request.authorizationOrigin
	}
	if p.MachineCredentialID() != "" && (!p.MachineAllows(definition, request.Input, origin) || !MachineDirectEntry(entry) || !definition.AllowsOrigin(origin)) {
		return contracts.Problem("forbidden")
	}
	if g.identity != nil && definition.Domain != "identity" {
		if lock {
			if err := q.IAMResourceReadLock(ctx, dbgen.IAMResourceReadLockParams{TenantID: p.TenantID, Resource: definition.Entity}); err != nil {
				return contracts.Problem("unavailable")
			}
		}
		if p.Account.ID == 0 {
			enabled, err := q.IAMResourceEnabled(ctx, dbgen.IAMResourceEnabledParams{TenantID: p.TenantID, Resource: definition.Entity})
			if err != nil {
				return contracts.Problem("unavailable")
			}
			if !enabled.Valid || !enabled.Bool {
				return contracts.Problem("forbidden")
			}
		}
	}
	if g.identity != nil && p.Account.ID != 0 && !strings.HasPrefix(definition.Name, "identity.policy.") {
		if lock {
			if err := q.IAMPolicyReadLock(ctx, p.TenantID); err != nil {
				return contracts.Problem("unavailable")
			}
		}
		authorization := identity.AuthorizationInput{
			Principal: p, Action: definition.Name, Permission: definition.Permission, Resource: definition.Entity, Environment: origin,
		}
		authorization.ApplyCapabilityFields(definition)
		resourceID := definition.ResourceAttributeID
		if resourceID == "" {
			resourceID = definition.DelegationResourceID
		}
		if resourceID != "" {
			var target struct{ ID int64 }
			if err := contracts.Bind(contracts.Input{"ID": request.Input[resourceID]}, &target); err == nil {
				authorization.ResourceID = target.ID
			}
		}
		if p.MachineCredentialID() != "" && (!g.identity.AuthorizeMachineIssuer(ctx, q, authorization) ||
			!g.identity.AuthorizeDelegatedBinding(ctx, q, p, definition, authorization.ResourceID, lock)) {
			if !lock {
				g.observeDenied(ctx, p, request, definition.Name, "machine_issuer")
			}
			return contracts.Problem("forbidden")
		}
		decision, err := g.identity.AuthorizeAction(ctx, q, authorization)
		if err != nil || !decision.Allowed {
			if !lock {
				g.observeDenied(ctx, p, request, definition.Name, decision.Reason)
			}
			if err != nil {
				return contracts.Problem("unavailable")
			}
			return contracts.Problem("forbidden")
		}
		if request.authorization != nil {
			*request.authorization = decision
		}
	}
	refs, err := entrySources(entry, request.Input)
	if err != nil {
		return err
	}
	for _, ref := range refs {
		if err = g.available(ctx, q, p, contracts.Definition{Installation: ref.Installation, Application: ref.Application}, lock); err != nil {
			return err
		}
	}
	return nil
}
