//go:build integration

package tests

import (
	"testing"

	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	genidentity "horizon/foundation/gen/identity"
	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/capability/gateway"
	"horizon/foundation/kernel/capability/registry"
	"horizon/foundation/system/identity"
)

func TestIAMRowPolicyBeforeSQL(t *testing.T) {
	handler, pool, account := iamFixture(t)
	author := loginTenantIAM(t, handler, account.Username, "tenant-a")
	reviewer := policyTestAccount(t, author, pool, account.ID, "row-policy-reviewer", "ALL")
	for _, code := range []string{"visible-role", "hidden-role"} {
		checkIAMInvocation(t, author.invoke(t, "identity.role.create", map[string]any{
			"name": code, "code": code, "status": "active", "description": "", "grants": []any{},
		}, true))
	}
	const name = "test.reviewRows"
	draft := checkIAMInvocation(t, author.invoke(t, "identity.policy.draft", map[string]any{
		"baseVersion": 0, "reason": "Use a fixed resource filter for list and count", "rules": []map[string]any{
			{"action": name, "resource": "role", "effect": "filter_row", "attribute": "resource.code", "equals": "visible-role"},
			{"action": name, "resource": "role", "effect": "mask_output", "attribute": "restricted"},
			{"action": "identity.roles.read", "resource": "role", "effect": "filter_row", "attribute": "resource.code", "equals": "visible-role"},
		},
	}, true))
	checkIAMInvocation(t, reviewer.invoke(t, "identity.policy.publish", draft, true))
	checkStatus(t, author.invoke(t, "identity.roles.read", map[string]any{}, true), 403)
	iam := identity.New(pool, false)
	p, err := iam.PrincipalForTask(t.Context(), "tenant-a", account.ID)
	if err != nil {
		t.Fatal(err)
	}
	d := contracts.Definition{
		Name: name, Domain: "identity", Version: "1.0.0", Title: "List reviewed rows",
		Layer: contracts.LayerAtomic, Entity: "role", Permission: "roles:read",
		Input: contracts.Object(nil), Risk: contracts.RiskLow, Approval: contracts.ApprovalAuto, Executor: "go:test.reviewRows",
		ReadOnly: true, TransientResult: true, RowFilterAttributes: []string{"code"},
		Output: contracts.Object(map[string]*contracts.Schema{
			"items": contracts.Array(&contracts.Schema{Type: "string"}, 0, 50), "total": {Type: "integer"}, "restricted": {Type: "string"},
		}, "items", "total"),
		OutputAccess: []contracts.FieldAccess{{Path: "restricted", Permission: "roles:read"}},
	}
	h := contracts.Handler{ExecuteScopedTx: func(ctx context.Context, tx pgx.Tx, _ contracts.Input, filters []contracts.RowPredicate) (contracts.Output, error) {
		if len(filters) != 1 || filters[0].Attribute != "code" {
			return contracts.Output{}, contracts.Problem("invalid")
		}
		actor, err := identity.Current(ctx)
		if err != nil {
			return contracts.Output{}, err
		}
		var rows []string
		var total int
		err = tx.QueryRow(ctx, `SELECT array_agg(code ORDER BY id), count(*) FROM iam_roles WHERE tenant_id = $1 AND code = $2`,
			actor.TenantID, filters[0].Equals).Scan(&rows, &total)
		return contracts.Output{Data: map[string]any{"items": rows, "total": total, "restricted": "private"}}, err
	}}
	catalog := registry.New([]registry.Entry{{Definition: d, Handler: h}})
	if err := catalog.Err(); err != nil {
		t.Fatal(err)
	}
	g := gateway.New(gateway.Options{Identity: iam, Registry: catalog, Pool: pool})
	result, err := g.Invoke(identity.WithPrincipal(t.Context(), p), gateway.Request{Name: name, Input: contracts.Input{}, Origin: contracts.OriginHumanUI})
	if err != nil || result.Status != contracts.StatusSucceeded {
		t.Fatalf("filtered call failed: %+v %v", result, err)
	}
	data := result.Data.(map[string]any)
	rows := data["items"].([]any)
	if data["total"] != float64(1) || len(rows) != 1 || rows[0] != "visible-role" {
		t.Fatalf("row filter did not reach the parameterized query: %+v", data)
	}
	if _, visible := data["restricted"]; visible {
		t.Fatal("restricted field survived policy masking")
	}
}

