//go:build integration

package tests

import (
	"strings"
	"testing"

	genidentity "horizon/foundation/gen/identity"
	"horizon/foundation/system/identity"
)

func TestIAMRiskReportScopeAndSignals(t *testing.T) {
	handler, pool, account := iamFixture(t)
	admin := loginTenantIAM(t, handler, account.Username, "tenant-a")
	_, err := identity.Bootstrap(t.Context(), pool, "risk-foreign", "risk-foreign-admin", "TestOnly!2026Secure")
	if err != nil {
		t.Fatal(err)
	}
	grant := func(permission, scope string) map[string]any {
		return map[string]any{"permission": permission, "effect": "allow", "scope": scope}
	}
	target := checkIAMInvocation(t, admin.invoke(t, "identity.account.create", map[string]any{
		"username": "risk-review-target", "fullName": "Risk review member", "initialStatus": "active",
		"password": "TestOnly!2026Secure",
	}, true))
	targetID := int64(target["id"].(float64))
	seedTenantRole(t, pool, "tenant-a", "risk-member", account.ID, targetID,
		[]*genidentity.PermissionGrant{{Permission: "example:read", Effect: "allow", Scope: "ALL"}})
	member := loginTenantIAM(t, handler, "risk-review-target", "tenant-a")
	checkStatus(t, member.invoke(t, "identity.governance.report", map[string]any{}, true), 403)
	role := checkIAMInvocation(t, admin.invoke(t, "identity.role.create", map[string]any{
		"name": "Sensitive security role", "code": "risk-security", "description": "", "status": "active",
		"grants": []map[string]any{grant("accounts:security", "ALL")},
	}, true))
	assign := func(binding any) map[string]any {
		t.Helper()
		input := map[string]any{"accountId": target["id"], "roleId": role["id"], "minutes": 30, "reason": "Risk report review fixture"}
		if binding != nil {
			input["bindingId"] = binding
		}
		return checkIAMInvocation(t, admin.invoke(t, "identity.roleGrant.create", input, true))
	}
	active := assign(nil)
	expired := assign(nil)
	if _, err := pool.Exec(t.Context(), `UPDATE iam_context_role_grants SET created_at=now()-interval '1 hour', expires_at=now()-interval '1 minute' WHERE id=$1::uuid`, expired["id"]); err != nil {
		t.Fatal(err)
	}
	revoked := assign(nil)
	checkIAMInvocation(t, admin.invoke(t, "identity.roleGrant.revoke", revoked, true))
	// Separate tenant, age, authentication and non-security business failures.
	// Insert immutable audit fixtures at their intended timestamps, never edit them.
	for _, fixture := range []struct {
		tenant, action, outcome, reason, age string
		count                                int
	}{
		{"tenant-a", "risk-fixture.permission", "denied", "permission", "2 hours", 5},
		{"tenant-a", "risk-fixture.scope", "failed", "forbidden", "2 hours", 5},
		{"tenant-a", "risk-fixture.business", "failed", "not_found", "2 hours", 9},
		{"tenant-a", "risk-fixture.older", "denied", "permission", "30 hours", 6},
		{"tenant-a", "identity.login", "denied", "credentials", "2 hours", 5},
		{"tenant-a", "identity.login", "denied", "credentials", "30 hours", 5},
		{"risk-foreign", "identity.login", "denied", "foreign-secret", "2 hours", 12},
		{"risk-foreign", "risk-fixture.foreign", "denied", "foreign-secret", "2 hours", 12},
	} {
		if _, err := pool.Exec(t.Context(), `INSERT INTO iam_security_events
(tenant_id,account_id,action,outcome,reason,created_at)
SELECT $1,$2,$3,$4,$5,now()-$6::interval FROM generate_series(1,$7::integer)`,
			fixture.tenant, target["id"], fixture.action, fixture.outcome, fixture.reason, fixture.age, fixture.count); err != nil {
			t.Fatal(err)
		}
	}
	response := admin.invoke(t, "identity.governance.report", map[string]any{"windowHours": 24}, true)
	data := checkIAMInvocation(t, response)
	if strings.Contains(response.Body.String(), "foreign-secret") || strings.Contains(response.Body.String(), "password") {
		t.Fatal("risk report included other-tenant or credential data")
	}
	if data["authenticationFailures"] != float64(5) || data["windowHours"] != float64(24) || data["asOf"] == "" {
		t.Fatal("risk summary lost its snapshot/window", data)
	}
	auth := data["repeatedAuthentication"].([]any)
	if len(auth) != 1 || auth[0].(map[string]any)["occurrences"] != float64(5) || auth[0].(map[string]any)["username"] != "risk-review-target" {
		t.Fatal("repeated authentication report lost the tenant or time boundary", auth)
	}
	if _, exists := data["privilegedDirectGrants"]; exists {
		t.Fatal("risk report returned legacy direct grants")
	}
	rows := data["privilegedGrants"].([]any)
	if data["privilegedGrantTotal"] != float64(1) || len(rows) != 1 || rows[0].(map[string]any)["id"] != active["id"] || rows[0].(map[string]any)["roleName"] != "Sensitive security role" {
		t.Fatal("risk grants included expired/revoked/invalid bindings or lost display names", data)
	}
	checkGroups := func(data map[string]any, older bool) {
		t.Helper()
		found := map[string]bool{}
		for _, raw := range data["repeatedDenials"].([]any) {
			row := raw.(map[string]any)
			action := row["action"].(string)
			if strings.HasPrefix(action, "risk-fixture.") {
				if action == "risk-fixture.business" || action == "risk-fixture.foreign" {
					t.Fatal("risk groups included non-authorization failures or another tenant", row)
				}
				found[action] = true
				if row["affectedAccounts"] != float64(1) {
					t.Fatal("risk groups counted events as people", row)
				}
			}
		}
		if !found["risk-fixture.permission"] || !found["risk-fixture.scope"] || found["risk-fixture.older"] != older {
			t.Fatal("risk groups lost the time boundary or denied/failed outcome", found)
		}
	}
	checkGroups(data, false)
	checkGroups(checkIAMInvocation(t, admin.invoke(t, "identity.governance.report", map[string]any{"windowHours": 168}, true)), true)
	for _, hours := range []int{0, 169} {
		checkStatus(t, admin.invoke(t, "identity.governance.report", map[string]any{"windowHours": hours}, true), 400)
	}
	checkStatus(t, admin.invoke(t, "identity.governance.report", map[string]any{"tenantId": "risk-foreign"}, true), 400)
	if _, err := pool.Exec(t.Context(), `INSERT INTO iam_account_roles(tenant_id, account_id, role_id)
VALUES ('tenant-a', $1, $2)`, target["id"], role["id"]); err != nil {
		t.Fatal(err)
	}
	permanent := checkIAMInvocation(t, admin.invoke(t, "identity.governance.report", map[string]any{}, true))
	if permanent["privilegedGrantTotal"] != float64(1) {
		t.Fatal("legacy account role was treated as an effective grant", permanent)
	}
	// A bounded table must not turn its displayed row count into a false total.
	if _, err := pool.Exec(t.Context(), `INSERT INTO iam_context_role_grants
(tenant_id,principal_id,role_id,granted_by,reason,expires_at)
SELECT tenant_id,principal_id,role_id,granted_by,reason,expires_at
FROM iam_context_role_grants CROSS JOIN generate_series(1,55) WHERE id=$1::uuid`, active["id"]); err != nil {
		t.Fatal(err)
	}
	bounded := checkIAMInvocation(t, admin.invoke(t, "identity.governance.report", map[string]any{}, true))
	if bounded["privilegedGrantTotal"] != float64(56) || len(bounded["privilegedGrants"].([]any)) != 50 {
		t.Fatal("bounded risk rows lost the all-match total", bounded)
	}
	// The old department-wide management grant must not become workspace-wide.
	if _, err := pool.Exec(t.Context(), `UPDATE iam_accounts SET overrides=$2::jsonb WHERE id=$1`, target["id"],
		`[{"permission":"access:manage","effect":"allow","scope":"DEPARTMENT","departmentIds":[]}]`); err != nil {
		t.Fatal(err)
	}
	checkStatus(t, member.invoke(t, "identity.governance.report", map[string]any{}, true), 403)
}
