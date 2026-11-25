package identity

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	genidentity "horizon/foundation/gen/identity"
	"horizon/foundation/kernel/audit"
	"horizon/internal/repository/dbgen"
)

// RefreshSession is a browser authentication operation. Only the HTTP
// Set-Cookie response carries the replacement token.
func (s *Service) RefreshSession(ctx context.Context, payload *genidentity.RefreshSessionPayload) (*genidentity.LoginResult, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	tx, err := s.repo.pool.Begin(ctx)
	if err != nil {
		return nil, safe(err)
	}
	defer tx.Rollback(ctx)
	q := dbgen.New(tx)
	actor, login, err := s.lockMFAAccount(ctx, q)
	if err != nil {
		return nil, err
	}
	locked, err := q.IAMLockRefreshSession(ctx, dbgen.IAMLockRefreshSessionParams{
		TokenHash: actor.TokenHash, ExpectedCsrf: actor.CSRF, LoginID: login,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, problem("unauthenticated")
	}
	if err != nil {
		return nil, safe(err)
	}
	bound := s.WithReadQueries(q)
	policy, err := bound.policy(ctx, actor.TenantID)
	if err != nil {
		return nil, safe(err)
	}
	now := time.Now()
	if !now.Before(locked.AuthenticatedAt.Time.Add(time.Duration(policy.SessionMinutes)*time.Minute)) ||
		(policy.IdleMinutes > 0 && !now.Before(locked.LastSeenAt.Time.Add(time.Duration(policy.IdleMinutes)*time.Minute))) {
		return nil, problem("unauthenticated")
	}
	if err = s.VerifyCurrentPassword(ctx, actor, payload.Password); err != nil {
		return nil, err
	}
	if err = s.verifyManagementFactor(ctx, q, login, payload.Factor); err != nil {
		return nil, err
	}
	token, err := randomToken()
	if err != nil {
		return nil, safe(err)
	}
	csrf, err := randomToken()
	if err != nil {
		return nil, safe(err)
	}
	if err = q.SecurityAuditSetCorrelation(ctx, audit.Correlation(ctx)); err != nil {
		return nil, safe(err)
	}
	if _, err = q.IAMRotateBrowserSession(ctx, dbgen.IAMRotateBrowserSessionParams{
		OldToken: actor.TokenHash, ExpectedCsrf: actor.CSRF, NewToken: tokenHash(token), NewCsrf: csrf, Minutes: int32(policy.SessionMinutes),
	}); errors.Is(err, pgx.ErrNoRows) {
		return nil, problem("unauthenticated")
	} else if err != nil {
		return nil, safe(err)
	}
	if err = q.SecurityAuditAppend(ctx, dbgen.SecurityAuditAppendParams{
		TenantID: actor.TenantID, AccountID: actor.Account.ID, Binding: actor.IdentityBindingID,
		Action: "identity.session.refresh", Outcome: "allowed", CorrelationID: audit.Correlation(ctx),
		ResourceType: "session", ResourceID: locked.PublicID.String(),
	}); err != nil {
		return nil, safe(err)
	}
	if err = bound.loadTemporary(ctx, actor); err != nil {
		return nil, err
	}
	if err = loadAuthorizationVersions(ctx, q, actor); err != nil {
		return nil, safe(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, safe(err)
	}
	actor.CSRF, actor.TokenHash = csrf, tokenHash(token)
	return &genidentity.LoginResult{Profile: s.sessionProfile(actor), Cookie: s.policyCookie(token, false, policy.SessionMinutes)}, nil
}
