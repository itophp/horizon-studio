package identity

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"horizon/foundation/kernel/capability/contracts"
	"horizon/internal/repository/dbgen"
)

const (
	membershipInvited = "identity.membership.invited"
	membershipJoined  = "identity.membership.joined"
)

var invitationUsername = regexp.MustCompile(`^[A-Za-z0-9_.@-]{3,80}$`)
var loginRealmPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)

func authenticationRealm(value *string) string {
	if value == nil {
		return "local"
	}
	return *value
}

func membershipInvitationDefinition() contracts.Definition {
	definition := identityWrite("identity.membership.invite", "Invite an existing login to a tenant",
		"Issue a one-time invitation to a tenant without assigning roles.",
		"membership_invitation", "accounts:create", []string{membershipInvited},
		contracts.Object(map[string]*contracts.Schema{
			"username": contracts.Str(3, 80),
			"realm":    contracts.Str(1, 63),
		}, "username"))
	definition.TransientResult = true
	definition.AllowedOrigins = []contracts.Origin{contracts.OriginHumanUI}
	definition.Tags = append(definition.Tags, "manual-only")
	definition.Output = contracts.Object(map[string]*contracts.Schema{
		"id": {Type: "string"}, "code": {Type: "string"}, "expiresAt": {Type: "string"},
	}, "id", "code", "expiresAt")
	return definition
}

func membershipAcceptanceDefinition() contracts.Definition {
	definition := identityWrite("identity.membership.accept", "Accept a tenant invitation",
		"Redeem an invitation issued to the current verified login account.",
		"tenant_membership", "", []string{membershipJoined},
		contracts.Object(map[string]*contracts.Schema{
			"code": contracts.Str(64, 64),
		}, "code"))
	definition.AllowedOrigins = []contracts.Origin{contracts.OriginHumanUI}
	definition.Tags = append(definition.Tags, "manual-only", contracts.AuthenticatedActionTag)
	definition.Output = contracts.Object(map[string]*contracts.Schema{
		"tenantId": {Type: "string"},
	}, "tenantId")
	return definition
}

func (s *Service) createMembershipInvitation(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
	var request struct {
		Username string  `json:"username"`
		Realm    *string `json:"realm"`
	}
	if err := contracts.Bind(input, &request); err != nil || !invitationUsername.MatchString(request.Username) ||
		!loginRealmPattern.MatchString(authenticationRealm(request.Realm)) {
		return contracts.Output{}, contracts.Problem("invalid")
	}
	queries := dbgen.New(tx)
	inviter, err := s.workspaceIAMAdmin(ctx, queries)
	if err != nil || inviter.Account.DepartmentID != 0 || !inviter.HasTenantWideGrant("accounts:create") {
		return contracts.Output{}, problem("forbidden")
	}
	code, err := randomToken()
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	invitation, err := queries.IAMCreateMembershipInvitation(ctx, dbgen.IAMCreateMembershipInvitationParams{
		TenantID: inviter.TenantID, InviterAccountID: inviter.Account.ID,
		Username: strings.ToLower(request.Username), TokenHash: tokenHash(code),
		RecipientRealm: authenticationRealm(request.Realm),
	})
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	return contracts.Output{
		Data:   map[string]any{"id": invitation.ID.String(), "code": code, "expiresAt": nowString(invitation.ExpiresAt.Time)},
		Events: []contracts.Event{{Type: membershipInvited, AggregateType: "membership_invitation", AggregateID: invitation.ID.String()}},
	}, nil
}

func (s *Service) acceptMembershipInvitation(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
	var request struct {
		Code string `json:"code"`
	}
	if err := contracts.Bind(input, &request); err != nil {
		return contracts.Output{}, contracts.Problem("invalid")
	}
	present, err := Current(ctx)
	if err != nil {
		return contracts.Output{}, contracts.Problem("unauthenticated")
	}
	loginID, _, err := sessionLoginSubject(present)
	if err != nil {
		return contracts.Output{}, err
	}
	queries := dbgen.New(tx)
	actor, err := s.WithReadQueries(queries).PrincipalForSessionInvocation(ctx, present, false)
	if err != nil || actor.LoginAccountID != loginID.String() {
		return contracts.Output{}, contracts.Problem("unauthenticated")
	}
	invitation, _, err := s.redeemMembershipInvitation(ctx, queries, loginID, actor.TenantID, request.Code)
	if err != nil {
		return contracts.Output{}, err
	}
	return contracts.Output{
		Data:   map[string]any{"tenantId": invitation.TenantID},
		Events: []contracts.Event{{Type: membershipJoined, AggregateType: "membership_invitation", AggregateID: invitation.ID.String()}},
	}, nil
}

func (s *Service) redeemMembershipInvitation(ctx context.Context, queries *dbgen.Queries, loginID pgtype.UUID, tenant, code string) (dbgen.IAMLockMembershipInvitationRow, int64, error) {
	if _, err := queries.IAMLockActiveLogin(ctx, loginID); err != nil {
		return dbgen.IAMLockMembershipInvitationRow{}, 0, problem("unauthenticated")
	}
	invitation, err := queries.IAMLockMembershipInvitation(ctx, dbgen.IAMLockMembershipInvitationParams{
		LoginAccountID: loginID, TokenHash: tokenHash(code),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return invitation, 0, problem("forbidden")
	}
	if err != nil {
		return invitation, 0, safe(err)
	}
	if invitation.TenantID != tenant {
		return invitation, 0, problem("forbidden")
	}
	bound := *s
	bound.repo = &Repository{q: queries}
	inviter, err := bound.PrincipalForTask(ctx, invitation.TenantID, invitation.InviterAccountID)
	if err != nil || !inviter.HasTenantWideGrant("accounts:create") || !inviter.HasTenantWideGrant("access:manage") {
		return invitation, 0, problem("forbidden")
	}
	accountID, err := queries.IAMAcceptMembershipInvitation(ctx, dbgen.IAMAcceptMembershipInvitationParams{
		LoginAccountID: loginID, InvitationID: invitation.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return invitation, 0, problem("forbidden")
	}
	return invitation, accountID, safe(err)
}
