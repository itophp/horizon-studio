package identity

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/capability/registry"
	"horizon/internal/repository/dbgen"
)

func tenantMembershipListDefinition() contracts.Definition {
	definition := readDefinition("identity.membership.list", "List current workspace members",
		"Read current workspace membership states with formal workspace-wide management authority.",
		"tenant_membership", "access:manage", contracts.Object(map[string]*contracts.Schema{
			"items": contracts.Array(contracts.Object(map[string]*contracts.Schema{
				"id": contracts.Int(1, 1<<53-1), "username": {Type: "string"},
				"membershipStatus": {Type: "string"}, "accountStatus": {Type: "string"}, "loginStatus": {Type: "string"}, "principalKind": {Type: "string"},
				"expiresAt": {Type: "string"}, "protected": {Type: "boolean"},
			}, "id", "username", "membershipStatus", "accountStatus", "loginStatus", "principalKind", "expiresAt", "protected"), 0, 50),
			"nextAfter": contracts.Int(0, 1<<53-1),
		}, "items", "nextAfter"))
	definition.Entities = []string{"tenant_membership", "account", "security_principal", "login_account"}
	definition.Input = contracts.Object(map[string]*contracts.Schema{"after": contracts.Int(0, 1<<53-1)})
	definition.AllowedOrigins = []contracts.Origin{contracts.OriginHumanUI}
	definition.Tags = []string{"identity", "read", "manual-only"}
	return definition
}

func (s *Service) tenantMembers(ctx context.Context, input contracts.Input) (contracts.Output, error) {
	var request struct {
		After int64 `json:"after"`
	}
	if err := contracts.Bind(input, &request); err != nil {
		return contracts.Output{}, contracts.Problem("invalid")
	}
	actor, err := s.workspaceIAMAdmin(ctx, s.repo.q)
	if err != nil {
		return contracts.Output{}, err
	}
	rows, err := s.repo.q.IAMListTenantMembers(ctx, dbgen.IAMListTenantMembersParams{TenantID: actor.TenantID, AfterID: request.After})
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	more := len(rows) > 50
	if more {
		rows = rows[:50]
	}
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, map[string]any{
			"id": row.ID, "username": row.Username,
			"membershipStatus": row.MembershipStatus, "accountStatus": row.AccountStatus, "loginStatus": row.LoginStatus, "principalKind": row.PrincipalKind,
			"expiresAt": timeValue(row.ExpiresAt), "protected": row.Protected,
		})
	}
	next := int64(0)
	if more {
		next = rows[len(rows)-1].ID
	}
	return contracts.Output{Data: map[string]any{"items": items, "nextAfter": next}}, nil
}

