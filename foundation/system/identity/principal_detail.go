package identity

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"horizon/foundation/kernel/capability/contracts"
	"horizon/internal/repository/dbgen"
)

func principalDetailDefinition() contracts.Definition {
	item := contracts.Object(map[string]*contracts.Schema{
		"id": {Type: "string"}, "label": {Type: "string"}, "status": {Type: "string"}, "context": {Type: "string"}, "at": {Type: "string"},
	}, "id", "label", "status", "context", "at")
	properties := map[string]*contracts.Schema{
		"id": contracts.Str(36, 36), "displayName": {Type: "string"}, "kind": {Type: "string"}, "status": {Type: "string"},
		"source": {Type: "string"}, "createdAt": {Type: "string"}, "riskLevel": contracts.Enum("none", "elevated", "high"),
		"securityNotes": {Type: "string"},
	}
	required := []string{"id", "displayName", "kind", "status", "source", "createdAt", "riskLevel", "securityNotes"}
	for _, key := range principalSections {
		properties[key] = contracts.Array(item, 0, 50)
		required = append(required, key)
	}
	definition := readDefinition("identity.principal.detail", "Inspect a security principal",
		"Read principal metadata and scoped accounts, memberships, bindings, grants, sessions and audit records.",
		"security_principal", "access:manage", contracts.Object(properties, required...))
	definition.Input = contracts.Object(map[string]*contracts.Schema{
		"targetTenantId": contracts.Str(1, 256), "principalId": contracts.Str(36, 36), "reason": contracts.Str(5, 500),
	}, "targetTenantId", "principalId", "reason")
	definition.Tags = []string{"identity", "manual-only", PlatformOperatorTag}
	definition.AllowedOrigins = []contracts.Origin{contracts.OriginHumanUI}
	definition.TransientResult = true
	return definition
}

var principalSections = []string{"logins", "memberships", "bindings", "roles", "sessions", "auditEvents"}

func (s *Service) principalDetail(ctx context.Context, input contracts.Input) (contracts.Output, error) {
	var request struct {
		TargetTenantID, PrincipalID, Reason string
	}
	var id pgtype.UUID
	if contracts.Bind(input, &request) != nil || id.Scan(request.PrincipalID) != nil || !id.Valid || len(strings.TrimSpace(request.Reason)) < 5 {
		return contracts.Output{}, problem("invalid")
	}
	tx, err := s.repo.pool.Begin(ctx)
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	defer tx.Rollback(ctx)
	q := dbgen.New(tx)
	actor, err := s.platformActor(ctx, q)
	if err != nil {
		return contracts.Output{}, err
	}
	row, err := q.IAMPrincipalDetail(ctx, dbgen.IAMPrincipalDetailParams{TenantID: request.TargetTenantID, PrincipalID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, problem("not_found")
	}
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	data := map[string]any{
		"id": row.ID.String(), "kind": row.Kind, "status": row.Status, "displayName": row.DisplayName,
		"source": row.CreationSource, "createdAt": timeValue(row.CreatedAt), "riskLevel": row.RiskLevel, "securityNotes": row.SecurityNotes,
	}
	for index, raw := range [][]byte{row.Logins, row.Memberships, row.Bindings, row.Roles, row.Sessions, row.AuditEvents} {
		var list []map[string]any
		if err := json.Unmarshal(raw, &list); err != nil {
			return contracts.Output{}, safe(err)
		}
		data[principalSections[index]] = list
	}
	if err = s.auditPlatformPrincipal(ctx, q, actor, request.TargetTenantID, "detail", row.ID.String(), strings.TrimSpace(request.Reason)); err != nil {
		return contracts.Output{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return contracts.Output{}, safe(err)
	}
	return contracts.Output{Data: data}, nil
}
