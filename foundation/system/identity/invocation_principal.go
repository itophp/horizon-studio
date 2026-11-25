package identity

import "context"

// PrincipalForSessionInvocation rebuilds formal and unexpired session-scoped
// temporary authority from storage. Temporary grants remain separate from the
// formal grants used for publishing, approval and persistent delegation.
func (s *Service) PrincipalForSessionInvocation(ctx context.Context, current *Principal, temporary bool) (*Principal, error) {
	session, err := s.repo.q.IAMSession(ctx, current.TokenHash)
	if err != nil || session.TenantID != current.TenantID || session.AccountID != current.Account.ID || current.Account.DepartmentID != 0 ||
		session.IdentityBindingID.String() != current.IdentityBindingID || !CSRFMatches(current, session.CsrfToken) {
		return nil, problem("unauthenticated")
	}
	principal, err := s.PrincipalForBoundTask(ctx, current.TenantID, current.Account.ID, current.IdentityBindingID)
	if err != nil {
		return nil, err
	}
	if principal.SubjectID != current.SubjectID || principal.LoginAccountID != current.LoginAccountID || principal.MembershipID != current.MembershipID || principal.ActingOrganization.OrganizationID != current.ActingOrganization.OrganizationID || principal.ActingOrganization.Portal != current.ActingOrganization.Portal {
		return nil, problem("unauthenticated")
	}
	if temporary {
		if err = s.loadTemporary(ctx, principal); err != nil {
			return nil, err
		}
	}
	principal.CSRF = session.CsrfToken
	principal.TokenHash = current.TokenHash
	return principal, nil
}
