package gateway

import (
	"context"

	"horizon/foundation/system/identity"
	"horizon/internal/repository/dbgen"
)

// TaskReference is supplied only by trusted runners, never request payloads.
type TaskReference struct {
	RunID             int64
	TenantID          string
	AccountID         int64
	IdentityBindingID string
}
type taskReferenceKey struct{}

func WithTaskReference(ctx context.Context, reference TaskReference) context.Context {
	return context.WithValue(ctx, taskReferenceKey{}, reference)
}
func CurrentTaskReference(ctx context.Context) (TaskReference, bool) {
	value, ok := ctx.Value(taskReferenceKey{}).(TaskReference)
	return value, ok
}

func (g *Gateway) activeMachineRun(ctx context.Context, q *dbgen.Queries, p *identity.Principal, task TaskReference, parent string) bool {
	if task.TenantID != p.TenantID || task.AccountID != p.Account.ID || task.IdentityBindingID != p.IdentityBindingID {
		return false
	}
	active, err := q.ExecutionMachineRunActive(ctx, dbgen.ExecutionMachineRunActiveParams{
		TenantID: p.TenantID, AccountID: p.Account.ID, RunID: task.RunID,
		InvocationID: parent, CredentialID: p.MachineCredentialID(), DelegationID: p.AgentDelegationID(),
	})
	return err == nil && active
}
