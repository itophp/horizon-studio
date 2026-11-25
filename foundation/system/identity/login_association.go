package identity

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/capability/registry"
	"horizon/internal/repository/dbgen"
)

const loginAssociationEntity = "login_association"

func (s *Service) loginAssociationEntries() []registry.Entry {
	id := contracts.Str(36, 36)
	text := &contracts.Schema{Type: "string"}
	item := contracts.Object(map[string]*contracts.Schema{
		"id": id, "username": text, "realm": text, "createdAt": text, "revokedAt": text, "placed": {Type: "boolean"},
	}, "id", "username", "realm", "createdAt", "revokedAt", "placed")
	list := readDefinition("identity.loginAssociation.list", "List associated login accounts",
		"List login accounts associated by the current login.", loginAssociationEntity, "",
		contracts.Object(map[string]*contracts.Schema{"items": contracts.Array(item, 0, 50), "nextAfter": contracts.Str(0, 36)}, "items", "nextAfter"))
	list.Input = contracts.Object(map[string]*contracts.Schema{"after": id})
	link := identityWrite("identity.loginAssociation.link", "Associate a login account",
		"Verify both login accounts before attaching an unused login to the current principal.",
		loginAssociationEntity, "", []string{"identity.loginAssociation.linked"},
		contracts.Object(map[string]*contracts.Schema{
			"username": contracts.Str(3, 80), "realm": contracts.Str(1, 63),
			"password": contracts.Str(1, 128), "factor": contracts.Str(6, 32),
			"secondaryPassword": contracts.Str(1, 128), "secondaryFactor": contracts.Str(6, 32),
		}, "username", "password", "secondaryPassword"))
	unlink := identityWrite("identity.loginAssociation.unlink", "Detach an associated login",
		"Restore the original principal of an associated login with no workspace accounts.",
		loginAssociationEntity, "", []string{"identity.loginAssociation.unlinked"},
		contracts.Object(map[string]*contracts.Schema{"id": id, "password": contracts.Str(1, 128), "factor": contracts.Str(6, 32)}, "id", "password"), "id")
	for _, action := range []*contracts.Definition{&link, &unlink} {
		action.Entities = []string{loginAssociationEntity, "login_account", "login_session"}
		action.Output = contracts.Object(map[string]*contracts.Schema{"id": id}, "id")
		action.TransientResult = true
	}
	for _, action := range []*contracts.Definition{&list, &link, &unlink} {
		action.Tags = []string{"identity", "manual-only", contracts.AuthenticatedActionTag}
		action.AllowedOrigins = []contracts.Origin{contracts.OriginHumanUI}
	}
	return []registry.Entry{
		{Definition: list, Handler: contracts.Handler{Execute: s.listLoginAssociations}},
		{Definition: link, Handler: contracts.Handler{ExecuteTx: s.linkLoginAccount}},
		{Definition: unlink, Handler: contracts.Handler{ExecuteTx: s.unlinkLoginAccount}},
	}
}

func (s *Service) listLoginAssociations(ctx context.Context, input contracts.Input) (contracts.Output, error) {
	actor, err := Current(ctx)
	if err != nil || actor.IdentityBindingID != "" {
		return contracts.Output{}, problem("forbidden")
	}
	login, subject, err := sessionLoginSubject(actor)
	if err != nil {
		return contracts.Output{}, err
	}
	var after pgtype.UUID
	if raw, ok := input["after"].(string); ok && (after.Scan(raw) != nil || !after.Valid) {
		return contracts.Output{}, problem("invalid")
	}
	rows, err := s.repo.q.IAMOwnLoginAssociations(ctx, dbgen.IAMOwnLoginAssociationsParams{LoginID: login, PrincipalID: subject, AfterID: after})
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	next := ""
	if len(rows) > 50 {
		rows = rows[:50]
		next = rows[49].ID.String()
	}
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, map[string]any{"id": row.ID.String(), "username": row.Username, "realm": row.Realm,
			"createdAt": timeValue(row.CreatedAt), "revokedAt": timeValue(row.RevokedAt), "placed": row.Placed})
	}
	return contracts.Output{Data: map[string]any{"items": items, "nextAfter": next}}, nil
}

func (s *Service) verifyAssociationFactor(ctx context.Context, q *dbgen.Queries, login pgtype.UUID, factor *string) (bool, error) {
	if factor != nil && len(*factor) == 32 {
		return s.verifySecondFactor(ctx, q, login, nil, factor)
	}
	return s.verifySecondFactor(ctx, q, login, factor, nil)
}

func (s *Service) associationOwner(ctx context.Context, q *dbgen.Queries, password string, factor *string) (*Principal, pgtype.UUID, pgtype.UUID, error) {
	actor, login, err := s.lockMFAAccount(ctx, q)
	if err != nil || actor.IdentityBindingID != "" {
		return nil, login, pgtype.UUID{}, problem("forbidden")
	}
	_, subject, err := sessionLoginSubject(actor)
	if err != nil {
		return nil, login, subject, err
	}
	if err = s.VerifyCurrentPassword(ctx, actor, password); err != nil {
		return nil, login, subject, err
	}
	if err = s.verifyManagementFactor(ctx, q, login, factor); err != nil {
		return nil, login, subject, err
	}
	return actor, login, subject, nil
}

