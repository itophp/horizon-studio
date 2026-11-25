package identityapp

import (
	"context"
	_ "embed"
	"sort"

	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/plugin"
	"horizon/foundation/system/identity"
)

//go:embed plugin.yaml
var manifestYAML []byte

// installation is the persistent deployment key for the identity application.
const installation = "system.identity"

type Plugin struct {
	IAM *identity.Service
}

func (Plugin) Manifest() plugin.Manifest {
	manifest, err := plugin.ParseManifest(manifestYAML)
	if err != nil {
		panic(err)
	}
	return manifest
}

func (p Plugin) Register(_ context.Context, r *plugin.Registrar) error {
	entries := p.Entries()
	r.Entities([]contracts.Entity{accountProfileEntity(), organizationEntity(), departmentEntity(), departmentRoleEntity()})
	r.Capabilities(entries)
	events := make(map[string]struct{})
	for _, entry := range entries {
		for _, event := range entry.Definition.SuccessEvents {
			events[event] = struct{}{}
		}
	}
	names := make([]string, 0, len(events))
	for event := range events {
		names = append(names, event)
	}
	sort.Strings(names)
	r.Events(names...)
	r.Applications(AccountsRuntimeApplication(), OrganizationsRuntimeApplication(), PermissionsRuntimeApplication())
	return nil
}

func (Plugin) Start(context.Context) error { return nil }
func (Plugin) Stop(context.Context) error  { return nil }
