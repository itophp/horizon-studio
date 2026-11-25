//go:build integration

package tests

import (
	"testing"

	gencap "horizon/foundation/gen/capability"
	"horizon/foundation/system/settings"
)

func TestTransientCredentialResults(t *testing.T) {
	handler, pool, _ := iamFixture(t)
	owner := loginTenantIAM(t, handler, "root-admin", "tenant-a")
	invoke := func(name, key string, input map[string]any) gencap.CapabilityResult {
		t.Helper()
		response := owner.request(t, "POST", "/api/capabilities/"+name+"/invocations", map[string]any{
			"idempotencyKey": key, "input": input,
		}, true)
		checkStatus(t, response, 200)
		result := decodeBody[gencap.CapabilityResult](t, response)
		if result.Status != "succeeded" {
			t.Fatalf("%s returned %s (%s)", name, result.Status, result.ErrorCode)
		}
		return result
	}
	issue := func(name, key string, input map[string]any) (string, int64) {
		t.Helper()
		first := invoke(name, key, input)
		data, ok := first.Data.(map[string]any)
		if !ok || first.Replayed || data["secret"] == "" || data["secret"] == nil {
			t.Fatal("first invocation did not disclose a new secret")
		}
		replay := invoke(name, key, input)
		if !replay.Replayed || replay.InvocationID != first.InvocationID || replay.Data != nil ||
			len(replay.EventIds) != len(first.EventIds) || len(first.EventIds) == 0 || replay.EventIds[0] != first.EventIds[0] {
			t.Fatal("idempotent replay failed to preserve the outcome without returning the credential")
		}
		token := data["token"].(map[string]any)
		return first.InvocationID, int64(token["id"].(float64))
	}
	createInvocation, tokenID := issue("mcp.token.create", "create-once", map[string]any{
		"name": "transient result test", "days": 1, "password": "TestOnly!2026Secure",
	})
	rotateInvocation, _ := issue("mcp.token.rotate", "rotate-once", map[string]any{
		"id": tokenID, "password": "TestOnly!2026Secure",
	})
	workspace := decodeData[settings.SettingsWorkspace](t, owner.invoke(t, "settings.workspace.read", map[string]any{}, true))
	draft := decodeData[settings.ChangeRecord](t, owner.invoke(t, "settings.draft.create", map[string]any{
		"target":       map[string]any{"kind": "workspace", "id": 0},
		"baseRevision": workspace.Revision, "reason": "transient proof test",
		"changes": []map[string]any{{"key": "branding.name", "operation": "set", "value": `"Proof Test"`}},
	}, true))
	proofInput := map[string]any{
		"password": "TestOnly!2026Secure", "operation": "settings.publish", "resourceId": draft.ID, "revision": workspace.Revision,
	}
	proof := invoke("settings.password.verify", "read-reauth", proofInput)
	if data, ok := proof.Data.(map[string]any); !ok || data["proof"] == "" || data["proof"] == nil {
		t.Fatal("the reauthentication proof was not delivered")
	}

	ids := []string{createInvocation, rotateInvocation, proof.InvocationID}
	assertRedacted := func() {
		t.Helper()
		for _, id := range ids {
			var redacted bool
			if err := pool.QueryRow(t.Context(), `
				SELECT COALESCE(result->'data', 'null'::jsonb) = 'null'::jsonb
				FROM cap_invocations WHERE invocation_id=$1`, id).Scan(&redacted); err != nil {
				t.Fatal(err)
			}
			if !redacted {
				t.Fatal("transient result data was persisted")
			}
		}
	}
	assertRedacted()

	owner = loginTenantIAM(t, newRuntimeHandler(pool, "tenant-a"), "root-admin", "tenant-a")
	assertRedacted()
	replay := invoke("mcp.token.rotate", "rotate-once", map[string]any{
		"id": tokenID, "password": "TestOnly!2026Secure",
	})
	if !replay.Replayed || replay.InvocationID != rotateInvocation || replay.Data != nil {
		t.Fatal("a restarted runtime did not preserve redacted idempotency metadata")
	}
}
