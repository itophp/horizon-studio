package identity

import (
	"context"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"horizon/foundation/kernel/audit"
	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/capability/registry"
	"horizon/internal/repository/dbgen"
)

func appendLoginDenial(ctx context.Context, queries *dbgen.Queries, loginID pgtype.UUID, reason string) error {
	return queries.SecurityAuditAppend(ctx, dbgen.SecurityAuditAppendParams{
		Action: "identity.login", Outcome: "denied", ResourceType: "login_account", ResourceID: loginID.String(),
		Reason: reason, CorrelationID: audit.Correlation(ctx),
	})
}

func (s *Service) securityAuditEntries() []registry.Entry {
	item := contracts.Object(map[string]*contracts.Schema{
		"id": {Type: "integer"}, "action": {Type: "string"}, "outcome": {Type: "string"}, "reason": {Type: "string"},
		"accountId": {Type: "integer"}, "correlationId": {Type: "string"},
		"resourceType": {Type: "string"}, "resourceId": {Type: "string"}, "at": {Type: "string"},
		"principalId": {Type: "string"}, "loginAccountId": {Type: "string"}, "bindingId": {Type: "string"},
		"origin": {Type: "string"}, "capabilityVersion": {Type: "string"},
		"machineCredentialId": {Type: "string"}, "delegationId": {Type: "string"},
	}, "id", "action", "outcome", "reason", "accountId", "correlationId", "resourceType", "resourceId", "at")
	page := contracts.Object(map[string]*contracts.Schema{
		"items": {Type: "array", Items: item},
	}, "items")
	list := readDefinition("identity.audit.list", "List security audit events", "Read a scoped page of redacted security events.",
		"security_audit_event", "", page)
	list.Input = auditFilterSchema()
	export := list
	export.Name, export.Title, export.Executor = "identity.audit.export", "Export security audit events", "go:identity.audit.export"
	export.Input = auditFilterSchema()
	export.Input.Properties["requestId"] = contracts.Str(36, 36)
	for _, definition := range []*contracts.Definition{&list, &export} {
		definition.AllowedOrigins = []contracts.Origin{contracts.OriginHumanUI}
		definition.Tags = []string{"identity", "manual-only", contracts.AuthenticatedActionTag}
	}
	return append([]registry.Entry{
		{Definition: list, Handler: contracts.Handler{Execute: s.securityAuditList}},
		{Definition: export, Handler: contracts.Handler{ExecuteTx: s.exportSecurityAudit}},
	}, s.auditExportControlEntries()...)
}

func auditFilterSchema() *contracts.Schema {
	return contracts.Object(map[string]*contracts.Schema{
		"beforeId": contracts.Int(1, 1<<53-1), "afterId": contracts.Int(0, 1<<53-1), "limit": contracts.Int(1, 100), "workspace": {Type: "boolean"},
		"from": contracts.Str(1, 40), "until": contracts.Str(1, 40), "accountId": contracts.Int(1, 1<<53-1),
		"action": contracts.Str(1, 200), "outcome": contracts.Str(1, 64), "resourceType": contracts.Str(1, 200),
		"resourceId": contracts.Str(1, 256), "correlationId": contracts.Str(1, 256),
		"principalId": contracts.Str(36, 36), "loginAccountId": contracts.Str(36, 36), "bindingId": contracts.Str(36, 36),
		"origin": contracts.Str(1, 64), "delegationId": contracts.Str(36, 36),
	})
}

