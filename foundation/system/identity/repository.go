package identity

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	genidentity "horizon/foundation/gen/identity"
	"horizon/internal/repository/dbgen"
	"time"
)

type Repository struct {
	pool *pgxpool.Pool
	q    *dbgen.Queries
}

func newRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool, q: dbgen.New(pool)}
}
func encodeTenantGrants(grants []*genidentity.PermissionGrant) ([]byte, error) {
	type grantRecord struct {
		Permission string `json:"permission"`
		Effect     string `json:"effect"`
		Scope      string `json:"scope"`
	}
	records := make([]grantRecord, 0, len(grants))
	for _, grant := range grants {
		records = append(records, grantRecord{Permission: grant.Permission, Effect: grant.Effect, Scope: grant.Scope})
	}
	return json.Marshal(records)
}

func decodeGrants(value []byte) ([]*genidentity.PermissionGrant, error) {
	out := []*genidentity.PermissionGrant{}
	if err := json.Unmarshal(value, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []*genidentity.PermissionGrant{}
	}
	return out, nil
}
func timeValue(value pgtype.Timestamptz) string {
	if !value.Valid {
		return ""
	}
	return nowString(value.Time)
}

func (r *Repository) mapAccount(ctx context.Context, tenant string, row dbgen.IAMGetAccountRow) (*genidentity.AccountRecord, error) {
	grants, err := decodeGrants(row.Overrides)
	if err != nil {
		return nil, err
	}
	roles, err := r.q.IAMAccountRoles(ctx, dbgen.IAMAccountRolesParams{TenantID: tenant, AccountID: row.ID})
	if err != nil {
		return nil, err
	}
	return &genidentity.AccountRecord{Memberships: []*genidentity.DepartmentMembership{}, ID: row.ID, Username: row.Username, FullName: row.FullName, Email: row.Email, Status: row.Status, IsAdmin: row.IsAdmin, Overrides: grants, RoleIds: roles, CreatedAt: timeValue(row.CreatedAt), LastLogin: timeValue(row.LastLogin)}, nil
}

func (r *Repository) authorityAccount(ctx context.Context, tenant string, id int64) (*genidentity.AccountRecord, error) {
	row, err := r.q.IAMGetAuthorityAccount(ctx, dbgen.IAMGetAuthorityAccountParams{TenantID: tenant, ID: id})
	if err != nil {
		return nil, err
	}
	grants, err := decodeGrants(row.MachineGrants)
	if err != nil {
		return nil, err
	}
	return &genidentity.AccountRecord{
		ID: row.ID, Username: row.Username, FullName: row.Username, Status: row.Status, IsAdmin: row.IsAdmin,
		CreatedAt: timeValue(row.CreatedAt), LastLogin: timeValue(row.LastLogin),
		RoleIds: []int64{}, Overrides: grants, Memberships: []*genidentity.DepartmentMembership{},
	}, nil
}

func (r *Repository) account(ctx context.Context, scope Scope, id int64) (*genidentity.AccountRecord, error) {
	row, err := r.q.IAMGetAccount(ctx, dbgen.IAMGetAccountParams{ID: id, TenantID: scope.TenantID, AllData: scope.All, OwnID: scope.OwnID})
	if err != nil {
		return nil, err
	}
	return r.mapAccount(ctx, scope.TenantID, row)
}

