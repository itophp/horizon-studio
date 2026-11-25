package identity

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	genidentity "horizon/foundation/gen/identity"
	"horizon/foundation/kernel/audit"
	"horizon/internal/repository/dbgen"
)

func sessionLoginSubject(principal *Principal) (pgtype.UUID, pgtype.UUID, error) {
	var loginID, subjectID pgtype.UUID
	if principal.TokenHash == "" || loginID.Scan(principal.LoginAccountID) != nil || subjectID.Scan(principal.SubjectID) != nil {
		return loginID, subjectID, problem("unauthenticated")
	}
	return loginID, subjectID, nil
}

const DefaultInteractiveTenant = "workspace-default"

type interactiveTenantKey struct{}

// WithInteractiveTenant binds a deployment-selected tenant to browser sign-in
// and identity selection. Transport sets it from configuration.
func WithInteractiveTenant(ctx context.Context, tenant string) context.Context {
	if tenant == "" {
		tenant = DefaultInteractiveTenant
	}
	return context.WithValue(ctx, interactiveTenantKey{}, tenant)
}

func InteractiveTenant(ctx context.Context) string {
	tenant, _ := ctx.Value(interactiveTenantKey{}).(string)
	if tenant == "" {
		tenant = DefaultInteractiveTenant
	}
	return tenant
}

func (s *Service) ListSignInIdentities(ctx context.Context, _ *genidentity.ListSignInIdentitiesPayload) ([]*genidentity.SignInIdentity, error) {
	principal, err := currentLogin(ctx)
	if err != nil {
		return nil, err
	}
	return s.signInIdentities(ctx, principal)
}

func (s *Service) signInIdentities(ctx context.Context, principal *Principal) ([]*genidentity.SignInIdentity, error) {
	loginID, subjectID, err := sessionLoginSubject(principal)
	if err != nil {
		return nil, err
	}
	tenant := InteractiveTenant(ctx)
	identities := []*genidentity.SignInIdentity{}
	target, err := s.repo.q.IAMGetInteractiveAccount(ctx, dbgen.IAMGetInteractiveAccountParams{
		LoginAccountID: loginID, PrincipalID: subjectID, TenantID: tenant,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return identities, nil
	}
	if err != nil {
		return nil, safe(err)
	}
	offerBase, err := s.repo.q.IAMHasTenantAccountAuthority(ctx, dbgen.IAMHasTenantAccountAuthorityParams{TenantID: tenant, AccountID: target.AccountID})
	if err != nil {
		return nil, safe(err)
	}
	if offerBase {
		identities = append(identities, &genidentity.SignInIdentity{})
	}
	bindings, err := s.repo.q.IAMListIdentityBindings(ctx, subjectID)
	if err != nil {
		return nil, safe(err)
	}
	for _, binding := range bindings {
		if binding.TenantID != tenant {
			continue
		}
		id := binding.ID.String()
		verified, err := s.PrincipalForBoundTask(ctx, tenant, target.AccountID, id)
		if err != nil {
			continue
		}
		option := &genidentity.SignInIdentity{IdentityBindingID: &id, IdentityType: &binding.IdentityType}
		if organization := verified.ActingOrganization; organization.OrganizationID > 0 {
			option.ActingOrganizationName = &organization.Name
		}
		identities = append(identities, option)
	}
	return identities, nil
}

func (s *Service) SelectSignInIdentity(ctx context.Context, payload *genidentity.SelectSignInIdentityPayload) (*genidentity.SessionProfile, error) {
	principal, err := currentLogin(ctx)
	if err != nil {
		return nil, err
	}
	return s.selectSignInIdentity(ctx, principal, payload.IdentityBindingID, payload.ActingOrganizationID, payload.Portal)
}

func (s *Service) selectSignInIdentity(ctx context.Context, current *Principal, selectedBinding *string, organizationID *int64, portal *string) (*genidentity.SessionProfile, error) {
	tenant := InteractiveTenant(ctx)
	if !current.IsUnscopedLogin() && current.TenantID != tenant {
		return nil, problem("forbidden")
	}
	loginID, subjectID, err := sessionLoginSubject(current)
	if err != nil {
		return nil, err
	}
	target, err := s.repo.q.IAMGetInteractiveAccount(ctx, dbgen.IAMGetInteractiveAccountParams{
		LoginAccountID: loginID, PrincipalID: subjectID, TenantID: tenant,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, problem("forbidden")
	}
	if err != nil {
		return nil, safe(err)
	}
	var bindingID pgtype.UUID
	if selectedBinding != nil {
		if err = bindingID.Scan(*selectedBinding); err != nil || !bindingID.Valid {
			return nil, problem("forbidden")
		}
	}
	next, err := s.PrincipalForBoundTask(ctx, tenant, target.AccountID, bindingID.String())
	if err != nil || next.SubjectID != current.SubjectID || next.LoginAccountID != current.LoginAccountID {
		return nil, problem("forbidden")
	}
	if (organizationID != nil && *organizationID != next.ActingOrganization.OrganizationID) ||
		(portal != nil && *portal != next.ActingOrganization.Portal) {
		if err := s.repo.q.SecurityAuditAppend(ctx, dbgen.SecurityAuditAppendParams{
			TenantID: next.TenantID, AccountID: next.Account.ID,
			Binding: next.IdentityBindingID, Action: "identity.context.switch", Outcome: "denied",
			Reason: "context_mismatch", CorrelationID: audit.Correlation(ctx), ResourceType: "tenant", ResourceID: next.TenantID,
		}); err != nil {
			return nil, safe(err)
		}
		return nil, problem("forbidden")
	}
	if current.IsUnscopedLogin() {
		return s.promoteLoginSession(ctx, current, next, target.MembershipID, bindingID)
	}
	timing, err := s.repo.q.GovSessionTiming(ctx, current.TokenHash)
	if err != nil {
		return nil, safe(err)
	}
	if err = s.sessionPolicy(ctx, next, timing.CreatedAt.Time, timing.ExpiresAt.Time, timing.LastSeenAt.Time); err != nil {
		return nil, err
	}
	policy, err := s.policy(ctx, next.TenantID)
	if err != nil {
		return nil, safe(err)
	}
	if err = s.loadTemporary(ctx, next); err != nil {
		return nil, err
	}
	if err = loadAuthorizationVersions(ctx, s.repo.q, next); err != nil {
		return nil, safe(err)
	}
	csrf, err := randomToken()
	if err != nil {
		return nil, safe(err)
	}
	if _, err := s.repo.q.IAMSelectBrowserIdentity(ctx, dbgen.IAMSelectBrowserIdentityParams{
		TokenHash: current.TokenHash, ExpectedCsrfToken: current.CSRF, NewCsrfToken: csrf, MaxMinutes: int32(policy.SessionMinutes),
		TenantID: tenant, MembershipID: target.MembershipID, IdentityBindingID: bindingID,
	}); errors.Is(err, pgx.ErrNoRows) {
		return nil, problem("unauthenticated")
	} else if err != nil {
		return nil, safe(err)
	}
	next.CSRF = csrf
	next.TokenHash = current.TokenHash
	return s.sessionProfile(next), nil
}
