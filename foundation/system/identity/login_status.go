package identity

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"horizon/foundation/kernel/capability/contracts"
	"horizon/internal/repository/dbgen"
)

const loginSecurityPermission = "accounts:security"

func loginStatusProofDefinition() contracts.Definition {
	return contracts.Definition{
		Name: "identity.login.reauthenticate", Version: identityVersion, Title: "Confirm login security change",
		Description: "Issue a single-use proof for freezing or restoring a selected local login.",
		Domain:      "identity", Entity: "login_account", Layer: contracts.LayerAtomic, Permission: loginSecurityPermission,
		ReadOnly: true, TransientResult: true, Risk: contracts.RiskMedium, Approval: contracts.ApprovalAuto,
		AllowedOrigins: []contracts.Origin{contracts.OriginHumanUI}, Tags: []string{"identity", "manual-only"},
		Executor: "go:identity.login.reauthenticate",
		Input: contracts.Object(map[string]*contracts.Schema{
			"accountId": contracts.Int(1, 1<<53-1), "operation": {Type: "string", Enum: []string{"freeze", "restore"}},
			"password": contracts.Str(1, 128),
		}, "accountId", "operation", "password"),
		Output:   contracts.Object(map[string]*contracts.Schema{"proof": {Type: "string"}}, "proof"),
		Loggable: []string{"accountId", "operation"},
	}
}

func loginStatusDefinition(operation, status string) contracts.Definition {
	definition := identityWrite("identity.login."+operation, operation+" login",
		"Change a local login's status and revoke existing browser sessions and MCP credentials.",
		"login_account", loginSecurityPermission, []string{"identity.login." + status},
		contracts.Object(map[string]*contracts.Schema{
			"accountId": contracts.Int(1, 1<<53-1), "proof": contracts.Str(64, 64), "reason": contracts.Str(5, 500),
		}, "accountId", "proof", "reason"), "accountId", "reason")
	definition.AllowedOrigins = []contracts.Origin{contracts.OriginHumanUI}
	definition.Tags = append(definition.Tags, "manual-only")
	definition.Output = contracts.Object(map[string]*contracts.Schema{"id": {Type: "integer"}, "status": {Type: "string"}}, "id", "status")
	return definition
}

func loginStatusOperation(principal *Principal, operation string, accountID int64) string {
	return "identity.login." + operation + ":" + strconv.FormatInt(accountID, 10) + ":" + tokenHash(principal.CSRF)
}

func (s *Service) loginStatusProof(ctx context.Context, input contracts.Input) (contracts.Output, error) {
	principal, err := Current(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	if _, _, err = sessionLoginSubject(principal); err != nil || principal.IdentityBindingID != "" || !principal.CanPermanent(loginSecurityPermission) {
		return contracts.Output{}, contracts.Problem("forbidden")
	}
	var request struct {
		AccountID int64  `json:"accountId"`
		Operation string `json:"operation"`
		Password  string `json:"password"`
	}
	if err := contracts.Bind(input, &request); err != nil {
		return contracts.Output{}, contracts.Problem("invalid")
	}
	if err = s.VerifyCurrentPassword(ctx, principal, request.Password); err != nil {
		return contracts.Output{}, err
	}
	proof, err := randomToken()
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	if _, err = s.repo.q.GovCreateProof(ctx, dbgen.GovCreateProofParams{
		TokenHash: tokenHash(proof), TenantID: principal.TenantID, SessionHash: principal.TokenHash,
		ExpectedCsrf: principal.CSRF, Operation: loginStatusOperation(principal, request.Operation, request.AccountID),
	}); err != nil {
		return contracts.Output{}, safe(err)
	}
	return contracts.Output{Data: map[string]any{"proof": proof}}, nil
}

func (s *Service) changeLoginStatus(operation, oldStatus, newStatus string) contracts.TxExecutor {
	return func(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
		present, err := Current(ctx)
		if err != nil {
			return contracts.Output{}, err
		}
		queries := dbgen.New(tx)
		actor, err := s.WithReadQueries(queries).PrincipalForSessionInvocation(ctx, present, false)
		if err != nil || actor.IdentityBindingID != "" || !actor.CanPermanent(loginSecurityPermission) {
			return contracts.Output{}, contracts.Problem("forbidden")
		}
		actorLoginID, _, err := sessionLoginSubject(actor)
		if err != nil {
			return contracts.Output{}, err
		}
		var request struct {
			AccountID int64  `json:"accountId"`
			Proof     string `json:"proof"`
			Reason    string `json:"reason"`
		}
		if err := contracts.Bind(input, &request); err != nil || len(strings.TrimSpace(request.Reason)) < 5 {
			return contracts.Output{}, contracts.Problem("invalid")
		}
		if _, err = queries.GovConsumeProof(ctx, dbgen.GovConsumeProofParams{
			TokenHash: tokenHash(request.Proof), TenantID: actor.TenantID, SessionHash: actor.TokenHash,
			ExpectedCsrf: actor.CSRF, Operation: loginStatusOperation(actor, operation, request.AccountID),
		}); errors.Is(err, pgx.ErrNoRows) {
			return contracts.Output{}, contracts.Problem("forbidden")
		} else if err != nil {
			return contracts.Output{}, safe(err)
		}
		loginID, err := queries.IAMLockManagedLogin(ctx, dbgen.IAMLockManagedLoginParams{
			TenantID: actor.TenantID, AccountID: request.AccountID, ActorLoginID: actorLoginID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return contracts.Output{}, contracts.Problem("forbidden")
		}
		if err != nil {
			return contracts.Output{}, safe(err)
		}
		id, err := queries.IAMChangeManagedLoginStatus(ctx, dbgen.IAMChangeManagedLoginStatusParams{
			TenantID: actor.TenantID, AccountID: request.AccountID, LoginID: loginID, OldStatus: oldStatus, NewStatus: newStatus,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return contracts.Output{}, contracts.Problem("forbidden")
		}
		if err != nil {
			return contracts.Output{}, safe(err)
		}
		return contracts.Output{Data: map[string]any{"id": request.AccountID, "status": newStatus}, Events: []contracts.Event{{
			Type: "identity.login." + newStatus, AggregateType: "login_account", AggregateID: id.String(),
			Payload: map[string]any{"accountId": request.AccountID, "reason": strings.TrimSpace(request.Reason)},
		}}}, nil
	}
}
