package identity

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	genidentity "horizon/foundation/gen/identity"
	"horizon/foundation/kernel/audit"
	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/capability/registry"
	"horizon/internal/repository/dbgen"
)

func (s *Service) registrationPolicyEntry() registry.Entry {
	const name = "identity.tenant.registration.configure"
	def := identityWrite(name, "Configure registration", "Set the current workspace's required registration verification channel.",
		"tenant", "access:manage", []string{name + ".completed"}, contracts.Object(map[string]*contracts.Schema{
			"channel": {Type: "string", Enum: []string{"invite", "email", "sms"}},
		}, "channel"), "channel")
	def.Output = contracts.Object(map[string]*contracts.Schema{"id": {Type: "string"}, "channel": {Type: "string"}}, "id", "channel")
	def.Tags = []string{"identity", "manual-only"}
	def.AllowedOrigins = []contracts.Origin{contracts.OriginHumanUI}
	return registry.Entry{Definition: def, Handler: contracts.Handler{ExecuteTx: s.configureRegistration}}
}

func (s *Service) registrationPolicyReadEntry() registry.Entry {
	def := readDefinition("identity.tenant.registration.read", "Read workspace registration policy",
		"Read the workspace verification channel.", "tenant", "access:manage",
		contracts.Object(map[string]*contracts.Schema{
			"channel": {Type: "string", Enum: []string{"invite", "email", "sms"}},
		}, "channel"))
	def.Tags = []string{"identity", "manual-only"}
	def.AllowedOrigins = []contracts.Origin{contracts.OriginHumanUI}
	return registry.Entry{Definition: def, Handler: contracts.Handler{Execute: s.readRegistrationPolicy}}
}

func (s *Service) readRegistrationPolicy(ctx context.Context, _ contracts.Input) (contracts.Output, error) {
	actor, err := s.workspaceIAMAdmin(ctx, s.repo.q)
	if err != nil {
		return contracts.Output{}, err
	}
	policy, err := s.repo.q.IAMRegistrationPolicy(ctx, actor.TenantID)
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	return contracts.Output{Data: map[string]any{"channel": policy}}, nil
}

func (s *Service) configureRegistration(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
	var request struct{ Channel string }
	if err := contracts.Bind(input, &request); err != nil {
		return contracts.Output{}, problem("invalid")
	}
	if request.Channel != "invite" && (s.contactDelivery[request.Channel] == nil || s.mfaSecrets == nil) {
		return contracts.Output{}, problem("unavailable")
	}
	q := dbgen.New(tx)
	actor, err := s.workspaceIAMAdmin(ctx, q)
	if err != nil {
		return contracts.Output{}, err
	}
	id, err := q.IAMSetRegistrationPolicy(ctx, dbgen.IAMSetRegistrationPolicyParams{TenantID: actor.TenantID,
		Channel: request.Channel})
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	return contracts.Output{Data: map[string]any{"id": id, "channel": request.Channel}, Events: []contracts.Event{{
		Type: "identity.tenant.registration.configure.completed", AggregateType: "tenant", AggregateID: id,
		Payload: map[string]any{"channel": request.Channel},
	}}}, nil
}

func (s *Service) BeginApplicationRegistration(ctx context.Context, payload *genidentity.RequestRegistrationPayload) error {
	if err := s.available(); err != nil {
		return err
	}
	realm, err := applicationAuthentication(ctx)
	if err != nil {
		return err
	}
	tenant := InteractiveTenant(ctx)
	name := strings.TrimSpace(payload.DisplayName)
	if name == "" || !tenantIdentifier.MatchString(tenant) || !invitationUsername.MatchString(payload.Username) {
		return problem("invalid")
	}
	policy, err := s.policy(ctx, tenant)
	if err != nil {
		return safe(err)
	}
	if err = validatePasswordPolicy(policy, payload.Password); err != nil {
		return err
	}
	hash, err := HashPassword(payload.Password)
	if err != nil {
		return problem("invalid")
	}
	tx, err := s.repo.pool.Begin(ctx)
	if err != nil {
		return safe(err)
	}
	defer tx.Rollback(ctx)
	q := dbgen.New(tx)
	config, err := q.IAMRegistrationPolicy(ctx, tenant)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return safe(err)
	}
	if config == "invite" ||
		s.mfaSecrets == nil || s.contactDelivery[config] == nil {
		return nil
	}
	destination, valid := normalizeContact(config, payload.Destination)
	if !valid {
		return problem("invalid")
	}
	login, err := q.IAMCreateRegistrationLogin(ctx, dbgen.IAMCreateRegistrationLoginParams{
		TenantID: tenant, Status: "pending", DisplayName: name, Username: strings.ToLower(payload.Username),
		Realm: realm, PasswordHash: hash,
	})
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return nil
	}
	if err != nil {
		return safe(err)
	}
	id, digest, sealed, err := s.newContactChallenge()
	if err != nil {
		return safe(err)
	}
	if err = q.IAMRegisterContactChallenge(ctx, dbgen.IAMRegisterContactChallengeParams{
		ID: id, Channel: config, Destination: destination, TokenHash: digest, SealedToken: sealed,
		LoginID: login, TenantID: tenant,
	}); err != nil {
		return safe(err)
	}
	return safe(tx.Commit(ctx))
}

func (s *Service) VerifyApplicationRegistration(ctx context.Context, payload *genidentity.CompleteRegistrationPayload) error {
	if err := s.available(); err != nil {
		return err
	}
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
	digest := tokenHash(payload.Token)
	row, err := q.IAMReadPendingRegistration(ctx, dbgen.IAMReadPendingRegistrationParams{TokenHash: digest, Realm: realm})
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (row.TenantID != InteractiveTenant(ctx) || !VerifyPassword(payload.Password, row.PasswordHash)) {
		return problem("forbidden")
	}
	if err != nil {
		return safe(err)
	}
	config, err := q.IAMRegistrationPolicy(ctx, row.TenantID)
	if err != nil || config != row.Channel {
		return problem("forbidden")
	}
	if err = q.IAMConsumeRegistrationChallenge(ctx, digest); err != nil {
		return safe(err)
	}
	if _, err = q.IAMActivateRegistrationPrincipal(ctx, row.PrincipalID); err != nil {
		return safe(err)
	}
	account, err := q.IAMInstallRegistrationPlacement(ctx, dbgen.IAMInstallRegistrationPlacementParams{
		TenantID: row.TenantID, PrincipalID: row.PrincipalID, LoginAccountID: row.LoginAccountID,
		Username: row.Username,
	})
	if err != nil {
		return safe(err)
	}
	if err = s.writeAccountProfile(ctx, q, row.TenantID, account, row.DisplayName, ""); err != nil {
		return safe(err)
	}
	if err = q.IAMRecordRegistrationContact(ctx, dbgen.IAMRecordRegistrationContactParams{
		LoginAccountID: row.LoginAccountID, Channel: row.Channel, Destination: row.Destination,
	}); err != nil {
		return safe(err)
	}
	if err := q.IAMDeletePendingRegistration(ctx, row.LoginAccountID); err != nil {
		return safe(err)
	}
	if err = q.CapAppendOutbox(ctx, dbgen.CapAppendOutboxParams{
		TenantID: row.TenantID, EventID: "iam_registration_" + row.PrincipalID.String(), EventType: "identity.registration.completed",
		AggregateType: "security_principal", AggregateID: row.PrincipalID.String(), Payload: []byte("{}"),
		AccountID: account, CorrelationID: audit.Correlation(ctx),
	}); err != nil {
		return safe(err)
	}
	return safe(tx.Commit(ctx))
}
