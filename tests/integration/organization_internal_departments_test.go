//go:build integration

package tests

import (
	"testing"

	genidentity "horizon/foundation/gen/identity"
	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/system/identity"
)

func TestSingleOrganizationDepartmentDirectory(t *testing.T) {
	handler, pool, root := iamFixture(t)
	admin := loginTenantIAM(t, handler, root.Username, "tenant-a")

	var organizationID int64
	if err := pool.QueryRow(t.Context(), "SELECT id FROM identity_organizations WHERE tenant_id = 'tenant-a'").Scan(&organizationID); err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Bootstrap(t.Context(), pool, "tenant-b", "other-admin", "TestOnly!2026Secure"); err != nil {
		t.Fatal(err)
	}
	var foreignID int64
	if err := pool.QueryRow(t.Context(), "SELECT id FROM identity_organizations WHERE tenant_id = 'tenant-b'").Scan(&foreignID); err != nil {
		t.Fatal(err)
	}
	department := func(name, code string, parentID int64) map[string]any {
		return map[string]any{
			"name": name, "code": code, "parentId": parentID,
			"manager": "", "description": "", "status": "active",
		}
	}
	createDepartment := func(name, code string, parentID int64) genidentity.DepartmentRecord {
		t.Helper()
		return decodeData[genidentity.DepartmentRecord](t, admin.invoke(t, "identity.department.create", department(name, code, parentID), true))
	}
	parent := createDepartment("Root department", "root", 0)
	child := createDepartment("Existing team", "existing-team", parent.ID)
	if parent.OrganizationID != organizationID || child.OrganizationID != organizationID || child.ParentID != parent.ID {
		t.Fatalf("departments did not use the tenant organization: %+v %+v", parent, child)
	}
	var storedParentID int64
	if err := pool.QueryRow(t.Context(), "SELECT parent_id FROM identity_departments WHERE id = $1", child.ID).Scan(&storedParentID); err != nil || storedParentID != parent.ID {
		t.Fatalf("unexpected stored parent: %d (%v)", storedParentID, err)
	}

	foreign := department("Outside", "outside", 0)
	foreign["organizationId"] = foreignID
	response := admin.invoke(t, "identity.department.create", foreign, true)
	checkStatus(t, response, 200)
	if result := decodeBody[contracts.Result](t, response); result.ErrorCode != "invalid" {
		t.Fatalf("foreign organization accepted: %+v", result)
	}

	move := department("Moved team", child.Code, 0)
	move["id"], move["organizationId"] = child.ID, organizationID
	moved := decodeData[genidentity.DepartmentRecord](t, admin.invoke(t, "identity.department.update", move, true))
	if moved.ParentID != 0 || moved.Name != "Moved team" {
		t.Fatalf("department move failed: %+v", moved)
	}

	response = admin.invoke(t, "identity.department.create", department("Duplicate team", child.Code, 0), true)
	checkStatus(t, response, 200)
	if result := decodeBody[contracts.Result](t, response); result.ErrorCode != "conflict" {
		t.Fatalf("unexpected duplicate department result: %+v", result)
	}
	checkIAMInvocation(t, admin.invoke(t, "identity.department.delete", map[string]any{"id": child.ID}, true))
	var exists bool
	if err := pool.QueryRow(t.Context(), "SELECT EXISTS (SELECT 1 FROM identity_department_directory WHERE id = $1)", child.ID).Scan(&exists); err != nil || exists {
		t.Fatalf("deleted department is still in the directory: %v (%v)", exists, err)
	}
}
