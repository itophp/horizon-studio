package studio

import (
	"context"
	_ "embed"
	"sort"

	"horizon/foundation/kernel/plugin"
	"horizon/foundation/system/identity"
)

//go:embed plugin.yaml
var manifestYAML []byte

// Plugin adapts the module's declarations without copying business logic.
type Plugin struct{ Module *Module }

func (p Plugin) Manifest() plugin.Manifest {
	manifest, err := plugin.ParseManifest(manifestYAML)
	if err != nil {
		panic(err)
	}
	return manifest
}

func (p Plugin) Register(_ context.Context, r *plugin.Registrar) error {
	entries := p.Module.Entries()
	r.Capabilities(entries)
	r.Entities(Entities())
	r.Applications(RuntimeApplication())
	permissions, events := map[string]bool{}, map[string]bool{}
	for _, entry := range entries {
		permissions[entry.Definition.Permission] = true
		for _, event := range entry.Definition.SuccessEvents {
			events[event] = true
		}
	}
	names := make([]string, 0, len(permissions))
	for name := range permissions {
		names = append(names, name)
	}
	sort.Strings(names)
	catalog := map[string]plugin.Permission{}
	for _, definition := range identity.PermissionCatalog() {
		catalog[definition.Code] = plugin.Permission{Code: definition.Code, Module: definition.Module, Function: definition.Function, Action: definition.Action, ScopeConfigurable: definition.ScopeConfigurable, TemporaryEligible: definition.TemporaryEligible}
	}
	for _, name := range names {
		r.Permissions(catalog[name])
	}
	eventNames := make([]string, 0, len(events))
	for name := range events {
		eventNames = append(eventNames, name)
	}
	sort.Strings(eventNames)
	r.Events(eventNames...)
	return nil
}

func (p Plugin) AttachModel(generate func(context.Context, string, string) (string, error)) {
	p.Module.generateAI = generate
}

func (p Plugin) Start(context.Context) error { return nil }
func (p Plugin) Stop(context.Context) error  { return nil }
