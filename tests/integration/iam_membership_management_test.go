//go:build integration

package tests

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	genidentity "horizon/foundation/gen/identity"
	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/capability/gateway"
	"horizon/foundation/kernel/capability/registry"
	"horizon/foundation/system/identity"
)

func TestIAMTenantMemberDirectory(t *testing.T) {
	handler, pool, account := iamFixture(t)
	admin := loginTenantIAM(t, handler, account.Username, "tenant-a")
	other, err := identity.Bootstrap(t.Context(), pool, "member-other-tenant", "member-other-admin", "TestOnly!2026Secure")
	if err != nil {
		t.Fatal(err)
	}
	created := checkIAMInvocation(t, admin.invoke(t, "identity.account.create", map[string]any{
		"username": "member-no-management", "fullName": "Member without management", "initialStatus": "active", "password": "TestOnly!2026Secure",
	}, true))
	seedTenantRole(t, pool, "tenant-a", "workspace-member-reader", account.ID, int64(created["id"].(float64)),
		[]*genidentity.PermissionGrant{{Permission: "accounts:read", Scope: "OWN", Effect: "allow"}})
	member := loginTenantIAM(t, handler, "member-no-management", "tenant-a")
	checkStatus(t, member.invoke(t, "identity.membership.list", map[string]any{}, true), 403)
	// Seed additional existing placements without paying for unused password
	// hashes. All relationships are real PostgreSQL foreign keys.
	_, err = pool.Exec(t.Context(), `WITH subjects AS (
INSERT INTO iam_principals(kind) SELECT 'human' FROM generate_series(1,51) RETURNING id
), logins AS (
INSERT INTO iam_login_accounts(principal_id,username) SELECT id,'member-seed-'||id::text FROM subjects RETURNING id,principal_id,username
), memberships AS (
INSERT INTO iam_tenant_memberships(tenant_id,principal_id) SELECT $1,principal_id FROM logins RETURNING principal_id
)
INSERT INTO iam_accounts(tenant_id,login_account_id,username,status)
SELECT $1,login.id,login.username,'active' FROM logins login JOIN memberships member ON member.principal_id=login.principal_id`, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	first := checkIAMInvocation(t, admin.invoke(t, "identity.membership.list", map[string]any{}, true))
	items := first["items"].([]any)
	if len(items) != 50 || first["nextAfter"].(float64) <= 0 {
		t.Fatal("directory page bound missing", first)
	}
	seen := map[float64]bool{}
	for _, value := range items {
		row := value.(map[string]any)
		id := row["id"].(float64)
		if id == float64(other.ID) || row["username"] == other.Username {
			t.Fatal("cross-tenant member leaked")
		}
		if row["organizationName"] != nil || row["departmentName"] != nil {
			t.Fatal("IAM membership returned application structure", row)
		}
		if id == float64(account.ID) && row["protected"] != true {
			t.Fatal("administrator protection not projected")
		}
		seen[id] = true
	}
	last := checkIAMInvocation(t, admin.invoke(t, "identity.membership.list", map[string]any{"after": first["nextAfter"]}, true))
	if len(last["items"].([]any)) != 3 || last["nextAfter"] != float64(0) {
		t.Fatal("directory terminal page incorrect", last)
	}
	for _, value := range last["items"].([]any) {
		if seen[value.(map[string]any)["id"].(float64)] {
			t.Fatal("duplicate member across pages")
		}
	}
	response := admin.invoke(t, "identity.membership.list", map[string]any{}, true)
	for _, secret := range []string{"password", "tokenHash", "principalId", "loginAccountId", other.Username} {
		if strings.Contains(response.Body.String(), secret) {
			t.Fatal("directory leaked private data", secret)
		}
	}
	checkStatus(t, admin.invoke(t, "identity.membership.revoke", map[string]any{"accountId": other.ID}, true), 403)
	checkStatus(t, admin.invoke(t, "identity.membership.revoke", map[string]any{"accountId": account.ID}, true), 403)
	change := func(operation string, id any) *httptest.ResponseRecorder {
		return admin.invoke(t, "identity.membership."+operation, map[string]any{"accountId": id, "reason": "Membership lifecycle test"}, true)
	}
	checkStatus(t, change("suspend", other.ID), 403)
	checkStatus(t, change("suspend", account.ID), 403)
	checkIAMInvocation(t, change("suspend", created["id"]))
	checkStatus(t, member.request(t, "GET", "/api/auth/me", nil, false), 401)
	checkIAMInvocation(t, change("restore", created["id"]))
	checkStatus(t, member.request(t, "GET", "/api/auth/me", nil, false), 401)
	checkStatus(t, loginIAM(t, handler, "member-no-management").request(t, "GET", "/api/auth/me", nil, false), 200)
	checkIAMInvocation(t, admin.invoke(t, "identity.membership.revoke", map[string]any{"accountId": created["id"]}, true))
	checkStatus(t, change("restore", created["id"]), 403)
	checkStatus(t, member.request(t, "GET", "/api/auth/me", nil, false), 401)
	updated := checkIAMInvocation(t, admin.invoke(t, "identity.membership.list", map[string]any{}, true))
	found := false
	for _, value := range updated["items"].([]any) {
		row := value.(map[string]any)
		if row["id"] == created["id"] {
			found = row["membershipStatus"] == "revoked"
		}
	}
	if !found {
		t.Fatal("revoked membership disappeared or retained active status")
	}
	checkStatus(t, loginIAM(t, handler, other.Username).request(t, "GET", "/api/auth/me", nil, false), 200)
}

func TestIAMInvitationExpiryAndAuthority(t *testing.T) {
	handler, pool, account := iamFixture(t)
	admin := loginIAM(t, handler, account.Username)
	other, err := identity.Bootstrap(t.Context(), pool, "invitation-target", "invitation-target-admin", "TestOnly!2026Secure")
	if err != nil {
		t.Fatal(err)
	}
	recipient := loginIAM(t, handler, other.Username)
	issue := func() map[string]any {
		return checkIAMInvocation(t, admin.invoke(t, "identity.membership.invite", map[string]any{
			"username": other.Username, "organizationId": account.OrganizationID, "departmentId": account.DepartmentID,
		}, true))
	}
	expired := issue()
	if _, err = pool.Exec(t.Context(), `UPDATE iam_membership_invitations SET expires_at=now()-interval '1 second' WHERE id=$1::uuid`, expired["id"]); err != nil {
		t.Fatal(err)
	}
	checkStatus(t, recipient.invoke(t, "identity.membership.accept", map[string]any{"code": expired["code"]}, true), 403)
	revoked := issue()
	checkStatus(t, recipient.invoke(t, "identity.membership.invitation.revoke", map[string]any{"id": revoked["id"]}, true), 403)
	checkIAMInvocation(t, admin.invoke(t, "identity.membership.invitation.revoke", map[string]any{"id": revoked["id"]}, true))
	checkStatus(t, recipient.invoke(t, "identity.membership.accept", map[string]any{"code": revoked["code"]}, true), 403)
	issuedBy := checkIAMInvocation(t, admin.invoke(t, "identity.account.create", map[string]any{
		"username": "invitation-issuer", "fullName": "Invitation issuer", "initialStatus": "active", "organizationId": account.OrganizationID,
		"departmentId": account.DepartmentID, "roleIds": []int64{}, "password": "TestOnly!2026Secure",
		"overrides": []map[string]any{{"permission": "accounts:create", "effect": "allow", "scope": "ALL"}},
	}, true))
	inviter := loginIAM(t, handler, "invitation-issuer")
	current := checkIAMInvocation(t, inviter.invoke(t, "identity.membership.invite", map[string]any{
		"username": other.Username, "organizationId": account.OrganizationID, "departmentId": account.DepartmentID,
	}, true))
	// Existing invitations must not borrow authority revoked from their issuer.
	if _, err = pool.Exec(t.Context(), `UPDATE iam_accounts SET overrides='[]'::jsonb WHERE id=$1`, issuedBy["id"]); err != nil {
		t.Fatal(err)
	}
	checkStatus(t, recipient.invoke(t, "identity.membership.accept", map[string]any{"code": current["code"]}, true), 403)
	var accepted bool
	if err = pool.QueryRow(t.Context(), `SELECT accepted_at IS NOT NULL FROM iam_membership_invitations WHERE id=$1::uuid`, current["id"]).Scan(&accepted); err != nil || accepted {
		t.Fatalf("rejected invitation consumed: %v %v", accepted, err)
	}
}

func TestIAMLoginBeforeTenantMembership(t *testing.T) {
	handler, pool, root := iamFixture(t)
	admin := loginTenantIAM(t, handler, root.Username, "tenant-a")
	const recipient = "first-tenant-login"
	hash, err := identity.HashPassword("TestOnly!2026Secure")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(t.Context(), `WITH principal AS (
    INSERT INTO iam_principals(kind) VALUES ('human') RETURNING id
), login AS (
    INSERT INTO iam_login_accounts(principal_id, username) SELECT id, $1 FROM principal RETURNING id
)
INSERT INTO iam_password_credentials(login_account_id, password_hash) SELECT id, $2 FROM login`, recipient, hash); err != nil {
		t.Fatal(err)
	}
	newLogin := loginIAM(t, handler, recipient)
	me := newLogin.request(t, "GET", "/api/auth/me", nil, false)
	checkStatus(t, me, 200)
	profile := decodeBody[genidentity.SessionProfile](t, me)
	if profile.Account != nil || profile.TenantID != "" || len(profile.Permissions) != 0 {
		t.Fatalf("unscoped login unexpectedly contains work authority: %+v", profile)
	}
	checkStatus(t, newLogin.request(t, "GET", "/api/admin/snapshot", nil, false), 401)
	checkStatus(t, newLogin.invoke(t, "identity.membership.list", map[string]any{}, true), 401)
	selection := map[string]any{"tenantId": "tenant-a"}
	checkStatus(t, newLogin.request(t, "POST", "/api/auth/identity", selection, true), 403)
	makeInvite := func(name string) map[string]any {
		t.Helper()
		return checkIAMInvocation(t, admin.invoke(t, "identity.membership.invite", map[string]any{
			"username": name,
		}, true))
	}
	foreignCode := makeInvite("another-login")["code"]
	checkStatus(t, newLogin.request(t, "POST", "/api/auth/invitation", map[string]any{"code": foreignCode}, true), 403)
	invite := makeInvite(recipient)
	input := map[string]any{"code": invite["code"]}
	checkStatus(t, newLogin.request(t, "POST", "/api/auth/invitation", input, false), 403)
	checkStatus(t, newLogin.request(t, "POST", "/api/auth/invitation", input, true), 204)
	checkStatus(t, newLogin.request(t, "POST", "/api/auth/invitation", input, true), 403)
	checkStatus(t, newLogin.invoke(t, "identity.membership.list", map[string]any{}, true), 401)
	checkStatus(t, newLogin.request(t, "POST", "/api/auth/identity", selection, true), 403)
	var accountID int64
	if err := pool.QueryRow(t.Context(), "SELECT id FROM iam_accounts WHERE tenant_id = $1 AND username = $2", "tenant-a", recipient).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	seedTenantRole(t, pool, "tenant-a", "first-member-reader", root.ID, accountID,
		[]*genidentity.PermissionGrant{{Permission: "accounts:read", Scope: "OWN", Effect: "allow"}})
	selected := newLogin.request(t, "POST", "/api/auth/identity", selection, true)
	checkStatus(t, selected, 200)
	active := decodeBody[genidentity.SessionProfile](t, selected)
	if active.Account == nil || active.Account.Username != recipient || active.TenantID != "tenant-a" ||
		active.CsrfToken == newLogin.csrf || len(active.Permissions) != 1 || active.Permissions[0] != "accounts:read" {
		t.Fatalf("unexpected invitation context: %+v", active)
	}
	checkStatus(t, newLogin.request(t, "POST", "/api/auth/identity", selection, true), 403)
	newLogin.csrf = active.CsrfToken
	checkStatus(t, newLogin.invoke(t, "identity.membership.list", map[string]any{}, true), 403)
}

func TestIAMPlatformPrincipalLifecycle(t *testing.T) {
	handler, pool, root := iamFixture(t, func(iam *identity.Service, pool *pgxpool.Pool, root *genidentity.AccountRecord) {
		var id string
		if err := pool.QueryRow(t.Context(), `SELECT principal_id::text FROM iam_active_account_subjects WHERE tenant_id = 'tenant-a' AND account_id = $1`, root.ID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		iam.ConfigurePlatformOperators([]string{id})
	})
	other, err := identity.Bootstrap(t.Context(), pool, "platform-other", "platform-other-admin", "TestOnly!2026Secure")
	if err != nil {
		t.Fatal(err)
	}
	operator := loginIAM(t, handler, root.Username)
	tenantAdmin := loginIAM(t, handler, other.Username)
	checkStatus(t, tenantAdmin.invoke(t, "identity.principal.list", map[string]any{"targetTenantId": "platform-other"}, true), 403)

	change := func(operation, tenant, target string, extra map[string]any) *httptest.ResponseRecorder {
		t.Helper()
		input := map[string]any{"targetTenantId": tenant, "target": target, "reason": "Authorized lifecycle verification"}
		query := map[string]any{"targetTenantId": tenant, "target": target, "reason": input["reason"], "operation": operation, "password": "TestOnly!2026Secure"}
		proof := checkIAMInvocation(t, operator.invoke(t, "identity.principal.reauthenticate", query, true))
		input["proof"] = proof["proof"]
		for key, value := range extra {
			input[key] = value
		}
		return operator.invoke(t, "identity.principal."+operation, input, true)
	}
	created := checkIAMInvocation(t, change("create", "platform-other", "platform-member", map[string]any{"displayName": "Platform member", "initialPassword": "TestOnly!2026Secure"}))
	createdID := created["id"].(string)
	subject := loginIAM(t, handler, "platform-member")
	directory := checkIAMInvocation(t, operator.invoke(t, "identity.principal.list", map[string]any{"targetTenantId": "platform-other", "principalId": createdID}, true))
	if len(directory["items"].([]any)) != 1 {
		t.Fatalf("target directory missed the new principal: %v", directory)
	}
	checkStatus(t, change("suspend", "tenant-a", createdID, nil), 403)
	checkIAMInvocation(t, change("suspend", "platform-other", createdID, nil))
	checkStatus(t, subject.request(t, "GET", "/api/auth/me", nil, false), 401)
	checkIAMInvocation(t, change("restore", "platform-other", createdID, nil))
	checkStatus(t, subject.request(t, "GET", "/api/auth/me", nil, false), 401)
	checkStatus(t, loginIAM(t, handler, "platform-member").request(t, "GET", "/api/auth/me", nil, false), 200)
	var count int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM iam_security_events WHERE tenant_id='platform-other' AND resource_id=$1 AND action IN ('identity.principal.create','identity.principal.suspend','identity.principal.restore') AND outcome='allowed'`, createdID).Scan(&count); err != nil || count != 3 {
		t.Fatalf("global lifecycle audit did not reach the target tenant: %d %v", count, err)
	}
}

func TestIAMAssociateIndependentLogin(t *testing.T) {
	handler, pool, root := iamFixture(t)
	admin := loginIAM(t, handler, root.Username)
	const password = "TestOnly!2026Secure"
	const primary = "association-primary"
	const secondary = "association-secondary"
	checkIAMInvocation(t, admin.invoke(t, "identity.account.create", map[string]any{
		"username": primary, "fullName": "Primary login", "initialStatus": "active", "organizationId": root.OrganizationID,
		"departmentId": root.DepartmentID, "roleIds": []int64{}, "overrides": []any{}, "password": password,
	}, true))
	hash, err := identity.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(t.Context(), `WITH principal AS (
    INSERT INTO iam_principals(kind) VALUES ('human') RETURNING id
), login AS (
    INSERT INTO iam_login_accounts(principal_id, username) SELECT id, $1 FROM principal RETURNING id
)
INSERT INTO iam_password_credentials(login_account_id, password_hash) SELECT id, $2 FROM login`, secondary, hash); err != nil {
		t.Fatal(err)
	}
	lookup := func(username string) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(t.Context(), `SELECT principal_id::text FROM iam_login_accounts WHERE realm='local' AND username=$1`, username).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	owner := loginIAM(t, handler, primary)
	priorSession := loginIAM(t, handler, secondary)
	primaryID, original := lookup(primary), lookup(secondary)
	link := map[string]any{"username": secondary, "password": password, "secondaryPassword": "incorrect-password"}
	checkStatus(t, owner.invoke(t, "identity.loginAssociation.link", link, true), 403)
	if lookup(secondary) != original {
		t.Fatal("association accepted only one account's proof")
	}
	link["secondaryPassword"] = password
	checkStatus(t, admin.invoke(t, "identity.loginAssociation.link", link, true), 403)
	association := checkIAMInvocation(t, owner.invoke(t, "identity.loginAssociation.link", link, true))
	if lookup(secondary) != primaryID {
		t.Fatal("linked account did not resolve to the same principal")
	}
	checkStatus(t, priorSession.request(t, "GET", "/api/auth/me", nil, false), 401)
	current := loginIAM(t, handler, secondary)
	selection := map[string]any{"tenantId": "tenant-a", "departmentId": root.DepartmentID}
	checkStatus(t, current.request(t, "POST", "/api/auth/identity", selection, true), 403)
	listed := checkIAMInvocation(t, owner.invoke(t, "identity.loginAssociation.list", map[string]any{}, true))
	if len(listed["items"].([]any)) != 1 {
		t.Fatalf("wrong association list: %v", listed)
	}
	unlink := map[string]any{"id": association["id"], "password": password}
	checkStatus(t, admin.invoke(t, "identity.loginAssociation.unlink", unlink, true), 403)
	checkIAMInvocation(t, owner.invoke(t, "identity.loginAssociation.unlink", unlink, true))
	if lookup(secondary) != original {
		t.Fatal("rollback did not restore the original principal")
	}
	checkStatus(t, current.request(t, "GET", "/api/auth/me", nil, false), 401)
}

func TestIAMAccessReviewRevokesMembership(t *testing.T) {
	handler, pool, root := iamFixture(t)
	admin := loginTenantIAM(t, handler, root.Username, "tenant-a")
	created := checkIAMInvocation(t, admin.invoke(t, "identity.account.create", map[string]any{
		"username": "review-subject", "fullName": "Review subject", "initialStatus": "active",
		"password": "TestOnly!2026Secure",
	}, true))
	seedTenantRole(t, pool, "tenant-a", "review-subject", root.ID, int64(created["id"].(float64)), []*genidentity.PermissionGrant{
		{Permission: "example:read", Effect: "allow", Scope: "ALL"},
	})
	member := loginTenantIAM(t, handler, "review-subject", "tenant-a")
	checkStatus(t, member.invoke(t, "identity.review.list", map[string]any{}, true), 403)
	checkIAMInvocation(t, admin.invoke(t, "identity.review.configure", map[string]any{"intervalDays": 30}, true))
	first := checkIAMInvocation(t, admin.invoke(t, "identity.review.start", map[string]any{}, true))
	if first["created"] != float64(3) {
		t.Fatalf("wrong initial review snapshot: %v", first)
	}
	repeat := checkIAMInvocation(t, admin.invoke(t, "identity.review.start", map[string]any{}, true))
	if repeat["created"] != float64(0) {
		t.Fatalf("review snapshot duplicated open items: %v", repeat)
	}
	var memberID, rootID int64
	if err := pool.QueryRow(t.Context(), `SELECT
  (SELECT review.id FROM iam_access_reviews review JOIN iam_active_account_subjects account ON account.principal_id=review.principal_id WHERE review.tenant_id='tenant-a' AND review.kind='membership' AND account.account_id=$1),
  (SELECT review.id FROM iam_access_reviews review JOIN iam_active_account_subjects account ON account.principal_id=review.principal_id WHERE review.tenant_id='tenant-a' AND review.kind='membership' AND account.account_id=$2)`,
		created["id"], root.ID).Scan(&memberID, &rootID); err != nil {
		t.Fatal(err)
	}
	decide := func(id int64, decision string) *httptest.ResponseRecorder {
		return admin.invoke(t, "identity.review.decide", map[string]any{"id": id, "decision": decision, "reason": "Annual membership review"}, true)
	}
	checkStatus(t, decide(rootID, "retain"), 403)
	done := checkIAMInvocation(t, decide(memberID, "revoke"))
	if done["status"] != "revoked" {
		t.Fatalf("review decision was not applied: %v", done)
	}
	checkStatus(t, member.request(t, "GET", "/api/auth/me", nil, false), 401)
	checkStatus(t, decide(memberID, "retain"), 403)
}

func TestIAMScheduledReviewUsesDedicatedServicePrincipal(t *testing.T) {
	_, pool, _ := iamFixture(t)
	iam := identity.New(pool, false)
	catalog := registry.New(iam.Entries())
	if err := catalog.Err(); err != nil {
		t.Fatal(err)
	}
	g := gateway.New(gateway.Options{Identity: iam, Registry: catalog, Pool: pool})
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `UPDATE iam_tenants SET next_review_at = now() - interval '1 minute' WHERE id = 'tenant-a'`); err != nil {
		t.Fatal(err)
	}
	const name = "identity.review.runDue"
	system, err := iam.NewPrincipal(&genidentity.AccountRecord{ID: 0}, "tenant-a", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Invoke(identity.WithPrincipal(ctx, system), gateway.Request{
		Name: name, Version: "1.0.0", Origin: contracts.OriginEvent, IdempotencyKey: "wrong-service",
		Input: contracts.Input{"expectedDueAt": time.Now().UTC().Format(time.RFC3339)},
	}); contracts.Code(err) != "forbidden" {
		t.Fatalf("unbound system caller could run the review: %v", err)
	}
	done := make(chan error, 1)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		iam.RunAccessReviews(ctx, func(ctx context.Context, name, version string, input contracts.Input, key string) (*contracts.Result, error) {
			result, err := g.Invoke(ctx, gateway.Request{Name: name, Version: version, Origin: contracts.OriginEvent, Input: input, IdempotencyKey: key})
			if err == nil && result.Status != contracts.StatusSucceeded {
				err = fmt.Errorf("scheduled review returned %s", result.Status)
			}
			done <- err
			return result, err
		})
	}()
	defer func() {
		cancel()
		<-stopped
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	var recorded bool
	err = pool.QueryRow(ctx, `SELECT EXISTS (
        SELECT 1 FROM iam_security_events audit
        JOIN iam_principals worker ON worker.id::text = audit.authority->>'principalId'
        WHERE audit.tenant_id = 'tenant-a' AND audit.action = $1 AND audit.outcome = 'allowed'
        AND audit.correlation_id <> '' AND worker.kind = 'service' AND worker.creation_source = 'iam-access-review'
    )`, name).Scan(&recorded)
	if err != nil || !recorded {
		t.Fatalf("service review did not produce attributable audit: %t %v", recorded, err)
	}
}

func TestIAMAccessReviewRevokesOnlyCurrentRoleAssignment(t *testing.T) {
	handler, pool, root := iamFixture(t)
	admin := loginTenantIAM(t, handler, root.Username, "tenant-a")
	role := checkIAMInvocation(t, admin.invoke(t, "identity.role.create", map[string]any{
		"name": "Reviewed role", "code": "reviewed-role", "description": "", "status": "active",
		"grants": []map[string]any{{"permission": "roles:read", "effect": "allow", "scope": "ALL"}},
	}, true))
	account := checkIAMInvocation(t, admin.invoke(t, "identity.account.create", map[string]any{
		"username": "role-review-subject", "fullName": "Role review subject", "initialStatus": "active",
		"password": "TestOnly!2026Secure",
	}, true))
	grant := func() map[string]any {
		t.Helper()
		return checkIAMInvocation(t, admin.invoke(t, "identity.roleGrant.create", map[string]any{
			"accountId": account["id"], "roleId": role["id"], "minutes": 120, "reason": "Review independently assigned role",
		}, true))
	}
	first := grant()
	seedTenantRole(t, pool, "tenant-a", "role-review-base", root.ID, int64(account["id"].(float64)), []*genidentity.PermissionGrant{
		{Permission: "example:read", Effect: "allow", Scope: "ALL"},
	})
	member := loginTenantIAM(t, handler, "role-review-subject", "tenant-a")
	snapshot := func(grant map[string]any) int64 {
		t.Helper()
		checkIAMInvocation(t, admin.invoke(t, "identity.review.start", map[string]any{}, true))
		var reviewID int64
		if err := pool.QueryRow(t.Context(), `SELECT review.id FROM iam_access_reviews review
WHERE review.tenant_id = 'tenant-a' AND review.kind = 'context_role' AND review.status = 'pending' AND review.target_id=$1::uuid`, grant["id"]).Scan(&reviewID); err != nil {
			t.Fatal(err)
		}
		return reviewID
	}
	revoke := func(reviewID int64, expected string) {
		t.Helper()
		result := checkIAMInvocation(t, admin.invoke(t, "identity.review.decide", map[string]any{
			"id": reviewID, "decision": "revoke", "reason": "Annual role assignment review",
		}, true))
		if result["status"] != expected {
			t.Fatalf("want %s, received %v", expected, result)
		}
	}
	stale := snapshot(first)
	checkIAMInvocation(t, admin.invoke(t, "identity.roleGrant.revoke", first, true))
	replacement := grant()
	revoke(stale, "superseded")
	if response := member.invoke(t, "identity.roles.read", map[string]any{}, true); response.Code != 200 {
		t.Fatalf("replacement role lost authority: %d %s", response.Code, response.Body.String())
	}
	revoke(snapshot(replacement), "revoked")
	checkStatus(t, member.invoke(t, "identity.roles.read", map[string]any{}, true), 403)
}

func TestIAMVerifiedContactPasswordRecovery(t *testing.T) {
	var service *identity.Service
	var deliveredCode, deliveredPurpose string
	handler, pool, root := iamFixture(t, func(iam *identity.Service, _ *pgxpool.Pool, _ *genidentity.AccountRecord) {
		service = iam
		iam.ConfigureContactDelivery(func(_ context.Context, destination, purpose, code, _ string) error {
			if destination != "recovery@example.test" || len(code) != 64 {
				return fmt.Errorf("unexpected verification delivery")
			}
			deliveredCode, deliveredPurpose = code, purpose
			return nil
		}, nil)
	})
	admin := loginTenantIAM(t, handler, root.Username, "tenant-a")
	otherSession := loginTenantIAM(t, handler, root.Username, "tenant-a")
	proof := checkIAMInvocation(t, admin.invoke(t, "identity.mfa.reauthenticate", map[string]any{
		"operation": "verifyContact", "password": "TestOnly!2026Secure",
	}, true))["proof"]
	checkIAMInvocation(t, admin.invoke(t, "identity.contact.start", map[string]any{
		"proof": proof, "channel": "email", "destination": "recovery@example.test",
	}, true))
	if delivered, err := service.ProcessContactDelivery(t.Context()); err != nil || !delivered || deliveredPurpose != "verify" {
		t.Fatalf("contact challenge was not delivered: %v %v", delivered, err)
	}
	verify := map[string]any{"code": deliveredCode}
	checkStatus(t, otherSession.invoke(t, "identity.contact.verify", verify, true), 403)
	checkIAMInvocation(t, admin.invoke(t, "identity.contact.verify", verify, true))
	checkStatus(t, admin.invoke(t, "identity.contact.verify", verify, true), 403)
	anonymous := iamClient{handler: handler}
	start := func(username string) *httptest.ResponseRecorder {
		return anonymous.request(t, "POST", "/api/auth/password-reset/request", map[string]string{"username": username, "channel": "email"}, false)
	}
	checkStatus(t, start("unknown-recovery-account"), 204)
	checkStatus(t, start(root.Username), 204)
	if delivered, err := service.ProcessContactDelivery(t.Context()); err != nil || !delivered || deliveredPurpose != "password_reset" {
		t.Fatalf("reset challenge was not delivered: %v %v", delivered, err)
	}
	const newPassword = "Fresh.Test.Command!2026"
	reset := map[string]string{"token": deliveredCode, "newPassword": newPassword}
	checkStatus(t, anonymous.request(t, "POST", "/api/auth/password-reset/complete", reset, false), 204)
	checkStatus(t, anonymous.request(t, "POST", "/api/auth/password-reset/complete", reset, false), 403)
	checkStatus(t, admin.request(t, "GET", "/api/auth/me", nil, false), 401)
	checkStatus(t, otherSession.request(t, "GET", "/api/auth/me", nil, false), 401)
	checkStatus(t, anonymous.request(t, "POST", "/api/auth/login", map[string]string{"username": root.Username, "password": newPassword}, false), 200)
	var leaked bool
	if err := pool.QueryRow(t.Context(), `SELECT EXISTS (
SELECT 1 FROM cap_invocations WHERE capability LIKE 'identity.contact.%' AND (
input ? 'destination' OR input ? 'code' OR input ? 'proof' OR result->'data' IS NOT NULL AND result->'data' <> 'null'::jsonb
OR result::text LIKE '%' || $1 || '%' OR result::text LIKE '%' || $2 || '%'))`, verify["code"], deliveredCode).Scan(&leaked); err != nil || leaked {
		t.Fatalf("contact or verification secrets reached invocation storage: %v %v", leaked, err)
	}
}

func TestIAMRegistrationRequiresVerifiedContact(t *testing.T) {
	var service *identity.Service
	var code string
	handler, _, root := iamFixture(t, func(iam *identity.Service, _ *pgxpool.Pool, _ *genidentity.AccountRecord) {
		service = iam
		iam.ConfigureContactDelivery(func(_ context.Context, _ string, purpose, value, _ string) error {
			if purpose == "registration" {
				code = value
			}
			return nil
		}, nil)
	})
	admin := loginTenantIAM(t, handler, root.Username, "tenant-a")
	public := iamClient{handler: handler}
	request := map[string]string{"tenantId": "tenant-a", "username": "registered-person", "displayName": "Registered person",
		"password": "TestOnly!2026Secure", "destination": "registration@example.test"}
	start := func() *httptest.ResponseRecorder {
		return public.request(t, "POST", "/api/auth/registration/request", request, false)
	}
	checkStatus(t, start(), 204)
	if sent, err := service.ProcessContactDelivery(t.Context()); err != nil || sent {
		t.Fatalf("private workspace queued a registration: %v %v", sent, err)
	}
	checkIAMInvocation(t, admin.invoke(t, "identity.tenant.registration.configure", map[string]any{"channel": "email"}, true))
	checkStatus(t, start(), 204)
	checkStatus(t, public.request(t, "POST", "/api/auth/login", map[string]string{"username": request["username"], "password": request["password"]}, false), 401)
	if sent, err := service.ProcessContactDelivery(t.Context()); err != nil || !sent || len(code) != 64 {
		t.Fatalf("registration challenge missing: %v %v", sent, err)
	}
	complete := func(password string) *httptest.ResponseRecorder {
		return public.request(t, "POST", "/api/auth/registration/complete", map[string]string{"token": code, "password": password}, false)
	}
	checkStatus(t, complete("Invalid.Pass!2026"), 403)
	checkStatus(t, complete(request["password"]), 204)
	checkStatus(t, complete(request["password"]), 403)
	registrationLogin := public.request(t, "POST", "/api/auth/login", map[string]string{
		"username": request["username"], "password": request["password"],
	}, false)
	checkStatus(t, registrationLogin, 200)
	profile := decodeBody[genidentity.LoginResult](t, registrationLogin).Profile
	if profile.Account != nil || profile.TenantID != "" || len(profile.Permissions) != 0 {
		t.Fatalf("unexpected registered account scope: %+v", profile)
	}
	registered := iamClient{handler: handler, cookie: registrationLogin.Result().Cookies()[0], csrf: profile.CsrfToken}
	checkStatus(t, registered.request(t, "POST", "/api/auth/identity", map[string]any{"tenantId": "tenant-a"}, true), 403)
}
