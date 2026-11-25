//go:build integration

package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"horizon/applications/example"
	"horizon/foundation/kernel/approval"
	"horizon/foundation/kernel/audit"
	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/capability/gateway"
	"horizon/foundation/kernel/capability/registry"
	runtime "horizon/foundation/kernel/workflow"
	"horizon/foundation/system/executionplans"
	"horizon/foundation/system/governance"
	"horizon/foundation/system/identity"
	orchmodule "horizon/foundation/system/orchestration"
	"horizon/internal/repository/dbgen"
)

func TestMachineWorkflowPlanRevalidatesEachNode(t *testing.T) {
	handler, pool, root := iamFixture(t)
	ctx := t.Context()
	iam := pluginIAM(t, pool)
	core := governance.New(pool, iam, governance.Options{EncryptionKey: []byte("01234567890123456789012345678901")})
	admin := loginTenantIAM(t, handler, root.Username, "tenant-a")
	const password = "TestOnly!2026Secure"
	machine := checkIAMInvocation(t, admin.invoke(t, "identity.machine.create", map[string]any{
		"kind": "service", "name": "Workflow service", "password": password,
		"grants": []map[string]any{
			{"permission": "example:create", "scope": "OWN", "effect": "allow"},
			{"permission": "assistant:use", "scope": "ALL", "effect": "allow"},
		},
	}, true))
	entries := example.New(pool).Entries()
	exampleRegistry := registry.New(entries)
	target, ok := exampleRegistry.Resolve("example.record.create", "1.0.0")
	if !ok {
		t.Fatal("missing target")
	}
	probe := target
	probe.Definition.Name = "test.machineWorkflow.probe"
	probe.Handler.ExecuteTx = func(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
		if input["title"] == "revoked during workflow" {
			principal, err := identity.Current(ctx)
			if err != nil {
				return contracts.Output{}, err
			}
			if _, err = tx.Exec(ctx, `UPDATE iam_machine_credentials SET revoked_at=now() WHERE id=$1::uuid`, principal.MachineCredentialID()); err != nil {
				return contracts.Output{}, err
			}
		}
		return target.Handler.ExecuteTx(ctx, tx, input)
	}
	entries = append(entries, probe)
	read := registry.Entry{
		Definition: contracts.Definition{
			Name: "test.machineWorkflow.read", Version: "1.0.0", Title: "Read workflow test data",
			Domain: "example", Entity: "example_record", Permission: "example:create", Layer: contracts.LayerAtomic,
			Input: contracts.Object(nil), Output: contracts.Object(map[string]*contracts.Schema{"value": {Type: "boolean"}}, "value"),
			Risk: contracts.RiskLow, Approval: contracts.ApprovalAuto, ReadOnly: true, MachineCallable: true, Executor: "go:test.machineWorkflow.read",
		},
		Handler: contracts.Handler{ExecuteTx: func(ctx context.Context, _ pgx.Tx, _ contracts.Input) (contracts.Output, error) {
			principal, err := identity.Current(ctx)
			if err != nil || principal.MachineCredentialID() == "" {
				return contracts.Output{}, contracts.Problem("forbidden")
			}
			return contracts.Output{Data: map[string]any{"value": true}}, nil
		}},
	}
	entries = append(entries, read)
	engine := runtime.New(core, []runtime.Definition{{
		Name: "machine-plan", Version: "1.0.0", Input: target.Definition.Input,
		Steps: []runtime.Step{
			{ID: "probe", Capability: probe.Definition.Name, Version: "1.0.0", Input: map[string]runtime.Mapping{"title": {From: "input", Field: "title"}}},
			{ID: "review", DependsOn: []string{"probe"}, Agent: &runtime.Agent{
				Tools: []string{read.Definition.Key()}, Instruction: "Check the workflow status.", MaxIterations: 3,
				Output: contracts.Object(map[string]*contracts.Schema{"ready": {Type: "boolean"}}, "ready"),
			}},
			{ID: "native", Capability: target.Definition.Name, Version: "1.0.0", CallMode: "invoke_native", DependsOn: []string{"review"}, Input: map[string]runtime.Mapping{"title": {From: "input", Field: "title"}}},
		},
	}})
	engine.AgentModel = func(context.Context) (model.ToolCallingChatModel, error) {
		return &workflowModel{preferredTool: read.Definition.ToolName(), answer: `{"ready":true}`}, nil
	}
	plans := &executionplans.Module{Pool: pool, Definitions: map[string]contracts.Definition{}, ValidateWorkflow: engine.ValidatePlanWithQueries}
	entries = append(entries, plans.Entries()...)
	for _, entry := range entries {
		plans.Definitions[entry.Definition.Key()] = entry.Definition
	}
	catalog := registry.New(entries)
	if err := catalog.Err(); err != nil {
		t.Fatal(err)
	}
	g := gateway.New(gateway.Options{Pool: pool, Registry: catalog, Identity: iam, Approvals: approval.NewStore(pool), Audit: audit.NewStore(pool)})
	if err := engine.Attach(g, catalog); err != nil {
		t.Fatal(err)
	}
	owner, err := iam.PrincipalForTask(ctx, "tenant-a", root.ID)
	if err != nil {
		t.Fatal(err)
	}
	plan := func(action string, fields contracts.Input) {
		t.Helper()
		fields["capability"], fields["capabilityVersion"] = target.Definition.Name, target.Definition.Version
		result, err := g.Invoke(identity.WithPrincipal(ctx, owner), gateway.Request{
			Name: "execution.plan." + action, Origin: contracts.OriginHumanUI, IdempotencyKey: action, Input: fields,
		})
		if err != nil || result.Status != contracts.StatusSucceeded {
			t.Fatalf("%s: %+v %v", action, result, err)
		}
	}
	plan("create", contracts.Input{"planId": "machine-workflow", "name": "Machine workflow", "mode": "workflow", "workflowRef": "machine-plan@1.0.0"})
	plan("validate", contracts.Input{"planId": "machine-workflow", "planVersion": 1})
	plan("publish", contracts.Input{"planId": "machine-workflow", "planVersion": 1})
	plan("bind", contracts.Input{"planId": "machine-workflow", "planVersion": 1, "expectedRevision": 0, "reason": "Workflow service test"})
	issue := func(capabilities ...string) *identity.Principal {
		t.Helper()
		credential := checkIAMInvocation(t, admin.invoke(t, "identity.machine.credential.rotate", map[string]any{
			"accountId": machine["id"], "audience": "horizon-capability", "capabilities": capabilities, "minutes": 30, "password": password,
		}, true))
		principal, err := iam.AuthenticateMachine(ctx, credential["secret"].(string), "")
		if err != nil {
			t.Fatal(err)
		}
		return principal
	}
	invoke := func(principal *identity.Principal, title string) (*contracts.Result, error) {
		t.Helper()
		return g.Invoke(identity.WithPrincipal(ctx, principal), gateway.Request{
			Name: target.Definition.Name, Version: "1.0.0", Origin: contracts.OriginAPI, IdempotencyKey: title, Input: contracts.Input{"title": title},
		})
	}
	onlyRoot := issue(target.Definition.Key())
	if _, err := invoke(onlyRoot, "missing node permission"); contracts.Code(err) != "forbidden" {
		t.Fatalf("accepted workflow with unlisted node: %v", err)
	}
	for _, scenario := range []struct{ title, status string }{
		{"complete workflow", "succeeded"},
		{"revoked during workflow", "failed"},
	} {
		principal := issue(target.Definition.Key(), probe.Definition.Key(), read.Definition.Key())
		started, err := invoke(principal, scenario.title)
		if err != nil || started.Status != contracts.StatusExecuting {
			t.Fatalf("start %+v %v", started, err)
		}
		if err := engine.Tick(ctx); err != nil {
			t.Fatal(err)
		}
		var status string
		if err := pool.QueryRow(ctx, `SELECT status FROM cap_invocations WHERE tenant_id='tenant-a' AND invocation_id=$1`, started.InvocationID).Scan(&status); err != nil || status != scenario.status {
			t.Fatalf("workflow status=%s want %s: %v", status, scenario.status, err)
		}
		var nativeCalls int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM cap_invocations WHERE tenant_id='tenant-a' AND causation_id=$1 AND capability=$2 AND account_id=$3 AND machine_credential_id=$4::uuid`,
			started.InvocationID, target.Definition.Name, principal.Account.ID, principal.MachineCredentialID()).Scan(&nativeCalls); err != nil || (nativeCalls == 1) != (status == "succeeded") {
			t.Fatalf("native effect used stale or wrong machine authority: %d %v", nativeCalls, err)
		}
		var agentReads int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM cap_invocations WHERE tenant_id='tenant-a' AND causation_id=$1
 AND capability=$2 AND origin='agent' AND machine_credential_id=$3::uuid AND status='succeeded'`,
			started.InvocationID, read.Definition.Name, principal.MachineCredentialID()).Scan(&agentReads); err != nil || (agentReads == 1) != (status == "succeeded") {
			t.Fatalf("ADK read did not enforce current machine authority: %d %v", agentReads, err)
		}
	}
}

