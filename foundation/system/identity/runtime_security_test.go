package identity_test

import (
	"testing"

	ar "horizon/foundation/kernel/applicationruntime"
	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/system/identity"
)

func TestSecurityRuntimeApplicationAssembles(t *testing.T) {
	service := identity.New(nil, false)
	definitions := []contracts.Definition{}
	for _, entry := range service.Entries() {
		definitions = append(definitions, entry.Definition)
	}
	security := identity.SecurityRuntimeApplication()
	if _, _, err := ar.AssembleDeployment([]ar.ApplicationV2{security}, identity.Entities(), definitions); err != nil {
		t.Fatal(err)
	}
	if len(security.Protocol.Providers) != 0 || security.Permission != ar.AuthenticatedPermission {
		t.Fatal("security renderer must keep personal secrets out of provider projections")
	}
}

func TestGovernanceRuntimeApplicationAssembles(t *testing.T) {
	service := identity.New(nil, false)
	definitions := []contracts.Definition{}
	for _, entry := range service.Entries() {
		definitions = append(definitions, entry.Definition)
	}
	app := identity.GovernanceRuntimeApplication()
	if _, _, err := ar.AssembleDeployment([]ar.ApplicationV2{app}, identity.Entities(), definitions); err != nil {
		t.Fatal(err)
	}
	if len(app.Protocol.Providers) != 0 || app.Permission != "access:manage" {
		t.Fatal("policy governance must require management access and preview must not query")
	}
}
