//go:build integration

package tests

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"horizon/foundation/db/migrations"
	gencapability "horizon/foundation/gen/capability"
	"horizon/internal/repository/dbgen"
)

func studioCall(t *testing.T, actor iamClient, name string, input map[string]any) gencapability.CapabilityResult {
	t.Helper()
	reply := actor.invoke(t, name, input, true)
	checkStatus(t, reply, 200)
	return decodeBody[gencapability.CapabilityResult](t, reply)
}

func TestStudioManagementRuntimeAndStartupGate(t *testing.T) {
	handler, pool, account := iamFixture(t)
	owner := loginIAM(t, handler, "root-admin")
	catalog := studioCall(t, owner, "application.runtime.resolve", map[string]any{"applicationId": "studio"})
	if catalog.Status != "succeeded" {
		t.Fatalf("Studio must resolve through the enabled runtime application: %+v", catalog)
	}

	root := t.TempDir()
	for _, name := range []string{"go.mod", "cmd/api/main.go"} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	linked := studioCall(t, owner, "studio.project.link", map[string]any{
		"path": root, "name": "Managed demo", "developmentBrief": "Use project standards.",
	})
	if linked.Status != "succeeded" {
		t.Fatalf("link failed: %+v", linked)
	}
	project := linked.Data.(map[string]any)
	id := int64(project["id"].(float64))
	if existing := studioCall(t, owner, "studio.project.environment", map[string]any{"project": id}); existing.Data.(map[string]any)["ready"] != false {
		t.Fatal("a checkout without a CLI was marked ready")
	}
	if runtime.GOOS != "windows" {
		script := `#!/bin/sh
case "$1" in
  env)
    printf '%s\n' '[{"name":"go","status":"ok","required":true},{"name":"api_address","status":"ok","required":true},{"name":"postgresql","status":"ok","required":true},{"name":"node","status":"ok","required":true},{"name":"web_dependencies","status":"unavailable","required":true},{"name":"web_port","status":"ok","required":true}]'
    exit 1
    ;;
  start|restart) touch was-started; printf '%s\n' '[]' ;;
  stop) touch was-stopped; printf '%s\n' '[]' ;;
  status)
    printf '%s\n' '[{"service":"backend","status":"running","origin":"http://127.0.0.1:8108"},{"service":"frontend","status":"stopped","origin":"http://127.0.0.1:5188"},{"service":"frontend","status":"running","origin":"http://example.org:5173"}]'
    ;;
esac
`
		cliPath := filepath.Join(root, "horizon-cli")
		if err := os.WriteFile(cliPath, []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
		for _, action := range []string{"start", "restart"} {
			result := studioCall(t, owner, "studio.project.control", map[string]any{"project": id, "action": action, "target": "backend"})
			if result.ErrorCode != "initialization_required" {
				t.Fatalf("%s should be blocked before calling the CLI: %+v", action, result)
			}
		}
		if _, err := os.Stat(filepath.Join(root, "was-started")); !os.IsNotExist(err) {
			t.Fatalf("start ran despite the failed check: %v", err)
		}
		stopped := studioCall(t, owner, "studio.project.control", map[string]any{"project": id, "action": "stop", "target": "backend"})
		if stopped.Status != "succeeded" {
			t.Fatalf("stop must remain available: %+v", stopped)
		}
		if _, err := os.Stat(filepath.Join(root, "was-stopped")); err != nil {
			t.Fatal(err)
		}
		readyScript := strings.ReplaceAll(script, `"unavailable"`, `"ok"`)
		readyScript = strings.ReplaceAll(readyScript, "exit 1", "exit 0")
		if err := os.WriteFile(cliPath, []byte(readyScript), 0700); err != nil {
			t.Fatal(err)
		}
		started := studioCall(t, owner, "studio.project.control", map[string]any{"project": id, "action": "start", "target": "all"})
		if started.Status != "succeeded" {
			t.Fatalf("all required checks passed, but start failed: %+v", started)
		}
		if _, err := os.Stat(filepath.Join(root, "was-started")); err != nil {
			t.Fatal(err)
		}
		status := studioCall(t, owner, "studio.project.status", map[string]any{"project": id})
		services := status.Data.(map[string]any)["services"].([]any)
		if services[0].(map[string]any)["origin"] != "http://127.0.0.1:8108" ||
			services[1].(map[string]any)["origin"] != nil || services[2].(map[string]any)["origin"] != nil {
			t.Fatalf("only the reported, running loopback address may be returned: %+v", services)
		}

	}

	ctx := context.Background()
	queries := dbgen.New(pool)
	if _, err := queries.StudioProjectCreate(ctx, dbgen.StudioProjectCreateParams{
		TenantID: "tenant-a", Root: project["root"].(string),
		CreatedBy: account.ID + 1000, Name: "Foreign overwrite",
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("cross-owner upsert must fail without changing the project: %v", err)
	}

	created := studioCall(t, owner, "studio.requirement.create", map[string]any{
		"project": id, "title": "新增需求验收", "description": "通过 Studio 的正常入口创建",
	})
	if created.Status != "succeeded" || len(created.EventIds) != 1 {
		t.Fatalf("requirement creation did not commit exactly one event: %+v", created)
	}
	requirementID := int64(created.Data.(map[string]any)["id"].(float64))
	var eventType, aggregateType, aggregateID string
	if err := pool.QueryRow(ctx, `SELECT event_type, aggregate_type, aggregate_id
		FROM cap_outbox WHERE tenant_id = 'tenant-a' AND event_id = $1`, created.EventIds[0]).
		Scan(&eventType, &aggregateType, &aggregateID); err != nil {
		t.Fatal(err)
	}
	if eventType != "studio.requirement.created" || aggregateType != "studio_requirement" || aggregateID != fmt.Sprint(requirementID) {
		t.Fatalf("unexpected requirement event: %s on %s #%s", eventType, aggregateType, aggregateID)
	}

	updated := studioCall(t, owner, "studio.requirement.update", map[string]any{
		"project": id, "id": requirementID, "title": "更新后的需求",
	})
	if updated.Status != "succeeded" || updated.Data.(map[string]any)["title"] != "更新后的需求" || len(updated.EventIds) != 1 {
		t.Fatalf("requirement edit did not commit exactly one event: %+v", updated)
	}
	var updateEventType, updateAggregateType, updateAggregateID string
	if err := pool.QueryRow(ctx, `SELECT event_type, aggregate_type, aggregate_id
		FROM cap_outbox WHERE tenant_id = 'tenant-a' AND event_id = $1`, updated.EventIds[0]).
		Scan(&updateEventType, &updateAggregateType, &updateAggregateID); err != nil {
		t.Fatal(err)
	}
	if updateEventType != "studio.requirement.updated" || updateAggregateType != "studio_requirement" || updateAggregateID != fmt.Sprint(requirementID) {
		t.Fatalf("unexpected requirement edit event: %s on %s #%s", updateEventType, updateAggregateType, updateAggregateID)
	}
	edited := findRequirement(t, studioCall(t, owner, "studio.requirement.list", map[string]any{"project": id}), requirementID)
	if edited["title"] != "更新后的需求" || edited["description"] != "通过 Studio 的正常入口创建" {
		t.Fatalf("omitting description must preserve it: %+v", edited)
	}
	studioCall(t, owner, "studio.requirement.update", map[string]any{
		"project": id, "id": requirementID, "title": "更新后的需求", "description": "人工修订说明", "source": "manual",
	})
	edited = findRequirement(t, studioCall(t, owner, "studio.requirement.list", map[string]any{"project": id}), requirementID)
	if edited["description"] != "人工修订说明" || edited["source"] != "manual" {
		t.Fatalf("requirement edit was not readable: %+v", edited)
	}

	var archivedRequirementID int64
	for i := 2; i <= 3; i++ {
		var requirement int64
		if err := pool.QueryRow(ctx, `INSERT INTO studio_requirements(tenant_id,project_id,title,created_by,archived)
			VALUES ('tenant-a',$1,$2,$3,$4) RETURNING id`, id, fmt.Sprint("Requirement ", i), account.ID, i == 3).Scan(&requirement); err != nil {
			t.Fatal(err)
		}
		if i == 2 {
			if _, err := pool.Exec(ctx, `INSERT INTO studio_items(tenant_id,project_id,requirement_id,kind,title,stage,created_by)
			VALUES ('tenant-a',$1,$2,'execution','Accepted task','accepted',$3)`, id, requirement, account.ID); err != nil {
				t.Fatal(err)
			}
		}
		if i == 3 {
			archivedRequirementID = requirement
		}
	}
	result := studioCall(t, owner, "studio.project.list", map[string]any{})
	projects := result.Data.(map[string]any)["projects"].([]any)
	row := projects[0].(map[string]any)
	if row["name"] != "Managed demo" || row["requirementsPending"] != float64(1) || row["requirementsTotal"] != float64(3) {
		t.Fatalf("incorrect management projection: %+v", row)
	}

	blocked := studioCall(t, owner, "studio.requirement.update", map[string]any{
		"project": id, "id": archivedRequirementID, "title": "不应写入",
	})
	if blocked.ErrorCode != "conflict" {
		t.Fatalf("archived requirement was editable: %+v", blocked)
	}
	mismatch := studioCall(t, owner, "studio.requirement.update", map[string]any{
		"project": id + 999, "id": requirementID, "title": "不应写入",
	})
	if mismatch.ErrorCode != "not_found" {
		t.Fatalf("requirement edit accepted a foreign project id: %+v", mismatch)
	}
	checkSucceeded(t, owner.invoke(t, "identity.account.create", map[string]any{
		"username": "studio-editor", "fullName": "Other Studio editor", "email": "", "status": "active",
		"organizationId": account.OrganizationID, "departmentId": account.DepartmentID,
		"roleIds": []int64{}, "overrides": []map[string]any{
			{"permission": "studio:read", "effect": "allow", "scope": "OWN"},
			{"permission": "studio:write", "effect": "allow", "scope": "OWN"},
		},
		"password": "TestOnly!2026Secure",
	}, true))
	other := loginIAM(t, handler, "studio-editor")
	foreign := studioCall(t, other, "studio.requirement.update", map[string]any{
		"project": id, "id": requirementID, "title": "越权修改",
	})
	if foreign.ErrorCode != "not_found" {
		t.Fatalf("foreign requirement edit was not rejected: %+v", foreign)
	}
	checkSucceeded(t, owner.invoke(t, "identity.account.create", map[string]any{
		"username": "studio-viewer", "fullName": "Studio viewer", "email": "", "status": "active",
		"organizationId": account.OrganizationID, "departmentId": account.DepartmentID,
		"roleIds": []int64{}, "overrides": []map[string]any{
			{"permission": "studio:read", "effect": "allow", "scope": "OWN"},
		},
		"password": "TestOnly!2026Secure",
	}, true))
	viewer := loginIAM(t, handler, "studio-viewer")
	checkStatus(t, viewer.invoke(t, "studio.requirement.update", map[string]any{
		"project": id, "id": requirementID, "title": "无写权限修改",
	}, true), http.StatusForbidden)

	var crossTenantProjectID, crossTenantRequirementID int64
	if err := pool.QueryRow(ctx, `INSERT INTO studio_projects(tenant_id, root, created_by) VALUES ('tenant-b', '/tmp/tenant-b-project', 1) RETURNING id`).Scan(&crossTenantProjectID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO studio_requirements(tenant_id, project_id, title, created_by) VALUES ('tenant-b', $1, '跨租户需求', 1) RETURNING id`, crossTenantProjectID).Scan(&crossTenantRequirementID); err != nil {
		t.Fatal(err)
	}
	crossTenant := studioCall(t, owner, "studio.requirement.update", map[string]any{
		"project": crossTenantProjectID, "id": crossTenantRequirementID, "title": "跨租户修改",
	})
	if crossTenant.ErrorCode != "not_found" {
		t.Fatalf("cross-tenant requirement edit was not rejected: %+v", crossTenant)
	}

	if row["developmentBrief"] != "Use project standards." || row["api"] != nil || row["web"] != nil {
		t.Fatalf("project details must keep the brief separate from runtime addresses: %+v", row)
	}
	studioCall(t, owner, "studio.project.link", map[string]any{"path": root, "name": "Updated name"})
	info := studioCall(t, owner, "studio.project.get", map[string]any{"id": id})
	if info.Data.(map[string]any)["developmentBrief"] != "Use project standards." {
		t.Fatal("omitting the brief from an edit must preserve it")
	}
	studioCall(t, owner, "studio.project.link", map[string]any{"path": root, "developmentBrief": ""})
	info = studioCall(t, owner, "studio.project.get", map[string]any{"id": id})
	if info.Data.(map[string]any)["developmentBrief"] != "" {
		t.Fatal("an explicitly empty brief must clear the saved text")
	}

	planned := studioCall(t, owner, "studio.item.create", map[string]any{
		"project": id, "kind": "workflow", "title": "Plan an approval workflow",
		"catalogTarget": "demo-approval@1.0.0", "checks": "Run the relevant tests.",
	})
	if planned.Status != "succeeded" || planned.Data.(map[string]any)["stage"] != "planned" {
		t.Fatalf("new catalog item should start in planning: %+v", planned)
	}
	planID := int64(planned.Data.(map[string]any)["id"].(float64))
	duplicate := studioCall(t, owner, "studio.item.create", map[string]any{
		"project": id, "kind": "workflow", "title": "Duplicate plan", "catalogTarget": "demo-approval@1.0.0",
	})
	if duplicate.ErrorCode != "conflict" {
		t.Fatalf("a fixed target can be planned only once per project: %+v", duplicate)
	}
	task := studioCall(t, owner, "studio.item.create", map[string]any{
		"project": id, "kind": "execution", "title": "Implement the approval workflow",
		"requirementId": requirementID, "subjectItemId": planID,
	})
	if task.Status != "succeeded" {
		t.Fatalf("cannot create a task from the requirement and catalog plan: %+v", task)
	}
	items := studioCall(t, owner, "studio.item.list", map[string]any{"project": id}).Data.(map[string]any)["items"].([]any)
	linkedTask := items[0].(map[string]any)
	if linkedTask["requirementId"] != float64(requirementID) || linkedTask["subjectItemId"] != float64(planID) {
		t.Fatalf("task associations must be readable by Studio: %+v", linkedTask)
	}
	taskID := int64(task.Data.(map[string]any)["id"].(float64))
	pinned := studioCall(t, owner, "studio.item.pin", map[string]any{
		"project": id, "id": taskID, "pinned": true,
	})
	if pinned.Status != "succeeded" || pinned.Data.(map[string]any)["pinned"] != true || len(pinned.EventIds) != 1 {
		t.Fatalf("session pin did not commit exactly one event: %+v", pinned)
	}
	var pinEvent string
	if err := pool.QueryRow(ctx, `SELECT event_type FROM cap_outbox WHERE tenant_id = 'tenant-a' AND event_id = $1`, pinned.EventIds[0]).Scan(&pinEvent); err != nil {
		t.Fatal(err)
	}
	if pinEvent != "studio.item.pinned" {
		t.Fatalf("unexpected pin event: %s", pinEvent)
	}
	archivedSession := studioCall(t, owner, "studio.item.archive", map[string]any{
		"project": id, "id": taskID, "archived": true,
	})
	if archivedSession.Status != "succeeded" || archivedSession.Data.(map[string]any)["archived"] != true || archivedSession.Data.(map[string]any)["pinned"] != false {
		t.Fatalf("archiving a session must clear its pin: %+v", archivedSession)
	}
	if blockedPin := studioCall(t, owner, "studio.item.pin", map[string]any{
		"project": id, "id": taskID, "pinned": true,
	}); blockedPin.ErrorCode != "conflict" {
		t.Fatalf("an archived session accepted a pin: %+v", blockedPin)
	}
	items = studioCall(t, owner, "studio.item.list", map[string]any{"project": id}).Data.(map[string]any)["items"].([]any)
	if listed := findItem(t, items, taskID); listed["pinned"] != false || listed["archived"] != true {
		t.Fatalf("session flags were not readable: %+v", listed)
	}
	if restored := studioCall(t, owner, "studio.item.archive", map[string]any{
		"project": id, "id": taskID, "archived": false,
	}); restored.Data.(map[string]any)["archived"] != false {
		t.Fatalf("a session could not be restored: %+v", restored)
	}
	if foreignPin := studioCall(t, other, "studio.item.pin", map[string]any{
		"project": id, "id": taskID, "pinned": true,
	}); foreignPin.ErrorCode != "conflict" {
		t.Fatalf("a foreign account pinned another owner's session: %+v", foreignPin)
	}
	checkStatus(t, viewer.invoke(t, "studio.item.archive", map[string]any{
		"project": id, "id": taskID, "archived": true,
	}, true), http.StatusForbidden)

	installations := studioCall(t, owner, "application.runtime.installation.list", map[string]any{}).Data.(map[string]any)
	var studioRevision any
	for _, value := range installations["items"].([]any) {
		item := value.(map[string]any)
		if item["pluginId"] == "com.horizon.studio" {
			studioRevision = item["revision"]
			break
		}
	}
	if studioRevision == nil {
		t.Fatal("Studio is absent from application management")
	}
	disabled := studioCall(t, owner, "application.runtime.installation.disable", map[string]any{
		"pluginId": "com.horizon.studio", "revision": studioRevision,
	})
	if disabled.Status != "succeeded" {
		t.Fatalf("cannot disable Studio in application management: %+v", disabled)
	}
	catalog = studioCall(t, owner, "application.runtime.list", map[string]any{})
	for _, value := range catalog.Data.(map[string]any)["items"].([]any) {
		if value.(map[string]any)["id"] == "studio" {
			t.Fatal("disabled Studio is still visible in the menu catalog")
		}
	}
	checkStatus(t, owner.invoke(t, "studio.project.list", map[string]any{}, true), http.StatusServiceUnavailable)

	db := stdlib.OpenDB(*pool.Config().ConnConfig)
	defer db.Close()
	migrator, err := goose.NewProvider(goose.DialectPostgres, db, migrations.Files)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrator.DownTo(t.Context(), 40); err != nil {
		t.Fatalf("rollback failed after managing Studio through the runtime: %v", err)
	}
}

func findRequirement(t *testing.T, result gencapability.CapabilityResult, id int64) map[string]any {
	t.Helper()
	for _, value := range result.Data.(map[string]any)["requirements"].([]any) {
		row := value.(map[string]any)
		if row["id"] == float64(id) {
			return row
		}
	}
	t.Fatalf("requirement %d is missing from the project list", id)
	return nil
}

func findItem(t *testing.T, items []any, id int64) map[string]any {
	t.Helper()
	for _, value := range items {
		row := value.(map[string]any)
		if row["id"] == float64(id) {
			return row
		}
	}
	t.Fatalf("item %d is missing from the project item list", id)
	return nil
}
