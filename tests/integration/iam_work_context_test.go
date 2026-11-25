//go:build integration

package tests

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"horizon/foundation/ai/automation"
	"horizon/foundation/ai/harness"
	genidentity "horizon/foundation/gen/identity"
	businessmcp "horizon/foundation/integrations/mcp"
	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/workflow"
	"horizon/foundation/system/governance"
	"horizon/foundation/system/identity"
	"horizon/foundation/system/settings"
)

func TestIAMContextRoleAuthority(t *testing.T) {
	handler, pool, home := iamFixture(t)
	root := loginIAM(t, handler, home.Username)
	grant := func(permission, scope string) map[string]any {
		return map[string]any{"permission": permission, "effect": "allow", "scope": scope}
	}
	createAccount := func(username string, grants ...map[string]any) genidentity.AccountRecord {
		t.Helper()
		if grants == nil {
			grants = []map[string]any{}
		}
		response := root.invoke(t, "identity.account.create", map[string]any{
			"username": username, "fullName": username, "initialStatus": "active", "organizationId": home.OrganizationID,
			"departmentId": home.DepartmentID, "roleIds": []int64{}, "overrides": grants, "password": "TestOnly!2026Secure",
		}, true)
		checkStatus(t, response, 200)
		return decodeData[genidentity.AccountRecord](t, response)
	}
	managerAccount := createAccount("iam-grant-manager", grant("access:manage", "ALL"), grant("roles:create", "ALL"), grant("accounts:read", "DEPARTMENT"))
	memberAccount := createAccount("iam-bound-member", grant("example:read", "ALL"), grant("example:create", "ALL"))
	manager := loginIAM(t, handler, managerAccount.Username)
	member := loginIAM(t, handler, memberAccount.Username)
	role := map[string]any{"name": "Context role", "code": "context-role", "description": "", "status": "active", "grants": []map[string]any{grant("accounts:read", "ALL")}}
	checkStatus(t, manager.invoke(t, "identity.role.create", role, true), 403)
	created := checkIAMInvocation(t, root.invoke(t, "identity.role.create", role, true))
	assign := map[string]any{"accountId": memberAccount.ID, "roleId": created["id"], "minutes": 15, "reason": "Scoped membership review"}
	deniedGrant := manager.request(t, "POST", "/api/capabilities/identity.roleGrant.create/invocations", map[string]any{
		"input": assign, "idempotencyKey": "over-ceiling-grant",
	}, true)
	checkStatus(t, deniedGrant, 403)
	// Check the very same failed grant, rather than relying on an unrelated audit
	// fixture. The persisted whitelist identifies the requested target and role;
	// the security projection links actor/context/reason to that invocation.
	var invocationID, traceID string
	var auditMatches int
	if err := pool.QueryRow(t.Context(), `SELECT invocation_id, trace_id FROM cap_invocations
WHERE tenant_id='tenant-a' AND account_id=$1 AND capability='identity.roleGrant.create'
AND idempotency_key='over-ceiling-grant' AND status='failed' AND error_code='forbidden'
AND input->>'accountId'=$2 AND input->>'roleId'=$3`, managerAccount.ID,
		fmt.Sprint(memberAccount.ID), fmt.Sprint(created["id"])).Scan(&invocationID, &traceID); err != nil {
		t.Fatal("failed grant lost target/role or failure record", err)
	}
	if requestID := deniedGrant.Header().Get("X-Request-ID"); requestID == "" || traceID != "req_"+requestID {
		t.Fatalf("failed grant lost the request correlation: %q", traceID)
	}
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM iam_security_events
WHERE tenant_id='tenant-a' AND account_id=$1 AND department_id=$2
AND action='identity.roleGrant.create' AND outcome='failed' AND reason='forbidden'
AND resource_type='invocation' AND resource_id=$3 AND correlation_id=$4`, managerAccount.ID,
		managerAccount.DepartmentID, invocationID, traceID).Scan(&auditMatches); err != nil || auditMatches != 1 {
		t.Fatalf("failed grant has no unique correlated audit: %d %v", auditMatches, err)
	}
	auditRows := checkIAMInvocation(t, manager.invoke(t, "identity.audit.list", map[string]any{"limit": 50}, true))["items"].([]any)
	foundDenial := false
	for _, raw := range auditRows {
		row := raw.(map[string]any)
		if row["resourceId"] == invocationID && row["outcome"] == "failed" {
			foundDenial = row["accountId"] == float64(managerAccount.ID) && row["reason"] == "forbidden" && row["correlationId"] == traceID
		}
	}
	if !foundDenial {
		t.Fatal("failed grant audit cannot be queried by its actor")
	}
	assign["roleId"] = home.RoleIds[0]
	checkStatus(t, root.invoke(t, "identity.roleGrant.create", assign, true), 403)
	assign["roleId"] = created["id"]
	ref := checkIAMInvocation(t, member.invoke(t, "example.record.create", map[string]any{"title": "Supplier contact reference"}, true))
	binding := checkIAMInvocation(t, member.invoke(t, "identity.binding.claim", map[string]any{
		"identityType": "com.horizon.example.supplier-contact", "version": "1.0.0", "entityId": ref["id"],
	}, true))
	assign["bindingId"] = binding["id"]
	assigned := checkIAMInvocation(t, root.invoke(t, "identity.roleGrant.create", assign, true))
	readsAccounts := func(client iamClient) bool {
		t.Helper()
		response := client.request(t, "GET", "/api/admin/snapshot", nil, false)
		checkStatus(t, response, 200)
		return len(decodeBody[genidentity.AdminSnapshot](t, response).Accounts) > 0
	}
	if readsAccounts(member) {
		t.Fatal("binding role reached the account's base context")
	}
	selected := member.request(t, "POST", "/api/auth/identity", map[string]any{
		"tenantId": "tenant-a", "departmentId": memberAccount.DepartmentID, "identityBindingId": binding["id"],
	}, true)
	checkStatus(t, selected, 200)
	member.csrf = decodeBody[genidentity.SessionProfile](t, selected).CsrfToken
	if !readsAccounts(member) {
		t.Fatal("current contextual role not loaded")
	}
	checkIAMInvocation(t, root.invoke(t, "identity.roleGrant.revoke", assigned, true))
	if readsAccounts(member) {
		t.Fatal("revoked contextual role remained effective")
	}
	delete(assign, "bindingId")
	assigned = checkIAMInvocation(t, root.invoke(t, "identity.roleGrant.create", assign, true))
	if !readsAccounts(member) {
		t.Fatal("tenant grant missing from bound context")
	}
	if _, err := pool.Exec(context.Background(), `UPDATE iam_context_role_grants
SET created_at = now() - interval '15 minutes', expires_at = now() - interval '1 minute' WHERE id = $1::uuid`, assigned["id"]); err != nil {
		t.Fatal(err)
	}
	if readsAccounts(member) {
		t.Fatal("expired contextual role remained effective")
	}
}

func TestIAMBrowserTenantIsSelectedByDeployment(t *testing.T) {
	handler, pool, home := iamFixture(t)
	guest, err := identity.Bootstrap(t.Context(), pool, "tenant-b", "tenant-b-admin", "TestOnly!2026Secure")
	if err != nil {
		t.Fatal(err)
	}
	client := loginTenantIAM(t, handler, home.Username, "tenant-a")
	other := loginTenantIAM(t, handler, guest.Username, "tenant-b")
	listed := client.request(t, "GET", "/api/auth/identities", nil, false)
	checkStatus(t, listed, 200)
	if identities := decodeBody[[]genidentity.SignInIdentity](t, listed); len(identities) != 1 || identities[0].IdentityBindingID != nil {
		t.Fatalf("unexpected sign-in identities: %+v", identities)
	}
	checkStatus(t, client.request(t, "GET", "/api/auth/contexts", nil, false), 404)
	checkStatus(t, client.request(t, "POST", "/api/auth/context", map[string]any{"tenantId": "tenant-b"}, true), 404)
	scoped := client.request(t, "POST", "/api/auth/identity", map[string]any{"tenantId": "tenant-b"}, true)
	checkStatus(t, scoped, 200)
	if profile := decodeBody[genidentity.SessionProfile](t, scoped); profile.TenantID != "tenant-a" || profile.Account.ID != home.ID {
		t.Fatalf("client-supplied tenant changed the session: %+v", profile)
	}
	client.handler = other.handler
	checkStatus(t, client.request(t, "GET", "/api/auth/me", nil, false), 401)
	foreign := loginIAM(t, handler, guest.Username)
	checkStatus(t, foreign.request(t, "GET", "/api/admin/snapshot", nil, false), 401)
	checkStatus(t, foreign.request(t, "POST", "/api/auth/identity", map[string]any{}, true), 403)
}

func TestIAMIdentityBindingLifecycle(t *testing.T) {
	handler, pool, home := iamFixture(t)
	guest, err := identity.Bootstrap(context.Background(), pool, "tenant-b", "tenant-b-admin", "TestOnly!2026Secure")
	if err != nil {
		t.Fatal(err)
	}
	owner := loginIAM(t, handler, home.Username)
	record := checkIAMInvocation(t, owner.invoke(t, "example.record.create", map[string]any{"title": "Employee reference"}, true))
	input := map[string]any{"identityType": "com.horizon.example.employee", "version": "1.0.0", "entityId": record["id"]}
	checkStatus(t, owner.invoke(t, "identity.binding.record", input, true), 403)
	binding := checkIAMInvocation(t, owner.invoke(t, "identity.binding.claim", input, true))
	target := map[string]any{"tenantId": "tenant-a", "departmentId": home.DepartmentID, "identityBindingId": binding["id"]}
	bound := loginIAM(t, handler, home.Username)
	selected := bound.request(t, "POST", "/api/auth/identity", target, true)
	checkStatus(t, selected, 200)
	profile := decodeBody[genidentity.SessionProfile](t, selected)
	if profile.IdentityBindingID == nil || *profile.IdentityBindingID != binding["id"] {
		t.Fatalf("unexpected binding in selected context: %+v", profile.IdentityBindingID)
	}
	bound.csrf = profile.CsrfToken
	checkStatus(t, bound.invoke(t, "identity.membership.list", map[string]any{}, true), 403)
	checkStatus(t, bound.invoke(t, "identity.governance.report", map[string]any{}, true), 403)
	other := loginIAM(t, handler, guest.Username)
	foreignTarget := map[string]any{"tenantId": "tenant-b", "departmentId": guest.DepartmentID, "identityBindingId": binding["id"]}
	checkStatus(t, other.request(t, "POST", "/api/auth/identity", foreignTarget, true), 403)
	started := bound.invoke(t, "orchestration.workflow.run", map[string]any{
		"flow": "example-create-archive", "version": "1.0.0", "input": map[string]any{"title": "Workflow after binding revocation"},
	}, true)
	run := checkIAMInvocation(t, started)
	invocation := decodeBody[contracts.Result](t, started)
	tracePath := "/api/capabilities/invocations/" + invocation.InvocationID
	checkStatus(t, bound.request(t, "GET", tracePath, nil, false), 200)
	checkStatus(t, owner.request(t, "GET", tracePath, nil, false), 404)
	listed := owner.request(t, "GET", "/api/capabilities/invocations", nil, false)
	checkStatus(t, listed, 200)
	if strings.Contains(listed.Body.String(), invocation.InvocationID) {
		t.Fatal("base context listed a bound invocation")
	}
	checkStatus(t, owner.invoke(t, "orchestration.workflow.get", map[string]any{"runId": run["runId"]}, true), 200)
	task := checkIAMInvocation(t, bound.invoke(t, "automation.task.create", map[string]any{
		"name": "Bound reminder", "description": "Summarize the current workspace", "scheduleKind": "daily", "time": "09:00", "timezone": "UTC",
	}, true))
	queued := checkIAMInvocation(t, bound.invoke(t, "automation.run.now", task, true))
	hidden := owner.invoke(t, "automation.task.get", task, true)
	checkStatus(t, hidden, 200)
	if result := decodeBody[contracts.Result](t, hidden); result.Status != contracts.StatusFailed || result.ErrorCode != "not_found" {
		t.Fatalf("base context could see a bound task: %+v", result)
	}
	workspace := decodeData[settings.SettingsWorkspace](t, bound.invoke(t, "settings.workspace.read", map[string]any{
		"targetKind": "workspace", "targetId": 0,
	}, true))
	draft := decodeData[settings.ChangeRecord](t, bound.invoke(t, "settings.draft.create", map[string]any{
		"target": map[string]any{"kind": "workspace", "id": 0}, "baseRevision": workspace.Revision, "reason": "Bound publication",
		"changes": []map[string]any{{"key": "branding.name", "operation": "set", "value": `"Bound trial"`}},
	}, true))
	proof := decodeData[settings.ReauthProof](t, bound.invoke(t, "settings.password.verify", map[string]any{
		"password": "TestOnly!2026Secure", "operation": "settings.schedule", "resourceId": draft.ID, "revision": workspace.Revision,
	}, true))
	scheduled := checkIAMInvocation(t, bound.invoke(t, "settings.schedule.create", map[string]any{
		"id": draft.ID, "baseRevision": workspace.Revision, "proof": proof.Proof,
		"scheduledAt": time.Now().UTC().Add(5 * time.Minute).Format(time.RFC3339),
	}, true))
	invitation := checkIAMInvocation(t, bound.invoke(t, "identity.membership.invite", map[string]any{
		"username": guest.Username, "organizationId": home.OrganizationID, "departmentId": home.DepartmentID,
	}, true))
	checkBindingExecutionContexts(t, owner, bound, home.DepartmentID, record["id"])
	checkStatus(t, other.invoke(t, "identity.membership.accept", map[string]any{"code": invitation["code"]}, true), 403)
	checkStatus(t, bound.request(t, "GET", "/api/auth/me", nil, false), 401)
	checkStatus(t, owner.request(t, "GET", "/api/auth/me", nil, false), 200)
	checkStatus(t, owner.request(t, "POST", "/api/auth/identity", target, true), 403)
	engine := orchestration.NewWithGraphs(governance.New(pool, identity.New(pool, false), governance.Options{
		EncryptionKey: []byte("01234567890123456789012345678901"),
	}), nil, nil)
	ctx := context.Background()
	if err := engine.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	var status, errorCode string
	if err := pool.QueryRow(ctx, `SELECT status, error_code FROM ai_runs WHERE tenant_id = 'tenant-a' AND id = $1`, int64(run["runId"].(float64))).Scan(&status, &errorCode); err != nil || status != "failed" || errorCode != "forbidden" {
		t.Fatalf("workflow failed to enforce its original binding: %s %s %v", status, errorCode, err)
	}
	checkIAMDeferredRevocation(t, pool, int64(queued["id"].(float64)), int64(scheduled["id"].(float64)))
}

func checkIAMDeferredRevocation(t *testing.T, pool *pgxpool.Pool, runID, scheduleID int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	iam := identity.New(pool, false)
	worker := automation.NewWorker(pool, &harness.Service{}, iam)
	go worker.Run(ctx)
	var status, code string
	for {
		if err := pool.QueryRow(ctx, `SELECT status, error_code FROM automation_runs WHERE id = $1`, runID).Scan(&status, &code); err != nil {
			t.Fatal(err)
		}
		if status == "skipped" && code == "forbidden" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("automation retained authority: %s %s", status, code)
		case <-time.After(25 * time.Millisecond):
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE gov_settings_schedules SET scheduled_at = now() WHERE id = $1`, scheduleID); err != nil {
		t.Fatal(err)
	}
	service := settings.New(governance.New(pool, iam, governance.Options{}))
	if err := service.ProcessScheduledOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT status, error_code FROM gov_settings_schedules WHERE id = $1`, scheduleID).Scan(&status, &code); err != nil || status != "failed" || code != "unauthenticated" {
		t.Fatalf("settings publication retained authority: %s %s %v", status, code, err)
	}
}

func TestIAMDeviceSessionRevocation(t *testing.T) {
	handler, pool, account := iamFixture(t)
	otherAccount, err := identity.Bootstrap(context.Background(), pool, "other-tenant", "other-device-owner", "TestOnly!2026Secure")
	if err != nil {
		t.Fatal(err)
	}
	primary := loginIAM(t, handler, account.Username)
	secondary := loginIAM(t, handler, account.Username)
	other := loginIAM(t, handler, otherAccount.Username)
	list := func(client iamClient) []map[string]any {
		t.Helper()
		response := client.invoke(t, "identity.session.list", map[string]any{}, true)
		checkStatus(t, response, 200)
		return decodeData[struct{ Items []map[string]any }](t, response).Items
	}
	devices := list(primary)
	foreign := list(other)
	if len(devices) != 2 || devices[0]["current"] != true || devices[1]["current"] != false || len(foreign) != 1 {
		t.Fatalf("unexpected device ownership or current marker: %v %v", devices, foreign)
	}
	verify := func(client iamClient, id any) string {
		t.Helper()
		result := checkIAMInvocation(t, client.invoke(t, "identity.session.reauthenticate", map[string]any{"id": id, "password": "TestOnly!2026Secure"}, true))
		return result["proof"].(string)
	}
	target := devices[1]["id"]
	foreignID := foreign[0]["id"]
	checkStatus(t, primary.invoke(t, "identity.session.reauthenticate", map[string]any{"id": target, "password": "incorrect"}, true), 403)
	proof := verify(primary, target)
	checkStatus(t, secondary.invoke(t, "identity.session.revoke", map[string]any{"id": target, "proof": proof}, true), 403)
	checkStatus(t, primary.invoke(t, "identity.session.revoke", map[string]any{"id": foreignID, "proof": proof}, true), 403)
	checkStatus(t, primary.invoke(t, "identity.session.revoke", map[string]any{"id": foreignID, "proof": verify(primary, foreignID)}, true), 403)
	request := map[string]any{"id": target, "proof": proof}
	checkIAMInvocation(t, primary.invoke(t, "identity.session.revoke", request, true))
	checkStatus(t, primary.invoke(t, "identity.session.revoke", request, true), 403)
	checkStatus(t, secondary.request(t, "GET", "/api/auth/me", nil, false), 401)
	checkStatus(t, primary.request(t, "GET", "/api/auth/me", nil, false), 200)
	checkStatus(t, other.request(t, "GET", "/api/auth/me", nil, false), 200)
	var leaked bool
	if err := pool.QueryRow(context.Background(), `SELECT EXISTS (SELECT 1 FROM cap_invocations
WHERE capability LIKE 'identity.session.%' AND (input ? 'password' OR input ? 'proof' OR result::text LIKE '%' || $1 || '%'))`, proof).Scan(&leaked); err != nil || leaked {
		t.Fatalf("session proof or password was persisted in the invocation history: %v", err)
	}
}

func TestIAMAuthenticatorAndRecoveryCodes(t *testing.T) {
	handler, pool, account := iamFixture(t)
	primary := loginIAM(t, handler, account.Username)
	secondary := loginIAM(t, handler, account.Username)
	status := checkIAMInvocation(t, primary.invoke(t, "identity.mfa.status", map[string]any{}, true))
	if status["enabled"] != false || status["pending"] != false || status["recoveryRemaining"] != float64(0) {
		t.Fatalf("unexpected initial MFA status: %+v", status)
	}
	enrollment := checkIAMInvocation(t, primary.invoke(t, "identity.mfa.enroll", map[string]any{"password": "TestOnly!2026Secure"}, true))
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(enrollment["secret"].(string))
	if err != nil || len(secret) != 20 {
		t.Fatal("invalid authenticator enrollment")
	}
	codeAt := func(counter int64) string {
		var timestamp [8]byte
		binary.BigEndian.PutUint64(timestamp[:], uint64(counter))
		mac := hmac.New(sha1.New, secret)
		_, _ = mac.Write(timestamp[:])
		sum := mac.Sum(nil)
		offset := int(sum[len(sum)-1] & 15)
		return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(sum[offset:offset+4])&0x7fffffff)%1000000)
	}
	ctx := context.Background()
	var counter int64
	if err := pool.QueryRow(ctx, `SELECT floor(extract(epoch FROM statement_timestamp()) / 30)::bigint`).Scan(&counter); err != nil {
		t.Fatal(err)
	}
	request := map[string]any{"code": codeAt(counter)}
	checkStatus(t, secondary.invoke(t, "identity.mfa.activate", request, true), 403)
	issued := checkIAMInvocation(t, primary.invoke(t, "identity.mfa.activate", request, true))
	recoveryCodes := issued["recoveryCodes"].([]any)
	if len(recoveryCodes) != 10 {
		t.Fatal("unexpected recovery code count")
	}
	status = checkIAMInvocation(t, primary.invoke(t, "identity.mfa.status", map[string]any{}, true))
	if status["enabled"] != true || status["pending"] != false || status["recoveryRemaining"] != float64(10) || status["secret"] != nil {
		t.Fatalf("unexpected activated MFA projection: %+v", status)
	}
	checkStatus(t, secondary.request(t, "GET", "/api/auth/me", nil, false), 401)
	anonymous := iamClient{handler: handler}
	login := func(field string, value any, status int) {
		t.Helper()
		credentials := map[string]any{"username": account.Username, "password": "TestOnly!2026Secure"}
		if field != "" {
			credentials[field] = value
		}
		checkStatus(t, anonymous.request(t, "POST", "/api/auth/login", credentials, false), status)
	}
	login("", nil, 401)
	login("mfaCode", codeAt(counter), 401)
	login("mfaCode", codeAt(counter+1), 200)
	login("mfaCode", codeAt(counter+1), 401)
	login("recoveryCode", recoveryCodes[0], 200)
	login("recoveryCode", recoveryCodes[0], 401)
	refresh := func(client iamClient, factor any) *httptest.ResponseRecorder {
		t.Helper()
		input := map[string]any{"password": "TestOnly!2026Secure"}
		if factor != nil {
			input["factor"] = factor
		}
		return client.request(t, "POST", "/api/auth/session/refresh", input, true)
	}
	checkStatus(t, refresh(primary, nil), 403)
	refreshed := refresh(primary, recoveryCodes[9])
	checkStatus(t, refreshed, 200)
	checkStatus(t, primary.request(t, "GET", "/api/auth/me", nil, false), 401)
	nextProfile := decodeBody[struct{ Profile genidentity.SessionProfile }](t, refreshed).Profile
	primary.cookie, primary.csrf = refreshed.Result().Cookies()[0], nextProfile.CsrfToken
	checkStatus(t, refresh(primary, recoveryCodes[9]), 403)
	reauth := func(operation string, factor any) *httptest.ResponseRecorder {
		t.Helper()
		return primary.invoke(t, "identity.mfa.reauthenticate", map[string]any{
			"operation": operation, "password": "TestOnly!2026Secure", "recoveryCode": factor,
		}, true)
	}
	checkStatus(t, reauth("disable", recoveryCodes[0]), 403)
	proof := checkIAMInvocation(t, reauth("renewRecoveryCodes", recoveryCodes[1]))["proof"]
	checkStatus(t, primary.invoke(t, "identity.mfa.disable", map[string]any{"proof": proof}, true), 403)
	renewed := checkIAMInvocation(t, primary.invoke(t, "identity.mfa.renewRecoveryCodes", map[string]any{"proof": proof}, true))
	newCodes := renewed["recoveryCodes"].([]any)
	if len(newCodes) != 10 {
		t.Fatal("recovery code renewal did not replace all codes")
	}
	checkStatus(t, primary.invoke(t, "identity.mfa.renewRecoveryCodes", map[string]any{"proof": proof}, true), 403)
	checkStatus(t, reauth("disable", recoveryCodes[2]), 403)
	replaceProof := checkIAMInvocation(t, reauth("replace", newCodes[0]))["proof"]
	replacement := checkIAMInvocation(t, primary.invoke(t, "identity.mfa.replace", map[string]any{"proof": replaceProof}, true))
	login("recoveryCode", newCodes[1], 200)
	secret, err = base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(replacement["secret"].(string))
	if err != nil || len(secret) != 20 {
		t.Fatal("invalid replacement authenticator secret", err)
	}
	if err = pool.QueryRow(ctx, `SELECT floor(extract(epoch FROM statement_timestamp()) / 30)::bigint`).Scan(&counter); err != nil {
		t.Fatal(err)
	}
	replaceInput := map[string]any{"code": codeAt(counter)}
	replaced := checkIAMInvocation(t, primary.invoke(t, "identity.mfa.activateReplacement", replaceInput, true))
	checkStatus(t, primary.invoke(t, "identity.mfa.activateReplacement", replaceInput, true), 403)
	checkStatus(t, reauth("disable", newCodes[2]), 403)
	disableProof := checkIAMInvocation(t, reauth("disable", replaced["recoveryCodes"].([]any)[0]))["proof"]
	checkIAMInvocation(t, primary.invoke(t, "identity.mfa.disable", map[string]any{"proof": disableProof}, true))
	status = checkIAMInvocation(t, primary.invoke(t, "identity.mfa.status", map[string]any{}, true))
	if status["enabled"] != false || status["recoveryRemaining"] != float64(0) {
		t.Fatalf("disabled authenticator kept recovery material: %+v", status)
	}
	var exposed bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (
SELECT 1 FROM cap_invocations WHERE capability LIKE 'identity.mfa.%' AND
(input ? 'password' OR input ? 'code' OR result::text LIKE '%' || $1 || '%' OR result::text LIKE '%' || $2 || '%'))`, enrollment["secret"], recoveryCodes[0]).Scan(&exposed); err != nil || exposed {
		t.Fatal("authenticator or recovery material persisted in the invocation history", err)
	}
}

