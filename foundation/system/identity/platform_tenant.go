package identity

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"

	"horizon/foundation/kernel/audit"
	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/capability/registry"
	"horizon/internal/repository/dbgen"
)

var tenantIdentifier = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,62}$`)

func (s *Service) tenantEntries() []registry.Entry {
	tags := []string{"identity", "manual-only", PlatformOperatorTag}
	origins := []contracts.Origin{contracts.OriginHumanUI}
	item := contracts.Object(map[string]*contracts.Schema{
		"id": {Type: "string"}, "status": {Type: "string"}, "createdAt": {Type: "string"},
	}, "id", "status", "createdAt")
	list := readDefinition("identity.tenant.list", "List workspaces", "Read the workspace directory with platform operator authority.",
		"tenant", "access:manage", contracts.Object(map[string]*contracts.Schema{
			"items": contracts.Array(item, 0, 50), "nextAfter": {Type: "string"},
		}, "items", "nextAfter"))
	list.Input = contracts.Object(map[string]*contracts.Schema{"after": contracts.Str(1, 256)})
	list.TransientResult, list.Tags, list.AllowedOrigins = true, tags, origins
	entries := []registry.Entry{{Definition: list, Handler: contracts.Handler{Execute: s.platformTenants}}}
	for _, operation := range []string{"create", "deactivate", "restore"} {
		name := "identity.tenant." + operation
		fields := map[string]*contracts.Schema{"targetTenantId": contracts.Str(1, 256), "reason": contracts.Str(5, 500), "proof": contracts.Str(64, 64)}
		required := []string{"targetTenantId", "reason", "proof"}
		if operation == "create" {
			fields["target"], fields["initialPassword"] = contracts.Str(3, 80), contracts.Str(12, 128)
			required = append(required, "target", "initialPassword")
		}
		definition := identityWrite(name, "Manage workspace", "Create a workspace or change its availability using platform authority.",
			"tenant", "access:manage", []string{name + ".completed"}, contracts.Object(fields, required...), "targetTenantId", "reason")
		definition.Output = contracts.Object(map[string]*contracts.Schema{"id": {Type: "string"}}, "id")
		definition.TransientResult, definition.Tags, definition.AllowedOrigins = true, tags, origins
		if operation == "create" {
			definition.Entities = append([]string{"tenant", "account", "login_account", "password_credential", "security_principal", "tenant_membership", "role"}, s.tenantProvisioning.entities...)
		}
		entries = append(entries, registry.Entry{Definition: definition, Handler: contracts.Handler{ExecuteTx: s.changeTenant(operation)}})
	}
	return entries
}

func (s *Service) platformTenants(ctx context.Context, input contracts.Input) (contracts.Output, error) {
	if _, err := s.platformActor(ctx, s.repo.q); err != nil {
		return contracts.Output{}, err
	}
	var request struct{ After string }
	if err := contracts.Bind(input, &request); err != nil {
		return contracts.Output{}, problem("invalid")
	}
	rows, err := s.repo.q.IAMPlatformListTenants(ctx, request.After)
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	next := ""
	if len(rows) > 50 {
		rows = rows[:50]
		next = rows[49].ID
	}
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, map[string]any{"id": row.ID, "status": row.Status, "createdAt": timeValue(row.CreatedAt)})
	}
	return contracts.Output{Data: map[string]any{"items": items, "nextAfter": next}}, nil
}

func (s *Service) changeTenant(operation string) contracts.TxExecutor {
	return func(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
		var request struct {
			principalOperation
			Proof           string `json:"proof"`
			InitialPassword string `json:"initialPassword"`
		}
		if err := contracts.Bind(input, &request); err != nil || len(strings.TrimSpace(request.Reason)) < 5 {
			return contracts.Output{}, problem("invalid")
		}
		q := dbgen.New(tx)
		actor, err := s.platformActor(ctx, q)
		if err != nil {
			return contracts.Output{}, err
		}
		if actor.TenantID == request.TenantID {
			return contracts.Output{}, problem("forbidden")
		}
		if operation != "create" {
			request.Target = request.TenantID
		}
		if err = consumePlatformProof(ctx, q, actor, request.principalOperation, "tenant."+operation, request.Proof); err != nil {
			return contracts.Output{}, err
		}
		if operation == "create" {
			if !tenantIdentifier.MatchString(request.TenantID) || !invitationUsername.MatchString(request.Target) {
				return contracts.Output{}, problem("invalid")
			}
			hash, err := HashPassword(request.InitialPassword)
			if err != nil {
				return contracts.Output{}, problem("invalid")
			}
			accountID, err := bootstrapTenant(ctx, q, request.TenantID, request.Target, hash)
			if errors.Is(err, errTenantAlreadyBootstrapped) {
				return contracts.Output{}, problem("conflict")
			} else if err != nil {
				return contracts.Output{}, safe(err)
			}
			if provision := s.tenantProvisioning.provision; provision != nil {
				if err := provision(ctx, tx, request.TenantID, accountID); err != nil {
					return contracts.Output{}, safe(err)
				}
			}
		} else {
			previous, next := "active", "inactive"
			if operation == "restore" {
				previous, next = next, previous
			}
			if _, err = q.IAMChangeTenantStatus(ctx, dbgen.IAMChangeTenantStatusParams{
				NextStatus: next, TenantID: request.TenantID, PreviousStatus: previous,
			}); errors.Is(err, pgx.ErrNoRows) {
				return contracts.Output{}, problem("forbidden")
			} else if err != nil {
				return contracts.Output{}, safe(err)
			}
		}
		action := "identity.tenant." + operation
		if err = q.SecurityAuditAppend(ctx, dbgen.SecurityAuditAppendParams{
			TenantID: request.TenantID, AccountID: actor.Account.ID,
			Action: action, Outcome: "allowed", Reason: strings.TrimSpace(request.Reason), CorrelationID: audit.Correlation(ctx),
			ResourceType: "tenant", ResourceID: request.TenantID,
		}); err != nil {
			return contracts.Output{}, safe(err)
		}
		return contracts.Output{Data: map[string]any{"id": request.TenantID}, Events: []contracts.Event{{
			Type: action + ".completed", AggregateType: "tenant", AggregateID: request.TenantID,
		}}}, nil
	}
}
