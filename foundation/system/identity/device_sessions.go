package identity

import (
	"context"
	"errors"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"horizon/foundation/kernel/capability/contracts"
	"horizon/internal/repository/dbgen"
)

type deviceLabelKey struct{}

func WithLoginDevice(ctx context.Context, userAgent string) context.Context {
	var label strings.Builder
	count := 0
	for _, character := range userAgent {
		if count == 160 {
			break
		}
		if unicode.IsPrint(character) {
			label.WriteRune(character)
			count++
		}
	}
	return context.WithValue(ctx, deviceLabelKey{}, strings.TrimSpace(label.String()))
}

func deviceSessionsDefinition() contracts.Definition {
	item := contracts.Object(map[string]*contracts.Schema{
		"id": {Type: "string"}, "device": {Type: "string"}, "tenantId": {Type: "string"}, "current": {Type: "boolean"},
		"createdAt": {Type: "string"}, "lastSeenAt": {Type: "string"}, "expiresAt": {Type: "string"},
	}, "id", "device", "tenantId", "current", "createdAt", "lastSeenAt", "expiresAt")
	return contracts.Definition{
		Name: "identity.session.list", Version: identityVersion, Title: "List signed-in devices",
		Description: "List the current login account's unexpired browser sessions across workspaces.",
		Domain:      "identity", Entity: "session", Layer: contracts.LayerAtomic,
		Risk: contracts.RiskLow, Approval: contracts.ApprovalAuto, ReadOnly: true, TransientResult: true,
		Tags: []string{"identity", "manual-only", contracts.AuthenticatedActionTag}, AllowedOrigins: []contracts.Origin{contracts.OriginHumanUI},
		Executor: "go:identity.session.list", Input: contracts.Object(nil),
		Output: contracts.Object(map[string]*contracts.Schema{"items": {Type: "array", Items: item}}, "items"),
	}
}

func deviceSessionRevocationDefinition() contracts.Definition {
	definition := identityWrite("identity.session.revoke", "Sign out a device",
		"Reauthenticate and revoke a browser session belonging to the current login account.",
		"session", "", []string{"identity.session.revoked"},
		contracts.Object(map[string]*contracts.Schema{"id": contracts.Str(36, 36), "proof": contracts.Str(64, 64)}, "id", "proof"), "id")
	definition.Tags = append(definition.Tags, "manual-only", contracts.AuthenticatedActionTag)
	definition.AllowedOrigins = []contracts.Origin{contracts.OriginHumanUI}
	definition.Output = contracts.Object(map[string]*contracts.Schema{"id": {Type: "string"}}, "id")
	return definition
}

func deviceSessionReauthenticationDefinition() contracts.Definition {
	return contracts.Definition{
		Name: "identity.session.reauthenticate", Version: identityVersion, Title: "Confirm device sign-out",
		Description: "Verify your password and issue a single-use proof for signing out the selected device.",
		Domain:      "identity", Entity: "session", Layer: contracts.LayerAtomic,
		Risk: contracts.RiskMedium, Approval: contracts.ApprovalAuto, ReadOnly: true, TransientResult: true,
		Tags: []string{"identity", "manual-only", contracts.AuthenticatedActionTag}, AllowedOrigins: []contracts.Origin{contracts.OriginHumanUI},
		Executor: "go:identity.session.reauthenticate", Loggable: []string{"id"},
		Input:  contracts.Object(map[string]*contracts.Schema{"id": contracts.Str(36, 36), "password": contracts.Str(1, 128)}, "id", "password"),
		Output: contracts.Object(map[string]*contracts.Schema{"proof": {Type: "string"}}, "proof"),
	}
}

func deviceSessionOperation(principal *Principal, publicID pgtype.UUID) string {
	return "identity.session.revoke:" + publicID.String() + ":" + tokenHash(principal.CSRF)
}

func (s *Service) reauthenticateDeviceSession(ctx context.Context, input contracts.Input) (contracts.Output, error) {
	principal, err := Current(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	if _, _, err = sessionLoginSubject(principal); err != nil {
		return contracts.Output{}, err
	}
	var publicID pgtype.UUID
	if err = publicID.Scan(input["id"]); err != nil || !publicID.Valid {
		return contracts.Output{}, contracts.Problem("invalid")
	}
	password, _ := input["password"].(string)
	if err = s.VerifyCurrentPassword(ctx, principal, password); err != nil {
		return contracts.Output{}, err
	}
	proof, err := randomToken()
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	if _, err = s.repo.q.GovCreateProof(ctx, dbgen.GovCreateProofParams{
		TokenHash: tokenHash(proof), TenantID: principal.TenantID, SessionHash: principal.TokenHash,
		ExpectedCsrf: principal.CSRF, Operation: deviceSessionOperation(principal, publicID),
	}); err != nil {
		return contracts.Output{}, safe(err)
	}
	return contracts.Output{Data: map[string]any{"proof": proof}}, nil
}

func (s *Service) deviceSessions(ctx context.Context, _ contracts.Input) (contracts.Output, error) {
	principal, err := Current(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	loginID, _, err := sessionLoginSubject(principal)
	if err != nil {
		return contracts.Output{}, err
	}
	rows, err := s.repo.q.IAMListDeviceSessions(ctx, dbgen.IAMListDeviceSessionsParams{LoginAccountID: loginID, CurrentToken: principal.TokenHash})
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	items := make([]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, map[string]any{"id": row.PublicID.String(), "device": row.DeviceLabel, "tenantId": row.TenantID,
			"current": row.CurrentDevice, "createdAt": nowString(row.CreatedAt.Time),
			"lastSeenAt": nowString(row.LastSeenAt.Time), "expiresAt": nowString(row.ExpiresAt.Time)})
	}
	return contracts.Output{Data: map[string]any{"items": items}}, nil
}

func (s *Service) revokeDeviceSession(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
	principal, err := Current(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	loginID, _, err := sessionLoginSubject(principal)
	if err != nil {
		return contracts.Output{}, err
	}
	var publicID pgtype.UUID
	if err = publicID.Scan(input["id"]); err != nil || !publicID.Valid {
		return contracts.Output{}, contracts.Problem("invalid")
	}
	proof, _ := input["proof"].(string)
	queries := dbgen.New(tx)
	if _, err = queries.GovConsumeProof(ctx, dbgen.GovConsumeProofParams{
		TokenHash: tokenHash(proof), TenantID: principal.TenantID, SessionHash: principal.TokenHash,
		ExpectedCsrf: principal.CSRF, Operation: deviceSessionOperation(principal, publicID),
	}); errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, contracts.Problem("forbidden")
	} else if err != nil {
		return contracts.Output{}, safe(err)
	}
	id, err := queries.IAMRevokeDeviceSession(ctx, dbgen.IAMRevokeDeviceSessionParams{
		LoginAccountID: loginID, PublicID: publicID, CurrentToken: principal.TokenHash, CurrentContext: principal.CSRF,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, contracts.Problem("forbidden")
	}
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	return contracts.Output{Data: map[string]any{"id": id.String()}, Events: []contracts.Event{{
		Type: "identity.session.revoked", AggregateType: "session", AggregateID: id.String(),
	}}}, nil
}
