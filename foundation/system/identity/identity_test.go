package identity

import (
	"context"
	genidentity "horizon/foundation/gen/identity"
	"testing"
)

func TestPasswordHashing(t *testing.T) {
	password := "Correct-Horse-2026!"
	first, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	second, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("salt was reused")
	}
	if !VerifyPassword(password, first) || VerifyPassword("wrong", first) || VerifyPassword(password, "not-an-argon-hash") {
		t.Fatal("password verification mismatch")
	}
	if _, err = HashPassword("short"); err == nil {
		t.Fatal("short password accepted")
	}
	if _, err = HashPassword("四个汉字"); err == nil {
		t.Fatal("password policy counted bytes instead of characters")
	}
}
func TestAccountOverridesReplaceRoleScopeAndDenyWins(t *testing.T) {
	account := &genidentity.AccountRecord{ID: 10}
	roles := []*genidentity.PermissionGrant{{Permission: "accounts:read", Effect: "allow", Scope: "ALL"}, {Permission: "accounts:delete", Effect: "allow", Scope: "ALL"}}
	overrides := []*genidentity.PermissionGrant{{Permission: "accounts:read", Effect: "allow", Scope: "OWN"}, {Permission: "accounts:delete", Effect: "deny", Scope: "ALL"}}
	p, err := NewPrincipal(account, "tenant-a", MergeGrants(roles, overrides))
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{}
	scope, err := service.scope(t.Context(), p, "accounts:read")
	if err != nil || scope.All || scope.OwnID != 10 || scope.TenantID != "tenant-a" {
		t.Fatal("account override did not narrow inherited scope", scope, err)
	}
	if _, err := service.scope(t.Context(), p, "accounts:delete"); err == nil {
		t.Fatal("explicit denial bypassed")
	}
}
func TestPolicyUnionsRolesAndRejectsMissingContext(t *testing.T) {
	p, err := NewPrincipal(&genidentity.AccountRecord{ID: 1}, "t", []*genidentity.PermissionGrant{{Permission: "accounts:read", Effect: "allow", Scope: "OWN"}, {Permission: "roles:read", Effect: "allow", Scope: "ALL"}})
	if err != nil {
		t.Fatal(err)
	}
	if !p.Can("accounts:read") || !p.Can("roles:read") || p.Can("accounts:update") {
		t.Fatal("role union is incorrect")
	}
	if _, err = Current(context.Background()); err == nil {
		t.Fatal("untrusted identity accepted")
	}
	if CSRFMatches(p, "") {
		t.Fatal("empty CSRF accepted")
	}
	p.CSRF = "server-nonce"
	if !CSRFMatches(p, "server-nonce") || CSRFMatches(p, "attacker") {
		t.Fatal("CSRF mismatch")
	}
}

func TestGovernanceCatalogDoesNotMakeManagementTemporary(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range PermissionCatalog() {
		if seen[d.Code] {
			t.Fatal("duplicate permission", d.Code)
		}
		seen[d.Code] = true
		if d.Module == "system" {
			expected := d.Code == "files:read" || d.Code == "dictionaries:use"
			if d.TemporaryEligible != expected {
				t.Fatal("management became temporary", d.Code)
			}
			if seedViewerPermission(d.Code) {
				t.Fatal("new privilege entered old viewer seed", d.Code)
			}
		}
	}
	for _, code := range []string{loginSecurityPermission, "executionPlans:read", "executionPlans:edit", "executionPlans:validate", "executionPlans:publish", "executionPlans:bind", "executionPlans:rollback", "executionPlans:records", "orchestration:read", "orchestration:run", "orchestration:cancel", "memory:read", "memory:write", "memory:collect", "memory:replay", "memory:manage", "applications:read", "applications:manage", "aiConfig:read", "aiConfig:update", "knowledge:update"} {
		d := permissionDefinition(code)
		if d == nil || d.TemporaryEligible || d.ScopeConfigurable {
			t.Fatalf("unsafe management permission: %s", code)
		}
	}
	// Business application permissions are plugin catalog data; the central
	// directory must never name them again.
	for _, code := range []string{"articles:read", "drive:use", "mail:read", "projects:read", "example:create"} {
		if permissionDefinition(code) != nil {
			t.Fatalf("business permission returned to the central directory: %s", code)
		}
	}

	for _, code := range []string{"organizations:create", "organizations:delete"} {
		if permissionDefinition(code) != nil {
			t.Fatalf("retired organization permission is still registered: %s", code)
		}
	}
	if len(seen) != 69 {
		t.Fatal("unexpected catalog", len(seen))
	}
}
