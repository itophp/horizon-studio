package identity

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/capability/registry"
	"horizon/internal/repository/dbgen"
)

func (s *Service) mfaManagementEntries() []registry.Entry {
	tags := []string{"identity", "manual-only", contracts.AuthenticatedActionTag}
	origins := []contracts.Origin{contracts.OriginHumanUI}
	reauth := readDefinition("identity.mfa.reauthenticate", "Confirm authenticator management",
		"Issue a short-lived proof after verifying the current password and second factor.", "totp_credential", "",
		contracts.Object(map[string]*contracts.Schema{"proof": contracts.Str(64, 64)}, "proof"))
	reauth.Input = contracts.Object(map[string]*contracts.Schema{
		"password": contracts.Str(1, 128), "operation": {Type: "string", Enum: []string{"renewRecoveryCodes", "disable", "verifyContact", "replace"}},
		"code": contracts.Str(6, 6), "recoveryCode": contracts.Str(32, 32),
	}, "password", "operation")
	reauth.TransientResult, reauth.Tags, reauth.AllowedOrigins = true, tags, origins
	entries := []registry.Entry{{Definition: reauth, Handler: contracts.Handler{Execute: s.mfaManagementProof}}}
	for _, operation := range []string{"renewRecoveryCodes", "disable"} {
		name := "identity.mfa." + operation
		definition := identityWrite(name, "Manage authenticator",
			"Apply an authenticator change after verifying the password proof and an active second factor.",
			"totp_credential", "", []string{name + ".completed"}, contracts.Object(map[string]*contracts.Schema{
				"proof": contracts.Str(64, 64),
			}, "proof"))
		definition.Entities = []string{"totp_credential", "mfa_recovery_code"}
		definition.TransientResult, definition.Tags, definition.AllowedOrigins = true, tags, origins
		definition.Output = contracts.Object(map[string]*contracts.Schema{
			"enabled": {Type: "boolean"}, "recoveryCodes": contracts.Array(contracts.Str(32, 32), 0, 10),
		}, "enabled", "recoveryCodes")
		entries = append(entries, registry.Entry{Definition: definition, Handler: contracts.Handler{ExecuteTx: s.manageMFA(operation)}})
	}
	return entries
}

func mfaProofPurpose(p *Principal, operation, sealedSecret string) string {
	return "identity.mfa." + operation + ":" + tokenHash(p.CSRF+":"+sealedSecret)
}