func checkBindingExecutionContexts(t *testing.T, base, bound iamClient, departmentID int64, bindingEntityID any) {
	t.Helper()
	write := map[string]any{"input": map[string]any{"title": "Bound invocation"}, "idempotencyKey": "iam-bound-invocation"}
	created := checkIAMInvocation(t, bound.request(t, "POST", "/api/capabilities/example.record.create/invocations", write, true))
	checkStatus(t, base.request(t, "POST", "/api/capabilities/example.record.create/invocations", write, true), 409)
	replayed := bound.request(t, "POST", "/api/capabilities/example.record.create/invocations", write, true)
	checkStatus(t, replayed, 200)
	if !decodeBody[contracts.Result](t, replayed).Replayed {
		t.Fatal("same-context retry did not replay")
	}
	checkStatus(t, bound.invoke(t, "mcp.token.create", map[string]any{
		"name": "bound client", "password": "incorrect password", "days": 1, "departmentId": departmentID,
	}, true), 403)
	token := decodeData[businessmcp.MCPTokenCreated](t, bound.invoke(t, "mcp.token.create", map[string]any{
		"name": "bound client", "password": "TestOnly!2026Secure", "days": 1, "departmentId": departmentID,
	}, true))
	server := httptest.NewServer(bound.handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client, err := sdk.NewClient(&sdk.Implementation{Name: "iam-binding-test", Version: "1"}, nil).Connect(ctx, &sdk.StreamableClientTransport{
		Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: workflowBearer{token: token.Secret}},
		DisableStandaloneSSE: true, MaxRetries: -1,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	approval, err := client.CallTool(ctx, &sdk.CallToolParams{Name: "example_record_archive", Arguments: map[string]any{"id": created["id"]}})
	if err != nil || approval.IsError {
		t.Fatalf("bound tool failed: %v %+v", err, approval)
	}
	output, ok := approval.StructuredContent.(map[string]any)
	if !ok || output["status"] != string(contracts.StatusAwaitingApproval) {
		t.Fatalf("expected bound pending approval: %+v", approval.StructuredContent)
	}
	decision := fmt.Sprintf("/api/capabilities/approvals/%.0f/decisions", output["approvalId"])
	checkStatus(t, base.request(t, "POST", decision, map[string]any{"approve": true}, true), 403)
	checkIAMInvocation(t, bound.request(t, "POST", decision, map[string]any{"approve": true}, true))
	checkIAMInvocation(t, base.invoke(t, "example.record.archive", map[string]any{"id": bindingEntityID}, true))
	if _, err := client.ListTools(ctx, nil); err == nil {
		t.Fatal("MCP token still authorized after revoking the original binding")
	}
}

func checkIAMInvocation(t *testing.T, response *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	checkStatus(t, response, 200)
	result := decodeBody[contracts.Result](t, response)
	if result.Status != contracts.StatusSucceeded {
		t.Fatalf("invocation did not succeed: %s", response.Body.String())
	}
	data, _ := result.Data.(map[string]any)
	return data
}