func (r *Repository) accounts(ctx context.Context, scope Scope) ([]*genidentity.AccountRecord, error) {
	rows, err := r.q.IAMListAccounts(ctx, dbgen.IAMListAccountsParams{TenantID: scope.TenantID, AllData: scope.All, OwnID: scope.OwnID})
	if err != nil {
		return nil, err
	}
	out := make([]*genidentity.AccountRecord, 0, len(rows))
	for _, row := range rows {
		item, err := r.mapAccount(ctx, scope.TenantID, dbgen.IAMGetAccountRow(row))
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (r *Repository) deleteAccount(ctx context.Context, scope Scope, id int64) error {
	_, err := r.q.IAMDeleteAccount(ctx, dbgen.IAMDeleteAccountParams{ID: id, TenantID: scope.TenantID, AllData: scope.All, OwnID: scope.OwnID})
	return err
}

func (r *Repository) mapRole(ctx context.Context, tenant string, row dbgen.IAMGetRoleRow) (*genidentity.RoleRecord, error) {
	grants, err := decodeGrants(row.Grants)
	if err != nil {
		return nil, err
	}
	count, err := r.q.IAMRoleMemberCount(ctx, dbgen.IAMRoleMemberCountParams{TenantID: tenant, RoleID: row.ID})
	if err != nil {
		return nil, err
	}
	return &genidentity.RoleRecord{ID: row.ID, Name: row.Name, Code: row.Code, Status: row.Status, Description: row.Description, Grants: grants, MemberCount: count, CreatedAt: timeValue(row.CreatedAt)}, nil
}

func (r *Repository) role(ctx context.Context, scope Scope, id int64) (*genidentity.RoleRecord, error) {
	row, err := r.q.IAMGetRole(ctx, dbgen.IAMGetRoleParams{ID: id, TenantID: scope.TenantID, AllData: scope.All})
	if err != nil {
		return nil, err
	}
	return r.mapRole(ctx, scope.TenantID, row)
}

func (r *Repository) roles(ctx context.Context, scope Scope) ([]*genidentity.RoleRecord, error) {
	rows, err := r.q.IAMListRoles(ctx, dbgen.IAMListRolesParams{TenantID: scope.TenantID, AllData: scope.All})
	if err != nil {
		return nil, err
	}
	out := make([]*genidentity.RoleRecord, 0, len(rows))
	for _, row := range rows {
		item, err := r.mapRole(ctx, scope.TenantID, dbgen.IAMGetRoleRow(row))
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (r *Repository) deleteRole(ctx context.Context, scope Scope, id int64) error {
	_, err := r.q.IAMDeleteRole(ctx, dbgen.IAMDeleteRoleParams{ID: id, TenantID: scope.TenantID, AllData: scope.All})
	return err
}

func (r *Repository) session(ctx context.Context, hash string) (dbgen.IAMSessionRow, error) {
	return r.q.IAMSession(ctx, hash)
}
func (r *Repository) deleteSession(ctx context.Context, hash string) error {
	return r.q.IAMDeleteSession(ctx, hash)
}
func (r *Repository) loginBlocked(ctx context.Context, key string) (bool, error) {
	row, err := r.q.IAMLoginThrottle(ctx, key)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return row.Failures >= 5 && row.LockedUntil.Time.After(time.Now()), nil
}
func (r *Repository) loginFailure(ctx context.Context, key string) error {
	return r.q.IAMLoginFailure(ctx, key)
}
func (r *Repository) createRole(ctx context.Context, scope Scope, payload *RoleInput) (*genidentity.RoleRecord, error) {
	grants, err := encodeTenantGrants(payload.Grants)
	if err != nil {
		return nil, err
	}
	idResult, err := r.q.IAMCreateRole(ctx, dbgen.IAMCreateRoleParams{Name: payload.Name, Code: payload.Code, Description: payload.Description, Status: payload.Status, Grants: grants, TenantID: scope.TenantID, TemplateKey: "custom"})
	if err != nil {
		return nil, err
	}
	return r.role(ctx, Scope{TenantID: scope.TenantID, All: true}, idResult)
}

func (r *Repository) updateRole(ctx context.Context, scope Scope, id int64, payload *RoleInput) (*genidentity.RoleRecord, error) {
	grants, err := encodeTenantGrants(payload.Grants)
	if err != nil {
		return nil, err
	}
	idResult, err := r.q.IAMUpdateRole(ctx, dbgen.IAMUpdateRoleParams{Name: payload.Name, Code: payload.Code, Description: payload.Description, Status: payload.Status, Grants: grants, TenantID: scope.TenantID, AllData: scope.All, ID: id})
	if err != nil {
		return nil, err
	}
	return r.role(ctx, Scope{TenantID: scope.TenantID, All: true}, idResult)
}
