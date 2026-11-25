//go:build integration

package tests

import (
	"fmt"
	"net/http/httptest"
	"testing"

	genidentity "horizon/foundation/gen/identity"
	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/system/identity"
)

func createIAMGovernanceUser(t *testing.T, admin iamClient, name string) int64 {
	t.Helper()
	created := checkIAMInvocation(t, admin.invoke(t, "identity.account.create", map[string]any{
		"username": name, "fullName": name, "initialStatus": "active", "password": accountCompletionPassword,
	}, true))
	return int64(created["id"].(float64))
}

func TestIAMRoleCopyAndPreviewRevision(t *testing.T) {
	handler, _, root := iamFixture(t)
	admin := loginTenantIAM(t, handler, root.Username, "tenant-a")
	role := map[string]any{
		"name": "Reviewable role", "code": "reviewable", "description": "", "status": "active",
		"grants": []map[string]any{{"permission": "accounts:read", "scope": "ALL", "effect": "allow"}},
	}
	created := checkIAMInvocation(t, admin.invoke(t, "identity.role.create", role, true))
	copy := checkIAMInvocation(t, admin.invoke(t, "identity.role.copy", map[string]any{"sourceId": created["id"], "name": "Role copy", "code": "reviewable-copy"}, true))
	if copy["id"] == created["id"] || copy["status"] != "inactive" || len(copy["grants"].([]any)) != 1 {
		t.Fatalf("invalid copied role: %v", copy)
	}
	memberID := createIAMGovernanceUser(t, admin, "role-preview-member")
	grant := checkIAMInvocation(t, admin.invoke(t, "identity.roleGrant.create", map[string]any{
		"accountId": memberID, "roleId": created["id"], "minutes": 15, "reason": "Verify role impact scope",
	}, true))
	loginTenantIAM(t, handler, "role-preview-member", "tenant-a")
	impact := checkIAMInvocation(t, admin.invoke(t, "identity.roleGrant.previewRevoke", map[string]any{"id": grant["id"]}, true))
	if impact["principals"] != float64(1) || impact["sessions"] != float64(1) {
		t.Fatalf("context role revocation missed an affected session: %v", impact)
	}
	role["id"], role["grants"] = created["id"], []any{}
	preview := checkIAMInvocation(t, admin.invoke(t, "identity.role.preview", role, true))
	if len(preview["removed"].([]any)) != 1 || preview["impact"].(map[string]any)["principals"] != float64(1) {
		t.Fatalf("missing role changes or impact: %v", preview)
	}
	role["status"], role["revision"] = "inactive", preview["revision"]
	stale := admin.invoke(t, "identity.role.update", role, true)
	checkStatus(t, stale, 200)
	if rejected := decodeBody[contracts.Result](t, stale); rejected.ErrorCode != "conflict" {
		t.Fatalf("unpreviewed change applied: %+v", rejected)
	}
	delete(role, "revision")
	preview = checkIAMInvocation(t, admin.invoke(t, "identity.role.preview", role, true))
	role["revision"] = preview["revision"]
	checkIAMInvocation(t, admin.invoke(t, "identity.role.update", role, true))
}

func TestIAMResourceCatalogDisableAndRestore(t *testing.T) {
	handler, pool, root := iamFixture(t)
	admin := loginTenantIAM(t, handler, root.Username, "tenant-a")
	other, err := identity.Bootstrap(t.Context(), pool, "resource-other", "resource-other-admin", accountCompletionPassword)
	if err != nil {
		t.Fatal(err)
	}
	otherAdmin := loginTenantIAM(t, handler, other.Username, "resource-other")
	const resource = "automation_task"
	list := func(client iamClient) map[string]any {
		t.Helper()
		items := checkIAMInvocation(t, client.invoke(t, "identity.resource.list", map[string]any{"search": resource}, true))["items"].([]any)
		for _, item := range items {
			if row := item.(map[string]any); row["resource"] == resource {
				return row
			}
		}
		t.Fatalf("resource not found in catalog: %v", items)
		return nil
	}
	if row := list(admin); row["enabled"] != true || len(row["actions"].([]any)) == 0 {
		t.Fatalf("resource contracts missing: %v", row)
	}
	set := map[string]any{"resource": resource, "enabled": false, "reason": "Disable runtime operations", "password": accountCompletionPassword}
	checkIAMInvocation(t, admin.invoke(t, "identity.resource.setStatus", set, true))
	checkStatus(t, admin.invoke(t, "automation.task.list", map[string]any{}, true), 403)
	if list(admin)["enabled"] != false || list(otherAdmin)["enabled"] != true {
		t.Fatal("resource availability leaked across tenants")
	}
	set["resource"] = "tenant"
	checkStatus(t, admin.invoke(t, "identity.resource.setStatus", set, true), 403)
	set["resource"], set["enabled"] = resource, true
	checkIAMInvocation(t, admin.invoke(t, "identity.resource.setStatus", set, true))
	checkIAMInvocation(t, admin.invoke(t, "automation.task.list", map[string]any{}, true))
}

