package automation

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/capability/gateway"
	"horizon/foundation/system/governance"
	"horizon/foundation/system/identity"
	"horizon/internal/repository/dbgen"
)

const serviceScheduleCapability = "automation.task.createService"

type serviceExecution struct {
	AccountID    int64           `json:"accountId"`
	CredentialID string          `json:"credentialId"`
	Capability   string          `json:"capability"`
	Version      string          `json:"capabilityVersion"`
	Input        contracts.Input `json:"input"`
}

func serviceScheduleDefinition() contracts.Definition {
	d := taskWriteDefinition("create", "Schedule a service capability")
	d.Name = serviceScheduleCapability
	d.Executor = "go:" + d.Name
	d.Description = "Schedule one version of a low-risk capability under an issued Service credential."
	d.Tags = []string{"manual-only"}
	d.AllowedOrigins = []contracts.Origin{contracts.OriginHumanUI}
	d.Input.Properties["description"].Description = "Description of the scheduled action."
	d.Input.Properties["accountId"] = contracts.Int(1, 1<<53-1)
	d.Input.Properties["credentialId"] = contracts.Str(36, 36)
	d.Input.Properties["capability"] = contracts.Str(1, 80)
	d.Input.Properties["capabilityVersion"] = contracts.Str(1, 32)
	d.Input.Properties["arguments"] = contracts.Str(2, 16384)
	d.Input.Properties["arguments"].Description = "JSON object matching the selected capability's input."
	d.Input.Properties["password"] = contracts.Str(1, 256)
	d.Input.Required = append(d.Input.Required, "accountId", "credentialId", "capability", "capabilityVersion", "arguments", "password")
	return d
}

func (m *Module) createServiceSchedule(ctx context.Context, tx pgx.Tx, in contracts.Input) (contracts.Output, error) {
	if m.Core == nil || m.Gateway == nil {
		return contracts.Output{}, contracts.Problem("unavailable")
	}
	var execution serviceExecution
	if err := contracts.Bind(in, &execution); err != nil ||
		json.Unmarshal([]byte(text(in, "arguments")), &execution.Input) != nil || execution.Input == nil {
		return contracts.Output{}, contracts.Problem("invalid")
	}
	password, _ := in["password"].(string)
	queries := dbgen.New(tx)
	service, err := m.Core.Identity.ServiceSchedulePrincipal(ctx, queries, execution.AccountID, execution.CredentialID, password)
	if err != nil {
		return contracts.Output{}, err
	}
	if err = m.Gateway.ValidateServiceAutomationTarget(service, execution.Capability, execution.Version, execution.Input); err != nil {
		return contracts.Output{}, err
	}
	return m.createTask(ctx, tx, in, &execution)
}

func (m *Module) storeServiceExecution(ctx context.Context, queries *dbgen.Queries, owner *identity.Principal, taskID int64, execution *serviceExecution) error {
	raw, err := json.Marshal(execution)
	if err != nil {
		return err
	}
	sealed, err := m.Core.Seal(owner.TenantID, serviceScheduleCapability, fmt.Sprint(taskID), string(raw))
	if err != nil {
		return err
	}
	return queries.AutomationTaskSetServiceExecution(ctx, dbgen.AutomationTaskSetServiceExecutionParams{
		TenantID: owner.TenantID, OwnerID: owner.Account.ID, ID: taskID, ServiceExecution: pgtype.Text{String: sealed, Valid: true},
	})
}

func (w *Worker) executeService(ctx context.Context, row dbgen.AutomationRunsClaimRow) {
	runCtx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()
	err := w.invokeService(runCtx, row)
	status, code := "completed", ""
	if err != nil {
		status, code = "failed", governance.Code(err)
		if code == "forbidden" || code == "unauthenticated" {
			status = "skipped"
		}
	}
	w.finish(ctx, row.TenantID, row.ID, status, code, 0)
	w.record(ctx, row.TenantID, row.TaskID, status == "failed")
	if status == "skipped" {
		w.stop(row)
	}
}

func (w *Worker) invokeService(ctx context.Context, row dbgen.AutomationRunsClaimRow) error {
	if w.Core == nil || w.Gateway == nil {
		return contracts.Problem("unavailable")
	}
	var execution serviceExecution
	raw, err := w.Core.Unseal(row.TenantID, serviceScheduleCapability, fmt.Sprint(row.TaskID), row.ServiceExecution.String)
	if err != nil || json.Unmarshal([]byte(raw), &execution) != nil || execution.Input == nil {
		return contracts.Problem("invalid")
	}
	service, err := w.Identity.PrincipalForScheduledService(ctx, row.TenantID, execution.AccountID, execution.CredentialID)
	if err != nil {
		return err
	}
	correlation := fmt.Sprintf("automation:%d", row.ID)
	result, err := w.Gateway.Invoke(identity.WithPrincipal(ctx, service), gateway.Request{
		Name: execution.Capability, Version: execution.Version, Input: execution.Input,
		Origin: contracts.OriginAutomation, CorrelationID: correlation, IdempotencyKey: correlation,
	})
	if err != nil {
		return err
	}
	if result.Status != contracts.StatusSucceeded {
		if result.ErrorCode != "" {
			return contracts.Problem(result.ErrorCode)
		}
		return contracts.Problem("internal")
	}
	return nil
}
