//go:build integration

package tests

import (
	"fmt"
	genidentity "horizon/foundation/gen/identity"
	"horizon/foundation/system/governance"
	"horizon/foundation/system/identity"
	"horizon/foundation/system/notifications"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTemporaryAccessLifecycleAndBoundaries(t *testing.T) {
	var notifier *notifications.Service
	handler, pool, adminAccount := iamFixture(t, func(iam *identity.Service, pool *pgxpool.Pool, _ *genidentity.AccountRecord) {
		notifier = notifications.New(governance.New(pool, iam, governance.Options{PublicWorkspace: "tenant-a"}))
	})
	root := loginTenantIAM(t, handler, adminAccount.Username, "tenant-a")
	grant := func(permission, scope string) *genidentity.PermissionGrant {
		return &genidentity.PermissionGrant{Permission: permission, Effect: "allow", Scope: scope}
	}
	account := func(username string, grants ...*genidentity.PermissionGrant) *genidentity.AccountRecord {
		t.Helper()
		response := root.invoke(t, "identity.account.create", map[string]any{
			"username": username, "fullName": username, "initialStatus": "active", "password": "TestOnly!2026Secure",
		}, true)
		checkStatus(t, response, 200)
		created := decodeData[genidentity.AccountRecord](t, response)
		rows := make([]map[string]any, 0, len(grants))
		for _, grant := range grants {
			rows = append(rows, map[string]any{
				"permission": grant.Permission, "effect": grant.Effect, "scope": grant.Scope,
			})
		}
		role := checkIAMInvocation(t, root.invoke(t, "identity.role.create", map[string]any{
			"name": username, "code": username, "description": "", "status": "active", "grants": rows,
		}, true))
		checkIAMInvocation(t, root.invoke(t, "identity.roleGrant.create", map[string]any{
			"accountId": created.ID, "roleId": role["id"], "minutes": 120, "reason": "Temporary access test base role",
		}, true))
		return &created
	}
	applicant := account("temporary-applicant", grant("example:read", "ALL"))
	reviewerAccount := account("limited-reviewer", grant("temporary:approve", "ALL"), grant("accounts:read", "OWN"))
	managerAccount := account("policy-manager", grant("access:manage", "ALL"))
	applicantClient := loginTenantIAM(t, handler, applicant.Username, "tenant-a")
	reviewer := loginTenantIAM(t, handler, reviewerAccount.Username, "tenant-a")
	manager := loginTenantIAM(t, handler, managerAccount.Username, "tenant-a")
	create := func(client iamClient, grants ...*genidentity.PermissionGrant) *genidentity.TemporaryAccessRecord {
		response := client.request(t, "POST", "/api/admin/temporary-access", &genidentity.RequestTemporaryAccessPayload{Grants: grants, Minutes: 60, Reason: "Support incident requiring scoped access"}, true)
		checkStatus(t, response, 200)
		return decodeBody[*genidentity.TemporaryAccessRecord](t, response)
	}
	reviewPayload := func(minutes int, grants ...*genidentity.PermissionGrant) *genidentity.ReviewTemporaryAccessPayload {
		return &genidentity.ReviewTemporaryAccessPayload{Decision: "approve", Minutes: minutes, Grants: grants, Note: "Adjusted by approver"}
	}
	reviewPath := func(id int64) string { return fmt.Sprintf("/api/admin/temporary-access/%d/review", id) }
	snapshot := func(client iamClient) *genidentity.AdminSnapshot {
		response := client.request(t, "GET", "/api/admin/snapshot", nil, false)
		checkStatus(t, response, 200)
		return decodeBody[*genidentity.AdminSnapshot](t, response)
	}
	if len(snapshot(applicantClient).Accounts) != 0 {
		t.Fatal("unprivileged applicant can already read accounts")
	}
	t.Run("directory and catalog available without protected resource permissions", func(t *testing.T) {
		response := applicantClient.request(t, "GET", "/api/admin/temporary-access", nil, false)
		checkStatus(t, response, 200)
		workspace := decodeBody[genidentity.TemporaryAccessWorkspace](t, response)
		// The workspace catalog merges the platform directory with declarative
		// plugin permissions, so it must cover the platform set and expose the
		// plugin-declared codes for requesting temporary access.
		platformCodes := map[string]bool{}
		for _, permission := range identity.PermissionCatalog() {
			platformCodes[permission.Code] = true
		}
		workspaceCodes := map[string]bool{}
		for _, permission := range workspace.Catalog {
			workspaceCodes[permission.Code] = true
			delete(platformCodes, permission.Code)
			if permission.Function == "memory" && permission.TemporaryEligible {
				t.Fatal("memory permissions must not be eligible for temporary access")
			}
		}
		if workspace.CanReview || len(platformCodes) != 0 {
			t.Fatal("request directory or independent approval permission incorrect")
		}
		if workspaceCodes["drive:use"] || workspaceCodes["drive:create"] || workspaceCodes["drive:cleanup"] {
			t.Fatal("retired cloud drive permissions are still in the workspace catalog")
		}
		if workspaceCodes["articles:read"] || workspaceCodes["articles:write"] || workspaceCodes["articles:review"] {
			t.Fatal("retired article permissions are still in the workspace catalog")
		}
		for _, declared := range []string{"projects:read", "mail:send", "example:read"} {
			if !workspaceCodes[declared] {
				t.Fatalf("plugin permission %s missing from workspace catalog", declared)
			}
		}
		response = manager.request(t, "GET", "/api/admin/temporary-access", nil, false)
		checkStatus(t, response, 200)
		if decodeBody[genidentity.TemporaryAccessWorkspace](t, response).CanReview {
			t.Fatal("access manager implicitly received approval")
		}
	})
	request := create(applicantClient, grant("accounts:read", "ALL"))
	t.Run("notify tenant reviewers without a department", func(t *testing.T) {
		if err := notifier.ProcessOnce(t.Context()); err != nil {
			t.Fatal(err)
		}
		var recipients, departmentRecipients int
		if err := pool.QueryRow(t.Context(), `
			SELECT count(*), count(n.department_id)
			FROM gov_notifications n JOIN gov_events e ON e.tenant_id = n.tenant_id AND e.id = n.event_id
			WHERE e.tenant_id = 'tenant-a' AND e.request_id = $1 AND e.event = 'requested'
			  AND n.recipient_id IN ($2, $3)
		`, request.ID, adminAccount.ID, reviewerAccount.ID).Scan(&recipients, &departmentRecipients); err != nil {
			t.Fatal(err)
		}
		if recipients != 2 || departmentRecipients != 0 {
			t.Fatalf("expected two tenant reviewers and no department assignments, got %d and %d", recipients, departmentRecipients)
		}
	})
	t.Run("reject missing approval permission, self approval, administrative leases and invalid input", func(t *testing.T) {
		checkStatus(t, manager.request(t, "POST", reviewPath(request.ID), reviewPayload(30, grant("accounts:read", "ALL")), true), 403)
		self := create(reviewer, grant("accounts:read", "ALL"))
		checkStatus(t, reviewer.request(t, "POST", reviewPath(self.ID), reviewPayload(30, grant("accounts:read", "ALL")), true), 403)
		for _, permission := range []string{"access:manage", "temporary:approve", "roles:update"} {
			checkStatus(t, applicantClient.request(t, "POST", "/api/admin/temporary-access", &genidentity.RequestTemporaryAccessPayload{Minutes: 60, Reason: "Valid request reason", Grants: []*genidentity.PermissionGrant{grant(permission, "ALL")}}, true), 400)
		}
		checkStatus(t, applicantClient.request(t, "POST", "/api/admin/temporary-access", &genidentity.RequestTemporaryAccessPayload{Minutes: 61, Reason: "Valid request reason", Grants: []*genidentity.PermissionGrant{grant("accounts:read", "ALL")}}, true), 400)
	})
	t.Run("approval cannot exceed permanent authority", func(t *testing.T) {
		checkStatus(t, reviewer.request(t, "POST", reviewPath(request.ID), reviewPayload(30, grant("accounts:read", "ALL")), true), 403)
		// Even an approved broader temporary range cannot be re-delegated.
		broader := create(reviewer, grant("accounts:read", "ALL"))
		checkStatus(t, root.request(t, "POST", reviewPath(broader.ID), reviewPayload(30, grant("accounts:read", "ALL")), true), 200)
		checkStatus(t, reviewer.request(t, "POST", reviewPath(request.ID), reviewPayload(30, grant("accounts:read", "ALL")), true), 403)
	})
	t.Run("approver edits duration and scope without altering original request", func(t *testing.T) {
		response := root.request(t, "POST", reviewPath(request.ID), reviewPayload(30, grant("accounts:read", "OWN")), true)
		checkStatus(t, response, 200)
		approved := decodeBody[genidentity.TemporaryAccessRecord](t, response)
		if approved.RequestedMinutes != 60 || approved.RequestedGrants[0].Scope != "ALL" || approved.ApprovedMinutes != 30 || approved.ApprovedGrants[0].Scope != "OWN" || approved.ReviewerID != adminAccount.ID {
			t.Fatal("original request or approved amendment lost")
		}
		start, err := time.Parse(time.RFC3339, approved.ReviewedAt)
		if err != nil {
			t.Fatal(err)
		}
		end, err := time.Parse(time.RFC3339, approved.ExpiresAt)
		if err != nil || end.Sub(start) != 30*time.Minute {
			t.Fatal("validity did not start at approval", err)
		}
		visible := snapshot(applicantClient).Accounts
		if len(visible) != 1 || visible[0].ID != applicant.ID {
			t.Fatal("approved temporary permission did not stay within the applicant's own scope")
		}
		checkStatus(t, root.request(t, "POST", reviewPath(request.ID), reviewPayload(60, grant("accounts:read", "ALL")), true), 409)
	})
	t.Run("database clock expiration immediately removes operation and data access", func(t *testing.T) {
		if _, err := pool.Exec(t.Context(), "UPDATE iam_temporary_requests SET reviewed_at=now()-interval '31 minutes',expires_at=now()-interval '1 second' WHERE id=$1", request.ID); err != nil {
			t.Fatal(err)
		}
		if len(snapshot(applicantClient).Accounts) != 0 {
			t.Fatal("expired grant still authorizes")
		}
		response := applicantClient.request(t, "GET", "/api/admin/temporary-access", nil, false)
		checkStatus(t, response, 200)
		records := decodeBody[genidentity.TemporaryAccessWorkspace](t, response).Requests
		if len(records) != 1 || records[0].Status != "expired" {
			t.Fatal("expiration not visible in history")
		}
	})
	t.Run("revocation removes temporary scope", func(t *testing.T) {
		lease := create(applicantClient, grant("accounts:read", "ALL"))
		checkStatus(t, root.request(t, "POST", reviewPath(lease.ID), reviewPayload(60, grant("accounts:read", "ALL")), true), 200)
		if len(snapshot(applicantClient).Accounts) != 4 {
			t.Fatal("tenant-wide temporary scope did not activate")
		}
		checkStatus(t, root.request(t, "POST", fmt.Sprintf("/api/admin/temporary-access/%d/revoke", lease.ID), nil, true), 200)
		if len(snapshot(applicantClient).Accounts) != 0 {
			t.Fatal("revocation did not remove temporary access")
		}
		var actor int64
		if err := pool.QueryRow(t.Context(), "SELECT revoked_by FROM iam_temporary_requests WHERE id=$1 AND revoked_at IS NOT NULL", lease.ID).Scan(&actor); err != nil || actor != adminAccount.ID {
			t.Fatal("revocation audit missing", err)
		}
	})
	t.Run("withdrawal, rejection and owner boundaries", func(t *testing.T) {
		pending := create(applicantClient, grant("departments:read", "ALL"))
		checkStatus(t, reviewer.request(t, "POST", fmt.Sprintf("/api/admin/temporary-access/%d/cancel", pending.ID), nil, true), 404)
		checkStatus(t, applicantClient.request(t, "POST", fmt.Sprintf("/api/admin/temporary-access/%d/cancel", pending.ID), nil, true), 200)
		checkStatus(t, root.request(t, "POST", reviewPath(pending.ID), reviewPayload(30, grant("departments:read", "ALL")), true), 409)
		reject := create(applicantClient, grant("departments:read", "ALL"))
		response := root.request(t, "POST", reviewPath(reject.ID), &genidentity.ReviewTemporaryAccessPayload{Decision: "reject", Minutes: 30, Grants: []*genidentity.PermissionGrant{}, Note: "Not required"}, true)
		checkStatus(t, response, 200)
		if decodeBody[genidentity.TemporaryAccessRecord](t, response).Status != "rejected" {
			t.Fatal("rejection missing")
		}
	})
	t.Run("parallel approval has one winner and cannot double activate", func(t *testing.T) {
		pending := create(applicantClient, grant("departments:read", "ALL"))
		codes := make(chan int, 2)
		for range 2 {
			go func() {
				codes <- root.request(t, "POST", reviewPath(pending.ID), reviewPayload(30, grant("departments:read", "ALL")), true).Code
			}()
		}
		first, second := <-codes, <-codes
		if !((first == 200 && second == 409) || (first == 409 && second == 200)) {
			t.Fatal("approval race was not atomic", first, second)
		}
	})
	t.Run("temporary writes cannot change accounts or issue permanent roles", func(t *testing.T) {
		requested := []*genidentity.PermissionGrant{grant("accounts:update", "ALL"), grant("accounts:create", "ALL")}
		lease := create(manager, requested...)
		checkStatus(t, root.request(t, "POST", reviewPath(lease.ID), reviewPayload(30, requested...), true), 200)
		checkStatus(t, manager.invoke(t, "identity.account.update", map[string]any{
			"id": managerAccount.ID, "username": managerAccount.Username, "fullName": "Updated display name", "status": "active",
		}, true), 403)
		checkStatus(t, manager.invoke(t, "identity.account.create", map[string]any{
			"username": "lease-created-account", "fullName": "Lease created account", "initialStatus": "active", "password": "TestOnly!2026Secure",
		}, true), 403)
	})
	t.Run("tenant boundary applies to inbox, approval, cancellation and scope IDs", func(t *testing.T) {
		if _, err := identity.Bootstrap(t.Context(), pool, "tenant-b", "foreign-reviewer", "TestOnly!2026Secure"); err != nil {
			t.Fatal(err)
		}
		foreign := loginTenantIAM(t, handler, "foreign-reviewer", "tenant-b")
		response := foreign.request(t, "GET", "/api/admin/temporary-access", nil, false)
		checkStatus(t, response, 200)
		if len(decodeBody[genidentity.TemporaryAccessWorkspace](t, response).Requests) != 0 {
			t.Fatal("cross tenant inbox leak")
		}
		checkStatus(t, foreign.request(t, "POST", reviewPath(request.ID), reviewPayload(30, grant("accounts:read", "ALL")), true), 404)
		checkStatus(t, foreign.request(t, "POST", fmt.Sprintf("/api/admin/temporary-access/%d/revoke", request.ID), nil, true), 404)
		checkStatus(t, foreign.request(t, "POST", "/api/admin/temporary-access", &genidentity.RequestTemporaryAccessPayload{Minutes: 60, Reason: "Valid request reason", Grants: []*genidentity.PermissionGrant{grant("accounts:read", "CUSTOM")}}, true), 400)
	})
}