func TestIAMPolicyReviewReplayAndRollback(t *testing.T) {
	handler, pool, account := iamFixture(t)
	author := loginTenantIAM(t, handler, account.Username, "tenant-a")
	reviewer := policyTestAccount(t, author, pool, account.ID, "policy-reviewer", "ALL")
	prior := checkIAMInvocation(t, author.invoke(t, "identity.policy.draft", map[string]any{
		"baseVersion": 0, "reason": "Start with empty restrictions", "rules": []any{},
	}, true))
	checkIAMInvocation(t, reviewer.invoke(t, "identity.policy.publish", prior, true))
	profile := func() string {
		response := author.request(t, "GET", "/api/auth/me", nil, false)
		checkStatus(t, response, 200)
		return *decodeBody[genidentity.SessionProfile](t, response).AuthorizationVersion
	}
	before := profile()
	call := func() *contracts.Result {
		response := author.request(t, "POST", "/api/capabilities/example.record.create/invocations", map[string]any{
			"input": map[string]any{"title": "Before authorization rule"}, "idempotencyKey": "before-authorization-rule",
		}, true)
		if response.Code == 403 {
			return nil
		}
		checkStatus(t, response, 200)
		result := decodeBody[contracts.Result](t, response)
		return &result
	}
	if result := call(); result == nil || result.Status != contracts.StatusSucceeded || result.Replayed {
		t.Fatalf("initial invocation: %+v", result)
	}
	restriction := checkIAMInvocation(t, author.invoke(t, "identity.policy.draft", map[string]any{
		"baseVersion": prior["id"], "reason": "Suspend new records for review", "rules": []map[string]any{
			{"action": "example.record.create", "resource": "example_record", "effect": "deny"},
		},
	}, true))
	preview := checkIAMInvocation(t, reviewer.invoke(t, "identity.policy.preview", restriction, true))
	if len(preview["additions"].([]any)) != 1 || preview["stale"] != false {
		t.Fatalf("wrong policy difference: %+v", preview)
	}
	if preview["authoredByMe"] != false || preview["published"] != false || len(preview["proposedRules"].([]any)) != 1 || preview["reason"] != "Suspend new records for review" {
		t.Fatalf("incomplete review projection: %+v", preview)
	}
	if own := checkIAMInvocation(t, author.invoke(t, "identity.policy.preview", restriction, true)); own["authoredByMe"] != true {
		t.Fatal("draft ownership projection missing", own)
	}
	contexts := checkIAMInvocation(t, reviewer.invoke(t, "identity.policy.searchContexts", map[string]any{"keyword": account.Username}, true))
	items := contexts["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["accountId"] != float64(account.ID) {
		t.Fatalf("tenant subject was not offered for policy simulation: %+v", contexts)
	}
	simulation := checkIAMInvocation(t, reviewer.invoke(t, "identity.policy.simulate", map[string]any{
		"id": restriction["id"], "accountId": account.ID,
		"action": "example.record.create", "resource": "example_record", "permission": "example:create",
	}, true))
	if simulation["currentAllowed"] != true || simulation["proposedAllowed"] != false || simulation["proposedReason"] != "policy_deny" {
		t.Fatalf("wrong policy simulation: %+v", simulation)
	}
	self := author.invoke(t, "identity.policy.publish", restriction, true)
	if result := decodeBody[contracts.Result](t, self); result.Status != contracts.StatusFailed || result.ErrorCode != "conflict" {
		t.Fatalf("author could approve their own policy: %s", self.Body.String())
	}
	checkIAMInvocation(t, reviewer.invoke(t, "identity.policy.publish", restriction, true))
	if before == profile() || call() != nil {
		t.Fatal("new policy did not invalidate the authorization version and block replay")
	}
	rollback := checkIAMInvocation(t, reviewer.invoke(t, "identity.policy.rollback", map[string]any{
		"id": prior["id"], "baseVersion": restriction["id"], "reason": "Restore validated restrictions",
	}, true))
	checkIAMInvocation(t, author.invoke(t, "identity.policy.publish", rollback, true))
	if result := call(); result == nil || result.Status != contracts.StatusSucceeded || !result.Replayed {
		t.Fatalf("rollback did not restore valid replay: %+v", result)
	}
}

