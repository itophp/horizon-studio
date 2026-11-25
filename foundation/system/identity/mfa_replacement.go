package identity

import (
	"context"
	"crypto/rand"
	"errors"
	"net/url"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/capability/registry"
	"horizon/internal/repository/dbgen"
)

func (s *Service) mfaReplacementEntries() []registry.Entry {
	start := mfaEnrollDefinition()
	start.Name, start.Title, start.Executor = "identity.mfa.replace", "Set up a replacement authenticator", "go:identity.mfa.replace"
	start.Description = "Stage a replacement after reauthentication while the current authenticator continues protecting the account."
	start.Input = contracts.Object(map[string]*contracts.Schema{"proof": contracts.Str(64, 64)}, "proof")
	start.SuccessEvents = []string{"identity.mfa.replacement.started"}

	activate := mfaActivateDefinition()
	activate.Name, activate.Title, activate.Executor = "identity.mfa.activateReplacement", "Activate replacement authenticator", "go:identity.mfa.activateReplacement"
	activate.Description = "Verify the staged authenticator and replace the previous authenticator and recovery codes atomically."
	activate.SuccessEvents = []string{"identity.mfa.replacement.activated"}
	return []registry.Entry{
		{Definition: start, Handler: contracts.Handler{ExecuteTx: s.stageTOTPReplacement}},
		{Definition: activate, Handler: contracts.Handler{ExecuteTx: s.activateTOTPReplacement}},
	}
}

func (s *Service) stageTOTPReplacement(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
	q := dbgen.New(tx)
	actor, login, err := s.lockMFAAccount(ctx, q)
	if err != nil {
		return contracts.Output{}, err
	}
	row, err := q.IAMReadTOTP(ctx, login)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !row.ActivatedAt.Valid {
		return contracts.Output{}, problem("forbidden")
	}
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	proof, _ := input["proof"].(string)
	if _, err = q.GovConsumeProof(ctx, dbgen.GovConsumeProofParams{
		TokenHash: tokenHash(proof), TenantID: actor.TenantID, SessionHash: actor.TokenHash,
		ExpectedCsrf: actor.CSRF, Operation: mfaProofPurpose(actor, "replace", row.SealedSecret),
	}); errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, problem("forbidden")
	} else if err != nil {
		return contracts.Output{}, safe(err)
	}
	secret, sealed, err := s.newTOTPSecret(login)
	if err != nil {
		return contracts.Output{}, err
	}
	expires, err := q.IAMStageTOTPReplacement(ctx, dbgen.IAMStageTOTPReplacementParams{
		LoginID: login, Secret: sealed, ContextHash: tokenHash(actor.CSRF),
	})
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	return contracts.Output{
		Data:   map[string]any{"uri": totpURI(actor.Account.Username, secret), "secret": secret, "expiresAt": timeValue(expires)},
		Events: []contracts.Event{{Type: "identity.mfa.replacement.started", AggregateType: "totp_credential", AggregateID: login.String()}},
	}, nil
}

func (s *Service) activateTOTPReplacement(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
	q := dbgen.New(tx)
	actor, login, err := s.lockMFAAccount(ctx, q)
	if err != nil {
		return contracts.Output{}, err
	}
	row, err := q.IAMReadTOTPReplacement(ctx, login)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (!row.ReplacementSecret.Valid || row.ReplacementContextHash.String != tokenHash(actor.CSRF)) {
		return contracts.Output{}, problem("forbidden")
	}
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	code, _ := input["code"].(string)
	counter, err := s.matchTOTP(login, dbgen.IAMReadTOTPRow{Counter: row.Counter, SealedSecret: row.ReplacementSecret.String}, code)
	if err != nil || counter < 0 {
		return contracts.Output{}, problem("forbidden")
	}
	if _, err = q.IAMActivateTOTPReplacement(ctx, dbgen.IAMActivateTOTPReplacementParams{
		LoginID: login, Counter: counter, Secret: row.ReplacementSecret.String, ContextHash: row.ReplacementContextHash.String,
	}); err != nil {
		return contracts.Output{}, safe(err)
	}
	if err = q.IAMDeleteRecoveryCodes(ctx, login); err != nil {
		return contracts.Output{}, safe(err)
	}
	codes, err := issueRecoveryCodes(ctx, q, login)
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	if err = q.IAMRevokeAlternativesAfterMFA(ctx, dbgen.IAMRevokeAlternativesAfterMFAParams{LoginAccountID: login, CurrentToken: actor.TokenHash}); err != nil {
		return contracts.Output{}, safe(err)
	}
	return contracts.Output{
		Data:   map[string]any{"recoveryCodes": codes},
		Events: []contracts.Event{{Type: "identity.mfa.replacement.activated", AggregateType: "totp_credential", AggregateID: login.String()}},
	}, nil
}

func (s *Service) lockMFAAccount(ctx context.Context, q *dbgen.Queries) (*Principal, pgtype.UUID, error) {
	present, err := Current(ctx)
	if err != nil {
		return nil, pgtype.UUID{}, err
	}
	login, _, err := sessionLoginSubject(present)
	if err != nil {
		return nil, login, err
	}
	if _, err = q.IAMLockActiveLogin(ctx, login); err != nil {
		return nil, login, problem("forbidden")
	}
	actor, err := s.WithReadQueries(q).PrincipalForSessionInvocation(ctx, present, false)
	return actor, login, err
}

func (s *Service) newTOTPSecret(login pgtype.UUID) (string, string, error) {
	if s.mfaSecrets == nil {
		return "", "", problem("unavailable")
	}
	var raw [20]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", "", safe(err)
	}
	secret := mfaEncoding.EncodeToString(raw[:])
	sealed, err := s.mfaSecrets.Seal("iam", login.String(), "totp", secret)
	if err != nil {
		return "", "", problem("unavailable")
	}
	return secret, sealed, nil
}

func totpURI(username, secret string) string {
	uri := url.URL{Scheme: "otpauth", Host: "totp", Path: "/Horizon:" + username,
		RawQuery: (url.Values{"issuer": {"Horizon"}, "secret": {secret}, "algorithm": {"SHA1"}, "digits": {"6"}, "period": {"30"}}).Encode()}
	return uri.String()
}
