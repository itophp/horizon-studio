package identity

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	genidentity "horizon/foundation/gen/identity"
	"horizon/internal/repository/dbgen"
	"strings"
	"unicode/utf8"
)

func validTemporaryMinutes(minutes int) bool {
	for _, value := range []int{30, 60, 120, 240, 480, 1440, 4320, 10080} {
		if minutes == value {
			return true
		}
	}
	return false
}
func (s *Service) canReview(p *Principal) bool {
	return p.CanPermanent("temporary:approve")
}

func currentTemporaryActor(ctx context.Context) (*Principal, error) {
	p, err := Current(ctx)
	if err != nil {
		return nil, err
	}
	if p.IdentityBindingID != "" {
		return nil, problem("forbidden")
	}
	return p, nil
}
func (s *Service) temporaryRecords(ctx context.Context, p *Principal) ([]*genidentity.TemporaryAccessRecord, error) {
	rows, err := s.repo.q.IAMListTemporaryRequests(ctx, dbgen.IAMListTemporaryRequestsParams{TenantID: p.TenantID, AllRequests: s.canReview(p), ApplicantID: p.Account.ID})
	if err != nil {
		return nil, safe(err)
	}
	out := []*genidentity.TemporaryAccessRecord{}
	for _, row := range rows {
		requested, err := decodeGrants(row.RequestedGrants)
		if err != nil {
			return nil, safe(err)
		}
		approved, err := decodeGrants(row.ApprovedGrants)
		if err != nil {
			return nil, safe(err)
		}
		out = append(out, &genidentity.TemporaryAccessRecord{ID: row.ID, ApplicantID: row.ApplicantID, ApplicantName: row.ApplicantName, RequestedGrants: requested, RequestedMinutes: int(row.RequestedMinutes), Reason: row.Reason, Status: row.Status, ApprovedGrants: approved, ApprovedMinutes: int(row.ApprovedMinutes), ReviewerID: row.ReviewerID, ReviewerName: row.ReviewerName, ReviewNote: row.ReviewNote, CreatedAt: timeValue(row.CreatedAt), ReviewedAt: timeValue(row.ReviewedAt), ExpiresAt: timeValue(row.ExpiresAt)})
	}
	return out, nil
}
func (s *Service) temporaryRecord(ctx context.Context, p *Principal, id int64) (*genidentity.TemporaryAccessRecord, error) {
	rows, err := s.temporaryRecords(ctx, p)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.ID == id {
			return row, nil
		}
	}
	return nil, problem("not_found")
}
func (s *Service) TemporaryAccess(ctx context.Context, _ *genidentity.TemporaryAccessPayload) (*genidentity.TemporaryAccessWorkspace, error) {
	p, err := currentTemporaryActor(ctx)
	if err != nil {
		return nil, err
	}
	requests, err := s.temporaryRecords(ctx, p)
	if err != nil {
		return nil, err
	}
	return &genidentity.TemporaryAccessWorkspace{Requests: requests, Catalog: s.permissionCatalog(), EffectiveGrants: p.Grants, CanReview: s.canReview(p)}, nil
}
func (s *Service) validateTemporary(ctx context.Context, p *Principal, grants []*genidentity.PermissionGrant, minutes int) error {
	policy, err := s.policy(ctx, p.TenantID)
	if err != nil {
		return safe(err)
	}
	allowed := false
	for _, v := range policy.AllowedMinutes {
		allowed = allowed || v == minutes
	}
	if !allowed || minutes > policy.MaximumMinutes || !validTemporaryMinutes(minutes) || len(grants) == 0 || len(grants) > 30 {
		return problem("invalid")
	}
	if err := s.validateTenantGrants(grants); err != nil {
		return err
	}
	for _, grant := range grants {
		definition := s.permissionDefinition(grant.Permission)
		if definition == nil || !definition.TemporaryEligible || (!definition.ScopeConfigurable && grant.Scope != "ALL") {
			return problem("invalid")
		}
	}
	return nil
}
func (s *Service) RequestTemporaryAccess(ctx context.Context, payload *genidentity.RequestTemporaryAccessPayload) (*genidentity.TemporaryAccessRecord, error) {
	p, err := currentTemporaryActor(ctx)
	if err != nil {
		return nil, err
	}
	reason := strings.TrimSpace(payload.Reason)
	policy, err := s.policy(ctx, p.TenantID)
	if err != nil {
		return nil, safe(err)
	}
	if utf8.RuneCountInString(reason) < policy.ReasonMin || utf8.RuneCountInString(reason) > 500 {
		return nil, problem("invalid")
	}
	if err = s.validateTemporary(ctx, p, payload.Grants, payload.Minutes); err != nil {
		return nil, err
	}
	raw, err := encodeTenantGrants(payload.Grants)
	if err != nil {
		return nil, safe(err)
	}
	id, err := s.repo.q.IAMCreateTemporaryRequest(ctx, dbgen.IAMCreateTemporaryRequestParams{TenantID: p.TenantID, ApplicantID: p.Account.ID, RequestedGrants: raw, RequestedMinutes: int32(payload.Minutes), Reason: reason})
	if err != nil {
		return nil, safe(err)
	}
	return s.temporaryRecord(ctx, p, id)
}