func TestIAMPolicyGovernanceScopeAndStaleDraft(t *testing.T) {
	handler, pool, account := iamFixture(t)
	author := loginTenantIAM(t, handler, account.Username, "tenant-a")
	reviewer := policyTestAccount(t, author, pool, account.ID, "policy-full-reviewer", "ALL")
	// Model a restricted policy role directly to verify the independent tenant-wide guard.
	limited := policyTestAccount(t, author, pool, account.ID, "policy-limited-reviewer", "OWN")
	draft := func(reason string) map[string]any {
		t.Helper()
		return checkIAMInvocation(t, author.invoke(t, "identity.policy.draft", map[string]any{"baseVersion": 0, "reason": reason, "rules": []any{}}, true))
	}
	first, stale := draft("First reviewed candidate"), draft("Concurrent candidate remains draft")
	checkIAMInvocation(t, reviewer.invoke(t, "identity.policy.publish", first, true))
	preview := checkIAMInvocation(t, reviewer.invoke(t, "identity.policy.preview", stale, true))
	if preview["stale"] != true || preview["baseVersion"] != float64(0) || preview["currentVersion"] != first["id"] {
		t.Fatal("concurrent draft was not marked stale", preview)
	}
	if result := decodeBody[contracts.Result](t, reviewer.invoke(t, "identity.policy.publish", stale, true)); result.Status != contracts.StatusFailed || result.ErrorCode != "conflict" {
		t.Fatal("stale candidate changed the published policy", result)
	}
	for _, operation := range []string{"history", "preview", "draft", "publish", "rollback", "simulate"} {
		input := map[string]any{"id": stale["id"]}
		switch operation {
		case "history":
			input = map[string]any{}
		case "draft":
			input = map[string]any{"baseVersion": first["id"], "reason": "Limited reviewer may not draft", "rules": []any{}}
		case "rollback":
			input = map[string]any{"id": first["id"], "baseVersion": first["id"], "reason": "Limited reviewer may not roll back"}
		case "simulate":
			input = map[string]any{"id": stale["id"], "accountId": account.ID, "action": "example.record.create", "resource": "example_record", "permission": "example:create"}
		}
		checkStatus(t, limited.invoke(t, "identity.policy."+operation, input, true), 403)
	}
	guest, err := identity.Bootstrap(t.Context(), pool, "tenant-b", "policy-foreign-admin", "TestOnly!2026Secure")
	if err != nil {
		t.Fatal(err)
	}
	foreign := loginTenantIAM(t, handler, guest.Username, "tenant-b")
	history := checkIAMInvocation(t, foreign.invoke(t, "identity.policy.history", map[string]any{}, true))
	if len(history["items"].([]any)) != 0 || history["version"] != float64(0) {
		t.Fatal("another tenant's policy history leaked", history)
	}
	response := foreign.invoke(t, "identity.policy.preview", first, true)
	result := decodeBody[contracts.Result](t, response)
	if result.Status != contracts.StatusFailed || result.ErrorCode != "not_found" {
		t.Fatal("foreign policy was readable by guessed version", result)
	}
}

func policyTestAccount(t *testing.T, author iamClient, pool *pgxpool.Pool, grantorID int64, username, scope string) iamClient {
	t.Helper()
	account := checkIAMInvocation(t, author.invoke(t, "identity.account.create", map[string]any{
		"username": username, "fullName": username, "initialStatus": "active", "password": "TestOnly!2026Secure",
	}, true))
	seedTenantRole(t, pool, "tenant-a", username, grantorID, int64(account["id"].(float64)), []*genidentity.PermissionGrant{
		{Permission: "access:manage", Effect: "allow", Scope: scope},
	})
	return loginTenantIAM(t, author.handler, username, "tenant-a")
}
