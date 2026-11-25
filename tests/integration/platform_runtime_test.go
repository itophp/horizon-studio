//go:build integration

package tests

import (
	"net/http"
	"testing"

	gencap "horizon/foundation/gen/capability"
	genidentity "horizon/foundation/gen/identity"
	"horizon/foundation/runtime/app"
	"horizon/foundation/system/governance"
	"horizon/foundation/system/identity"
)

func TestPlatformRuntimeInstallationsAndProviders(t *testing.T) {
	_, pool, _ := iamFixture(t)
	const existing = "platform-existing"
	const fresh = "platform-fresh"
	newHandler := func(tenant string) http.Handler {
		iam := identity.New(pool, false)
		core := governance.New(pool, iam, governance.Options{
			EncryptionKey: []byte("01234567890123456789012345678901"), PublicWorkspace: tenant,
		})
		return deploymentHandler(app.System{}, core, iam)
	}
	if _, err := identity.Bootstrap(t.Context(), pool, existing, existing, "TestOnly!2026Secure"); err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Bootstrap(t.Context(), pool, fresh, fresh, "TestOnly!2026Secure"); err != nil {
		t.Fatal(err)
	}

	routes := map[string]string{
		"system-settings": "/settings", "ai-settings": "/ai-settings", "personal-preferences": "/preferences", "agent-collaboration": "/mcp",
	}
	protocol := func(client iamClient, id string) map[string]any {
		t.Helper()
		res := client.invoke(t, "application.runtime.resolve", map[string]any{"applicationId": id}, true)
		checkSucceeded(t, res)
		return decodeBody[gencap.CapabilityResult](t, res).Data.(map[string]any)["protocol"].(map[string]any)
	}
	for _, tenant := range []string{existing, fresh} {
		handler := newHandler(tenant)
		root := loginTenantIAM(t, handler, tenant, tenant)
		for id, route := range routes {
			pages := protocol(root, id)["pages"].([]any)
			expected := []string{route}
			if len(pages) != len(expected) {
				t.Fatalf("%s %s returned %d pages, want %d", tenant, id, len(pages), len(expected))
			}
			for index, expectedRoute := range expected {
				if pages[index].(map[string]any)["route"] != expectedRoute {
					t.Fatalf("%s %s page %d route mismatch", tenant, id, index)
				}
			}
		}
		if providers := protocol(root, "agent-collaboration")["providers"].([]any); len(providers) != 2 {
			t.Fatalf("administrator missing audit provider: %v", providers)
		}
		for _, name := range []string{"mcp.token.list", "mcp.audit.list"} {
			res := root.invoke(t, name, map[string]any{}, true)
			checkSucceeded(t, res)
			if data, ok := decodeBody[gencap.CapabilityResult](t, res).Data.([]any); !ok || len(data) != 0 {
				t.Fatalf("%s must return an empty array: %v", name, data)
			}
		}
		if tenant == existing {
			checkSucceeded(t, root.invoke(t, "mcp.token.create", map[string]any{
				"name": "platform test", "password": "TestOnly!2026Secure", "days": 1,
			}, true))
			res := root.invoke(t, "mcp.token.list", map[string]any{}, true)
			checkSucceeded(t, res)
			rows := decodeBody[gencap.CapabilityResult](t, res).Data.([]any)
			if len(rows) != 1 {
				t.Fatal("expected exactly one personal token")
			}
			row := rows[0].(map[string]any)
			if _, present := row["secret"]; present || row["revokedBy"] != float64(0) {
				t.Fatal("unexpected token list projection")
			}
		}
		restarted := loginTenantIAM(t, newHandler(tenant), tenant, tenant)
		for id := range routes {
			protocol(restarted, id)
		}
		if tenant != fresh {
			continue
		}
		readerName := "platform-reader"
		account := checkIAMInvocation(t, root.invoke(t, "identity.account.create", map[string]any{
			"username": readerName, "fullName": "Platform Reader", "email": "reader@example.test",
			"initialStatus": "active",
			"password":      "TestOnly!2026Secure",
		}, true))
		profile := decodeBody[genidentity.SessionProfile](t, root.request(t, "GET", "/api/auth/me", nil, false))
		seedTenantRole(t, pool, tenant, "platform-reader", profile.Account.ID, int64(account["id"].(float64)),
			[]*genidentity.PermissionGrant{{Permission: "applications:read", Effect: "allow", Scope: "ALL"}})
		reader := loginTenantIAM(t, handler, readerName, tenant)
		catalog := decodeData[map[string]any](t, reader.invoke(t, "application.runtime.list", map[string]any{}, true))
		for _, app := range catalog["items"].([]any) {
			if app.(map[string]any)["id"] == "ai-settings" {
				t.Fatal("ordinary account can discover the AI configuration application")
			}
		}
		denied := decodeBody[gencap.CapabilityResult](t, reader.invoke(t, "application.runtime.resolve", map[string]any{"applicationId": "ai-settings"}, true))
		if denied.Status != "failed" || denied.ErrorCode != "not_found" {
			t.Fatalf("ordinary account resolved the AI configuration application: %+v", denied)
		}
		checkStatus(t, reader.invoke(t, "ai.config.read", map[string]any{}, true), 403)
		checkStatus(t, reader.invoke(t, "settings.workspace.read", map[string]any{"targetKind": "workspace", "targetId": 0}, true), 403)
		preferences := reader.invoke(t, "application.runtime.resolve", map[string]any{"applicationId": "personal-preferences"}, true)
		checkSucceeded(t, preferences)
		definition := decodeBody[gencap.CapabilityResult](t, preferences).Data.(map[string]any)
		view := definition["protocol"].(map[string]any)
		page := view["pages"].([]any)[0].(map[string]any)
		children := page["root"].(map[string]any)["layout"].(map[string]any)["children"].([]any)
		form := children[1].(map[string]any)["layout"].(map[string]any)["children"].([]any)[0].(map[string]any)["component"].(map[string]any)
		if form["kind"] != "change-list-form" || len(form["events"].([]any)) != 1 {
			t.Fatal("personal preferences require a declared form and projected action")
		}
		checkSucceeded(t, reader.request(t, "POST", "/api/capabilities/settings.preferences.update/invocations", map[string]any{
			"version": "1.0.0", "idempotencyKey": "runtime-test-preferences",
			"input": map[string]any{"changes": []map[string]string{{"key": "display.theme", "operation": "set", "value": "\"dark\""}}},
			"runtime": map[string]any{
				"applicationId": "personal-preferences", "pageId": page["id"],
				"metadataVersion": definition["metadataVersion"], "installationRevision": definition["installationRevision"],
			},
		}, true))
		providers := protocol(reader, "agent-collaboration")["providers"].([]any)
		if len(providers) != 1 || providers[0].(map[string]any)["id"] != "tokens" {
			t.Fatalf("unexpected providers for ordinary account: %v", providers)
		}
		checkSucceeded(t, reader.invoke(t, "mcp.token.list", map[string]any{}, true))
		if result := decodeBody[gencap.CapabilityResult](t, reader.invoke(t, "mcp.audit.list", map[string]any{}, true)); result.Status == "succeeded" {
			t.Fatal("ordinary account read audit data")
		}
	}
}
