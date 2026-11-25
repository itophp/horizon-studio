package identityapp

import (
	"context"
	"slices"

	genidentity "horizon/foundation/gen/identity"
	"horizon/foundation/kernel/capability/contracts"
	iam "horizon/foundation/system/identity"
	"horizon/internal/repository/dbgen"
)

type structureService struct {
	iam  *iam.Service
	repo *structureRepository
}

type structureDeleteInput struct{ ID int64 }

// organizationInput is the reviewed payload of organization updates.
type organizationInput struct {
	ID          int64
	Name        string
	Code        string
	Kind        string
	ParentID    int64
	Status      string
	Description string
}

// departmentInput is the reviewed payload of department creation and update.
type departmentInput struct {
	ID             int64
	Name           string
	Code           string
	OrganizationID int64
	ParentID       int64
	Manager        string
	Status         string
	Description    string
}

func (s *structureService) permanentDepartmentAuthority(ctx context.Context, id, parent, org int64) error {
	_, scope, err := s.iam.Authorize(ctx, "departments:update", true)
	if err != nil {
		return contracts.Problem("forbidden")
	}
	old, err := s.repo.department(ctx, scope, id)
	if err != nil {
		return contracts.Problem("forbidden")
	}
	if old.ParentID != parent && !scope.All && !slices.Contains(scope.OrgIDs, org) && !slices.Contains(scope.DeptIDs, parent) {
		return contracts.Problem("forbidden")
	}
	return nil
}

func (s *structureService) UpdateOrganization(ctx context.Context, payload *organizationInput) (*genidentity.OrganizationRecord, error) {
	_, scope, err := s.iam.Authorize(ctx, "organizations:update", false)
	if err != nil {
		return nil, err
	}
	old, err := s.repo.organization(ctx, scope, payload.ID)
	if err != nil {
		return nil, iam.MapStorageError(err)
	}
	if old.Kind != payload.Kind || old.ParentID != payload.ParentID || old.Status != payload.Status {
		return nil, contracts.Problem("invalid")
	}
	record, err := s.repo.updateOrganization(ctx, scope, payload.ID, payload)
	return record, iam.MapStorageError(err)
}
func (s *structureService) validateDepartment(ctx context.Context, p *iam.Principal, id, org, parent int64, status string) error {
	company, err := s.repo.organization(ctx, iam.Scope{TenantID: p.TenantID, All: true}, org)
	if err != nil || company.Kind != "company" || company.Status != "active" {
		return contracts.Problem("invalid")
	}
	if parent > 0 {
		record, err := s.repo.department(ctx, iam.Scope{TenantID: p.TenantID, All: true}, parent)
		if err != nil || record.OrganizationID != org || record.Status != "active" {
			return contracts.Problem("invalid")
		}
		if id > 0 {
			tree, err := s.repo.departmentTree(ctx, p.TenantID, id)
			if err != nil {
				return iam.MapStorageError(err)
			}
			if slices.Contains(tree, parent) {
				return contracts.Problem("invalid")
			}
		}
	}
	if status != "active" && id > 0 {
		anchors, err := s.repo.q.IAMRootAnchors(ctx, p.TenantID)
		if err != nil {
			return iam.MapStorageError(err)
		}
		for _, anchor := range anchors {
			if anchor.DepartmentID == id {
				return contracts.Problem("conflict")
			}
		}
	}
	return nil
}
func (s *structureService) CreateDepartment(ctx context.Context, payload *departmentInput) (*genidentity.DepartmentRecord, error) {
	p, scope, err := s.iam.Authorize(ctx, "departments:create", false)
	if err != nil {
		return nil, err
	}
	organizations, err := s.repo.q.IAMListOrganizations(ctx, dbgen.IAMListOrganizationsParams{TenantID: p.TenantID, AllData: true})
	if err != nil {
		return nil, iam.MapStorageError(err)
	}
	if len(organizations) != 1 || (payload.OrganizationID != 0 && payload.OrganizationID != organizations[0].ID) {
		return nil, contracts.Problem("invalid")
	}
	payload.OrganizationID = organizations[0].ID
	if !scope.All && !slices.Contains(scope.OrgIDs, payload.OrganizationID) && !slices.Contains(scope.DeptIDs, payload.ParentID) {
		return nil, contracts.Problem("forbidden")
	}
	if err = s.validateDepartment(ctx, p, 0, payload.OrganizationID, payload.ParentID, payload.Status); err != nil {
		return nil, err
	}
	record, err := s.repo.createDepartment(ctx, scope, payload)
	return record, iam.MapStorageError(err)
}
func (s *structureService) UpdateDepartment(ctx context.Context, payload *departmentInput) (*genidentity.DepartmentRecord, error) {
	p, scope, err := s.iam.Authorize(ctx, "departments:update", false)
	if err != nil {
		return nil, err
	}
	old, err := s.repo.department(ctx, scope, payload.ID)
	if err != nil {
		return nil, iam.MapStorageError(err)
	}
	if old.ParentID != payload.ParentID || old.Status != payload.Status {
		if err = s.permanentDepartmentAuthority(ctx, payload.ID, payload.ParentID, payload.OrganizationID); err != nil {
			return nil, err
		}
	}
	if old.OrganizationID != payload.OrganizationID {
		return nil, contracts.Problem("invalid")
	}
	if old.ParentID != payload.ParentID && !scope.All && !slices.Contains(scope.OrgIDs, payload.OrganizationID) && !slices.Contains(scope.DeptIDs, payload.ParentID) {
		return nil, contracts.Problem("forbidden")
	}
	if err = s.validateDepartment(ctx, p, payload.ID, payload.OrganizationID, payload.ParentID, payload.Status); err != nil {
		return nil, err
	}
	record, err := s.repo.updateDepartment(ctx, scope, payload.ID, payload)
	return record, iam.MapStorageError(err)
}
func (s *structureService) DeleteDepartment(ctx context.Context, payload *structureDeleteInput) error {
	_, scope, err := s.iam.Authorize(ctx, "departments:delete", false)
	if err != nil {
		return err
	}
	return iam.MapStorageError(s.repo.deleteDepartment(ctx, scope, payload.ID))
}