func TestWorkflowExecutionPlanApprovalRollbackAndRestart(t *testing.T) {
	_, pool, root := iamFixture(t)
	ctx := context.Background()
	iam := pluginIAM(t, pool)
	core := governance.New(pool, iam, governance.Options{EncryptionKey: []byte("01234567890123456789012345678901")})
	conditional := example.ExampleArchivePlan()
	conditional.Name = "conditional-archive"
	native := conditional.Steps[1]
	native.DependsOn = nil
	native.When = &runtime.When{From: "check", Field: "status", Equals: "draft"}
	native.ID = "draft"
	second := native
	second.ID = "archived"
	second.When = &runtime.When{From: "check", Field: "status", Equals: "archived"}
	conditional.Steps = []runtime.Step{conditional.Steps[0], native, second}
	definitions := []runtime.Definition{example.ExampleArchivePlan(), conditional}
	engine := runtime.New(core, definitions)
	plans := &executionplans.Module{Pool: pool, Definitions: map[string]contracts.Definition{}, ValidateWorkflow: engine.ValidatePlanWithQueries}
	entries := append(example.New(pool).Entries(), plans.Entries()...)
	entries = append(entries, (&orchmodule.Module{Engine: engine}).Entries()...)
	for _, entry := range entries {
		plans.Definitions[entry.Definition.Key()] = entry.Definition
	}
	catalog := registry.New(entries)
	if err := catalog.Err(); err != nil {
		t.Fatal(err)
	}
	g := gateway.New(gateway.Options{Pool: pool, Registry: catalog, Identity: iam, Approvals: approval.NewStore(pool), Audit: audit.NewStore(pool)})
	if err := engine.Attach(g, catalog); err != nil {
		t.Fatal(err)
	}
	p, err := iam.PrincipalForTask(ctx, "tenant-a", root.ID)
	if err != nil {
		t.Fatal(err)
	}
	ctx = identity.WithPrincipal(ctx, p)
	invoke := func(name, key string, input contracts.Input) *contracts.Result {
		t.Helper()
		r, e := g.Invoke(ctx, gateway.Request{Name: name, IdempotencyKey: key, Input: input, Origin: contracts.OriginHumanUI})
		if e != nil {
			t.Fatal(name, e)
		}
		return r
	}
	created := invoke("example.record.create", "record", contracts.Input{"title": "Native workflow result"})
	if created.Status != contracts.StatusSucceeded {
		t.Fatal(created)
	}
	recordID := created.Data.(map[string]any)["id"]
	target := func(extra contracts.Input) contracts.Input {
		out := contracts.Input{"capability": "example.record.archive", "capabilityVersion": "1.0.0"}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	must := func(action string, in contracts.Input) {
		t.Helper()
		r := invoke("execution.plan."+action, "plan-"+action, in)
		if r.Status != contracts.StatusSucceeded {
			t.Fatal(action, r)
		}
	}
	must("create", target(contracts.Input{"planId": "archive-flow", "name": "Archive with check", "mode": "workflow", "workflowRef": "conditional-archive@1.0.0"}))
	for _, action := range []string{"validate", "publish"} {
		must(action, target(contracts.Input{"planId": "archive-flow", "planVersion": 1}))
	}
	must("bind", target(contracts.Input{"planId": "archive-flow", "planVersion": 1, "expectedRevision": 0, "reason": "test workflow"}))
	started := invoke("example.record.archive", "archive-flow-run", contracts.Input{"id": recordID})
	if started.Status != contracts.StatusExecuting || started.Data != nil {
		t.Fatal("async receipt changed business output", started)
	}
	replayedStart := invoke("example.record.archive", "archive-flow-run", contracts.Input{"id": recordID})
	if !replayedStart.Replayed || replayedStart.InvocationID != started.InvocationID || replayedStart.Status != contracts.StatusExecuting {
		t.Fatal("accepted run was duplicated", replayedStart)
	}
	q := dbgen.New(pool)
	if err = engine.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	cards, err := q.CapListApprovals(ctx, dbgen.CapListApprovalsParams{TenantID: "tenant-a", AccountID: root.ID})
	if err != nil || len(cards) != 1 {
		t.Fatal(cards, err)
	}
	// Native calls are still automated high-risk actions requiring approval.
	var status string
	if err = pool.QueryRow(ctx, `SELECT status FROM example_records WHERE tenant_id='tenant-a' AND id=$1`, recordID).Scan(&status); err != nil || status != "draft" {
		t.Fatal(status, err)
	}
	platform := contracts.DefaultExecutionBinding(plans.Definitions["example.record.archive@1.0.0"])
	must("rollback", target(contracts.Input{"planId": platform.Plan.PlanID, "planVersion": 1, "expectedRevision": 1, "reason": "rollback during approval"}))
	done, err := g.Decide(ctx, cards[0].ID, true, "")
	if err != nil || done.Status != contracts.StatusSucceeded {
		t.Fatal(done, err)
	}
	// Reconstruct the real Eino engine and resume the same durable run.
	engine = runtime.New(core, definitions)
	if err = engine.Attach(g, catalog); err != nil {
		t.Fatal(err)
	}
	var run pgtype.Int8
	if err = pool.QueryRow(ctx, `SELECT run_id FROM cap_execution_records WHERE tenant_id='tenant-a' AND invocation_id=$1`, started.InvocationID).Scan(&run); err != nil || !run.Valid {
		t.Fatal(run, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE orch_checkpoints SET lease_until=now()-interval '1 second' WHERE tenant_id='tenant-a' AND run_id=$1`, run); err != nil {
		t.Fatal(err)
	}
	if err = engine.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	row, err := q.CapGetInvocation(ctx, dbgen.CapGetInvocationParams{TenantID: "tenant-a", AccountID: root.ID, InvocationID: started.InvocationID})
	if err != nil || row.Status != "succeeded" {
		t.Fatal(row, err)
	}
	var result struct {
		Data map[string]any `json:"data"`
	}
	if err = json.Unmarshal(row.Result, &result); err != nil || result.Data["status"] != "archived" {
		t.Fatal(result, err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM cap_outbox WHERE event_type='example.record.archived'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("business event duplicated", count, err)
	}
	replay := invoke("example.record.archive", "archive-flow-run", contracts.Input{"id": recordID})
	if !replay.Replayed || replay.Status != contracts.StatusSucceeded {
		t.Fatal(replay)
	}
	// The completed outer call preserves the native event identifiers.
	if len(replay.EventIDs) != 1 {
		t.Fatal("native event identities lost", replay)
	}
	plans.ValidateWorkflow = engine.ValidatePlanWithQueries
	// A pinned Workflow is selected even though the active binding is now Direct.
	fresh := invoke("example.record.create", "cancel-record", contracts.Input{"title": "Cancel workflow"})
	cancelID := fresh.Data.(map[string]any)["id"]
	pending, err := g.InvokePinned(ctx, gateway.Request{Name: "example.record.archive", Version: "1.0.0", Input: contracts.Input{"id": cancelID}, IdempotencyKey: "pinned-workflow", Origin: contracts.OriginHumanUI}, "archive-flow", 1)
	if err != nil || pending.Status != contracts.StatusExecuting {
		t.Fatal(pending, err)
	}
	if err = engine.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	var cancelRun pgtype.Int8
	if err = pool.QueryRow(ctx, `SELECT run_id FROM cap_execution_records WHERE tenant_id='tenant-a' AND invocation_id=$1`, pending.InvocationID).Scan(&cancelRun); err != nil {
		t.Fatal(err)
	}
	cancelled := invoke("orchestration.workflow.cancel", "cancel-plan", contracts.Input{"runId": cancelRun.Int64})
	if cancelled.Status != contracts.StatusSucceeded {
		t.Fatal(cancelled)
	}
	cancelledRow, err := q.CapGetInvocation(ctx, dbgen.CapGetInvocationParams{TenantID: "tenant-a", AccountID: root.ID, InvocationID: pending.InvocationID})
	if err != nil || cancelledRow.Status != "cancelled" {
		t.Fatal(cancelledRow, err)
	}
	if err = pool.QueryRow(ctx, `SELECT status FROM example_records WHERE tenant_id='tenant-a' AND id=$1`, cancelID).Scan(&status); err != nil || status != "draft" {
		t.Fatal("cancel committed native action", status, err)
	}
	// A failed prerequisite closes the original invocation without native effects.
	failed, err := g.InvokePinned(ctx, gateway.Request{Name: "example.record.archive", Version: "1.0.0", Input: contracts.Input{"id": 999999}, IdempotencyKey: "failed-plan", Origin: contracts.OriginHumanUI}, "archive-flow", 1)
	if err != nil || failed.Status != contracts.StatusExecuting {
		t.Fatal(failed, err)
	}
	if err = engine.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	failedRow, err := q.CapGetInvocation(ctx, dbgen.CapGetInvocationParams{TenantID: "tenant-a", AccountID: root.ID, InvocationID: failed.InvocationID})
	if err != nil || failedRow.Status != "failed" {
		t.Fatal(failedRow, err)
	}
	// Self active-plan recursion is rejected before publication.
	bad := conditional
	bad.Name = "recursive-archive"
	bad.Steps[1].CallMode = "resolve_active_plan"
	rejectedEngine := runtime.New(core, []runtime.Definition{bad})
	if err = rejectedEngine.Attach(g, catalog); err != nil {
		t.Fatal(err)
	}
	if _, err = rejectedEngine.ValidatePlanReference(ctx, plans.Definitions["example.record.archive@1.0.0"], "recursive-archive@1.0.0"); err == nil {
		t.Fatal("recursive plan accepted")
	}
	// Restore the engine after the negative adapter registration.
	if err = engine.Attach(g, catalog); err != nil {
		t.Fatal(err)
	}
	// Ordinary callers cannot select native mode or escape the run/owner proof.
	if _, err = g.InvokeNative(ctx, gateway.Request{Name: "example.record.archive", Version: "1.0.0", Input: contracts.Input{"id": recordID}, Origin: contracts.OriginHumanUI}, started.InvocationID); contracts.Code(err) != "forbidden" {
		t.Fatal("native escape accepted", err)
	}
	// A pinned Direct version remains selectable after the active binding changes.
	pinned, err := g.InvokePinned(ctx, gateway.Request{Name: "example.record.get", Version: "1.0.0", Input: contracts.Input{"id": recordID}, Origin: contracts.OriginHumanUI}, "example.record.get@1.0.0.default", 1)
	// Ensure the deployment fallback exists for a reference not invoked yet.
	if err != nil {
		t.Fatal(fmt.Sprint("pinned read: ", err))
	}
	if pinned.Status != contracts.StatusSucceeded {
		t.Fatal(pinned)
	}
}
