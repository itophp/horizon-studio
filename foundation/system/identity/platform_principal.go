package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"horizon/foundation/kernel/audit"
	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/capability/registry"
	"horizon/internal/repository/dbgen"
)

const PlatformOperatorTag = "platform-operator"

func ParsePlatformOperators(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	seen := map[string]bool{}
	var operators []string
	for _, value := range strings.Split(raw, ",") {
		var id pgtype.UUID
		if err := id.Scan(strings.TrimSpace(value)); err != nil || !id.Valid || id.Bytes == [16]byte{} || seen[id.String()] {
			return nil, errors.New("IAM_PLATFORM_OPERATOR_IDS must contain distinct principal UUIDs")
		}
		seen[id.String()] = true
		operators = append(operators, id.String())
	}
	return operators, nil
}

func (s *Service) ConfigurePlatformOperators(ids []string) {
	s.platformOperators = make(map[string]bool, len(ids))
	for _, id := range ids {
		s.platformOperators[id] = true
	}
}

func (s *Service) IsPlatformOperator(p *Principal) bool {
	return p != nil && p.TokenHash != "" && p.IdentityBindingID == "" && s.platformOperators[p.SubjectID]
}

type principalOperation struct {
	TenantID      string `json:"targetTenantId"`
	Target        string `json:"target"`
	Reason        string `json:"reason"`
	RiskLevel     string `json:"riskLevel"`
	SecurityNotes string `json:"securityNotes"`
}

func (o principalOperation) proofPurpose(actor *Principal, operation string) string {
	input := contracts.Input{
		"tenant": o.TenantID, "target": o.Target, "reason": strings.TrimSpace(o.Reason), "context": actor.CSRF,
	}
	if operation == "setSecurity" {
		input["riskLevel"], input["securityNotes"] = o.RiskLevel, o.SecurityNotes
	}
	return "identity.principal." + operation + ":" + contracts.Digest(input)
}

func (s *Service) platformActor(ctx context.Context, q *dbgen.Queries) (*Principal, error) {
	actor, err := s.workspaceIAMAdmin(ctx, q)
	if err != nil || !s.IsPlatformOperator(actor) {
		return nil, problem("forbidden")
	}
	return actor, nil
}

func (s *Service) principalEntries() []registry.Entry {
	const entity = "security_principal"
	tags := []string{"identity", "manual-only", PlatformOperatorTag}
	item := contracts.Object(map[string]*contracts.Schema{
		"id": contracts.Str(36, 36), "kind": {Type: "string"}, "status": {Type: "string"},
		"displayName": {Type: "string"}, "source": {Type: "string"}, "createdAt": {Type: "string"},
		"riskLevel": contracts.Enum("none", "elevated", "high"), "workspaceCount": {Type: "integer"}, "loginCount": {Type: "integer"},
	}, "id", "kind", "status", "displayName", "source", "createdAt", "riskLevel", "workspaceCount", "loginCount")
	list := readDefinition("identity.principal.list", "List security principals",
		"Read principal identities for a specified tenant using platform operator authority.", entity, "access:manage",
		contracts.Object(map[string]*contracts.Schema{"items": contracts.Array(item, 0, 50), "nextAfter": contracts.Str(0, 36)}, "items", "nextAfter"))
	list.Input = contracts.Object(map[string]*contracts.Schema{
		"targetTenantId": contracts.Str(1, 256), "after": contracts.Str(36, 36), "principalId": contracts.Str(36, 36),
	}, "targetTenantId")
	list.TransientResult, list.AllowedOrigins, list.Tags = true, []contracts.Origin{contracts.OriginHumanUI}, tags

	proof := readDefinition("identity.principal.reauthenticate", "Confirm principal management",
		"Issue one single-use proof bound to the target tenant, target and reason.", entity, "access:manage",
		contracts.Object(map[string]*contracts.Schema{"proof": contracts.Str(64, 64)}, "proof"))
	proof.Input = contracts.Object(map[string]*contracts.Schema{
		"targetTenantId": contracts.Str(1, 256), "target": contracts.Str(3, 80), "reason": contracts.Str(5, 500),
		"operation": contracts.Enum("create", "suspend", "disable", "restore", "tenant.create", "tenant.deactivate", "tenant.restore", "setSecurity"),
		"password":  contracts.Str(1, 128), "riskLevel": contracts.Enum("none", "elevated", "high"), "securityNotes": contracts.Str(0, 500),
		"factor": contracts.Str(6, 32),
	}, "targetTenantId", "target", "reason", "operation", "password")
	proof.TransientResult, proof.AllowedOrigins, proof.Tags = true, []contracts.Origin{contracts.OriginHumanUI}, tags
	entries := []registry.Entry{
		{Definition: list, Handler: contracts.Handler{Execute: s.platformPrincipals}},
		{Definition: proof, Handler: contracts.Handler{Execute: s.principalProof}},
		{Definition: principalDetailDefinition(), Handler: contracts.Handler{Execute: s.principalDetail}},
	}
	for _, action := range []string{"create", "suspend", "disable", "restore", "setSecurity"} {
		name := "identity.principal." + action
		fields := map[string]*contracts.Schema{
			"targetTenantId": contracts.Str(1, 256), "target": contracts.Str(3, 80), "reason": contracts.Str(5, 500), "proof": contracts.Str(64, 64),
		}
		required := []string{"targetTenantId", "target", "reason", "proof"}
		if action == "setSecurity" {
			fields["riskLevel"], fields["securityNotes"] = contracts.Enum("none", "elevated", "high"), contracts.Str(0, 500)
			required = append(required, "riskLevel", "securityNotes")
		}
		if action == "create" {
			fields["displayName"], fields["initialPassword"], fields["realm"] = contracts.Str(1, 100), contracts.Str(12, 128), contracts.Str(1, 63)
			required = append(required, "displayName", "initialPassword")
		}
		definition := identityWrite(name, "Manage security principal", fmt.Sprintf("Apply %s to the selected principal with explicit platform authority.", action),
			entity, "access:manage", []string{platformPrincipalChanged}, contracts.Object(fields, required...), "targetTenantId", "target", "reason")
		definition.Output = contracts.Object(map[string]*contracts.Schema{"id": contracts.Str(36, 36)}, "id")
		definition.AllowedOrigins, definition.Tags = []contracts.Origin{contracts.OriginHumanUI}, tags
		// The password and proof can never be queued or persisted in an approval.
		definition.TransientResult = true
		if action == "create" {
			definition.Entities = []string{entity, "login_account", "password_credential"}
		}
		entries = append(entries, registry.Entry{Definition: definition, Handler: contracts.Handler{ExecuteTx: s.changePrincipal(action)}})
	}
	return entries
}