func (s *Service) verifyManagementFactor(ctx context.Context, q *dbgen.Queries, login pgtype.UUID, factor *string) error {
	throttle := tokenHash("mfa-management:" + login.String())
	blocked, err := s.repo.loginBlocked(ctx, throttle)
	if err != nil || blocked {
		return problem("forbidden")
	}
	valid, err := s.verifyAssociationFactor(ctx, q, login, factor)
	if err != nil {
		return err
	}
	if !valid {
		if err = s.repo.loginFailure(ctx, throttle); err != nil {
			return safe(err)
		}
		return problem("forbidden")
	}
	return nil
}

func (s *Service) linkLoginAccount(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
	var payload struct {
		Username, Password, SecondaryPassword string
		Realm, Factor, SecondaryFactor        *string
	}
	if err := contracts.Bind(input, &payload); err != nil {
		return contracts.Output{}, problem("invalid")
	}
	username, realm := strings.ToLower(strings.TrimSpace(payload.Username)), authenticationRealm(payload.Realm)
	if !invitationUsername.MatchString(username) || !loginRealmPattern.MatchString(realm) {
		return contracts.Output{}, problem("invalid")
	}
	q := dbgen.New(tx)
	actor, login, subject, err := s.associationOwner(ctx, q, payload.Password, payload.Factor)
	if err != nil {
		return contracts.Output{}, err
	}
	if actor.Account.IsAdmin || s.platformOperators[subject.String()] {
		return contracts.Output{}, problem("forbidden")
	}
	// Use the password-login bucket so linking cannot bypass the source account's lockout.
	throttle := tokenHash(realm + ":" + username)
	blocked, err := s.repo.loginBlocked(ctx, throttle)
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	if blocked {
		dummyPasswordWork(payload.SecondaryPassword)
		return contracts.Output{}, problem("forbidden")
	}
	source, err := q.IAMLockUnplacedLoginForAssociation(ctx, dbgen.IAMLockUnplacedLoginForAssociationParams{Username: username, Realm: realm})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, safe(err)
	}
	valid := false
	if err == nil {
		valid = VerifyPassword(payload.SecondaryPassword, source.PasswordHash)
	} else {
		dummyPasswordWork(payload.SecondaryPassword)
	}
	if valid && source.PrincipalID != subject && !s.platformOperators[source.PrincipalID.String()] {
		valid, err = s.verifyAssociationFactor(ctx, q, source.ID, payload.SecondaryFactor)
		if err != nil {
			return contracts.Output{}, err
		}
		if valid {
			return s.attachLoginAccount(ctx, q, login, subject, source)
		}
	}
	if err = s.repo.loginFailure(ctx, throttle); err != nil {
		return contracts.Output{}, safe(err)
	}
	return contracts.Output{}, problem("forbidden")
}

func (s *Service) attachLoginAccount(ctx context.Context, q *dbgen.Queries, login, subject pgtype.UUID, source dbgen.IAMLockUnplacedLoginForAssociationRow) (contracts.Output, error) {
	if _, err := q.IAMAttachUnplacedLogin(ctx, dbgen.IAMAttachUnplacedLoginParams{LoginID: source.ID, PrincipalID: subject, OriginalPrincipalID: source.PrincipalID}); err != nil {
		return contracts.Output{}, safe(err)
	}
	id, err := q.IAMRecordLoginAssociation(ctx, dbgen.IAMRecordLoginAssociationParams{
		PrincipalID: subject, OriginalPrincipalID: source.PrincipalID, PrimaryLoginID: login, AssociatedLoginID: source.ID,
	})
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	if err = q.IAMClearAssociationSessions(ctx, source.ID); err != nil {
		return contracts.Output{}, safe(err)
	}
	return loginAssociationOutput(id, "identity.loginAssociation.linked"), nil
}

func (s *Service) unlinkLoginAccount(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
	var payload struct {
		ID, Password string
		Factor       *string
	}
	var id pgtype.UUID
	if err := contracts.Bind(input, &payload); err != nil || id.Scan(payload.ID) != nil || !id.Valid {
		return contracts.Output{}, problem("invalid")
	}
	q := dbgen.New(tx)
	_, login, subject, err := s.associationOwner(ctx, q, payload.Password, payload.Factor)
	if err != nil {
		return contracts.Output{}, err
	}
	row, err := q.IAMLockOwnLoginAssociation(ctx, dbgen.IAMLockOwnLoginAssociationParams{ID: id, LoginID: login, PrincipalID: subject})
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, problem("forbidden")
	}
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	if _, err = q.IAMLockActiveLogin(ctx, row.AssociatedLoginID); err != nil {
		return contracts.Output{}, problem("forbidden")
	}
	_, err = q.IAMDetachUnplacedLogin(ctx, dbgen.IAMDetachUnplacedLoginParams{PrincipalID: subject, LoginID: row.AssociatedLoginID, OriginalPrincipalID: row.OriginalPrincipalID})
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, problem("conflict")
	}
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	if err = q.IAMMarkLoginAssociationRevoked(ctx, id); err != nil {
		return contracts.Output{}, safe(err)
	}
	if err = q.IAMClearAssociationSessions(ctx, row.AssociatedLoginID); err != nil {
		return contracts.Output{}, safe(err)
	}
	return loginAssociationOutput(id, "identity.loginAssociation.unlinked"), nil
}

func loginAssociationOutput(id pgtype.UUID, event string) contracts.Output {
	return contracts.Output{Data: map[string]any{"id": id.String()}, Events: []contracts.Event{{
		Type: event, AggregateType: loginAssociationEntity, AggregateID: id.String(),
	}}}
}
