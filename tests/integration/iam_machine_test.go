//go:build integration

package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"horizon/applications/example"
	genidentity "horizon/foundation/gen/identity"
	"horizon/foundation/kernel/approval"
	"horizon/foundation/kernel/audit"
	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/capability/gateway"
	"horizon/foundation/kernel/capability/registry"
	"horizon/foundation/system/identity"
	"horizon/internal/repository/dbgen"
)

func TestIAMMachineCredentials(t *testing.T) {
	handler, pool, root := iamFixture(t)
	owner := loginTenantIAM(t, handler, root.Username, "tenant-a")
	password := "TestOnly!2026Secure"
	create := func(kind string) map[string]any {
		t.Helper()
		return checkIAMInvocation(t, owner.invoke(t, "identity.machine.create", map[string]any{
			"kind": kind, "name": kind + " integration principal", "password": password,
			"grants": []map[string]any{{"permission": "example:create", "scope": "OWN", "effect": "allow"}},
		}, true))
	}
	service := create("service")
	agent := create("agent")
	var machineHasLogin bool
	if err := pool.QueryRow(t.Context(), `SELECT EXISTS (SELECT 1 FROM iam_login_accounts WHERE principal_id IN ($1::uuid, $2::uuid))`, service["principalId"], agent["principalId"]).Scan(&machineHasLogin); err != nil || machineHasLogin {
		t.Fatalf("machine principals must not have human login accounts: %v %v", machineHasLogin, err)
	}
	rotate := func(target any, audience, key string) *httptest.ResponseRecorder {
		t.Helper()
		return owner.request(t, http.MethodPost, "/api/capabilities/identity.machine.credential.rotate/invocations", map[string]any{
			"idempotencyKey": key, "input": map[string]any{
				"accountId": target, "audience": audience, "minutes": 30, "password": password,
				"capabilities": []string{"example.record.create@1.0.0", "example.record.archive@1.0.0", "identity.accounts.read@1.0.0"},
			},
		}, true)
	}
	invoke := func(name, version, secret string, want int) contracts.Result {
		t.Helper()
		body, err := json.Marshal(map[string]any{
			"version": version, "idempotencyKey": "external-job-1", "input": map[string]any{"title": "Machine-owned record"},
		})
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, "/api/capabilities/"+name+"/machine-invocations", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+secret)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		checkStatus(t, response, want)
		return decodeBody[contracts.Result](t, response)
	}
	issued := checkIAMInvocation(t, rotate(service["id"], "horizon-capability", "first"))
	secret := issued["secret"].(string)
	replayIssue := rotate(service["id"], "horizon-capability", "first")
	checkStatus(t, replayIssue, 200)
	if result := decodeBody[contracts.Result](t, replayIssue); !result.Replayed || result.Data != nil {
		t.Fatalf("credential rotation replay disclosed data or reissued a credential: %+v", result)
	}
	first := invoke("example.record.create", "1.0.0", secret, 200)
	if first.Status != contracts.StatusSucceeded || first.Replayed {
		t.Fatalf("machine invocation: %+v", first)
	}
	var accountID int64
	if err := pool.QueryRow(t.Context(), `SELECT account_id FROM cap_invocations WHERE invocation_id = $1`, first.InvocationID).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	if float64(accountID) != service["id"] {
		t.Fatal("invocation was attributed to the credential issuer")
	}
	if replay := invoke("example.record.create", "1.0.0", secret, 200); !replay.Replayed || replay.InvocationID != first.InvocationID {
		t.Fatalf("machine invocation did not replay its own result: %+v", replay)
	}
	invoke("example.record.get", "1.0.0", secret, 403)
	invoke("example.record.archive", "1.0.0", secret, 403)
	invoke("identity.accounts.read", "1.0.0", secret, 403)
	checkStatus(t, (iamClient{handler: handler, cookie: &http.Cookie{Name: owner.cookie.Name, Value: secret}}).request(t, http.MethodGet, "/api/auth/me", nil, false), 401)
	agentCredential := checkIAMInvocation(t, rotate(agent["id"], "horizon-agent", "agent"))
	invoke("example.record.create", "1.0.0", agentCredential["secret"].(string), 401)

	replacement := checkIAMInvocation(t, rotate(service["id"], "horizon-capability", "replacement"))
	invoke("example.record.create", "1.0.0", secret, 401)
	checkIAMInvocation(t, owner.invoke(t, "identity.machine.credential.revoke", map[string]any{"accountId": service["id"], "password": password}, true))
	invoke("example.record.create", "1.0.0", replacement["secret"].(string), 401)
	expiring := checkIAMInvocation(t, rotate(service["id"], "horizon-capability", "expires"))
	if _, err := pool.Exec(t.Context(), `UPDATE iam_machine_credentials SET created_at=now()-interval '2 minutes', expires_at=now()-interval '1 minute' WHERE id=$1`, expiring["id"]); err != nil {
		t.Fatal(err)
	}
	invoke("example.record.create", "1.0.0", expiring["secret"].(string), 401)
	active := checkIAMInvocation(t, rotate(service["id"], "horizon-capability", "before-suspension"))
	checkIAMInvocation(t, owner.invoke(t, "identity.membership.suspend", map[string]any{"accountId": service["id"], "reason": "Suspend machine access"}, true))
	invoke("example.record.create", "1.0.0", active["secret"].(string), 401)
	checkIAMInvocation(t, owner.invoke(t, "identity.membership.restore", map[string]any{"accountId": service["id"], "reason": "Restore machine membership"}, true))
	invoke("example.record.create", "1.0.0", active["secret"].(string), 401)
}

