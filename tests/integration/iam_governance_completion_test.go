//go:build integration

package tests

import (
	"testing"

	genidentity "horizon/foundation/gen/identity"
	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/system/identity"
)

func TestIAMPolicyDiagnosticsAndSubjectApproval(t *testing.T) {
	handler, pool, root := iamFixture(t)
	author := loginTenantIAM(t, handler, root.Username, "tenant-a")
	reviewerID := createIAMGovernanceUser(t, author, "policy-governance-reviewer")
	grants := []*genidentity.PermissionGrant{{Permission: "access:manage", Effect: "allow", Scope: "ALL"}}
	seedTenantRole(t, pool, "tenant-a", "policy-reviewer", root.ID, reviewerID, grants)

	rule := map[string]any{"action": "identity.roles.read", "resource": "role", "effect": "deny"}
	checked := checkIAMInvocation(t, author.invoke(t, "identity.policy.check", map[string]any{"rules": []any{rule, rule}}, true))
	issue := checked["issues"].([]any)[0].(map[string]any)
	if issue["rule"] != float64(2) || issue["code"] != "conflict" {
		t.Fatalf("duplicate rule did not identify the conflicting row: %v", issue)
	}
	draft := checkIAMInvocation(t, author.invoke(t, "identity.policy.draft", map[string]any{
		"baseVersion": 0, "reason": "Deny a registered role action", "rules": []any{rule},
	}, true))
	contexts := checkIAMInvocation(t, author.invoke(t, "identity.policy.searchContexts", map[string]any{"keyword": "policy-governance-reviewer"}, true))
	items := contexts["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["accountId"] != float64(reviewerID) || items[0].(map[string]any)["username"] != "policy-governance-reviewer" {
		t.Fatalf("eligible context is missing: %v", contexts)
	}
	foreign, err := identity.Bootstrap(t.Context(), pool, "policy-foreign", "policy-other-tenant-admin", accountCompletionPassword)
	if err != nil {
		t.Fatal(err)
	}
	contexts = checkIAMInvocation(t, author.invoke(t, "identity.policy.searchContexts", map[string]any{"keyword": foreign.Username}, true))
	if len(contexts["items"].([]any)) != 0 {
		t.Fatalf("search exposed another tenant: %v", contexts)
	}
	input := map[string]any{"id": draft["id"], "accountId": root.ID, "action": "identity.roles.read", "resource": "role"}
	result := checkIAMInvocation(t, author.invoke(t, "identity.policy.simulate", input, true))
	if result["currentAllowed"] != true || result["proposedReason"] != "policy_deny" || result["proposedConstraints"] == nil {
		t.Fatalf("simulation did not use the registered action: %v", result)
	}
	input["permission"] = "access:manage"
	if result := decodeBody[contracts.Result](t, author.invoke(t, "identity.policy.simulate", input, true)); result.ErrorCode != "invalid" {
		t.Fatalf("simulation accepted a mismatched permission: %+v", result)
	}
	delete(input, "permission")
	input["accountId"] = foreign.ID
	checkStatus(t, author.invoke(t, "identity.policy.simulate", input, true), 403)

	// A second login placed under the author Principal still has its own account ID.
	if _, err := pool.Exec(t.Context(), `UPDATE iam_login_accounts reviewer SET principal_id = author.principal_id
FROM iam_login_accounts author WHERE reviewer.username = 'policy-governance-reviewer' AND author.username = $1`, root.Username); err != nil {
		t.Fatal(err)
	}
	seedTenantRole(t, pool, "tenant-a", "associated-policy-reviewer", root.ID, reviewerID, grants)
	reviewer := loginTenantIAM(t, handler, "policy-governance-reviewer", "tenant-a")
	meta := checkIAMInvocation(t, reviewer.invoke(t, "identity.policy.preview", draft, true))
	if meta["authoredByMe"] != true || meta["createdAt"] == "" || meta["authorName"] == "" {
		t.Fatalf("policy preview lost the author Principal: %v", meta)
	}
	rejected := decodeBody[contracts.Result](t, reviewer.invoke(t, "identity.policy.publish", draft, true))
	if rejected.ErrorCode != "conflict" {
		t.Fatalf("same Principal was allowed to publish: %+v", rejected)
	}
}