func (s *Service) membershipManagementEntries() []registry.Entry {
	change := func(operation, description, oldStatus, nextStatus string, replaceExpiry, requireUnexpired bool) registry.Entry {
		fields := map[string]*contracts.Schema{"accountId": contracts.Int(1, 1<<53-1), "reason": contracts.Str(5, 500)}
		if replaceExpiry {
			fields["expiresAt"] = contracts.Str(0, 40)
		}
		name := "identity.membership." + operation
		event := name + ".completed"
		definition := identityWrite(name, description, description, "tenant_membership", "access:manage", []string{event},
			contracts.Object(fields, "accountId", "reason"), "accountId", "reason", "expiresAt")
		if !replaceExpiry {
			definition.Loggable = []string{"accountId", "reason"}
		}
		definition.AllowedOrigins = []contracts.Origin{contracts.OriginHumanUI}
		definition.Tags = append(definition.Tags, "manual-only")
		definition.Output = contracts.Object(map[string]*contracts.Schema{
			"id": {Type: "integer"}, "status": {Type: "string"}, "expiresAt": {Type: "string"},
		}, "id", "status", "expiresAt")
		return registry.Entry{Definition: definition, Handler: contracts.Handler{ExecuteTx: func(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
			var request struct {
				AccountID int64  `json:"accountId"`
				Reason    string `json:"reason"`
				ExpiresAt string `json:"expiresAt"`
			}
			if err := contracts.Bind(input, &request); err != nil || len(strings.TrimSpace(request.Reason)) < 5 {
				return contracts.Output{}, contracts.Problem("invalid")
			}
			var expiry pgtype.Timestamptz
			if request.ExpiresAt != "" {
				parsed, err := time.Parse(time.RFC3339Nano, request.ExpiresAt)
				if err != nil {
					return contracts.Output{}, contracts.Problem("invalid")
				}
				expiry = pgtype.Timestamptz{Time: parsed, Valid: true}
			}
			queries := dbgen.New(tx)
			actor, err := s.workspaceIAMAdmin(ctx, queries)
			if err != nil {
				return contracts.Output{}, err
			}
			row, err := queries.IAMChangeTenantMembership(ctx, dbgen.IAMChangeTenantMembershipParams{
				TenantID: actor.TenantID, AccountID: request.AccountID, ActorID: actor.Account.ID,
				OldStatus: oldStatus, NewStatus: nextStatus, ExpiresAt: expiry, ReplaceExpiry: replaceExpiry, RequireUnexpired: requireUnexpired,
			})
			if errors.Is(err, pgx.ErrNoRows) {
				return contracts.Output{}, contracts.Problem("forbidden")
			}
			if err != nil {
				return contracts.Output{}, safe(err)
			}
			data := map[string]any{"id": request.AccountID, "status": row.Status, "expiresAt": timeValue(row.ExpiresAt)}
			return contracts.Output{Data: data, Events: []contracts.Event{{
				Type: event, AggregateType: "tenant_membership", AggregateID: row.ID.String(),
				Payload: map[string]any{"accountId": request.AccountID, "reason": strings.TrimSpace(request.Reason), "expiresAt": timeValue(row.ExpiresAt)},
			}}}, nil
		}}}
	}
	const revokedInvitation = "identity.membership.invitation.revoked"
	revoke := identityWrite("identity.membership.invitation.revoke", "Revoke membership invitation", "Expire an unaccepted invitation in the current tenant.",
		"membership_invitation", "access:manage", []string{revokedInvitation},
		contracts.Object(map[string]*contracts.Schema{"id": contracts.Str(36, 36)}, "id"), "id")
	revoke.AllowedOrigins = []contracts.Origin{contracts.OriginHumanUI}
	revoke.Tags = append(revoke.Tags, "manual-only")
	revoke.Output = contracts.Object(map[string]*contracts.Schema{"id": contracts.Str(36, 36)}, "id")
	return []registry.Entry{
		change("suspend", "Suspend tenant membership", "active", "suspended", false, false),
		change("restore", "Restore suspended tenant membership", "suspended", "active", true, false),
		change("setExpiry", "Set tenant membership expiry", "active", "active", true, true),
		{Definition: revoke, Handler: contracts.Handler{ExecuteTx: s.revokeMembershipInvitation}},
	}
}

func (s *Service) revokeMembershipInvitation(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
	var request struct {
		ID string `json:"id"`
	}
	var id pgtype.UUID
	if err := contracts.Bind(input, &request); err != nil || id.Scan(request.ID) != nil || !id.Valid {
		return contracts.Output{}, contracts.Problem("invalid")
	}
	queries := dbgen.New(tx)
	actor, err := s.workspaceIAMAdmin(ctx, queries)
	if err != nil {
		return contracts.Output{}, err
	}
	_, err = queries.IAMRevokeMembershipInvitation(ctx, dbgen.IAMRevokeMembershipInvitationParams{TenantID: actor.TenantID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, contracts.Problem("forbidden")
	}
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	return contracts.Output{Data: map[string]any{"id": id.String()}, Events: []contracts.Event{{
		Type: "identity.membership.invitation.revoked", AggregateType: "membership_invitation", AggregateID: id.String(),
	}}}, nil
}
