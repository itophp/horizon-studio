package identity

import (
	"context"
	genidentity "horizon/foundation/gen/identity"
	"horizon/internal/repository/dbgen"
	"net/mail"
	"strings"
)

func contains(ids []int64, id int64) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

// AccountProfileWriter persists application-owned profile data in the caller's transaction.
type AccountProfileWriter func(context.Context, dbgen.DBTX, string, int64, string, string) error

func (s *Service) ConfigureAccountProfileWriter(writer AccountProfileWriter) {
	s.applicationAccountProfileWriter = writer
}

func (s *Service) writeAccountProfile(ctx context.Context, q *dbgen.Queries, tenant string, id int64, name, email string) error {
	if s.applicationAccountProfileWriter == nil {
		return nil
	}
	return s.applicationAccountProfileWriter(ctx, q.Connection(), tenant, id, name, email)
}

func (s *Service) CreateAccount(ctx context.Context, payload *AccountInput) (*genidentity.AccountRecord, error) {
	actor, scope, err := s.Authorize(ctx, "accounts:create", true)
	if err != nil || !scope.All {
		return nil, problem("forbidden")
	}
	if err := validateAccountProfile(payload); err != nil {
		return nil, err
	}
	if err := s.validatePolicyPassword(ctx, actor, payload.Password); err != nil {
		return nil, err
	}
	hash, err := HashPassword(payload.Password)
	if err != nil {
		return nil, problem("invalid")
	}
	id, err := s.repo.q.IAMCreateAccount(ctx, dbgen.IAMCreateAccountParams{
		TenantID: actor.TenantID, Username: payload.Username,
		Status: payload.Status, PasswordHash: hash,
	})
	if err != nil {
		return nil, safe(err)
	}
	if err := s.writeAccountProfile(ctx, s.repo.q, actor.TenantID, id, payload.FullName, payload.Email); err != nil {
		return nil, safe(err)
	}
	return s.repo.account(ctx, tenantScope(actor), id)
}

func validateAccountProfile(payload *AccountInput) error {
	payload.Username = strings.ToLower(strings.TrimSpace(payload.Username))
	payload.FullName = strings.TrimSpace(payload.FullName)
	if !invitationUsername.MatchString(payload.Username) || payload.FullName == "" {
		return problem("invalid")
	}
	if payload.Email != "" {
		if _, err := mail.ParseAddress(payload.Email); err != nil {
			return problem("invalid")
		}
	}
	return nil
}

func (s *Service) UpdateAccount(ctx context.Context, payload *AccountInput) (*genidentity.AccountRecord, error) {
	actor, scope, err := s.Authorize(ctx, "accounts:update", true)
	if err != nil || (!scope.All && scope.OwnID != payload.ID) {
		return nil, problem("forbidden")
	}
	if err := validateAccountProfile(payload); err != nil {
		return nil, err
	}
	old, err := s.repo.account(ctx, scope, payload.ID)
	if err != nil {
		return nil, safe(err)
	}
	if old.IsAdmin && (old.ID != actor.Account.ID || payload.Status != "active") {
		return nil, problem("forbidden")
	}
	if !scope.All && old.Status != payload.Status {
		return nil, problem("forbidden")
	}
	var passwordHash string
	if payload.Password != "" {
		if err := s.validatePolicyPassword(ctx, actor, payload.Password); err != nil {
			return nil, err
		}
		passwordHash, err = HashPassword(payload.Password)
		if err != nil {
			return nil, problem("invalid")
		}
	}
	_, err = s.repo.q.IAMUpdateAccountLogin(ctx, dbgen.IAMUpdateAccountLoginParams{
		TenantID: actor.TenantID, ID: payload.ID, Username: payload.Username,
		Status: payload.Status, ExpectedStatus: old.Status,
	})
	if err != nil {
		return nil, safe(err)
	}
	if passwordHash != "" {
		if _, err := s.repo.q.IAMSetPassword(ctx, dbgen.IAMSetPasswordParams{
			TenantID: actor.TenantID, ID: payload.ID, PasswordHash: passwordHash,
		}); err != nil {
			return nil, safe(err)
		}
	}
	if passwordHash != "" || payload.Status != "active" {
		if err := s.repo.q.IAMRevokeSessions(ctx, dbgen.IAMRevokeSessionsParams{
			TenantID: actor.TenantID, AccountID: payload.ID,
		}); err != nil {
			return nil, safe(err)
		}
	}
	if err := s.writeAccountProfile(ctx, s.repo.q, actor.TenantID, payload.ID, payload.FullName, payload.Email); err != nil {
		return nil, safe(err)
	}
	return s.repo.account(ctx, scope, payload.ID)
}

func (s *Service) DeleteAccount(ctx context.Context, payload *DeleteInput) error {
	p, scope, err := s.Authorize(ctx, "accounts:delete", true)
	if err != nil || !scope.All {
		return problem("forbidden")
	}
	record, err := s.repo.account(ctx, scope, payload.ID)
	if err != nil {
		return safe(err)
	}
	if record.IsAdmin || record.ID == p.Account.ID {
		return problem("conflict")
	}
	return safe(s.repo.deleteAccount(ctx, scope, payload.ID))
}

func (s *Service) roleAuthority(ctx context.Context, permission string) (*Principal, error) {
	p, err := Current(ctx)
	if err != nil {
		return nil, err
	}
	base, err := p.delegable()
	if err != nil || !base.Can(permission) || !base.Can("access:manage") {
		return nil, problem("forbidden")
	}
	return base, nil
}
func (s *Service) CreateRole(ctx context.Context, payload *RoleInput) (*genidentity.RoleRecord, error) {
	p, err := s.roleAuthority(ctx, "roles:create")
	if err != nil {
		return nil, err
	}
	if err = s.validateTenantGrants(payload.Grants); err != nil {
		return nil, err
	}
	if err = s.validateRoleGrantCeiling(ctx, p, payload.Grants); err != nil {
		return nil, err
	}
	record, err := s.repo.createRole(ctx, tenantScope(p), payload)
	return record, safe(err)
}
func (s *Service) UpdateRole(ctx context.Context, payload *RoleInput) (*genidentity.RoleRecord, error) {
	p, err := s.roleAuthority(ctx, "roles:update")
	if err != nil {
		return nil, err
	}
	if err = s.validateTenantGrants(payload.Grants); err != nil {
		return nil, err
	}
	if err = s.validateRoleGrantCeiling(ctx, p, payload.Grants); err != nil {
		return nil, err
	}
	old, err := s.repo.role(ctx, tenantScope(p), payload.ID)
	if err != nil {
		return nil, safe(err)
	}
	if payload.Revision != "" && payload.Revision != roleRevision(old, payload) {
		return nil, problem("conflict")
	}
	if old.Status != payload.Status || !sameJSON(old.Grants, payload.Grants) {
		if err := s.repo.q.IAMRevokeChangedContextRoles(ctx, dbgen.IAMRevokeChangedContextRolesParams{TenantID: p.TenantID, RoleID: payload.ID}); err != nil {
			return nil, safe(err)
		}
	}
	record, err := s.repo.updateRole(ctx, tenantScope(p), payload.ID, payload)
	return record, safe(err)
}
func (s *Service) DeleteRole(ctx context.Context, payload *DeleteInput) error {
	p, err := s.roleAuthority(ctx, "roles:delete")
	if err != nil {
		return err
	}
	return safe(s.repo.deleteRole(ctx, tenantScope(p), payload.ID))
}
