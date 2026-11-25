package identity

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"horizon/foundation/kernel/audit"
	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/capability/registry"
	"horizon/internal/repository/dbgen"
)

const auditExportPermitEntity = "audit_export_permit"

func (s *Service) auditExportControlEntries() []registry.Entry {
	policyOutput := contracts.Object(map[string]*contracts.Schema{"required": {Type: "boolean"}}, "required")
	read := readDefinition("identity.audit.exportPolicy", "Read the audit export policy", "Read the current workspace approval requirement.",
		"tenant", "", policyOutput)
	configure := identityWrite("identity.audit.configureExport", "Configure audit export approval", "Set the workspace approval requirement for audit exports.",
		"tenant", "access:manage", []string{"identity.audit.exportPolicy.changed"},
		contracts.Object(map[string]*contracts.Schema{
			"required": {Type: "boolean"}, "reason": contracts.Str(5, 500),
			"password": contracts.Str(1, 128), "factor": contracts.Str(6, 32),
		}, "required", "reason", "password"), "required")
	configure.Output = policyOutput
	permit := contracts.Object(map[string]*contracts.Schema{
		"id": contracts.Str(36, 36), "expiresAt": {Type: "string"}, "workspace": {Type: "boolean"}, "limit": {Type: "integer"},
	}, "id", "expiresAt", "workspace", "limit")
	list := readDefinition("identity.audit.exportPermits", "List approved audit exports", "List unused approvals belonging to the current work context.",
		auditExportPermitEntity, "", contracts.Object(map[string]*contracts.Schema{"items": contracts.Array(permit, 0, 50)}, "items"))
	filter := auditFilterSchema()
	request := identityWrite("identity.audit.requestExport", "Request audit export approval", "Request an independent approval for one audit page.",
		auditExportPermitEntity, "", []string{"identity.audit.export.authorized"},
		contracts.Object(map[string]*contracts.Schema{
			"requestId": contracts.Str(36, 36), "filter": filter, "reason": contracts.Str(5, 500),
		}, "requestId", "filter", "reason"), "requestId", "reason", "filter")
	request.Approval = contracts.ApprovalFourEyes
	request.Output = contracts.Object(map[string]*contracts.Schema{"id": {Type: "string"}}, "id")
	for _, definition := range []*contracts.Definition{&read, &configure, &request, &list} {
		definition.AllowedOrigins = []contracts.Origin{contracts.OriginHumanUI}
		definition.Tags = []string{"identity", "manual-only"}
	}
	request.Tags = append(request.Tags, contracts.AuthenticatedActionTag, contracts.TagSeparatePrincipalApproval)
	read.Tags = append(read.Tags, contracts.AuthenticatedActionTag)
	list.Tags = append(list.Tags, contracts.AuthenticatedActionTag)
	return []registry.Entry{
		{Definition: read, Handler: contracts.Handler{Execute: s.readAuditExportPolicy}},
		{Definition: list, Handler: contracts.Handler{Execute: s.listAuditExportPermits}},
		{Definition: configure, Handler: contracts.Handler{ExecuteTx: s.configureAuditExport}},
		{Definition: request, Handler: contracts.Handler{ExecuteTx: s.issueAuditExportPermit}},
	}
}

func (s *Service) listAuditExportPermits(ctx context.Context, _ contracts.Input) (contracts.Output, error) {
	actor, err := Current(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	var member pgtype.UUID
	if member.Scan(actor.MembershipID) != nil || !member.Valid {
		return contracts.Output{}, problem("forbidden")
	}
	rows, err := s.repo.q.IAMListAuditExportPermits(ctx, dbgen.IAMListAuditExportPermitsParams{
		TenantID: actor.TenantID, AccountID: actor.Account.ID,
		MembershipID: member, BindingID: actor.IdentityBindingID,
	})
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, map[string]any{"id": row.RequestID, "expiresAt": timeValue(row.ExpiresAt), "workspace": row.Workspace, "limit": row.MaxRows})
	}
	return contracts.Output{Data: map[string]any{"items": items}}, nil
}

