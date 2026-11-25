//go:build integration

package tests

import (
	"testing"

	gencap "horizon/foundation/gen/capability"
	genidentity "horizon/foundation/gen/identity"
	"horizon/foundation/runtime/app"
	"horizon/foundation/system/governance"
	"horizon/foundation/system/identity"
)

func TestAnalyticsRuntimeGatewayPermissions(t *testing.T) {
	handler, pool, rootAccount := iamFixture(t)
	anonymous := iamClient{handler: handler}
	checkStatus(t, anonymous.invoke(t, "identity.analytics.read", map[string]any{}, false), 401)

	root := loginTenantIAM(t, handler, "root-admin", "tenant-a")
	read := func(client iamClient) map[string]any {
		t.Helper()
		response := client.invoke(t, "identity.analytics.read", map[string]any{"timeZone": "America/New_York"}, true)
		checkSucceeded(t, response)
		return decodeBody[gencap.CapabilityResult](t, response).Data.(map[string]any)
	}
	resolve := func(client iamClient) map[string]any {
		t.Helper()
		response := client.invoke(t, "application.runtime.resolve", map[string]any{"applicationId": "analytics"}, true)
		checkSucceeded(t, response)
		view := decodeBody[gencap.CapabilityResult](t, response).Data.(map[string]any)
		if view["applicationVersion"] != "1.0.1.0" {
			t.Fatal(view)
		}
		return view["protocol"].(map[string]any)
	}
	rootData := read(root)
	for _, field := range []string{"accounts", "organizations", "roles", "departments", "trend", "distribution", "recentAccounts", "activity"} {
		if rootData[field] == nil {
			t.Fatalf("missing authorized field %s", field)
		}
	}

	roleInput := map[string]any{
		"name": "Analytics reader", "code": "analytics-reader", "description": "", "status": "active",
		"grants": []map[string]any{{"permission": "accounts:read", "effect": "allow", "scope": "OWN"}},
	}
	response := root.invoke(t, "identity.role.create", roleInput, true)
	checkSucceeded(t, response)
	role := decodeData[genidentity.RoleRecord](t, response)
	response = root.invoke(t, "identity.account.create", map[string]any{
		"username": "analytics-reader", "fullName": "Analytics Reader", "email": "private@example.test", "initialStatus": "active",
		"password": "TestOnly!2026Secure",
	}, true)
	checkSucceeded(t, response)
	account := decodeData[genidentity.AccountRecord](t, response)
	seedTenantRole(t, pool, "tenant-a", "analytics-base", rootAccount.ID, account.ID, []*genidentity.PermissionGrant{
		{Permission: "example:read", Effect: "allow", Scope: "ALL"},
	})
	checkSucceeded(t, root.invoke(t, "identity.roleGrant.create", map[string]any{
		"accountId": account.ID, "roleId": role.ID, "minutes": 120, "reason": "Verify analytics own scope",
	}, true))
	reader := loginTenantIAM(t, handler, "analytics-reader", "tenant-a")
	protocol := resolve(reader)
	providers := protocol["providers"].([]any)
	if len(providers) != 1 || providers[0].(map[string]any)["permission"] != "authenticated" {
		t.Fatal(providers)
	}
	data := read(reader)
	if len(data) != 4 || data["accounts"].(map[string]any)["total"] != float64(1) ||
		len(data["trend"].([]any)) != 7 || len(data["activity"].([]any)) != 1 {
		t.Fatal(data)
	}
	row := data["recentAccounts"].([]any)[0].(map[string]any)
	if row["username"] != "analytics-reader" || len(row) != 6 {
		t.Fatal(row)
	}

	roleInput["id"] = role.ID
	roleInput["grants"] = []any{}
	checkSucceeded(t, root.invoke(t, "identity.role.update", roleInput, true))
	resolve(reader)
	if denied := read(reader); len(denied) != 1 || len(denied["activity"].([]any)) != 0 {
		t.Fatal("revoked scope remained visible", denied)
	}

	if _, err := identity.Bootstrap(t.Context(), pool, "analytics-other", "analytics-other-admin", "TestOnly!2026Secure"); err != nil {
		t.Fatal(err)
	}
	otherIAM := identity.New(pool, false)
	otherHandler := deploymentHandler(app.System{}, governance.New(pool, otherIAM, governance.Options{
		EncryptionKey: []byte("01234567890123456789012345678901"), PublicWorkspace: "analytics-other",
	}), otherIAM)
	other := loginTenantIAM(t, otherHandler, "analytics-other-admin", "analytics-other")
	resolve(other)
	otherData := read(other)
	if otherData["accounts"].(map[string]any)["total"] != float64(1) ||
		otherData["recentAccounts"].([]any)[0].(map[string]any)["username"] != "analytics-other-admin" {
		t.Fatal("unexpected tenant projection", otherData)
	}
	checkStatus(t, root.invoke(t, "application.runtime.application.disable", map[string]any{
		"applicationId": "analytics", "pluginId": "builtin:analytics", "revision": 1,
	}, true), 403)
	resolve(root)
}
