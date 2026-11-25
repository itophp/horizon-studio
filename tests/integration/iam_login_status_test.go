//go:build integration

package tests

import (
	"testing"

	genidentity "horizon/foundation/gen/identity"
	"horizon/foundation/kernel/capability/contracts"
)

func TestIAMLoginFreezeAndRestore(t *testing.T) {
	handler, pool, account := iamFixture(t)
	admin := loginTenantIAM(t, handler, account.Username, "tenant-a")
	create := func(username string) any {
		t.Helper()
		return checkIAMInvocation(t, admin.invoke(t, "identity.account.create", map[string]any{
			"username": username, "fullName": username, "initialStatus": "active", "password": "TestOnly!2026Secure",
		}, true))["id"]
	}
	targetID := create("freeze-target")
	otherID := create("freeze-other")
	seedTenantRole(t, pool, "tenant-a", "freeze-base", account.ID, int64(targetID.(float64)), []*genidentity.PermissionGrant{
		{Permission: "example:read", Effect: "allow", Scope: "ALL"},
	})
	target := loginTenantIAM(t, handler, "freeze-target", "tenant-a")
	proof := func(operation string, id any) any {
		t.Helper()
		return checkIAMInvocation(t, admin.invoke(t, "identity.login.reauthenticate", map[string]any{
			"accountId": id, "operation": operation, "password": "TestOnly!2026Secure",
		}, true))["proof"]
	}
	change := func(operation string, id, confirmation any) contracts.Result {
		t.Helper()
		response := admin.invoke(t, "identity.login."+operation, map[string]any{"accountId": id, "proof": confirmation, "reason": "Verify lifecycle security"}, true)
		return decodeBody[contracts.Result](t, response)
	}
	freezeProof := proof("freeze", targetID)
	if result := change("freeze", otherID, freezeProof); result.Status == contracts.StatusSucceeded {
		t.Fatal("proof was accepted for a different account")
	}
	if result := change("freeze", targetID, freezeProof); result.Status != contracts.StatusSucceeded {
		t.Fatalf("freeze: %+v", result)
	}
	checkStatus(t, target.request(t, "GET", "/api/auth/me", nil, false), 401)
	anonymous := iamClient{handler: handler}
	checkStatus(t, anonymous.request(t, "POST", "/api/auth/login", map[string]any{"username": "freeze-target", "password": "TestOnly!2026Secure"}, false), 401)
	if result := change("restore", targetID, freezeProof); result.Status == contracts.StatusSucceeded {
		t.Fatal("freeze proof was accepted for restore")
	}
	if result := change("restore", targetID, proof("restore", targetID)); result.Status != contracts.StatusSucceeded {
		t.Fatalf("restore: %+v", result)
	}
	if result := change("freeze", targetID, freezeProof); result.Status == contracts.StatusSucceeded {
		t.Fatal("consumed freeze proof was accepted again")
	}
	checkStatus(t, target.request(t, "GET", "/api/auth/me", nil, false), 401)
	fresh := loginTenantIAM(t, handler, "freeze-target", "tenant-a")
	checkStatus(t, fresh.request(t, "GET", "/api/auth/me", nil, false), 200)
}
