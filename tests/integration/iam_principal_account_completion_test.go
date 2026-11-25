//go:build integration

package tests

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	genidentity "horizon/foundation/gen/identity"
	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/system/identity"
)

const accountCompletionPassword = "TestOnly!2026Secure"

func TestIAMBrowserSessionRefresh(t *testing.T) {
	handler, pool, root := iamFixture(t)
	actor := loginTenantIAM(t, handler, root.Username, "tenant-a")
	body := map[string]string{"password": accountCompletionPassword}
	const path = "/api/auth/session/refresh"
	checkStatus(t, actor.request(t, "POST", path, body, false), 403)
	checkStatus(t, actor.request(t, "POST", path, map[string]string{"password": "incorrect"}, true), 403)
	var device string
	if err := pool.QueryRow(t.Context(), `UPDATE iam_sessions SET expires_at=now()+interval '1 minute'
		WHERE tenant_id='tenant-a' AND account_id=$1 RETURNING public_id::text`, root.ID).Scan(&device); err != nil {
		t.Fatal(err)
	}
	refreshed := actor.request(t, "POST", path, body, true)
	checkStatus(t, refreshed, 200)
	cookies := refreshed.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Value == actor.cookie.Value || !cookies[0].HttpOnly || strings.Contains(refreshed.Body.String(), cookies[0].Value) {
		t.Fatal("refresh did not rotate the token exclusively through the HttpOnly cookie")
	}
	profile := decodeBody[struct{ Profile genidentity.SessionProfile }](t, refreshed).Profile
	if profile.CsrfToken == actor.csrf || profile.Account.ID != root.ID {
		t.Fatal("refresh failed to rotate CSRF and retain the account")
	}
	checkStatus(t, actor.request(t, "GET", "/api/auth/me", nil, false), 401)
	next := iamClient{handler: handler, cookie: cookies[0], csrf: profile.CsrfToken}
	checkStatus(t, next.request(t, "GET", "/api/auth/me", nil, false), 200)
	var keptID string
	if err := pool.QueryRow(t.Context(), `SELECT public_id::text FROM iam_sessions
		WHERE tenant_id='tenant-a' AND account_id=$1 AND refreshed_at IS NOT NULL
		  AND expires_at > now()+interval '2 minutes'`, root.ID).Scan(&keptID); err != nil || keptID != device {
		t.Fatalf("refresh changed device identity or failed to renew expiry: %s %v", keptID, err)
	}
}