func TestIAMBoundAgentDelegationChecksResourceRelationship(t *testing.T) {
	var service *identity.Service
	handler, pool, root := iamFixture(t, func(iam *identity.Service, _ *pgxpool.Pool, _ *genidentity.AccountRecord) {
		service = iam
	})
	admin := loginTenantIAM(t, handler, root.Username, "tenant-a")
	const password = "TestOnly!2026Secure"
	const bindingType = "com.horizon.example.employee"
	const employee = "binding-employee"
	account := checkIAMInvocation(t, admin.invoke(t, "identity.account.create", map[string]any{
		"username": employee, "fullName": "Binding employee", "initialStatus": "active", "password": password,
	}, true))
	accountID := int64(account["id"].(float64))
	seedTenantRole(t, pool, "tenant-a", employee, root.ID, accountID, []*genidentity.PermissionGrant{
		{Permission: "example:create", Effect: "allow", Scope: "ALL"},
		{Permission: "example:read", Effect: "allow", Scope: "OWN"},
	})
	bound := loginTenantIAM(t, handler, employee, "tenant-a")
	createRecord := func(title string) int64 {
		t.Helper()
		return int64(checkIAMInvocation(t, bound.invoke(t, "example.record.create", map[string]any{"title": title}, true))["id"].(float64))
	}
	first, second := createRecord("Related resource"), createRecord("Unrelated resource")
	binding := checkIAMInvocation(t, bound.invoke(t, "identity.binding.claim", map[string]any{
		"identityType": bindingType, "version": "1.0.0", "entityId": first,
	}, true))["id"].(string)
	contextInput := map[string]any{"tenantId": "tenant-a", "identityBindingId": binding}
	checkStatus(t, bound.request(t, "POST", "/api/auth/identity", contextInput, true), 403)
	role := checkIAMInvocation(t, admin.invoke(t, "identity.role.create", map[string]any{
		"name": "Bound employee", "code": "bound-employee", "description": "", "status": "active",
		"grants": []map[string]any{
			{"permission": "assistant:use", "effect": "allow", "scope": "ALL"},
			{"permission": "example:archive", "effect": "allow", "scope": "ALL"},
		},
	}, true))
	checkIAMInvocation(t, admin.invoke(t, "identity.roleGrant.create", map[string]any{
		"accountId": accountID, "roleId": role["id"], "bindingId": binding,
		"minutes": 120, "reason": "Bound agent delegation test",
	}, true))
	selected := bound.request(t, "POST", "/api/auth/identity", contextInput, true)
	checkStatus(t, selected, 200)
	bound.csrf = decodeBody[genidentity.SessionProfile](t, selected).CsrfToken
	agent := checkIAMInvocation(t, admin.invoke(t, "identity.machine.create", map[string]any{
		"kind": "agent", "name": "Bound agent", "password": password,
		"grants": []map[string]any{{"permission": "example:archive", "scope": "ALL", "effect": "allow"}},
	}, true))
	entries := example.New(pool).Entries()
	for i := range entries {
		if entries[i].Definition.Name == "example.record.archive" {
			entries[i].Definition.DelegationBindingType = bindingType
			entries[i].Definition.DelegationBindingAttribute = "boundEntity"
		}
	}
	const readName = "test.boundAgent.read"
	read := registry.Entry{Definition: contracts.Definition{
		Name: readName, Version: "1.0.0", Domain: "example", Entity: "example_record", Title: "Read a delegated resource",
		Layer: contracts.LayerAtomic, Permission: "example:archive", Executor: "go:" + readName, ReadOnly: true,
		Risk: contracts.RiskLow, Approval: contracts.ApprovalAuto, MachineCallable: true, DelegationResourceID: "id",
		DelegationBindingType: bindingType, DelegationBindingAttribute: "boundEntity",
		Input: contracts.Object(map[string]*contracts.Schema{"id": {Type: "integer"}}, "id"),
	}, Handler: contracts.Handler{ExecuteTx: func(_ context.Context, _ pgx.Tx, args contracts.Input) (contracts.Output, error) {
		return contracts.Output{Data: args}, nil
	}}}
	entries = append(entries, read)
	catalog := registry.New(append(entries, service.Entries()...))
	if err := catalog.Err(); err != nil {
		t.Fatal(err)
	}
	service.BindAuthorizationCatalog(catalog)
	if err := service.RegisterResourceAttributeProvider("example_record", func(ctx context.Context, q *dbgen.Queries, tenant string, id int64) (identity.ResourceAttributes, error) {
		record, err := q.ExampleRecordGet(ctx, dbgen.ExampleRecordGetParams{TenantID: tenant, ID: id, CreatedBy: accountID})
		return identity.ResourceAttributes{
			ObservedAt: time.Now(), Values: map[string]string{"boundEntity": fmt.Sprint(record.ID)},
		}, err
	}); err != nil {
		t.Fatal(err)
	}
	g := gateway.New(gateway.Options{Registry: catalog, Identity: service, Pool: pool, Approvals: approval.NewStore(pool), Audit: audit.NewStore(pool)})
	ctx := t.Context()
	owner, err := service.Authenticate(ctx, bound.cookie.Value)
	if err != nil {
		t.Fatal(err)
	}
	issue := func(resource int64) (*contracts.Result, error) {
		t.Helper()
		return g.Invoke(identity.WithPrincipal(ctx, owner), gateway.Request{
			Name: "identity.agentDelegation.create", Version: "1.0.0", Origin: contracts.OriginHumanUI, IdempotencyKey: fmt.Sprint(resource),
			Input: contracts.Input{"agentAccountId": agent["id"], "resourceId": resource, "capability": "example.record.archive@1.0.0",
				"additionalCapabilities": []any{read.Definition.Key()}, "maxRisk": "high", "uses": 2, "minutes": 30, "password": password},
		})
	}
	if _, err = issue(second); contracts.Code(err) != "forbidden" {
		t.Fatalf("unrelated resource delegated: %v", err)
	}
	grant, err := issue(first)
	if err != nil || grant.Status != contracts.StatusSucceeded {
		t.Fatalf("related resource denied: %+v %v", grant, err)
	}
	grantID := grant.Data.(map[string]any)["id"].(string)
	credential := checkIAMInvocation(t, admin.invoke(t, "identity.machine.credential.rotate", map[string]any{
		"accountId": agent["id"], "audience": "horizon-agent", "capabilities": []string{"example.record.archive@1.0.0", read.Definition.Key()},
		"minutes": 30, "password": password,
	}, true))["secret"].(string)
	machine, err := service.AuthenticateMachine(ctx, credential, grantID)
	if err != nil || machine.IdentityBindingID != "" {
		t.Fatalf("machine authentication reused owner identity: %+v %v", machine, err)
	}
	readResult, err := g.Invoke(identity.WithPrincipal(ctx, machine), gateway.Request{
		Name: readName, Version: "1.0.0", Origin: contracts.OriginAgent, IdempotencyKey: "bound-agent-read", Input: contracts.Input{"id": first},
	})
	if err != nil || readResult.Status != contracts.StatusSucceeded {
		t.Fatalf("delegated read failed: %+v %v", readResult, err)
	}
	var remaining int
	if err := pool.QueryRow(ctx, `SELECT remaining_uses FROM iam_agent_delegations WHERE id=$1::uuid`, grantID).Scan(&remaining); err != nil || remaining != 1 {
		t.Fatalf("delegated actions did not share a usage budget: %d %v", remaining, err)
	}
	pending, err := g.Invoke(identity.WithPrincipal(ctx, machine), gateway.Request{
		Name: "example.record.archive", Version: "1.0.0", Origin: contracts.OriginAgent, IdempotencyKey: "bound-agent-archive", Input: contracts.Input{"id": first},
	})
	if err != nil || pending.Status != contracts.StatusAwaitingApproval {
		t.Fatalf("bound machine did not reach approval: %+v %v", pending, err)
	}
	adminPrincipal, err := service.Authenticate(ctx, admin.cookie.Value)
	if err != nil {
		t.Fatal(err)
	}
	checkIAMInvocation(t, admin.invoke(t, "identity.binding.suspend", map[string]any{"id": binding, "reason": "Revoke bound task access"}, true))
	if _, err = g.Decide(identity.WithPrincipal(ctx, adminPrincipal), pending.ApprovalID, true, ""); contracts.Code(err) != "forbidden" {
		t.Fatalf("approval retained stale business binding: %v", err)
	}
}