func (s *Service) mfaManagementProof(ctx context.Context, input contracts.Input) (contracts.Output, error) {
	actor, err := Current(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	loginID, _, err := sessionLoginSubject(actor)
	if err != nil {
		return contracts.Output{}, err
	}
	var request struct {
		Password     string  `json:"password"`
		Operation    string  `json:"operation"`
		Code         *string `json:"code"`
		RecoveryCode *string `json:"recoveryCode"`
	}
	if err = contracts.Bind(input, &request); err != nil || request.Code != nil && request.RecoveryCode != nil {
		return contracts.Output{}, problem("invalid")
	}
	if err = s.VerifyCurrentPassword(ctx, actor, request.Password); err != nil {
		return contracts.Output{}, err
	}
	proof, err := randomToken()
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	tx, err := s.repo.pool.Begin(ctx)
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	defer tx.Rollback(ctx)
	q := dbgen.New(tx)
	repo := &Repository{q: q}
	if _, err = q.IAMLockActiveLogin(ctx, loginID); err != nil {
		return contracts.Output{}, problem("forbidden")
	}
	actor, err = s.WithReadQueries(q).PrincipalForSessionInvocation(ctx, actor, false)
	if err != nil {
		return contracts.Output{}, err
	}
	throttleKey := tokenHash("mfa-management:" + loginID.String())
	blocked, err := repo.loginBlocked(ctx, throttleKey)
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	if blocked {
		return contracts.Output{}, problem("forbidden")
	}
	row, err := q.IAMReadTOTP(ctx, loginID)
	if errors.Is(err, pgx.ErrNoRows) && request.Operation != "verifyContact" || err == nil && !row.ActivatedAt.Valid && request.Operation != "verifyContact" {
		return contracts.Output{}, problem("forbidden")
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, safe(err)
	}
	valid, err := s.verifySecondFactor(ctx, q, loginID, request.Code, request.RecoveryCode)
	if err != nil {
		return contracts.Output{}, err
	}
	if !valid {
		if err = repo.loginFailure(ctx, throttleKey); err != nil {
			return contracts.Output{}, safe(err)
		}
		if err = tx.Commit(ctx); err != nil {
			return contracts.Output{}, safe(err)
		}
		return contracts.Output{}, problem("forbidden")
	}
	if err = q.IAMLoginClear(ctx, throttleKey); err != nil {
		return contracts.Output{}, safe(err)
	}
	if _, err = q.GovCreateProof(ctx, dbgen.GovCreateProofParams{
		TokenHash: tokenHash(proof), TenantID: actor.TenantID, SessionHash: actor.TokenHash,
		ExpectedCsrf: actor.CSRF, Operation: mfaProofPurpose(actor, request.Operation, activeMFASecret(row)),
	}); err != nil {
		return contracts.Output{}, safe(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return contracts.Output{}, safe(err)
	}
	return contracts.Output{Data: map[string]any{"proof": proof}}, nil
}

func activeMFASecret(row dbgen.IAMReadTOTPRow) string {
	if row.ActivatedAt.Valid {
		return row.SealedSecret
	}
	return ""
}

func (s *Service) manageMFA(operation string) contracts.TxExecutor {
	return func(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
		var request struct {
			Proof string `json:"proof"`
		}
		if err := contracts.Bind(input, &request); err != nil {
			return contracts.Output{}, problem("invalid")
		}
		actor, err := Current(ctx)
		if err != nil {
			return contracts.Output{}, err
		}
		loginID, _, err := sessionLoginSubject(actor)
		if err != nil {
			return contracts.Output{}, err
		}
		q := dbgen.New(tx)
		if _, err = q.IAMLockActiveLogin(ctx, loginID); err != nil {
			return contracts.Output{}, problem("forbidden")
		}
		actor, err = s.WithReadQueries(q).PrincipalForSessionInvocation(ctx, actor, false)
		if err != nil {
			return contracts.Output{}, err
		}
		row, err := q.IAMReadTOTP(ctx, loginID)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && !row.ActivatedAt.Valid {
			return contracts.Output{}, problem("forbidden")
		}
		if err != nil {
			return contracts.Output{}, safe(err)
		}
		if _, err = q.GovConsumeProof(ctx, dbgen.GovConsumeProofParams{
			TokenHash: tokenHash(request.Proof), TenantID: actor.TenantID, SessionHash: actor.TokenHash,
			ExpectedCsrf: actor.CSRF, Operation: mfaProofPurpose(actor, operation, row.SealedSecret),
		}); errors.Is(err, pgx.ErrNoRows) {
			return contracts.Output{}, problem("forbidden")
		} else if err != nil {
			return contracts.Output{}, safe(err)
		}
		enabled := operation != "disable"
		codes := []string{}
		if enabled {
			if err = q.IAMDeleteRecoveryCodes(ctx, loginID); err != nil {
				return contracts.Output{}, safe(err)
			}
			codes, err = issueRecoveryCodes(ctx, q, loginID)
		} else {
			_, err = q.IAMDeactivateTOTP(ctx, loginID)
		}
		if err != nil {
			return contracts.Output{}, safe(err)
		}
		if err = q.IAMRevokeAlternativesAfterMFA(ctx, dbgen.IAMRevokeAlternativesAfterMFAParams{LoginAccountID: loginID, CurrentToken: actor.TokenHash}); err != nil {
			return contracts.Output{}, safe(err)
		}
		return contracts.Output{Data: map[string]any{"enabled": enabled, "recoveryCodes": codes}, Events: []contracts.Event{{
			Type: "identity.mfa." + operation + ".completed", AggregateType: "totp_credential", AggregateID: loginID.String(),
		}}}, nil
	}
}

func issueRecoveryCodes(ctx context.Context, q *dbgen.Queries, loginID pgtype.UUID) ([]string, error) {
	codes := make([]string, 0, 10)
	for range 10 {
		raw, err := randomToken()
		if err != nil {
			return nil, err
		}
		code := raw[:32]
		if err := q.IAMCreateRecoveryCode(ctx, dbgen.IAMCreateRecoveryCodeParams{LoginAccountID: loginID, CodeHash: tokenHash(code)}); err != nil {
			return nil, err
		}
		codes = append(codes, code)
	}
	return codes, nil
}
