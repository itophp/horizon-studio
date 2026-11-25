package identity

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"horizon/foundation/kernel/capability/contracts"
	"horizon/internal/repository/dbgen"
)

type SecretProtector interface {
	Seal(tenant, scope, key, raw string) (string, error)
	Unseal(tenant, scope, key, raw string) (string, error)
}

func (s *Service) AttachSecretProtector(protector SecretProtector) {
	s.mfaSecrets = protector
}

const mfaActivated = "identity.mfa.activated"

var mfaEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

func mfaStatusDefinition() contracts.Definition {
	return contracts.Definition{
		Name: "identity.mfa.status", Version: identityVersion, Title: "Read authenticator status",
		Description: "Read the current login account's authenticator status and unused recovery-code count without exposing secrets.",
		Domain:      "identity", Entity: "totp_credential", Layer: contracts.LayerAtomic, Executor: "go:identity.mfa.status",
		Risk: contracts.RiskLow, Approval: contracts.ApprovalAuto, ReadOnly: true, TransientResult: true,
		Tags: []string{"identity", "manual-only", contracts.AuthenticatedActionTag}, AllowedOrigins: []contracts.Origin{contracts.OriginHumanUI},
		Input: contracts.Object(nil), Output: contracts.Object(map[string]*contracts.Schema{
			"enabled": {Type: "boolean"}, "pending": {Type: "boolean"}, "recoveryRemaining": {Type: "integer"},
		}, "enabled", "pending", "recoveryRemaining"),
	}
}