func TestIAMAgentDelegationApproval(t *testing.T) {
	handler, pool, root := iamFixture(t)
	owner := loginTenantIAM(t, handler, root.Username, "tenant-a")
	password := "TestOnly!2026Secure"
	agent := checkIAMInvocation(t, owner.invoke(t, "identity.machine.create", map[string]any{
		"kind": "agent", "name": "Integration agent", "password": password,
		"grants": []map[string]any{{"permission": "example:archive", "scope": "ALL", "effect": "allow"}},
	}, true))
	credential := checkIAMInvocation(t, owner.invoke(t, "identity.machine.credential.rotate", map[string]any{
		"accountId": agent["id"], "audience": "horizon-agent", "capabilities": []string{"example.record.archive@1.0.0"},
		"minutes": 30, "password": password,
	}, true))
	createRecord := func(title string) any {
		t.Helper()
		return checkIAMInvocation(t, owner.invoke(t, "example.record.create", map[string]any{"title": title}, true))["id"]
	}
	first, second := createRecord("Approved resource"), createRecord("Different resource")
	delegate := func(resource any, risk string) string {
		t.Helper()
		return checkIAMInvocation(t, owner.invoke(t, "identity.agentDelegation.create", map[string]any{
			"agentAccountId": agent["id"], "resourceId": resource, "capability": "example.record.archive@1.0.0",
			"maxRisk": risk, "uses": 1, "minutes": 30, "password": password,
		}, true))["id"].(string)
	}
	invoke := func(id string, resource any, key string, want int) contracts.Result {
		t.Helper()
		body, err := json.Marshal(map[string]any{
			"version": "1.0.0", "idempotencyKey": key, "input": map[string]any{"id": resource},
		})
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, "/api/capabilities/example.record.archive/machine-invocations", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+credential["secret"].(string))
		request.Header.Set("X-Horizon-Delegation", id)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		checkStatus(t, response, want)
		return decodeBody[contracts.Result](t, response)
	}
	approve := func(result contracts.Result, want int) contracts.Result {
		t.Helper()
		if result.Status != contracts.StatusAwaitingApproval || result.ApprovalID <= 0 {
			t.Fatalf("agent did not request human approval: %+v", result)
		}
		response := owner.request(t, http.MethodPost, fmt.Sprintf("/api/capabilities/approvals/%d/decisions", result.ApprovalID), map[string]any{"approve": true}, true)
		checkStatus(t, response, want)
		return decodeBody[contracts.Result](t, response)
	}
	invoke(delegate(first, "medium"), first, "risk-too-high", 403)
	delegation := delegate(first, "high")
	invoke(delegation, second, "wrong-resource", 403)
	pending := invoke(delegation, first, "first-action", 200)
	if result := approve(pending, 200); result.Status != contracts.StatusSucceeded || result.Data != nil {
		t.Fatalf("approval failed or disclosed machine output to reviewer: %+v", result)
	}
	if replay := invoke(delegation, first, "first-action", 200); replay.Status != contracts.StatusSucceeded || !replay.Replayed {
		t.Fatalf("the same approved call did not replay: %+v", replay)
	}
	var remaining int32
	var audited bool
	if err := pool.QueryRow(t.Context(), `SELECT remaining_uses, EXISTS (
		SELECT 1 FROM iam_security_events e WHERE e.tenant_id = d.tenant_id AND e.account_id = d.agent_account_id
		AND e.resource_type = 'agent_delegation' AND e.resource_id = d.id::text
		AND e.reason = 'delegator_account:' || d.delegator_account_id::text AND e.correlation_id = $2)
		FROM iam_agent_delegations d WHERE d.id = $1`, delegation, pending.TraceID).Scan(&remaining, &audited); err != nil || remaining != 0 || !audited {
		t.Fatalf("usage count or execution audit is incorrect: remaining=%d audited=%t err=%v", remaining, audited, err)
	}
	approve(invoke(delegation, first, "exhausted", 200), 403)
	delegation = delegate(second, "high")
	pending = invoke(delegation, second, "before-revocation", 200)
	checkIAMInvocation(t, owner.invoke(t, "identity.agentDelegation.revoke", map[string]any{"id": delegation, "password": password}, true))
	approve(pending, 403)
	invoke(delegation, second, "before-revocation", 403)
}