// Resolve relative ranges once at approval. Later account moves cannot retarget the approved lease.
func (s *Service) resolveTemporary(ctx context.Context, p *Principal, account *genidentity.AccountRecord, grants []*genidentity.PermissionGrant) (map[string]Scope, error) {
	reference := *account
	reference.IsAdmin = false
	out := map[string]Scope{}
	for _, grant := range grants {
		target, err := s.NewPrincipal(&reference, p.TenantID, []*genidentity.PermissionGrant{grant})
		if err != nil {
			return nil, safe(err)
		}
		scope, err := s.scope(ctx, target, grant.Permission)
		if err != nil {
			return nil, err
		}
		out[grant.Permission] = scope
	}
	return out, nil
}
func (s *Service) withinApprovalScope(ctx context.Context, base *Principal, permission string, target Scope) error {
	if !base.Can(permission) || len(target.OrgIDs) != 0 || len(target.DeptIDs) != 0 ||
		base.ActingOrganization.OrganizationID != target.ActingOrganizationID {
		return problem("forbidden")
	}
	definition := s.permissionDefinition(permission)
	if definition != nil && !definition.ScopeConfigurable {
		return nil
	}
	ceiling, err := s.scope(ctx, base, permission)
	if err != nil {
		return err
	}
	if ceiling.All {
		return nil
	}
	if target.All {
		return problem("forbidden")
	}
	if target.OwnID != 0 && ceiling.OwnID != target.OwnID {
		return problem("forbidden")
	}
	return nil
}
func (s *Service) ReviewTemporaryAccess(ctx context.Context, payload *genidentity.ReviewTemporaryAccessPayload) (*genidentity.TemporaryAccessRecord, error) {
	p, err := currentTemporaryActor(ctx)
	if err != nil {
		return nil, err
	}
	if !s.canReview(p) {
		return nil, problem("forbidden")
	}
	row, err := s.repo.q.IAMGetTemporaryRequest(ctx, dbgen.IAMGetTemporaryRequestParams{TenantID: p.TenantID, ID: payload.ID})
	if err != nil {
		return nil, safe(err)
	}
	if row.ApplicantID == p.Account.ID {
		return nil, problem("forbidden")
	}
	if row.Status != "pending" {
		return nil, problem("conflict")
	}
	approved := []*genidentity.PermissionGrant{}
	resolved := map[string]Scope{}
	minutes := 0
	status := "rejected"
	if payload.Decision == "approve" {
		if err = s.validateTemporary(ctx, p, payload.Grants, payload.Minutes); err != nil {
			return nil, err
		}
		account, err := s.repo.authorityAccount(ctx, p.TenantID, row.ApplicantID)
		if err != nil {
			return nil, safe(err)
		}
		if account.Status != "active" {
			return nil, problem("conflict")
		}
		for _, deny := range account.Overrides {
			if deny.Effect == "deny" {
				for _, g := range payload.Grants {
					if deny.Permission == g.Permission {
						return nil, problem("forbidden")
					}
				}
			}
		}
		resolved, err = s.resolveTemporary(ctx, p, account, payload.Grants)
		if err != nil {
			return nil, err
		}
		base, err := p.permanent()
		if err != nil {
			return nil, problem("forbidden")
		}
		for permission, scope := range resolved {
			if err = s.withinApprovalScope(ctx, base, permission, scope); err != nil {
				return nil, err
			}
		}
		approved = payload.Grants
		minutes = payload.Minutes
		status = "approved"
	} else if payload.Decision != "reject" {
		return nil, problem("invalid")
	}
	if len(payload.Note) > 2000 {
		return nil, problem("invalid")
	}
	grantJSON, err := encodeTenantGrants(approved)
	if err != nil {
		return nil, safe(err)
	}
	scopeJSON, err := json.Marshal(resolved)
	if err != nil {
		return nil, safe(err)
	}
	_, err = s.repo.q.IAMReviewTemporaryRequest(ctx, dbgen.IAMReviewTemporaryRequestParams{TenantID: p.TenantID, ID: row.ID, Status: status, ApprovedGrants: grantJSON, ResolvedScopes: scopeJSON, ApprovedMinutes: int32(minutes), ReviewerID: pgtype.Int8{Int64: p.Account.ID, Valid: true}, ReviewNote: payload.Note})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, problem("conflict")
	}
	if err != nil {
		return nil, safe(err)
	}
	return s.temporaryRecord(ctx, p, row.ID)
}
func (s *Service) CancelTemporaryAccess(ctx context.Context, payload *genidentity.CancelTemporaryAccessPayload) (*genidentity.TemporaryAccessRecord, error) {
	p, err := currentTemporaryActor(ctx)
	if err != nil {
		return nil, err
	}
	_, err = s.repo.q.IAMCancelTemporaryRequest(ctx, dbgen.IAMCancelTemporaryRequestParams{TenantID: p.TenantID, ID: payload.ID, ApplicantID: p.Account.ID})
	if err != nil {
		return nil, safe(err)
	}
	return s.temporaryRecord(ctx, p, payload.ID)
}
func (s *Service) RevokeTemporaryAccess(ctx context.Context, payload *genidentity.RevokeTemporaryAccessPayload) (*genidentity.TemporaryAccessRecord, error) {
	p, err := currentTemporaryActor(ctx)
	if err != nil {
		return nil, err
	}
	_, err = s.repo.q.IAMRevokeTemporaryRequest(ctx, dbgen.IAMRevokeTemporaryRequestParams{TenantID: p.TenantID, ID: payload.ID, CanReview: s.canReview(p), ApplicantID: pgtype.Int8{Int64: p.Account.ID, Valid: true}})
	if err != nil {
		return nil, safe(err)
	}
	return s.temporaryRecord(ctx, p, payload.ID)
}
func (s *Service) loadTemporary(ctx context.Context, p *Principal) error {
	if p.IdentityBindingID != "" {
		return nil
	}
	rows, err := s.repo.q.IAMActiveTemporaryGrants(ctx, dbgen.IAMActiveTemporaryGrantsParams{TenantID: p.TenantID, ApplicantID: p.Account.ID})
	if err != nil {
		return safe(err)
	}
	for _, row := range rows {
		grants, err := decodeGrants(row.ApprovedGrants)
		if err != nil {
			return safe(err)
		}
		var scopes map[string]Scope
		if err = json.Unmarshal(row.ResolvedScopes, &scopes); err != nil {
			return safe(err)
		}
		for _, grant := range grants {
			if grant == nil {
				return problem("forbidden")
			}
			definition := s.permissionDefinition(grant.Permission)
			scope, ok := scopes[grant.Permission]
			tenantScope := grant.Scope == "ALL" && scope.All && scope.OwnID == 0
			ownScope := grant.Scope == "OWN" && !scope.All && scope.OwnID == p.Account.ID
			if definition == nil || !definition.TemporaryEligible || grant.Effect != "allow" ||
				!ok || scope.TenantID != p.TenantID || scope.ActingOrganizationID != 0 || len(scope.OrgIDs) != 0 ||
				len(scope.DeptIDs) != 0 || (!tenantScope && !ownScope) {
				return problem("forbidden")
			}
			if _, err = p.enforcer.AddPolicy(p.policySubject, grant.Permission, "allow"); err != nil {
				return safe(err)
			}
			p.Grants = append(p.Grants, grant)
			p.TemporaryScopes[grant.Permission] = append(p.TemporaryScopes[grant.Permission], scope)
		}
	}
	return nil
}