func (s *Service) securityAuditList(ctx context.Context, input contracts.Input) (contracts.Output, error) {
	principal, err := Current(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	if _, _, err = sessionLoginSubject(principal); err != nil {
		return contracts.Output{}, err
	}
	request := struct {
		AfterID        int64  `json:"afterId"`
		BeforeID       int64  `json:"beforeId"`
		Limit          int32  `json:"limit"`
		Workspace      bool   `json:"workspace"`
		From           string `json:"from"`
		Until          string `json:"until"`
		AccountID      *int64 `json:"accountId"`
		Action         string `json:"action"`
		Outcome        string `json:"outcome"`
		ResourceType   string `json:"resourceType"`
		ResourceID     string `json:"resourceId"`
		CorrelationID  string `json:"correlationId"`
		PrincipalID    string `json:"principalId"`
		LoginAccountID string `json:"loginAccountId"`
		BindingID      string `json:"bindingId"`
		Origin         string `json:"origin"`
		DelegationID   string `json:"delegationId"`
	}{BeforeID: math.MaxInt64, Limit: 30}
	if err = contracts.Bind(input, &request); err != nil {
		return contracts.Output{}, contracts.Problem("invalid")
	}
	if request.BeforeID <= request.AfterID {
		return contracts.Output{}, problem("invalid")
	}
	for _, id := range []*string{&request.PrincipalID, &request.LoginAccountID, &request.BindingID, &request.DelegationID} {
		if *id == "" {
			continue
		}
		var parsed pgtype.UUID
		if err := parsed.Scan(*id); err != nil || !parsed.Valid {
			return contracts.Output{}, problem("invalid")
		}
		*id = parsed.String()
	}
	if request.Workspace {
		principal, err = s.workspaceIAMAdmin(ctx, s.repo.q)
		if err != nil {
			return contracts.Output{}, err
		}
	}
	from, err := auditTime(request.From)
	if err != nil {
		return contracts.Output{}, problem("invalid")
	}
	until, err := auditTime(request.Until)
	if err != nil || (from.Valid && until.Valid && !from.Time.Before(until.Time)) {
		return contracts.Output{}, problem("invalid")
	}
	var subject pgtype.Int8
	if request.AccountID != nil {
		subject = pgtype.Int8{Int64: *request.AccountID, Valid: true}
	}
	rows, err := s.repo.q.SecurityAuditList(ctx, dbgen.SecurityAuditListParams{
		TenantID: principal.TenantID, AccountID: principal.Account.ID,
		Binding: principal.IdentityBindingID, AllEvents: request.Workspace, BeforeID: request.BeforeID, AfterID: request.AfterID, MaxRows: request.Limit,
		FromAt: from, UntilAt: until, SubjectAccountID: subject, FilterAction: request.Action,
		FilterOutcome: request.Outcome, FilterResourceID: request.ResourceID, FilterResourceType: request.ResourceType, FilterCorrelationID: request.CorrelationID,
		FilterPrincipalID: request.PrincipalID, FilterLoginAccountID: request.LoginAccountID, FilterBindingID: request.BindingID,
		FilterOrigin: request.Origin, FilterDelegationID: request.DelegationID,
	})
	if err != nil {
		return contracts.Output{}, err
	}
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, map[string]any{
			"id": row.ID, "action": row.Action, "outcome": row.Outcome, "reason": row.Reason,
			"accountId": row.AccountID, "correlationId": row.CorrelationID,
			"resourceType": row.ResourceType, "resourceId": row.ResourceID, "at": nowString(row.CreatedAt.Time),
			"principalId": row.PrincipalID, "loginAccountId": row.LoginAccountID, "bindingId": row.ActualBindingID,
			"origin": row.Origin, "capabilityVersion": row.CapabilityVersion, "machineCredentialId": row.MachineCredentialID,
			"delegationId": row.DelegationID,
		})
	}
	return contracts.Output{Data: map[string]any{"items": items}}, nil
}

func auditTime(value string) (pgtype.Timestamptz, error) {
	if value == "" {
		return pgtype.Timestamptz{}, nil
	}
	timestamp, err := time.Parse(time.RFC3339Nano, value)
	return pgtype.Timestamptz{Time: timestamp, Valid: err == nil}, err
}

func securityAuditEntity() contracts.Entity {
	return contracts.Entity{
		Name: "security_audit_event", Domain: "identity", Table: "iam_security_events", Key: "id",
		Description: "Append-only projection of trusted security outcomes and committed resource events.",
		Attributes: []contracts.Attribute{
			{Name: "id", Kind: contracts.AttributeSystem, Type: contracts.TypeInteger, Required: true},
			{Name: "tenantId", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true},
			{Name: "accountId", Kind: contracts.AttributeSystem, Type: contracts.TypeInteger, Required: true},
			{Name: "bindingId", Kind: contracts.AttributeSystem, Type: contracts.TypeUUID, Nullable: true},
			{Name: "action", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true},
			{Name: "outcome", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true},
			{Name: "reason", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true},
			{Name: "correlationId", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true},
			{Name: "resourceType", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true},
			{Name: "resourceId", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true},
			{Name: "authority", Kind: contracts.AttributeSystem, Type: contracts.TypeJSON, Shape: "object", Nullable: true},
		},
	}
}

func securityCheckpointEntity() contracts.Entity {
	return contracts.Entity{
		Name: "security_audit_checkpoint", Domain: "identity", Table: "iam_security_checkpoints", Key: "lastId",
		Description: "Ed25519-signed digest of a bounded sequence of security events.",
		Attributes: []contracts.Attribute{
			{Name: "lastId", Kind: contracts.AttributeSystem, Type: contracts.TypeInteger, Required: true},
			{Name: "previousId", Kind: contracts.AttributeSystem, Type: contracts.TypeInteger, Required: true},
			{Name: "previousDigest", Kind: contracts.AttributeSystem, Type: contracts.TypeBytes, Required: true},
			{Name: "digest", Kind: contracts.AttributeSystem, Type: contracts.TypeBytes, Required: true},
			{Name: "signature", Kind: contracts.AttributeSystem, Type: contracts.TypeBytes, Required: true},
			{Name: "keyId", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true},
		},
	}
}

func securityRetentionReceiptEntity() contracts.Entity {
	return contracts.Entity{
		Name: "security_audit_retention_receipt", Domain: "identity", Table: "iam_security_retention_receipts", Key: "checkpointId",
		Description: "Signed evidence that a verified audit checkpoint expired under the deployment retention policy.",
		Attributes: []contracts.Attribute{
			{Name: "checkpointId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "cutoff", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Required: true},
			{Name: "signature", Kind: contracts.AttributeSystem, Type: contracts.TypeBytes, Required: true},
			{Name: "keyId", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true},
		},
		Relations: []contracts.Relation{
			{Name: "checkpoint", Target: "security_audit_checkpoint", Kind: "one", Via: "checkpoint_id", Required: true},
		},
	}
}
