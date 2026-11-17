//go:build integration

package tests

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	gencapability "horizon/foundation/gen/capability"
)

func studioRequirementID(t *testing.T, result gencapability.CapabilityResult) int64 {
	t.Helper()
	return int64(result.Data.(map[string]any)["id"].(float64))
}

func TestStudioRequirementTree(t *testing.T) {
	handler, pool, account := iamFixture(t)
	owner := loginIAM(t, handler, "root-admin")
	root := t.TempDir()
	for _, name := range []string{"go.mod", "foundation/cmd/api/main.go"} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	project := studioCall(t, owner, "studio.project.link", map[string]any{"path": root, "name": "Requirement tree"})
	projectID := studioRequirementID(t, project)

	rootRequirement := studioRequirementID(t, studioCall(t, owner, "studio.requirement.create", map[string]any{
		"project": projectID, "title": "Root requirement", "description": "Root body",
	}))
	child := studioCall(t, owner, "studio.requirement.create", map[string]any{
		"project": projectID, "parent": rootRequirement, "title": "Child requirement", "description": "Child body",
	})
	if child.Status != "succeeded" || child.Data.(map[string]any)["parentId"] != float64(rootRequirement) {
		t.Fatalf("child requirement was not linked to its parent: %+v", child)
	}
	childID := studioRequirementID(t, child)
	listed := studioCall(t, owner, "studio.requirement.list", map[string]any{"project": projectID})
	childRow := findRequirement(t, listed, childID)
	rootRow := findRequirement(t, listed, rootRequirement)
	if childRow["parentId"] != float64(rootRequirement) || rootRow["parentId"] != float64(0) {
		t.Fatalf("requirement list does not expose the tree parent: child=%+v root=%+v", childRow, rootRow)
	}

	// The tree has exactly two levels and only a top-level requirement may be a parent.
	grandchild := studioCall(t, owner, "studio.requirement.create", map[string]any{
		"project": projectID, "parent": childID, "title": "Grandchild",
	})
	if grandchild.ErrorCode != "invalid" {
		t.Fatalf("a sub-requirement was accepted as a parent: %+v", grandchild)
	}

	// A child can move under another top-level requirement and back to the root level.
	secondRoot := studioRequirementID(t, studioCall(t, owner, "studio.requirement.create", map[string]any{
		"project": projectID, "title": "Second root",
	}))
	moved := studioCall(t, owner, "studio.requirement.update", map[string]any{
		"project": projectID, "id": childID, "title": "Child requirement", "parent": secondRoot,
	})
	if moved.Status != "succeeded" || moved.Data.(map[string]any)["parentId"] != float64(secondRoot) {
		t.Fatalf("child requirement did not move: %+v", moved)
	}
	promoted := studioCall(t, owner, "studio.requirement.update", map[string]any{
		"project": projectID, "id": childID, "title": "Child requirement", "parent": 0,
	})
	if promoted.Status != "succeeded" || promoted.Data.(map[string]any)["parentId"] != float64(0) {
		t.Fatalf("child requirement was not promoted to a root: %+v", promoted)
	}
	studioCall(t, owner, "studio.requirement.update", map[string]any{
		"project": projectID, "id": childID, "title": "Child requirement", "parent": secondRoot,
	})
	blocked := studioCall(t, owner, "studio.requirement.update", map[string]any{
		"project": projectID, "id": secondRoot, "title": "Second root", "parent": rootRequirement,
	})
	if blocked.ErrorCode != "conflict" {
		t.Fatalf("a requirement with children moved under another requirement: %+v", blocked)
	}
	studioCall(t, owner, "studio.requirement.archive", map[string]any{
		"project": projectID, "id": rootRequirement,
	})
	// rootRequirement has no active children after the move, so it archived alone.
	movedUnderArchived := studioCall(t, owner, "studio.requirement.update", map[string]any{
		"project": projectID, "id": childID, "title": "Child requirement", "parent": rootRequirement,
	})
	if movedUnderArchived.ErrorCode != "invalid" {
		t.Fatalf("a requirement moved under an archived parent: %+v", movedUnderArchived)
	}

	// Parent search only returns active top-level requirements and honours exclude.
	searched := studioCall(t, owner, "studio.requirement.search", map[string]any{"project": projectID, "keyword": "root"})
	names := map[int64]bool{}
	for _, value := range searched.Data.(map[string]any)["requirements"].([]any) {
		row := value.(map[string]any)
		names[int64(row["id"].(float64))] = true
	}
	if names[rootRequirement] || !names[secondRoot] || names[childID] {
		t.Fatalf("parent search returned archived or child requirements: %+v", names)
	}
	searched = studioCall(t, owner, "studio.requirement.search", map[string]any{"project": projectID, "exclude": secondRoot})
	for _, value := range searched.Data.(map[string]any)["requirements"].([]any) {
		if int64(value.(map[string]any)["id"].(float64)) == secondRoot {
			t.Fatal("excluded requirement appeared in parent search")
		}
	}

	// Archiving a parent cascades to every active child in one transaction.
	cascadeRoot := studioRequirementID(t, studioCall(t, owner, "studio.requirement.create", map[string]any{
		"project": projectID, "title": "Cascade root",
	}))
	cascadeChild := studioRequirementID(t, studioCall(t, owner, "studio.requirement.create", map[string]any{
		"project": projectID, "parent": cascadeRoot, "title": "Cascade child",
	}))
	cascaded := studioCall(t, owner, "studio.requirement.archive", map[string]any{"project": projectID, "id": cascadeRoot})
	if cascaded.Status != "succeeded" || len(cascaded.EventIds) != 2 {
		t.Fatalf("cascade archive did not emit one event per requirement: %+v", cascaded)
	}
	after := studioCall(t, owner, "studio.requirement.list", map[string]any{"project": projectID})
	if findRequirement(t, after, cascadeRoot)["archived"] != true || findRequirement(t, after, cascadeChild)["archived"] != true {
		t.Fatalf("cascade archive left a child active: %+v", after.Data)
	}
	again := studioCall(t, owner, "studio.requirement.archive", map[string]any{"project": projectID, "id": cascadeRoot})
	if again.ErrorCode != "conflict" {
		t.Fatalf("archiving an archived requirement was accepted: %+v", again)
	}

	// Search is scoped to the caller's own project.
	checkSucceeded(t, owner.invoke(t, "identity.account.create", map[string]any{
		"username": "tree-other", "fullName": "Other tree account", "email": "", "status": "active",
		"organizationId": account.OrganizationID, "departmentId": account.DepartmentID,
		"roleIds": []int64{}, "overrides": []map[string]any{
			{"permission": "studio:read", "effect": "allow", "scope": "OWN"},
		},
		"password": "TestOnly!2026Secure",
	}, true))
	other := loginIAM(t, handler, "tree-other")
	if blocked := studioCall(t, other, "studio.requirement.search", map[string]any{"project": projectID}); blocked.ErrorCode != "not_found" {
		t.Fatalf("foreign account searched a project requirement: %+v", blocked)
	}

	// A parent from another tenant is not a valid parent for this project.
	var foreignProjectID, foreignRequirementID int64
	ctx := context.Background()
	if err := pool.QueryRow(ctx, `INSERT INTO studio_projects(tenant_id, root, created_by) VALUES ('tenant-b', '/tmp/tenant-b-tree', 1) RETURNING id`).Scan(&foreignProjectID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO studio_requirements(tenant_id, project_id, title, created_by) VALUES ('tenant-b', $1, 'Foreign parent', 1) RETURNING id`, foreignProjectID).Scan(&foreignRequirementID); err != nil {
		t.Fatal(err)
	}
	foreignParent := studioCall(t, owner, "studio.requirement.create", map[string]any{
		"project": projectID, "parent": foreignRequirementID, "title": "Cross tenant child",
	})
	if foreignParent.ErrorCode != "invalid" {
		t.Fatalf("a parent from another tenant was accepted: %+v", foreignParent)
	}
	if blocked := studioCall(t, owner, "studio.requirement.search", map[string]any{"project": foreignProjectID}); blocked.ErrorCode != "not_found" {
		t.Fatalf("cross-tenant parent search was accepted: %+v", blocked)
	}
}