func (s *Service) mfaStatus(ctx context.Context, _ contracts.Input) (contracts.Output, error) {
	actor, err := Current(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	loginID, _, err := sessionLoginSubject(actor)
	if err != nil {
		return contracts.Output{}, err
	}
	status, err := s.repo.q.IAMMFAStatus(ctx, loginID)
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	return contracts.Output{Data: map[string]any{"enabled": status.Enabled, "pending": status.Pending, "recoveryRemaining": status.RecoveryRemaining}}, nil
}

func mfaEnrollDefinition() contracts.Definition {
	return contracts.Definition{
		Name: "identity.mfa.enroll", Version: identityVersion, Title: "Set up an authenticator",
		Description: "Verify the current password and create a ten-minute authenticator enrollment.",
		Domain:      "identity", Entity: "totp_credential", Layer: contracts.LayerAtomic,
		Risk: contracts.RiskMedium, Approval: contracts.ApprovalAuto, TransientResult: true,
		Idempotency: contracts.Idempotency{Required: true, Scope: "tenant+operation+key"},
		Tags:        []string{"identity", "manual-only", contracts.AuthenticatedActionTag}, AllowedOrigins: []contracts.Origin{contracts.OriginHumanUI},
		Executor: "go:identity.mfa.enroll", SuccessEvents: []string{"identity.mfa.enrollment.started"},
		Input:  contracts.Object(map[string]*contracts.Schema{"password": contracts.Str(1, 128)}, "password"),
		Output: contracts.Object(map[string]*contracts.Schema{"uri": {Type: "string"}, "secret": {Type: "string"}, "expiresAt": {Type: "string"}}, "uri", "secret", "expiresAt"),
	}
}

func mfaActivateDefinition() contracts.Definition {
	definition := identityWrite("identity.mfa.activate", "Activate authenticator", "Confirm an enrollment with a valid code and issue single-use recovery codes.",
		"totp_credential", "", []string{mfaActivated},
		contracts.Object(map[string]*contracts.Schema{"code": contracts.Str(6, 6)}, "code"))
	definition.Entities = []string{"totp_credential", "mfa_recovery_code"}
	definition.TransientResult = true
	definition.AllowedOrigins = []contracts.Origin{contracts.OriginHumanUI}
	definition.Tags = append(definition.Tags, "manual-only", contracts.AuthenticatedActionTag)
	definition.Output = contracts.Object(map[string]*contracts.Schema{"recoveryCodes": {Type: "array", Items: &contracts.Schema{Type: "string"}}}, "recoveryCodes")
	return definition
}

func (s *Service) enrollTOTP(ctx context.Context, input contracts.Input) (contracts.Output, error) {
	principal, err := Current(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	loginID, _, err := sessionLoginSubject(principal)
	if err != nil || s.mfaSecrets == nil {
		return contracts.Output{}, problem("unavailable")
	}
	password, _ := input["password"].(string)
	if err = s.VerifyCurrentPassword(ctx, principal, password); err != nil {
		return contracts.Output{}, err
	}
	secret, sealed, err := s.newTOTPSecret(loginID)
	if err != nil {
		return contracts.Output{}, err
	}
	expires, err := s.repo.q.IAMStartTOTPEnrollment(ctx, dbgen.IAMStartTOTPEnrollmentParams{
		LoginAccountID: loginID, SealedSecret: sealed, EnrollmentContextHash: tokenHash(principal.CSRF),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, problem("conflict")
	}
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	return contracts.Output{Data: map[string]any{"uri": totpURI(principal.Account.Username, secret), "secret": secret, "expiresAt": timeValue(expires)}, Events: []contracts.Event{{
		Type: "identity.mfa.enrollment.started", AggregateType: "totp_credential", AggregateID: loginID.String(),
	}}}, nil
}

func (s *Service) activateTOTP(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
	principal, err := Current(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	loginID, _, err := sessionLoginSubject(principal)
	if err != nil {
		return contracts.Output{}, err
	}
	queries := dbgen.New(tx)
	if _, err := queries.IAMLockActiveLogin(ctx, loginID); err != nil {
		return contracts.Output{}, problem("forbidden")
	}
	row, err := queries.IAMReadTOTP(ctx, loginID)
	if err != nil || !row.PendingValid || row.ActivatedAt.Valid || row.EnrollmentContextHash != tokenHash(principal.CSRF) {
		return contracts.Output{}, problem("forbidden")
	}
	code, _ := input["code"].(string)
	counter, err := s.matchTOTP(loginID, row, code)
	if err != nil || counter < 0 {
		return contracts.Output{}, problem("forbidden")
	}
	if _, err = queries.IAMActivateTOTP(ctx, dbgen.IAMActivateTOTPParams{
		LoginAccountID: loginID, Counter: counter, SealedSecret: row.SealedSecret, EnrollmentContextHash: row.EnrollmentContextHash,
	}); errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, problem("forbidden")
	} else if err != nil {
		return contracts.Output{}, safe(err)
	}
	codes, err := issueRecoveryCodes(ctx, queries, loginID)
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	if err = queries.IAMRevokeAlternativesAfterMFA(ctx, dbgen.IAMRevokeAlternativesAfterMFAParams{LoginAccountID: loginID, CurrentToken: principal.TokenHash}); err != nil {
		return contracts.Output{}, safe(err)
	}
	return contracts.Output{Data: map[string]any{"recoveryCodes": codes}, Events: []contracts.Event{{
		Type: mfaActivated, AggregateType: "totp_credential", AggregateID: loginID.String(),
	}}}, nil
}

func (s *Service) verifySecondFactor(ctx context.Context, queries *dbgen.Queries, loginID pgtype.UUID, mfaCode, recoveryCode *string) (bool, error) {
	row, err := queries.IAMReadTOTP(ctx, loginID)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !row.ActivatedAt.Valid {
		return true, nil
	}
	if err != nil {
		return false, safe(err)
	}
	switch {
	case mfaCode != nil && recoveryCode == nil:
		counter, matchErr := s.matchTOTP(loginID, row, *mfaCode)
		if matchErr != nil || counter < 0 {
			return false, matchErr
		}
		_, err = queries.IAMUseTOTP(ctx, dbgen.IAMUseTOTPParams{LoginAccountID: loginID, Counter: counter})
	case recoveryCode != nil && mfaCode == nil:
		_, err = queries.IAMUseRecoveryCode(ctx, dbgen.IAMUseRecoveryCodeParams{LoginAccountID: loginID, CodeHash: tokenHash(*recoveryCode)})
	default:
		return false, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, safe(err)
}

func (s *Service) matchTOTP(loginID pgtype.UUID, row dbgen.IAMReadTOTPRow, code string) (int64, error) {
	if s.mfaSecrets == nil {
		return -1, problem("unavailable")
	}
	encoded, err := s.mfaSecrets.Unseal("iam", loginID.String(), "totp", row.SealedSecret)
	if err != nil {
		return -1, problem("unavailable")
	}
	secret, err := mfaEncoding.DecodeString(encoded)
	if err != nil || len(secret) != 20 {
		return -1, problem("unavailable")
	}
	for _, offset := range []int64{0, -1, 1} {
		counter := row.Counter + offset
		if subtle.ConstantTimeCompare([]byte(totpAt(secret, counter)), []byte(code)) == 1 {
			return counter, nil
		}
	}
	return -1, nil
}

func totpAt(secret []byte, counter int64) string {
	var message [8]byte
	binary.BigEndian.PutUint64(message[:], uint64(counter))
	mac := hmac.New(sha1.New, secret)
	_, _ = mac.Write(message[:])
	sum := mac.Sum(nil)
	offset := int(sum[len(sum)-1] & 15)
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", value%1000000)
}