const platformPrincipalChanged = "identity.principal.changed"

func (s *Service) platformPrincipals(ctx context.Context, input contracts.Input) (contracts.Output, error) {
	actor, err := s.platformActor(ctx, s.repo.q)
	if err != nil {
		return contracts.Output{}, err
	}
	var request struct {
		TenantID    string `json:"targetTenantId"`
		After       string `json:"after"`
		PrincipalID string `json:"principalId"`
	}
	if err = contracts.Bind(input, &request); err != nil {
		return contracts.Output{}, problem("invalid")
	}
	var after, id pgtype.UUID
	if request.After != "" && after.Scan(request.After) != nil || request.PrincipalID != "" && id.Scan(request.PrincipalID) != nil {
		return contracts.Output{}, problem("invalid")
	}
	rows, err := s.repo.q.IAMListPrincipals(ctx, dbgen.IAMListPrincipalsParams{
		TenantID: pgtype.Text{String: request.TenantID, Valid: true}, AfterID: after, PrincipalID: id,
	})
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	next := ""
	if len(rows) > 50 {
		rows = rows[:50]
		next = rows[49].ID.String()
	}
	if err = s.auditPlatformPrincipal(ctx, s.repo.q, actor, request.TenantID, "list", request.PrincipalID, ""); err != nil {
		return contracts.Output{}, err
	}
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, map[string]any{"id": row.ID.String(), "kind": row.Kind, "status": row.Status,
			"displayName": row.DisplayName, "source": row.CreationSource, "createdAt": timeValue(row.CreatedAt),
			"riskLevel": row.RiskLevel, "workspaceCount": row.WorkspaceCount, "loginCount": row.LoginCount})
	}
	return contracts.Output{Data: map[string]any{"items": items, "nextAfter": next}}, nil
}

func (s *Service) principalProof(ctx context.Context, input contracts.Input) (contracts.Output, error) {
	var request struct {
		principalOperation
		Operation string  `json:"operation"`
		Password  string  `json:"password"`
		Factor    *string `json:"factor"`
	}
	if err := contracts.Bind(input, &request); err != nil || len(strings.TrimSpace(request.Reason)) < 5 {
		return contracts.Output{}, problem("invalid")
	}
	actor, err := s.platformActor(ctx, s.repo.q)
	if err != nil {
		return contracts.Output{}, err
	}
	q := s.repo.q
	var tx pgx.Tx
	if request.Operation == "setSecurity" {
		tx, err = s.repo.pool.Begin(ctx)
		if err != nil {
			return contracts.Output{}, safe(err)
		}
		defer tx.Rollback(ctx)
		q = dbgen.New(tx)
		actor, _, _, err = s.associationOwner(ctx, q, request.Password, request.Factor)
	} else {
		err = s.VerifyCurrentPassword(ctx, actor, request.Password)
	}
	if err != nil {
		return contracts.Output{}, err
	}
	proof, err := randomToken()
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	if _, err = q.GovCreateProof(ctx, dbgen.GovCreateProofParams{
		TokenHash: tokenHash(proof), TenantID: actor.TenantID, SessionHash: actor.TokenHash, ExpectedCsrf: actor.CSRF,
		Operation: request.proofPurpose(actor, request.Operation),
	}); err != nil {
		return contracts.Output{}, safe(err)
	}
	if tx != nil {
		if err = tx.Commit(ctx); err != nil {
			return contracts.Output{}, safe(err)
		}
	}
	return contracts.Output{Data: map[string]any{"proof": proof}}, nil
}

