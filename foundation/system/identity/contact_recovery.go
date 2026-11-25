package identity

import (
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	genidentity "horizon/foundation/gen/identity"
	"horizon/foundation/kernel/audit"
	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/capability/registry"
	"horizon/internal/repository/dbgen"
)

type ContactDelivery func(ctx context.Context, destination, purpose, code, messageID string) error

func (s *Service) ConfigureContactDelivery(email, sms ContactDelivery) {
	s.contactDelivery = map[string]ContactDelivery{"email": email, "sms": sms}
}

var contactPhone = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)

func normalizeContact(channel, raw string) (string, bool) {
	value := strings.TrimSpace(raw)
	switch channel {
	case "email":
		address, err := mail.ParseAddress(value)
		if err != nil || address.Address != value || address.Name != "" || len(value) > 254 || strings.ContainsAny(value, "\r\n") {
			return "", false
		}
		return value, true
	case "sms":
		return value, contactPhone.MatchString(value)
	default:
		return "", false
	}
}

func (s *Service) contactEntries() []registry.Entry {
	const entity = "verified_login_contact"
	tags := []string{"identity", "manual-only", contracts.AuthenticatedActionTag}
	origins := []contracts.Origin{contracts.OriginHumanUI}
	channel := &contracts.Schema{Type: "string", Enum: []string{"email", "sms"}}
	item := contracts.Object(map[string]*contracts.Schema{
		"channel": channel, "destination": {Type: "string"}, "verifiedAt": {Type: "string"},
	}, "channel", "destination", "verifiedAt")
	list := readDefinition("identity.contact.list", "List verified contacts", "Read the current login account's verified recovery contacts.",
		entity, "", contracts.Object(map[string]*contracts.Schema{"items": contracts.Array(item, 0, 2)}, "items"))
	list.TransientResult, list.Tags, list.AllowedOrigins = true, tags, origins
	start := identityWrite("identity.contact.start", "Send contact verification", "Queue a one-time contact verification challenge after reauthentication.",
		"iam_contact_challenge", "", []string{"identity.contact.challenge.queued"}, contracts.Object(map[string]*contracts.Schema{
			"proof": contracts.Str(64, 64), "channel": channel, "destination": contracts.Str(3, 254),
		}, "proof", "channel", "destination"), "channel")
	start.Output = contracts.Object(map[string]*contracts.Schema{"queued": {Type: "boolean"}}, "queued")
	verify := identityWrite("identity.contact.verify", "Verify recovery contact", "Consume the verification challenge and update the current login account's recovery contact.",
		entity, "", []string{"identity.contact.verified"}, contracts.Object(map[string]*contracts.Schema{"code": contracts.Str(64, 64)}, "code"))
	verify.Entities = []string{entity, "iam_contact_challenge"}
	verify.Output = contracts.Object(map[string]*contracts.Schema{"channel": channel}, "channel")
	for _, definition := range []*contracts.Definition{&start, &verify} {
		definition.TransientResult, definition.Tags, definition.AllowedOrigins = true, tags, origins
	}
	return []registry.Entry{
		{Definition: list, Handler: contracts.Handler{Execute: s.verifiedContacts}},
		{Definition: start, Handler: contracts.Handler{ExecuteTx: s.startContactVerification}},
		{Definition: verify, Handler: contracts.Handler{ExecuteTx: s.verifyContact}},
	}
}

