package identity

import (
	gen "horizon/foundation/gen/identity"
	"testing"
)

func TestAuthorizationVersionTracksScopeWithoutPermissionCodeChange(t *testing.T) {
	grant := &gen.PermissionGrant{Permission: "accounts:read", Effect: "allow", Scope: "OWN"}
	p, err := NewPrincipal(&gen.AccountRecord{ID: 1}, "tenant-a", []*gen.PermissionGrant{grant})
	if err != nil {
		t.Fatal(err)
	}
	before := authorizationVersion(p)
	if len(before) != 64 || !p.Can("accounts:read") {
		t.Fatal("invalid initial revision")
	}
	grant.Scope = "ALL"
	after := authorizationVersion(p)
	if before == after || !p.Can("accounts:read") {
		t.Fatal("scope-only change was not versioned")
	}
	p.CSRF = "secret-nonce"
	p.TokenHash = "private-token"
	p.Account.FullName = "Private name"
	if authorizationVersion(p) != after {
		t.Fatal("non-authorization data entered revision")
	}
	p.TemporaryScopes = map[string][]Scope{"accounts:read": {{TenantID: "tenant-a", OwnID: 1}}}
	if authorizationVersion(p) == after {
		t.Fatal("temporary scope missing from revision")
	}
	projected := profile(p)
	if projected.AuthorizationVersion == nil || *projected.AuthorizationVersion != authorizationVersion(p) {
		t.Fatal("session projection missing revision")
	}
}
