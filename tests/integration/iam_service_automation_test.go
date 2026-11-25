//go:build integration

package tests

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"horizon/foundation/ai/automation"
	genidentity "horizon/foundation/gen/identity"
	"horizon/foundation/kernel/audit"
	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/capability/gateway"
	"horizon/foundation/kernel/capability/registry"
	"horizon/foundation/kernel/eventhub"
	"horizon/foundation/system/governance"
	"horizon/foundation/system/identity"
)

func TestIAMServiceAutomationUsesOriginalCredential(t *testing.T) {

	var iam *identity.Service
	handler, pool, root := iamFixture(t, func(service *identity.Service, _ *pgxpool.Pool, _ *genidentity.AccountRecord) {
		iam = service
	})
	const (
		password = "TestOnly!2026Secure"
		target   = "test.serviceAutomation.read"
		version  = "1.0.0"
		sample   = "private scheduled arguments"
	)
	admin := loginTenantIAM(t, handler, root.Username, "tenant-a")
	machineInput := map[string]any{
		"kind": "service", "name": "Scheduled service", "password": password,
		"grants": []map[string]any{{"permission": "automation:read", "scope": "OWN", "effect": "allow"}},
	}
	service := checkIAMInvocation(t, admin.invoke(t, "identity.machine.create", machineInput, true))
	issue := func() string {
		t.Helper()
		return checkIAMInvocation(t, admin.invoke(t, "identity.machine.credential.rotate", map[string]any{
			"accountId": service["id"], "audience": identity.CapabilityAudience,
			"minutes": 30, "password": password, "capabilities": []string{target + "@" + version},
		}, true))["id"].(string)
	}
	original := issue()
	core := governance.New(pool, iam, governance.Options{EncryptionKey: []byte("01234567890123456789012345678901")})
	module := &automation.Module{Pool: pool, Core: core}
	entry := registry.Entry{Definition: contracts.Definition{
		Name: target, Version: version, Title: "Scheduled test read", Domain: "automation", Entity: "automation_task",
		Executor: "go:" + target, Layer: contracts.LayerAtomic, ReadOnly: true, MachineCallable: true,
		Risk: contracts.RiskLow, Approval: contracts.ApprovalAuto, Permission: "automation:read",
		AllowedOrigins: []contracts.Origin{contracts.OriginAutomation},
		Input:          contracts.Object(map[string]*contracts.Schema{"message": contracts.Str(1, 100)}, "message"),
	}, Handler: contracts.Handler{ExecuteTx: func(ctx context.Context, _ pgx.Tx, in contracts.Input) (contracts.Output, error) {
		p, err := identity.Current(ctx)
		if err != nil || p.SubjectID != service["principalId"] || p.MachineCredentialID() != original || in["message"] != sample {
			return contracts.Output{}, contracts.Problem("invalid")
		}
		return contracts.Output{}, nil
	}}}
	catalog := registry.New(append(module.Entries(), entry))
	if err := catalog.Err(); err != nil {
		t.Fatal(err)
	}
	g := gateway.New(gateway.Options{Pool: pool, Identity: iam, Registry: catalog, Audit: audit.NewStore(pool), Events: eventhub.NewStore(pool)})
	module.Gateway = g
	ctx := t.Context()
	owner, err := iam.Authenticate(ctx, admin.cookie.Value)
	if err != nil {
		t.Fatal(err)
	}
	ownerCtx := identity.WithPrincipal(ctx, owner)
	request := gateway.Request{
		Name: "automation.task.createService", Version: version, Origin: contracts.OriginHumanUI, IdempotencyKey: "new-service-schedule",
		Input: contracts.Input{
			"accountId": service["id"], "credentialId": original,
			"capability": target, "capabilityVersion": version, "arguments": fmt.Sprintf(`{"message":%q}`, sample), "password": password,
			"name": "Service schedule", "description": "Read the agreed scope", "scheduleKind": "daily", "time": "09:00",
		},
	}
	result, err := g.Invoke(ownerCtx, request)
	if err != nil || result.Status != contracts.StatusSucceeded {
		t.Fatalf("create service schedule: %+v %v", result, err)
	}
	taskID := result.Data.(map[string]any)["id"].(int64)
	var sealed, logged string
	if err = pool.QueryRow(ctx, `SELECT t.service_execution, i.input::text FROM automation_tasks t
		JOIN cap_invocations i ON i.tenant_id=t.tenant_id WHERE t.id=$1 AND i.invocation_id=$2`, taskID, result.InvocationID).Scan(&sealed, &logged); err != nil ||
		!strings.HasPrefix(sealed, "enc:v1:") || strings.Contains(logged, sample) || strings.Contains(logged, password) {
		t.Fatalf("schedule input was not protected: %v", err)
	}
	plain, err := core.Unseal(owner.TenantID, request.Name, fmt.Sprint(taskID), sealed)
	if err != nil || strings.Contains(plain, "departmentId") {
		t.Fatalf("the new schedule stored a department: %v", err)
	}
	if _, err = g.Invoke(ownerCtx, gateway.Request{Name: target, Version: version, Origin: contracts.OriginAutomation,
		Input: contracts.Input{"message": sample}}); contracts.Code(err) != "forbidden" {
		t.Fatalf("a human could execute a service occurrence: %v", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE automation_tasks SET next_run_at=now() WHERE id=$1`, taskID); err != nil {
		t.Fatal(err)
	}
	runWorker := func(wantStatus, wantCode string, after int64) int64 {
		t.Helper()
		workerCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		worker := automation.NewWorker(pool, nil, iam)
		worker.Core, worker.Gateway = core, g
		done := make(chan struct{})
		go func() {
			defer close(done)
			worker.Run(workerCtx)
		}()
		defer func() { cancel(); <-done }()
		poll := time.NewTicker(20 * time.Millisecond)
		defer poll.Stop()
		for {
			var id int64
			var status, code string
			err := pool.QueryRow(workerCtx, `SELECT id,status,error_code FROM automation_runs WHERE task_id=$1 AND id>$2
				AND status IN ('completed','skipped','failed') ORDER BY id DESC LIMIT 1`, taskID, after).Scan(&id, &status, &code)
			if err == nil {
				if status != wantStatus || code != wantCode {
					t.Fatalf("occurrence %d = %s/%s, want %s/%s", id, status, code, wantStatus, wantCode)
				}
				return id
			}
			select {
			case <-workerCtx.Done():
				t.Fatalf("occurrence did not reach its expected state: %v", err)
			case <-poll.C:
			}
		}
	}
	runID := runWorker("completed", "", 0)
	var actor, credential, origin string
	if err = pool.QueryRow(ctx, `SELECT a.authority->>'principalId',a.resource_id,i.origin FROM iam_security_events a
		JOIN cap_invocations i ON i.tenant_id=a.tenant_id AND i.correlation_id=a.correlation_id
		AND i.account_id=a.account_id AND i.capability=a.action
		WHERE a.correlation_id=$1 AND a.action=$2 AND a.outcome='allowed' AND a.resource_type='machine_credential'`,
		fmt.Sprintf("automation:%d", runID), target).Scan(&actor, &credential, &origin); err != nil ||
		actor != service["principalId"] || credential != original || origin != string(contracts.OriginAutomation) {
		t.Fatalf("audit did not attribute the occurrence to the original service: %s/%s/%s %v", actor, credential, origin, err)
	}
	queued, err := g.Invoke(ownerCtx, gateway.Request{Name: "automation.run.now", Version: version, Origin: contracts.OriginHumanUI,
		IdempotencyKey: "before-rotation", Input: contracts.Input{"id": taskID}})
	if err != nil || queued.Status != contracts.StatusSucceeded {
		t.Fatalf("queue before rotation: %+v %v", queued, err)
	}
	issue()
	runWorker("skipped", "forbidden", runID)
	var enabled bool
	if err = pool.QueryRow(ctx, `SELECT enabled FROM automation_tasks WHERE id=$1`, taskID).Scan(&enabled); err != nil || enabled {
		t.Fatalf("stale credential left its schedule active: %v %v", enabled, err)
	}
}