func (s *Service) verifiedContacts(ctx context.Context, _ contracts.Input) (contracts.Output, error) {
	actor, err := Current(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	login, _, err := sessionLoginSubject(actor)
	if err != nil {
		return contracts.Output{}, err
	}
	rows, err := s.repo.q.IAMListVerifiedContacts(ctx, login)
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	items := make([]map[string]any, 0, len(rows))
	for _, item := range rows {
		items = append(items, map[string]any{"channel": item.Channel, "destination": item.Destination, "verifiedAt": timeValue(item.VerifiedAt)})
	}
	return contracts.Output{Data: map[string]any{"items": items}}, nil
}

func (s *Service) startContactVerification(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
	var request struct{ Proof, Channel, Destination string }
	if err := contracts.Bind(input, &request); err != nil {
		return contracts.Output{}, problem("invalid")
	}
	destination, valid := normalizeContact(request.Channel, request.Destination)
	if !valid {
		return contracts.Output{}, problem("invalid")
	}
	if s.contactDelivery[request.Channel] == nil || s.mfaSecrets == nil {
		return contracts.Output{}, problem("unavailable")
	}
	current, err := Current(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	login, _, err := sessionLoginSubject(current)
	if err != nil {
		return contracts.Output{}, err
	}
	q := dbgen.New(tx)
	if _, err = q.IAMLockActiveLogin(ctx, login); err != nil {
		return contracts.Output{}, problem("forbidden")
	}
	actor, err := s.WithReadQueries(q).PrincipalForSessionInvocation(ctx, current, false)
	if err != nil {
		return contracts.Output{}, err
	}
	row, err := q.IAMReadTOTP(ctx, login)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, safe(err)
	}
	if _, err = q.GovConsumeProof(ctx, dbgen.GovConsumeProofParams{
		TokenHash: tokenHash(request.Proof), TenantID: actor.TenantID, SessionHash: actor.TokenHash,
		ExpectedCsrf: actor.CSRF, Operation: mfaProofPurpose(actor, "verifyContact", activeMFASecret(row)),
	}); errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, problem("forbidden")
	} else if err != nil {
		return contracts.Output{}, safe(err)
	}
	id, digest, sealed, err := s.newContactChallenge()
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	rows, err := q.IAMIssueContactChallenge(ctx, dbgen.IAMIssueContactChallengeParams{
		ID: id, Channel: request.Channel, Destination: destination, LoginAccountID: login, TokenHash: digest,
		SessionHash: actor.TokenHash, ContextHash: tokenHash(actor.CSRF), SealedToken: sealed,
	})
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	if rows != 1 {
		return contracts.Output{}, problem("conflict")
	}
	return contracts.Output{Data: map[string]any{"queued": true}, Events: []contracts.Event{{
		Type: "identity.contact.challenge.queued", AggregateType: "iam_contact_challenge", AggregateID: id.String(),
	}}}, nil
}

