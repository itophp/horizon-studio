package identityapp

import (
	"context"
	"time"

	genidentity "horizon/foundation/gen/identity"
	iam "horizon/foundation/system/identity"
	"horizon/internal/repository/dbgen"
)

type structureRepository struct{ q *dbgen.Queries }

func (r *structureRepository) mapOrganization(ctx context.Context, tenant string, row dbgen.IAMGetOrganizationRow) (*genidentity.OrganizationRecord, error) {
	return &genidentity.OrganizationRecord{ID: row.ID, Name: row.Name, Code: row.Code, Kind: row.Kind, ParentID: row.ParentID, Status: row.Status, Description: row.Description, CreatedAt: row.CreatedAt.Time.UTC().Format(time.RFC3339)}, nil
}

func (r *structureRepository) organization(ctx context.Context, scope iam.Scope, id int64) (*genidentity.OrganizationRecord, error) {
	row, err := r.q.IAMGetOrganization(ctx, dbgen.IAMGetOrganizationParams{ID: id, TenantID: scope.TenantID, AllData: scope.All, OrgIds: scope.OrgIDs})
	if err != nil {
		return nil, err
	}
	return r.mapOrganization(ctx, scope.TenantID, row)
}

func (r *structureRepository) mapDepartment(ctx context.Context, tenant string, row dbgen.IAMGetDepartmentRow) (*genidentity.DepartmentRecord, error) {
	return &genidentity.DepartmentRecord{ID: row.ID, Name: row.Name, Code: row.Code, OrganizationID: row.OrganizationID, ParentID: row.ParentID, Manager: row.Manager, Status: row.Status, Description: row.Description, CreatedAt: row.CreatedAt.Time.UTC().Format(time.RFC3339)}, nil
}

func (r *structureRepository) department(ctx context.Context, scope iam.Scope, id int64) (*genidentity.DepartmentRecord, error) {
	row, err := r.q.IAMGetDepartment(ctx, dbgen.IAMGetDepartmentParams{ID: id, TenantID: scope.TenantID, AllData: scope.All, OrgIds: scope.OrgIDs, DeptIds: scope.DeptIDs})
	if err != nil {
		return nil, err
	}
	return r.mapDepartment(ctx, scope.TenantID, row)
}

func (r *structureRepository) departmentTree(ctx context.Context, tenant string, id int64) ([]int64, error) {
	return r.q.IAMDepartmentTree(ctx, dbgen.IAMDepartmentTreeParams{TenantID: tenant, ID: id})
}
func (r *structureRepository) updateOrganization(ctx context.Context, scope iam.Scope, id int64, payload *organizationInput) (*genidentity.OrganizationRecord, error) {
	idResult, err := r.q.IAMUpdateOrganization(ctx, dbgen.IAMUpdateOrganizationParams{Name: payload.Name, Code: payload.Code, Description: payload.Description, TenantID: scope.TenantID, AllData: scope.All, OrgIds: scope.OrgIDs, ID: id})
	if err != nil {
		return nil, err
	}
	return r.organization(ctx, iam.Scope{TenantID: scope.TenantID, All: true}, idResult)
}

func (r *structureRepository) createDepartment(ctx context.Context, scope iam.Scope, payload *departmentInput) (*genidentity.DepartmentRecord, error) {
	idResult, err := r.q.IAMCreateDepartment(ctx, dbgen.IAMCreateDepartmentParams{Name: payload.Name, Code: payload.Code, OrganizationID: payload.OrganizationID, ParentID: payload.ParentID, Manager: payload.Manager, Status: payload.Status, Description: payload.Description, TenantID: scope.TenantID})
	if err != nil {
		return nil, err
	}
	return r.department(ctx, iam.Scope{TenantID: scope.TenantID, All: true}, idResult)
}

func (r *structureRepository) updateDepartment(ctx context.Context, scope iam.Scope, id int64, payload *departmentInput) (*genidentity.DepartmentRecord, error) {
	idResult, err := r.q.IAMUpdateDepartment(ctx, dbgen.IAMUpdateDepartmentParams{Name: payload.Name, Code: payload.Code, OrganizationID: payload.OrganizationID, ParentID: payload.ParentID, Manager: payload.Manager, Status: payload.Status, Description: payload.Description, TenantID: scope.TenantID, AllData: scope.All, OrgIds: scope.OrgIDs, DeptIds: scope.DeptIDs, ID: id})
	if err != nil {
		return nil, err
	}
	return r.department(ctx, iam.Scope{TenantID: scope.TenantID, All: true}, idResult)
}

func (r *structureRepository) deleteDepartment(ctx context.Context, scope iam.Scope, id int64) error {
	_, err := r.q.IAMDeleteDepartment(ctx, dbgen.IAMDeleteDepartmentParams{ID: id, TenantID: scope.TenantID, AllData: scope.All, OrgIds: scope.OrgIDs, DeptIds: scope.DeptIDs})
	return err
}

// ReadDirectoryStructure loads the application-owned organization and department directory.
func ReadDirectoryStructure(ctx context.Context, service *iam.Service, q *dbgen.Queries, actor *iam.Principal) ([]*genidentity.OrganizationRecord, []*genidentity.DepartmentRecord, error) {
	repo := &structureRepository{q: q}
	organizations := []*genidentity.OrganizationRecord{}
	departments := []*genidentity.DepartmentRecord{}
	if actor.Can("organizations:read") {
		scope, err := service.ScopeFor(ctx, actor, "organizations:read", false)
		if err != nil {
			return nil, nil, err
		}
		rows, err := q.IAMListOrganizations(ctx, dbgen.IAMListOrganizationsParams{TenantID: scope.TenantID, AllData: scope.All, OrgIds: scope.OrgIDs})
		if err != nil {
			return nil, nil, err
		}
		for _, row := range rows {
			item, err := repo.mapOrganization(ctx, scope.TenantID, dbgen.IAMGetOrganizationRow(row))
			if err != nil {
				return nil, nil, err
			}
			organizations = append(organizations, item)
		}
	}
	if actor.Can("departments:read") {
		scope, err := service.ScopeFor(ctx, actor, "departments:read", false)
		if err != nil {
			return nil, nil, err
		}
		rows, err := q.IAMListDepartments(ctx, dbgen.IAMListDepartmentsParams{TenantID: scope.TenantID, AllData: scope.All, OrgIds: scope.OrgIDs, DeptIds: scope.DeptIDs})
		if err != nil {
			return nil, nil, err
		}
		for _, row := range rows {
			item, err := repo.mapDepartment(ctx, scope.TenantID, dbgen.IAMGetDepartmentRow(row))
			if err != nil {
				return nil, nil, err
			}
			departments = append(departments, item)
		}
	}
	return organizations, departments, nil
}