func TestIAMAuditExportApprovalIsSingleUse(t *testing.T) {
	handler, pool, root := iamFixture(t)
	admin := loginTenantIAM(t, handler, root.Username, "tenant-a")
	created := checkIAMInvocation(t, admin.invoke(t, "identity.account.create", map[string]any{
		"username": "audit-separate-reviewer", "fullName": "Audit reviewer", "initialStatus": "active", "password": accountCompletionPassword,
	}, true))
	seedTenantRole(t, pool, "tenant-a", "audit-approver", root.ID, int64(created["id"].(float64)),
		[]*genidentity.PermissionGrant{{Permission: "capability:approve", Scope: "ALL", Effect: "allow"}})
	reviewer := loginTenantIAM(t, handler, "audit-separate-reviewer", "tenant-a")
	checkIAMInvocation(t, admin.invoke(t, "identity.audit.configureExport", map[string]any{
		"required": true, "password": accountCompletionPassword, "reason": "Enable independent audit approval",
	}, true))
	filter := map[string]any{"beforeId": 1000, "afterId": 0, "limit": 10, "workspace": true}
	const requestID = "6d3a6fde-70d6-47cb-b68d-26d498217093"
	checkStatus(t, admin.invoke(t, "identity.audit.export", filter, true), 403)
	request := admin.invoke(t, "identity.audit.requestExport", map[string]any{"requestId": requestID, "filter": filter, "reason": "Audit period review"}, true)
	checkStatus(t, request, 200)
	card := decodeBody[contracts.Result](t, request)
	if card.Status != contracts.StatusAwaitingApproval || card.ApprovalID == 0 {
		t.Fatalf("export did not enter approval: %+v", card)
	}
	decide := func(client iamClient) *httptest.ResponseRecorder {
		t.Helper()
		return client.request(t, "POST", fmt.Sprintf("/api/capabilities/approvals/%d/decisions", card.ApprovalID), map[string]any{"approve": true}, true)
	}
	checkStatus(t, decide(admin), 403)
	approved := decide(reviewer)
	checkStatus(t, approved, 200)
	if result := decodeBody[contracts.Result](t, approved); result.Status != contracts.StatusSucceeded {
		t.Fatalf("approval could not issue download permit: %s", approved.Body)
	}
	checkStatus(t, reviewer.invoke(t, "identity.audit.export", map[string]any{"requestId": requestID}, true), 403)
	filter["requestId"], filter["limit"] = requestID, 11
	checkStatus(t, admin.invoke(t, "identity.audit.export", filter, true), 403)
	permits := checkIAMInvocation(t, admin.invoke(t, "identity.audit.exportPermits", map[string]any{}, true))
	if len(permits["items"].([]any)) != 1 {
		t.Fatalf("missing requester permit: %v", permits)
	}
	exported := checkIAMInvocation(t, admin.invoke(t, "identity.audit.export", map[string]any{"requestId": requestID}, true))
	if len(exported["items"].([]any)) > 10 {
		t.Fatal("export exceeded the approved page")
	}
	checkStatus(t, admin.invoke(t, "identity.audit.export", map[string]any{"requestId": requestID}, true), 403)
	var persisted bool
	if err := pool.QueryRow(t.Context(), `SELECT EXISTS (SELECT 1 FROM cap_invocations
		WHERE tenant_id='tenant-a' AND capability='identity.audit.export' AND result->'data' <> 'null'::jsonb AND result->'data' IS NOT NULL)`).Scan(&persisted); err != nil || persisted {
		t.Fatalf("export content persisted: %v %v", persisted, err)
	}
}