func (s *Service) readAuditExportPolicy(ctx context.Context, _ contracts.Input) (contracts.Output, error) {
	actor, err := Current(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	required, err := s.repo.q.IAMAuditExportApproval(ctx, actor.TenantID)
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	return contracts.Output{Data: map[string]any{"required": required}}, nil
}

func (s *Service) configureAuditExport(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
	var request struct {
		Required         bool
		Reason, Password string
		Factor           *string
	}
	if contracts.Bind(input, &request) != nil || len(strings.TrimSpace(request.Reason)) < 5 {
		return contracts.Output{}, problem("invalid")
	}
	q := dbgen.New(tx)
	if _, err := s.workspaceIAMAdmin(ctx, q); err != nil {
		return contracts.Output{}, err
	}
	actor, _, _, err := s.associationOwner(ctx, q, request.Password, request.Factor)
	if err != nil {
		return contracts.Output{}, err
	}
	if err = q.IAMConfigureAuditExport(ctx, dbgen.IAMConfigureAuditExportParams{ID: actor.TenantID, AuditExportApproval: request.Required}); err != nil {
		return contracts.Output{}, safe(err)
	}
	if err = q.SecurityAuditAppend(ctx, dbgen.SecurityAuditAppendParams{
		TenantID: actor.TenantID, AccountID: actor.Account.ID,
		Action: "identity.audit.configureExport", Outcome: "allowed", Reason: strings.TrimSpace(request.Reason),
		CorrelationID: audit.Correlation(ctx), ResourceType: "tenant", ResourceID: actor.TenantID,
	}); err != nil {
		return contracts.Output{}, safe(err)
	}
	data := map[string]any{"required": request.Required}
	return contracts.Output{Data: data, Events: []contracts.Event{{
		Type: "identity.audit.exportPolicy.changed", AggregateType: "tenant", AggregateID: actor.TenantID, Payload: data,
	}}}, nil
}

func (s *Service) issueAuditExportPermit(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
	var request struct {
		RequestID, Reason string
		Filter            contracts.Input
	}
	var requestID pgtype.UUID
	if contracts.Bind(input, &request) != nil || requestID.Scan(request.RequestID) != nil || !requestID.Valid ||
		len(strings.TrimSpace(request.Reason)) < 5 || (request.Filter["beforeId"] == nil || request.Filter["afterId"] == nil) {
		return contracts.Output{}, problem("invalid")
	}
	actor, err := Current(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	q := dbgen.New(tx)
	if request.Filter["workspace"] == true && (actor.IdentityBindingID != "" || !actor.HasTenantWideGrant("access:manage")) {
		return contracts.Output{}, problem("forbidden")
	}
	var member pgtype.UUID
	if member.Scan(actor.MembershipID) != nil || !member.Valid {
		return contracts.Output{}, problem("forbidden")
	}
	if err = q.IAMPruneAuditExportPermits(ctx, dbgen.IAMPruneAuditExportPermitsParams{TenantID: actor.TenantID, AccountID: actor.Account.ID}); err != nil {
		return contracts.Output{}, safe(err)
	}
	filterJSON, err := json.Marshal(request.Filter)
	if err != nil {
		return contracts.Output{}, problem("invalid")
	}
	if err = q.IAMCreateAuditExportPermit(ctx, dbgen.IAMCreateAuditExportPermitParams{
		TenantID: actor.TenantID, AccountID: actor.Account.ID,
		MembershipID: member, BindingID: actor.IdentityBindingID, FilterDigest: contracts.Digest(request.Filter), RequestID: requestID.String(), Filters: filterJSON,
	}); err != nil {
		return contracts.Output{}, safe(err)
	}
	return contracts.Output{Data: map[string]any{"id": requestID.String()}, Events: []contracts.Event{{
		Type: "identity.audit.export.authorized", AggregateType: auditExportPermitEntity, AggregateID: requestID.String(),
	}}}, nil
}

func (s *Service) exportSecurityAudit(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
	actor, err := Current(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	q := dbgen.New(tx)
	required, err := q.IAMAuditExportApproval(ctx, actor.TenantID)
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	filter := make(contracts.Input, len(input))
	for key, value := range input {
		if key != "requestId" {
			filter[key] = value
		}
	}
	if required || input["requestId"] != nil {
		var member, requestID pgtype.UUID
		if member.Scan(actor.MembershipID) != nil || !member.Valid || requestID.Scan(input["requestId"]) != nil || !requestID.Valid {
			return contracts.Output{}, problem("forbidden")
		}
		digest := ""
		if len(filter) != 0 {
			digest = contracts.Digest(filter)
		}
		stored, err := q.IAMUseAuditExportPermit(ctx, dbgen.IAMUseAuditExportPermitParams{
			TenantID: actor.TenantID, AccountID: actor.Account.ID, MembershipID: member,
			BindingID: actor.IdentityBindingID, RequestID: requestID.String(), FilterDigest: digest,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return contracts.Output{}, problem("forbidden")
		} else if err != nil {
			return contracts.Output{}, safe(err)
		}
		if json.Unmarshal(stored, &filter) != nil || auditFilterSchema().Validate(filter) != nil {
			return contracts.Output{}, problem("unavailable")
		}
	}
	page, err := s.WithReadQueries(q).securityAuditList(ctx, filter)
	if err != nil {
		return contracts.Output{}, err
	}
	scope := "personal"
	if filter["workspace"] == true {
		scope = "workspace"
	}
	requestID, _ := input["requestId"].(string)
	if err = q.SecurityAuditAppend(ctx, dbgen.SecurityAuditAppendParams{
		TenantID: actor.TenantID, AccountID: actor.Account.ID, Binding: actor.IdentityBindingID,
		Action: "identity.audit.download", Outcome: "allowed", Reason: scope, CorrelationID: audit.Correlation(ctx),
		ResourceType: "audit_export_permit", ResourceID: requestID,
	}); err != nil {
		return contracts.Output{}, safe(err)
	}
	return page, nil
}