func (s *Service) verifyContact(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
	var request struct{ Code string }
	if err := contracts.Bind(input, &request); err != nil {
		return contracts.Output{}, problem("invalid")
	}
	present, err := Current(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	loginID, _, err := sessionLoginSubject(present)
	if err != nil {
		return contracts.Output{}, err
	}
	q := dbgen.New(tx)
	if _, err = q.IAMLockActiveLogin(ctx, loginID); err != nil {
		return contracts.Output{}, problem("forbidden")
	}
	actor, err := s.WithReadQueries(q).PrincipalForSessionInvocation(ctx, present, false)
	if err != nil {
		return contracts.Output{}, err
	}
	channel, err := q.IAMVerifyLoginContact(ctx, dbgen.IAMVerifyLoginContactParams{
		TokenHash: tokenHash(request.Code), LoginAccountID: loginID, SessionHash: actor.TokenHash, ContextHash: tokenHash(actor.CSRF),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, problem("forbidden")
	}
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	return contracts.Output{Data: map[string]any{"channel": channel}, Events: []contracts.Event{{
		Type: "identity.contact.verified", AggregateType: "verified_login_contact", AggregateID: loginID.String(),
		Payload: map[string]any{"channel": channel},
	}}}, nil
}

func (s *Service) newContactChallenge() (pgtype.UUID, string, string, error) {
	id := pgtype.UUID{Valid: true}
	if _, err := rand.Read(id.Bytes[:]); err != nil {
		return id, "", "", err
	}
	code, err := randomToken()
	if err != nil {
		return id, "", "", err
	}
	sealed, err := s.mfaSecrets.Seal("iam", id.String(), "contact-challenge", code)
	return id, tokenHash(code), sealed, err
}

func (s *Service) BeginApplicationPasswordRecovery(ctx context.Context, payload *genidentity.RequestPasswordResetPayload) error {
	if err := s.available(); err != nil {
		return err
	}
	if s.mfaSecrets == nil || s.contactDelivery[payload.Channel] == nil {
		return nil
	}
	id, digest, sealed, err := s.newContactChallenge()
	if err != nil {
		return safe(err)
	}
	username := strings.ToLower(strings.TrimSpace(payload.Username))
	realm, err := applicationAuthentication(ctx)
	if err != nil {
		return err
	}
	tx, err := s.repo.pool.Begin(ctx)
	if err != nil {
		return safe(err)
	}
	defer tx.Rollback(ctx)
	q := dbgen.New(tx)
	if err = q.IAMPasswordRecoveryLock(ctx, dbgen.IAMPasswordRecoveryLockParams{Username: username, Realm: realm}); err != nil {
		return safe(err)
	}
	_, err = q.IAMIssuePasswordReset(ctx, dbgen.IAMIssuePasswordResetParams{
		ID: id, TokenHash: digest, Channel: payload.Channel, Username: username, Realm: realm, SealedToken: sealed,
	})
	if err != nil {
		return safe(err)
	}
	return safe(tx.Commit(ctx))
}

func (s *Service) VerifyApplicationPasswordRecovery(ctx context.Context, payload *genidentity.CompletePasswordResetPayload) error {
	if err := s.available(); err != nil {
		return err
	}
	if payload.MfaCode != nil && payload.RecoveryCode != nil {
		return problem("invalid")
	}
	realm, err := applicationAuthentication(ctx)
	if err != nil {
		return err
	}
	digest := tokenHash(payload.Token)
	lookup := dbgen.IAMFindPasswordResetLoginParams{TokenHash: digest, Realm: realm}
	login, err := s.repo.q.IAMFindPasswordResetLogin(ctx, lookup)
	if errors.Is(err, pgx.ErrNoRows) {
		return problem("forbidden")
	}
	if err != nil {
		return safe(err)
	}
	tenants, err := s.repo.q.IAMActiveLoginTenants(ctx, login)
	if err != nil {
		return safe(err)
	}
	for _, tenant := range tenants {
		policy, err := s.policy(ctx, tenant)
		if err != nil {
			return safe(err)
		}
		if err = validatePasswordPolicy(policy, payload.NewPassword); err != nil {
			return err
		}
	}
	tx, err := s.repo.pool.Begin(ctx)
	if err != nil {
		return safe(err)
	}
	defer tx.Rollback(ctx)
	q := dbgen.New(tx)
	if _, err = q.IAMLockActiveLogin(ctx, login); err != nil {
		return problem("forbidden")
	}
	if _, err = q.IAMFindPasswordResetLogin(ctx, lookup); err != nil {
		return problem("forbidden")
	}
	repo := &Repository{q: q}
	key := tokenHash("password-recovery:" + login.String())
	blocked, err := repo.loginBlocked(ctx, key)
	if err != nil {
		return safe(err)
	}
	if blocked {
		return problem("forbidden")
	}
	valid, err := s.verifySecondFactor(ctx, q, login, payload.MfaCode, payload.RecoveryCode)
	if err != nil {
		return err
	}
	if !valid {
		if err = repo.loginFailure(ctx, key); err != nil {
			return safe(err)
		}
		if err = tx.Commit(ctx); err != nil {
			return safe(err)
		}
		return problem("forbidden")
	}
	hash, err := HashPassword(payload.NewPassword)
	if err != nil {
		return problem("invalid")
	}
	id, err := q.IAMConsumePasswordReset(ctx, dbgen.IAMConsumePasswordResetParams{TokenHash: digest, LoginAccountID: login})
	if err != nil {
		return problem("forbidden")
	}
	if _, err = q.IAMSetRecoveredPassword(ctx, dbgen.IAMSetRecoveredPasswordParams{Hash: hash, LoginID: login}); err != nil {
		return safe(err)
	}
	if err = q.IAMCancelPasswordResets(ctx, login); err != nil {
		return safe(err)
	}
	if err = q.IAMLoginClear(ctx, key); err != nil {
		return safe(err)
	}
	if err = q.SecurityAuditAppend(ctx, dbgen.SecurityAuditAppendParams{
		Action: "identity.password.reset", Outcome: "allowed", CorrelationID: audit.Correlation(ctx),
		ResourceType: "login_account", ResourceID: login.String(), Reason: "verified_contact:" + id.String(),
	}); err != nil {
		return safe(err)
	}
	return safe(tx.Commit(ctx))
}

func (s *Service) RunContactDelivery(ctx context.Context) {
	if s.repo == nil || s.mfaSecrets == nil || len(s.contactDelivery) == 0 {
		return
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		if err := s.repo.q.IAMPrunePendingRegistrations(ctx); err != nil && ctx.Err() == nil {
			slog.Error("IAM registration cleanup failed")
		}
		for range 10 {
			more, err := s.ProcessContactDelivery(ctx)
			if err != nil && ctx.Err() == nil {
				slog.Error("IAM contact delivery failed")
			}
			if !more || ctx.Err() != nil {
				break
			}
		}
		if err := s.repo.q.IAMPruneContactChallenges(ctx); err != nil && ctx.Err() == nil {
			slog.Error("IAM challenge cleanup failed")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) ProcessContactDelivery(ctx context.Context) (bool, error) {
	row, err := s.repo.q.IAMClaimContactDelivery(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	deliver := s.contactDelivery[row.Channel]
	if deliver == nil || row.SealedToken == "" {
		return true, problem("unavailable")
	}
	code, err := s.mfaSecrets.Unseal("iam", row.ID.String(), "contact-challenge", row.SealedToken)
	if err != nil {
		return true, problem("unavailable")
	}
	deadline, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err = deliver(deadline, row.Destination, row.Purpose, code, row.ID.String()); err != nil {
		return true, problem("unavailable")
	}
	return true, s.repo.q.IAMCompleteContactDelivery(ctx, row.ID)
}