func (s *Service) changePrincipal(action string) contracts.TxExecutor {
	return func(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
		var request struct {
			principalOperation
			Proof           string  `json:"proof"`
			DisplayName     string  `json:"displayName"`
			InitialPassword string  `json:"initialPassword"`
			Realm           *string `json:"realm"`
		}
		if err := contracts.Bind(input, &request); err != nil || len(strings.TrimSpace(request.Reason)) < 5 {
			return contracts.Output{}, problem("invalid")
		}
		q := dbgen.New(tx)
		actor, err := s.platformActor(ctx, q)
		if err != nil {
			return contracts.Output{}, err
		}
		if err = consumePlatformProof(ctx, q, actor, request.principalOperation, action, request.Proof); err != nil {
			return contracts.Output{}, err
		}
		var id pgtype.UUID
		if action == "create" {
			if !invitationUsername.MatchString(request.Target) || strings.TrimSpace(request.DisplayName) == "" ||
				!loginRealmPattern.MatchString(authenticationRealm(request.Realm)) {
				return contracts.Output{}, problem("invalid")
			}
			hash, hashErr := HashPassword(request.InitialPassword)
			if hashErr != nil {
				return contracts.Output{}, problem("invalid")
			}
			id, err = q.IAMProvisionHumanPrincipal(ctx, dbgen.IAMProvisionHumanPrincipalParams{
				TenantID: request.TenantID, Username: strings.ToLower(request.Target), PasswordHash: hash, DisplayName: strings.TrimSpace(request.DisplayName),
				Realm: authenticationRealm(request.Realm),
			})
		} else if action == "setSecurity" {
			if id.Scan(request.Target) != nil || !id.Valid {
				return contracts.Output{}, problem("invalid")
			}
			id, err = q.IAMUpdatePrincipalSecurity(ctx, dbgen.IAMUpdatePrincipalSecurityParams{
				TenantID: pgtype.Text{String: request.TenantID, Valid: true}, PrincipalID: id,
				RiskLevel: request.RiskLevel, SecurityNotes: strings.TrimSpace(request.SecurityNotes),
			})
		} else {
			if id.Scan(request.Target) != nil || !id.Valid || s.platformOperators[id.String()] {
				return contracts.Output{}, problem("forbidden")
			}
			previous, next := []string{"active"}, "suspended"
			switch action {
			case "disable":
				previous, next = []string{"active", "pending", "suspended"}, "inactive"
			case "restore":
				previous, next = []string{"pending", "suspended", "inactive"}, "active"
			}
			id, err = q.IAMTransitionPrincipal(ctx, dbgen.IAMTransitionPrincipalParams{
				TenantID: pgtype.Text{String: request.TenantID, Valid: true}, PrincipalID: id, PreviousStatuses: previous, NextStatus: next,
			})
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return contracts.Output{}, problem("forbidden")
		}
		if err != nil {
			return contracts.Output{}, safe(err)
		}
		if err = s.auditPlatformPrincipal(ctx, q, actor, request.TenantID, action, id.String(), strings.TrimSpace(request.Reason)); err != nil {
			return contracts.Output{}, err
		}
		return contracts.Output{Data: map[string]any{"id": id.String()}, Events: []contracts.Event{{
			Type: platformPrincipalChanged, AggregateType: "security_principal", AggregateID: id.String(),
		}}}, nil
	}
}

func (s *Service) auditPlatformPrincipal(ctx context.Context, q *dbgen.Queries, actor *Principal, tenant, action, id, reason string) error {
	return safe(q.SecurityAuditAppend(ctx, dbgen.SecurityAuditAppendParams{
		TenantID: tenant, AccountID: actor.Account.ID,
		Action: "identity.principal." + action, Outcome: "allowed", Reason: reason,
		CorrelationID: audit.Correlation(ctx), ResourceType: "security_principal", ResourceID: id,
	}))
}

func consumePlatformProof(ctx context.Context, q *dbgen.Queries, actor *Principal, request principalOperation, action, proof string) error {
	if _, err := q.GovConsumeProof(ctx, dbgen.GovConsumeProofParams{
		TokenHash: tokenHash(proof), TenantID: actor.TenantID, SessionHash: actor.TokenHash, ExpectedCsrf: actor.CSRF,
		Operation: request.proofPurpose(actor, action),
	}); errors.Is(err, pgx.ErrNoRows) {
		return problem("forbidden")
	} else if err != nil {
		return safe(err)
	}
	return nil
}
