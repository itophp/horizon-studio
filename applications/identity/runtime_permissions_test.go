package identityapp_test

import (
	"horizon/applications/identity"
	ar "horizon/foundation/kernel/applicationruntime"
	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/system/identity"
	"testing"
)

func TestPermissionsRuntimeApplicationAssembles(t *testing.T) {
	service := identity.New(nil, false)
	definitions := []contracts.Definition{}
	for _, entry := range append(service.Entries(), (identityapp.Plugin{IAM: service}).Entries()...) {
		definitions = append(definitions, entry.Definition)
	}
	app := identityapp.PermissionsRuntimeApplication()
	if _, _, err := ar.AssembleDeployment([]ar.ApplicationV2{app}, identity.Entities(), definitions); err != nil {
		t.Fatal(err)
	}
}

func TestPermissionsProtocolPageExposesFormCapabilities(t *testing.T) {
	app := identityapp.PermissionsRuntimeApplication()
	capabilities := map[string]bool{}
	for _, page := range app.Protocol.Pages {
		for _, ref := range ar.ProtocolPageCapabilityRefs(page) {
			capabilities[ref.Name+"@"+ref.Version] = true
		}
	}
	for _, name := range []string{
		"identity.role.update@1.0.0",
		"identity.roleGrant.create@1.0.0",
		"identity.role.delete@1.0.0",
	} {
		if !capabilities[name] {
			t.Fatal(name, capabilities)
		}
	}
}
