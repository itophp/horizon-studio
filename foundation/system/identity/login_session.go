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

func (s *Service) authenticateUnscopedLogin(ctx context.Context, hash string) (*Principal, error) {
	login, err := s.repo.q.IAMGetUnscopedLoginSession(ctx, hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, problem("unauthenticated")
	}
	if err != nil {
		return nil, safe(err)
	}
	return &Principal{TokenHash: hash, CSRF: login.CsrfToken, LoginAccountID: login.LoginAccountID.String(), SubjectID: login.PrincipalID.String()}, nil
}

func (s *Service) loginWithPassword(ctx context.Context, login dbgen.IAMUnscopedPasswordLoginRow, key string, policy SecurityPolicy, payload *genidentity.LoginPayload) (*genidentity.LoginResult, error) {
	token, err := randomToken()
	if err != nil {
		return nil, safe(err)
	}
	csrf, err := randomToken()
	if err != nil {
		return nil, safe(err)
	}
	tx, err := s.repo.pool.Begin(ctx)
	if err != nil {
		return nil, safe(err)
	}
	defer tx.Rollback(ctx)
	q := dbgen.New(tx)
	if _, err = q.IAMLockActiveLogin(ctx, login.ID); errors.Is(err, pgx.ErrNoRows) {
		return nil, problem("unauthenticated")
	} else if err != nil {
		return nil, safe(err)
	}
	valid, err := s.verifySecondFactor(ctx, q, login.ID, payload.MfaCode, payload.RecoveryCode)
	if err != nil {
		return nil, err
	}
	if !valid {
		if err = q.GovLoginFailure(ctx, dbgen.GovLoginFailureParams{AttemptKey: key, MaxFailures: int32(policy.LockAttempts), LockMinutes: int32(policy.LockMinutes)}); err != nil {
			return nil, safe(err)
		}
		if err = appendLoginDenial(ctx, q, login.ID, "second_factor"); err != nil {
			return nil, safe(err)
		}
		if err = tx.Commit(ctx); err != nil {
			return nil, safe(err)
		}
		return nil, problem("unauthenticated")
	}
	device, _ := ctx.Value(deviceLabelKey{}).(string)
	if _, err = q.IAMCreateUnscopedLoginSession(ctx, dbgen.IAMCreateUnscopedLoginSessionParams{
		TokenHash: tokenHash(token), CsrfToken: csrf, DeviceLabel: device,
		LoginAccountID: login.ID, PasswordHash: login.PasswordHash,
	}); errors.Is(err, pgx.ErrNoRows) {
		return nil, problem("unauthenticated")
	} else if err != nil {
		return nil, safe(err)
	}
	if err = q.IAMLoginClear(ctx, key); err != nil {
		return nil, safe(err)
	}
	if err = q.IAMPruneUnscopedLoginSessions(ctx); err != nil {
		return nil, safe(err)
	}
	if err = q.SecurityAuditAppend(ctx, dbgen.SecurityAuditAppendParams{
		Action: "identity.login", Outcome: "allowed", Reason: "credentials_verified",
		CorrelationID: audit.Correlation(ctx), ResourceType: "login_account", ResourceID: login.ID.String(),
	}); err != nil {
		return nil, safe(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, safe(err)
	}
	return s.completeInteractiveLogin(ctx, token, csrf, policy)
}

// Complete a verified login in the configured tenant when IAM can resolve its
// authority. Keep the short-lived login session for invitation redemption or
// selection between multiple organization identities.
func (s *Service) completeInteractiveLogin(ctx context.Context, token, csrf string, policy SecurityPolicy) (*genidentity.LoginResult, error) {
	current, err := s.authenticateUnscopedLogin(ctx, tokenHash(token))
	if err != nil {
		return nil, err
	}
	profile, err := s.selectSignInIdentity(ctx, current, nil, nil, nil)
	if err != nil {
		var denied *genidentity.IAMProblem
		if !errors.As(err, &denied) || denied.Code != "forbidden" {
			return nil, err
		}
		identities, err := s.signInIdentities(ctx, current)
		if err != nil {
			return nil, err
		}
		if len(identities) == 1 && identities[0].IdentityBindingID != nil {
			profile, err = s.selectSignInIdentity(ctx, current, identities[0].IdentityBindingID, nil, nil)
			if err != nil {
				return nil, err
			}
		}
	}
	if profile == nil {
		profile = s.sessionProfile(current)
	} else {
		policy, err = s.policy(ctx, profile.TenantID)
		if err != nil {
			return nil, err
		}
	}
	return &genidentity.LoginResult{Profile: profile, Cookie: s.policyCookie(token, false, policy.SessionMinutes)}, nil
}

func (s *Service) promoteLoginSession(ctx context.Context, current, next *Principal, membershipID, bindingID pgtype.UUID) (*genidentity.SessionProfile, error) {
	csrf, err := randomToken()
	if err != nil {
		return nil, safe(err)
	}
	policy, err := s.policy(ctx, next.TenantID)
	if err != nil {
		return nil, safe(err)
	}
	if err = loadAuthorizationVersions(ctx, s.repo.q, next); err != nil {
		return nil, safe(err)
	}
	tx, err := s.repo.pool.Begin(ctx)
	if err != nil {
		return nil, safe(err)
	}
	defer tx.Rollback(ctx)
	q := dbgen.New(tx)
	if err = q.SecurityAuditSetCorrelation(ctx, audit.Correlation(ctx)); err != nil {
		return nil, safe(err)
	}
	if _, err = q.IAMPromoteLoginSession(ctx, dbgen.IAMPromoteLoginSessionParams{
		TokenHash: current.TokenHash, ExpectedCsrf: current.CSRF, NewCsrf: csrf,
		SessionMinutes: int32(policy.SessionMinutes), BindingID: bindingID, MembershipID: membershipID,
		TenantID: next.TenantID,
	}); errors.Is(err, pgx.ErrNoRows) {
		return nil, problem("unauthenticated")
	} else if err != nil {
		return nil, safe(err)
	}
	if err = q.IAMTouchLogin(ctx, dbgen.IAMTouchLoginParams{TenantID: next.TenantID, ID: next.Account.ID}); err != nil {
		return nil, safe(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, safe(err)
	}
	next.CSRF, next.TokenHash = csrf, current.TokenHash
	return s.sessionProfile(next), nil
}

func (s *Service) RedeemSignInInvitation(ctx context.Context, payload *genidentity.RedeemSignInInvitationPayload) error {
	present, err := currentLogin(ctx)
	if err != nil || !present.IsUnscopedLogin() {
		return problem("unauthenticated")
	}
	login, _, err := sessionLoginSubject(present)
	if err != nil {
		return err
	}
	tx, err := s.repo.pool.Begin(ctx)
	if err != nil {
		return safe(err)
	}
	defer tx.Rollback(ctx)
	q := dbgen.New(tx)
	if _, err = q.IAMLockActiveLogin(ctx, login); err != nil {
		return problem("unauthenticated")
	}
	active, err := q.IAMLockUnscopedLoginSession(ctx, dbgen.IAMLockUnscopedLoginSessionParams{TokenHash: present.TokenHash, CsrfToken: present.CSRF})
	if errors.Is(err, pgx.ErrNoRows) || err == nil && active != login {
		return problem("unauthenticated")
	}
	if err != nil {
		return safe(err)
	}
	invitation, accountID, err := s.redeemMembershipInvitation(ctx, q, login, InteractiveTenant(ctx), payload.Code)
	if err != nil {
		return err
	}
	if err = q.CapAppendOutbox(ctx, dbgen.CapAppendOutboxParams{
		TenantID: invitation.TenantID, EventID: "iam_join_" + invitation.ID.String(), EventType: membershipJoined,
		AggregateType: "membership_invitation", AggregateID: invitation.ID.String(), Payload: []byte("{}"),
		AccountID: accountID, CorrelationID: audit.Correlation(ctx),
	}); err != nil {
		return safe(err)
	}
	return safe(tx.Commit(ctx))
}