func TestIAMPrincipalDetailAndRiskMetadata(t *testing.T) {
	handler, pool, root := iamFixture(t, func(iam *identity.Service, pool *pgxpool.Pool, root *genidentity.AccountRecord) {
		var id string
		if err := pool.QueryRow(t.Context(), `SELECT principal_id::text FROM iam_active_account_subjects WHERE tenant_id='tenant-a' AND account_id=$1`, root.ID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		iam.ConfigurePlatformOperators([]string{id})
	})
	operator := loginTenantIAM(t, handler, root.Username, "tenant-a")
	other, err := identity.Bootstrap(t.Context(), pool, "principal-other", "other-principal-admin", accountCompletionPassword)
	if err != nil {
		t.Fatal(err)
	}
	target := loginTenantIAM(t, handler, other.Username, "principal-other")
	var id string
	if err = pool.QueryRow(t.Context(), `SELECT principal_id::text FROM iam_active_account_subjects WHERE tenant_id='principal-other' AND account_id=$1`, other.ID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"targetTenantId": "principal-other", "principalId": id, "reason": "Reviewed identity metadata"}
	checkStatus(t, target.invoke(t, "identity.principal.detail", input, true), 403)
	detail := checkIAMInvocation(t, operator.invoke(t, "identity.principal.detail", input, true))
	if len(detail["logins"].([]any)) != 1 || len(detail["sessions"].([]any)) != 1 || len(detail["memberships"].([]any)) != 1 {
		t.Fatalf("incomplete principal detail: %v", detail)
	}
	update := map[string]any{
		"targetTenantId": "principal-other", "target": id, "riskLevel": "elevated",
		"securityNotes": "Internal review note", "reason": "Security reviewed the subject",
	}
	proof := make(map[string]any, len(update)+2)
	for k, v := range update {
		proof[k] = v
	}
	proof["password"], proof["operation"] = accountCompletionPassword, "setSecurity"
	update["proof"] = checkIAMInvocation(t, operator.invoke(t, "identity.principal.reauthenticate", proof, true))["proof"]
	update["riskLevel"] = "high"
	checkStatus(t, operator.invoke(t, "identity.principal.setSecurity", update, true), 403)
	update["riskLevel"] = "elevated"
	checkIAMInvocation(t, operator.invoke(t, "identity.principal.setSecurity", update, true))
	detail = checkIAMInvocation(t, operator.invoke(t, "identity.principal.detail", input, true))
	if detail["riskLevel"] != "elevated" || detail["securityNotes"] != update["securityNotes"] {
		t.Fatalf("security profile did not persist: %v", detail)
	}
	input["targetTenantId"] = "tenant-a"
	failed := operator.invoke(t, "identity.principal.detail", input, true)
	checkStatus(t, failed, 200)
	if result := decodeBody[contracts.Result](t, failed); result.ErrorCode != "not_found" || result.Data != nil {
		t.Fatalf("foreign principal was visible: %+v", result)
	}
	var audited int
	if err = pool.QueryRow(t.Context(), `SELECT count(*) FROM iam_security_events
		WHERE tenant_id='principal-other' AND action='identity.principal.detail' AND resource_id=$1`, id).Scan(&audited); err != nil || audited != 2 {
		t.Fatalf("principal inspection was not audited: %d %v", audited, err)
	}
}

func TestIAMReviewedLoginMergeAndRollback(t *testing.T) {
	handler, pool, root := iamFixture(t)
	admin := loginIAM(t, handler, root.Username)
	const primary = "duplicate-main"
	const secondary = "duplicate-second"
	create := func(username string) iamClient {
		t.Helper()
		overrides := []any{}
		if username == secondary {
			overrides = append(overrides, map[string]any{"permission": "roles:read", "scope": "ALL", "effect": "allow"})
		}
		checkIAMInvocation(t, admin.invoke(t, "identity.account.create", map[string]any{
			"username": username, "fullName": username, "initialStatus": "active", "organizationId": root.OrganizationID,
			"departmentId": root.DepartmentID, "roleIds": []int64{}, "overrides": overrides, "password": accountCompletionPassword,
		}, true))
		return loginIAM(t, handler, username)
	}
	first, second, unrelated := create(primary), create(secondary), create("duplicate-unrelated")
	placement := func(name string) (int64, string) {
		t.Helper()
		var id int64
		var subject string
		if err := pool.QueryRow(t.Context(), `SELECT p.id,l.principal_id::text
			FROM iam_accounts p JOIN iam_login_accounts l ON p.login_account_id=l.id
			WHERE p.tenant_id='tenant-a' AND l.username=$1`, name).Scan(&id, &subject); err != nil {
			t.Fatal(err)
		}
		return id, subject
	}
	_, firstID := placement(primary)
	secondPlacement, secondID := placement(secondary)
	request := checkIAMInvocation(t, first.invoke(t, "identity.loginMerge.request", map[string]any{
		"username": secondary, "password": accountCompletionPassword, "reason": "Same person verified by two accounts",
	}, true))
	decision := map[string]any{
		"id": request["id"], "password": accountCompletionPassword, "reason": "Independent account review", "decision": "apply",
	}
	premature := admin.invoke(t, "identity.loginMerge.decide", decision, true)
	checkStatus(t, premature, 200)
	if failed := decodeBody[contracts.Result](t, premature); failed.ErrorCode != "conflict" {
		t.Fatalf("merge without consent: %+v", failed)
	}
	list := checkIAMInvocation(t, unrelated.invoke(t, "identity.loginMerge.list", map[string]any{}, true))
	if len(list["items"].([]any)) != 0 {
		t.Fatal("unrelated login saw a private merge")
	}
	proof := map[string]any{"id": request["id"], "password": accountCompletionPassword}
	checkStatus(t, first.invoke(t, "identity.loginMerge.confirm", proof, true), 403)
	checkIAMInvocation(t, second.invoke(t, "identity.loginMerge.confirm", proof, true))
	checkStatus(t, first.invoke(t, "identity.loginMerge.decide", decision, true), 403)
	applied := checkIAMInvocation(t, admin.invoke(t, "identity.loginMerge.decide", decision, true))
	if applied["status"] != "applied" {
		t.Fatalf("independent decision failed: %v", applied)
	}
	checkStatus(t, first.request(t, http.MethodGet, "/api/auth/me", nil, false), 401)
	checkStatus(t, second.request(t, http.MethodGet, "/api/auth/me", nil, false), 401)
	if account, subject := placement(secondary); subject != firstID || account != secondPlacement {
		t.Fatal("merge did not preserve tenant account and resolve the common principal")
	}
	first = loginIAM(t, handler, primary)
	second = loginIAM(t, handler, secondary)
	firstProfile := decodeBody[genidentity.SessionProfile](t, first.request(t, "GET", "/api/auth/me", nil, false))
	secondProfile := decodeBody[genidentity.SessionProfile](t, second.request(t, "GET", "/api/auth/me", nil, false))
	if slices.Contains(firstProfile.Permissions, "roles:read") || !slices.Contains(secondProfile.Permissions, "roles:read") {
		t.Fatal("merge combined account-scoped permissions")
	}
	decision["decision"] = "rollback"
	checkIAMInvocation(t, admin.invoke(t, "identity.loginMerge.decide", decision, true))
	checkStatus(t, second.request(t, http.MethodGet, "/api/auth/me", nil, false), 401)
	if account, subject := placement(secondary); subject != secondID || account != secondPlacement {
		t.Fatal("rollback did not restore original principal")
	}
	checkStatus(t, first.request(t, http.MethodGet, "/api/auth/me", nil, false), 401)

	first, second = loginIAM(t, handler, primary), loginIAM(t, handler, secondary)
	stale := checkIAMInvocation(t, first.invoke(t, "identity.loginMerge.request", map[string]any{
		"username": secondary, "password": accountCompletionPassword, "reason": "Check expiration of verified session",
	}, true))
	proof["id"] = stale["id"]
	checkIAMInvocation(t, second.invoke(t, "identity.loginMerge.confirm", proof, true))
	checkStatus(t, first.request(t, "POST", "/api/auth/logout", nil, true), 200)
	decision["id"], decision["decision"] = stale["id"], "apply"
	expired := admin.invoke(t, "identity.loginMerge.decide", decision, true)
	checkStatus(t, expired, 200)
	if failed := decodeBody[contracts.Result](t, expired); failed.ErrorCode != "conflict" {
		t.Fatalf("merge used a revoked confirmation session: %+v", failed)
	}
}