func TestIAMAccessReviewRevokesDirectPermissionWithoutRestoringInheritedAccess(t *testing.T) {
	handler, pool, root := iamFixture(t)
	admin := loginIAM(t, handler, root.Username)
	role := checkIAMInvocation(t, admin.invoke(t, "identity.role.create", map[string]any{
		"name": "Inherited resource role", "code": "inherit-roles", "description": "", "status": "active",
		"grants": []any{map[string]any{"permission": "roles:read", "effect": "allow", "scope": "ALL"}},
	}, true))
	var secondary int64
	if err := pool.QueryRow(t.Context(), `SELECT id FROM identity_departments WHERE tenant_id = 'tenant-a'
AND organization_id = $1 AND id <> $2 ORDER BY id LIMIT 1`, root.OrganizationID, root.DepartmentID).Scan(&secondary); err != nil {
		t.Fatal(err)
	}
	readRoles := map[string]any{"permission": "roles:read", "effect": "allow", "scope": "ALL"}
	account := checkIAMInvocation(t, admin.invoke(t, "identity.account.create", map[string]any{
		"username": "direct-review-member", "fullName": "Direct review member", "initialStatus": "active",
		"organizationId": root.OrganizationID, "departmentId": root.DepartmentID,
		"roleIds": []any{role["id"]}, "password": accountCompletionPassword,
		"overrides": []any{
			readRoles,
			map[string]any{"permission": "accounts:read", "effect": "allow", "scope": "ALL"},
		},
		"departmentAccess": map[string]any{"memberships": []any{
			map[string]any{"organizationId": root.OrganizationID, "departmentId": secondary, "roleIds": []any{role["id"]}, "overrides": []any{readRoles}},
		}},
	}, true))
	member := loginIAM(t, handler, "direct-review-member")
	checkIAMInvocation(t, member.invoke(t, "identity.roles.read", map[string]any{}, true))
	reviewID := func(kind, permission string) float64 {
		t.Helper()
		queue := checkIAMInvocation(t, admin.invoke(t, "identity.review.list", map[string]any{"status": "pending"}, true))
		for _, raw := range queue["items"].([]any) {
			item := raw.(map[string]any)
			if item["kind"] == kind && item["permission"] == permission {
				if item["principalName"] != "Direct review member" || item["departmentName"] == "" {
					t.Fatalf("review omitted a display name: %v", item)
				}
				return item["id"].(float64)
			}
		}
		t.Fatalf("no pending review of %s: %v", permission, queue)
		return 0
	}
	decide := func(id float64, status string) {
		t.Helper()
		result := checkIAMInvocation(t, admin.invoke(t, "identity.review.decide", map[string]any{
			"id": id, "decision": "revoke", "reason": "Revoke direct access after review",
		}, true))
		if result["status"] != status {
			t.Fatalf("review status %v, want %s", result, status)
		}
	}
	checkIAMInvocation(t, admin.invoke(t, "identity.review.start", map[string]any{}, true))
	stale := reviewID("account_override", "accounts:read")
	if _, err := pool.Exec(t.Context(), `UPDATE iam_accounts SET overrides = jsonb_set(overrides, '{1,Scope}', '"OWN"')
WHERE tenant_id = 'tenant-a' AND id = $1`, account["id"]); err != nil {
		t.Fatal(err)
	}
	decide(stale, "superseded")
	decide(reviewID("account_override", "roles:read"), "revoked")
	checkStatus(t, member.invoke(t, "identity.roles.read", map[string]any{}, true), 403)
	checkIAMInvocation(t, member.invoke(t, "identity.accounts.read", map[string]any{}, true))
	selected := member.request(t, "POST", "/api/auth/identity", map[string]any{"tenantId": "tenant-a", "departmentId": secondary}, true)
	checkStatus(t, selected, 200)
	member.csrf = decodeBody[genidentity.SessionProfile](t, selected).CsrfToken
	checkIAMInvocation(t, member.invoke(t, "identity.roles.read", map[string]any{}, true))
	decide(reviewID("department_override", "roles:read"), "revoked")
	checkStatus(t, member.invoke(t, "identity.roles.read", map[string]any{}, true), 403)
}
