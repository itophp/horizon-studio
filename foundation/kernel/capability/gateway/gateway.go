// Package gateway is the single runtime entry point for business capabilities.
// Every origin — React button, agent tool, workflow node, external API, event or
// approval resumption — passes through the same identity, permission, schema,
// policy, idempotency, execution, outbox and audit pipeline.
package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"horizon/foundation/kernel/approval"
	"horizon/foundation/kernel/audit"
	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/capability/registry"
	"horizon/foundation/kernel/eventhub"
	"horizon/foundation/kernel/executionplan"
	canonical "horizon/foundation/kernel/memory"
	"horizon/foundation/kernel/policy"
	"horizon/foundation/system/identity"
	"horizon/internal/repository/dbgen"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// ApprovalTTL keeps a reviewed payload executable for a short window only.
	ApprovalTTL = 10 * time.Minute
	// MaxDepth is the hard ceiling for event → agent → capability chains.
	MaxDepth = 8
)

// Request is one invocation request. Security fields are not part of it: the
// principal comes from the trusted context, the origin from the adapter.
type Request struct {
	executionBinding    contracts.ExecutionBinding
	authorizationOrigin contracts.Origin
	authorization       *identity.AuthorizationDecision
	Runtime             *contracts.RuntimeReference
	Name                string
	Version             string
	Input               contracts.Input
	IdempotencyKey      string
	Origin              contracts.Origin
	CorrelationID       string
	CausationID         string
	TriggerDepth        int
}

// Options wires the gateway.
type Options struct {
	RuntimeCheck   func(context.Context, *contracts.RuntimeReference, contracts.Definition, *dbgen.Queries) error
	Registry       *registry.Registry
	Identity       *identity.Service
	Pool           *pgxpool.Pool
	Approvals      *approval.Store
	ApprovalCipher ApprovalCipher
	Audit          *audit.Store
	Events         *eventhub.Store
	Now            func() time.Time
	Observations   ObservationSink
}

type ObservationSink interface{ Submit(canonical.Envelope) }

// Gateway executes capabilities.
type Gateway struct {
	workflowPlans  WorkflowPlanRuntime
	runtimeCheck   func(context.Context, *contracts.RuntimeReference, contracts.Definition, *dbgen.Queries) error
	registry       *registry.Registry
	identity       *identity.Service
	pool           *pgxpool.Pool
	approvals      *approval.Store
	approvalCipher ApprovalCipher
	audit          *audit.Store
	events         *eventhub.Store
	now            func() time.Time
	observations   ObservationSink
}

// New builds a gateway.
func New(options Options) *Gateway {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Gateway{
		registry: options.Registry, runtimeCheck: options.RuntimeCheck, identity: options.Identity, pool: options.Pool,
		approvals: options.Approvals, approvalCipher: options.ApprovalCipher,
		audit: options.Audit, events: options.Events, now: now, observations: options.Observations,
	}
}

// Invoke runs the full pipeline and always returns a result for business
// outcomes. Request level denials (unauthenticated, forbidden, invalid, not
// found, conflict) are returned as coded errors so the transport can map them.
func (g *Gateway) Invoke(ctx context.Context, request Request) (*contracts.Result, error) {
	return g.invoke(ctx, request, nil)
}
func (g *Gateway) invoke(ctx context.Context, request Request, selection *contracts.ExecutionBinding) (*contracts.Result, error) {
	parent, hasParent := CurrentInvocation(ctx)
	if err := inheritRequest(ctx, &request); err != nil {
		return nil, err
	}
	p, err := identity.Current(ctx)
	if err != nil {
		return nil, contracts.Problem("unauthenticated")
	}
	if g.identity != nil && p.Account.ID > 0 {
		var refreshed *identity.Principal
		if p.MachineCredentialID() != "" {
			refreshed, err = g.identity.RefreshMachine(ctx, p, false)
			if err != nil {
				return nil, err
			}
		} else if p.TokenHash != "" {
			refreshed, err = g.identity.PrincipalForSessionInvocation(ctx, p, request.Origin == contracts.OriginHumanUI)
			if err != nil {
				return nil, err
			}
		} else {
			refreshed, err = g.identity.RefreshTaskPrincipal(ctx, p)
			if err != nil {
				return nil, contracts.Problem("forbidden")
			}
		}
		p = refreshed
		ctx = identity.WithPrincipal(ctx, p)
	}
	if err = g.validateRequest(&request); err != nil {
		return nil, err
	}
	entry, ok := g.registry.Resolve(request.Name, request.Version)
	if !ok {
		return nil, contracts.Problem("not_found")
	}
	if entry.Definition.TransientResult && entry.Definition.ReadOnly {
		request.IdempotencyKey = ""
	}
	definition := entry.Definition
	if request.Origin == contracts.OriginAutomation {
		if p.MachineCredentialID() == "" || !serviceAutomationTarget(entry) {
			g.observeDenied(ctx, p, request, definition.Name, "service_automation_scope")
			return nil, contracts.Problem("forbidden")
		}
		direct := contracts.DefaultExecutionBinding(definition)
		selection = &direct
	}
	if (p.IsDedicatedWorker() || slices.Contains(definition.Tags, "durable-system")) &&
		(request.Origin != contracts.OriginEvent || !p.AllowsSystemCapability(definition.Key())) {
		return nil, contracts.Problem("forbidden")
	}
	task, insideWorkflow := CurrentTaskReference(ctx)
	if credential := p.MachineCredentialID(); credential != "" {
		if g.identity == nil || hasParent || request.Version == "" || request.Runtime != nil ||
			!p.MachineAllows(definition, request.Input, request.Origin) || !MachineDirectEntry(entry) {
			g.observeDenied(ctx, p, request, definition.Name, "machine_scope")
			return nil, contracts.Problem("forbidden")
		}
		prefix := "machine:"
		if insideWorkflow || request.Origin == contracts.OriginWorkflow {
			if !insideWorkflow || (request.Origin != contracts.OriginWorkflow && request.Origin != contracts.OriginAgent) ||
				!g.activeMachineRun(ctx, dbgen.New(g.pool), p, task, request.CausationID) {
				return nil, contracts.Problem("forbidden")
			}
			prefix = "orch:" + strconv.FormatInt(task.RunID, 10) + ":machine:"
		} else if request.Origin == contracts.OriginAgent && p.AgentDelegationID() == "" {
			return nil, contracts.Problem("forbidden")
		}
		if request.IdempotencyKey != "" {
			request.IdempotencyKey = prefix + contracts.Digest(contracts.Input{"credential": credential, "delegation": p.AgentDelegationID(), "key": request.IdempotencyKey})
		}
	}
	if slices.Contains(definition.Tags, "system-only") && (p.Account.ID != 0 || request.Origin != contracts.OriginEvent) {
		return nil, contracts.Problem("forbidden")
	}
	if !definition.AllowsOrigin(request.Origin) {
		return nil, contracts.Problem("forbidden")
	}
	if err = checkInputAccess(p, definition, request.Input); err != nil {
		return nil, err
	}
	for _, tag := range definition.Tags {
		if strings.HasPrefix(tag, "parent:") && (!hasParent || request.Origin != contracts.OriginCapability || parent.Capability+"@"+parent.Version != strings.TrimPrefix(tag, "parent:")) {
			g.observeDenied(ctx, p, request, definition.Name, "origin")
			return nil, contracts.Problem("forbidden")
		}
	}
	if slices.Contains(definition.Tags, "manual-only") && request.Origin != contracts.OriginHumanUI && request.Origin != contracts.OriginAPI && request.Origin != contracts.OriginApproval {
		g.observeDenied(ctx, p, request, definition.Name, "origin")
		return nil, contracts.Problem("forbidden")
	}
	caller := callerOf(p)
	if (definition.Permission != "" && !p.Can(definition.Permission)) || !caller.Allowed(definition.Permission) {
		g.observeDenied(ctx, p, request, definition.Name, "permission")
		return nil, contracts.Problem("forbidden")
	}
	if err = definition.Input.Validate(request.Input); err != nil {
		return nil, contracts.Problem("invalid")
	}
	if err = g.checkEntry(ctx, dbgen.New(g.pool), p, entry, request, false); err != nil {
		return nil, err
	}
	if definition.Idempotency.Required && strings.TrimSpace(request.IdempotencyKey) == "" {
		return nil, contracts.Problem("invalid")
	}
	if request.Runtime != nil {
		if g.runtimeCheck == nil {
			return nil, contracts.Problem("unavailable")
		}
		if err = g.runtimeCheck(ctx, request.Runtime, definition, dbgen.New(g.pool)); err != nil {
			return nil, err
		}
	}
	digest := contracts.Digest(request.Input)
	if request.IdempotencyKey != "" {
		if replay, found, e := g.replayWait(ctx, p, definition, request.IdempotencyKey, digest); e != nil {
			return nil, e
		} else if found {
			return replay, nil
		}
	}
	decision := policy.Admit(definition, caller, request.Origin)
	if !decision.Allowed {
		g.observeDenied(ctx, p, request, definition.Name, "policy")
		return nil, contracts.Problem(decision.Code)
	}
	if definition.TransientResult && !definition.ReadOnly && decision.Approval != contracts.ApprovalAuto {
		return nil, contracts.Problem("forbidden")
	}
	if decision.Approval != contracts.ApprovalAuto {
		// Reuse the open card for the same reviewed payload instead of booking a
		// second invocation that could never be resumed.
		if existing, e := dbgen.New(g.pool).CapPendingApprovalFor(ctx, dbgen.CapPendingApprovalForParams{
			TenantID: p.TenantID, Capability: definition.Name,
			AccountID: p.Account.ID, ArgumentsDigest: digest, ActionKey: approvalActionKey(request),
			IdentityBindingID:   p.IdentityBindingID,
			MachineCredentialID: p.MachineCredentialID(),
			AgentDelegationID:   p.AgentDelegationID(),
		}); e == nil {
			return &contracts.Result{
				InvocationID: existing.InvocationID, Status: contracts.StatusAwaitingApproval,
				ApprovalID: existing.ID, TraceID: existing.TraceID, EventIDs: []string{},
			}, nil
		}
	}
	if selection != nil {
		request.executionBinding = *selection
	} else {
		request.executionBinding, err = executionplan.Resolve(ctx, dbgen.New(g.pool), p.TenantID, definition)
	}
	if err != nil {
		return nil, err
	}
	if len(definition.RowFilterAttributes) > 0 && request.executionBinding.Plan.Mode != "direct" {
		return nil, contracts.Problem("forbidden")
	}
	if p.MachineCredentialID() != "" && request.executionBinding.Plan.Mode != "direct" &&
		(request.executionBinding.Plan.Mode != "workflow" || insideWorkflow) {
		return nil, contracts.Problem("forbidden")
	}
	traceID := strings.TrimSpace(request.CorrelationID)
	if traceID == "" {
		traceID = identifier("trace_")
	}
	invocationID := identifier("inv_")
	redacted, err := json.Marshal(definition.Redact(request.Input))
	if err != nil {
		return nil, contracts.Problem("internal")
	}
	if decision.Approval != contracts.ApprovalAuto {
		result, e := g.requestApproval(ctx, p, entry, request, invocationID, traceID, digest, redacted, decision.Approval)
		if e != nil {
			// Booking failed, so its transaction rolled back: there is no
			// invocation and no card to inspect afterwards. Without this line the
			// caller only sees a bare internal code and the evidence is gone.
			slog.Error("capability approval booking failed", "capability", definition.Name,
				"origin", string(request.Origin), "tenant", p.TenantID, "error", e)
		}
		return result, e
	}
	return g.run(ctx, p, entry, request, invocationID, traceID, digest, redacted, false)
}

func (g *Gateway) validateRequest(request *Request) error {
	if request.Name == "" || request.TriggerDepth < 0 || request.TriggerDepth > MaxDepth {
		return contracts.Problem("invalid")
	}
	if request.Input == nil {
		request.Input = contracts.Input{}
	}
	if request.Origin == "" {
		return contracts.Problem("invalid")
	}
	switch request.Origin {
	case contracts.OriginCapability, contracts.OriginHumanUI, contracts.OriginAgent, contracts.OriginWorkflow, contracts.OriginAPI, contracts.OriginEvent, contracts.OriginApproval, contracts.OriginAutomation:
	default:
		return contracts.Problem("invalid")
	}
	if request.CorrelationID != "" && (len(request.CorrelationID) > 120 || strings.ContainsAny(request.CorrelationID, "\r\n")) {
		return contracts.Problem("invalid")
	}
	if request.CausationID != "" && (len(request.CausationID) > 120 || strings.ContainsAny(request.CausationID, "\r\n")) {
		return contracts.Problem("invalid")
	}
	return nil
}

// replay returns a stored idempotent outcome.
func (g *Gateway) replay(ctx context.Context, p *identity.Principal, definition contracts.Definition, key string, digest string) (*contracts.Result, bool, error) {
	row, err := dbgen.New(g.pool).CapFindIdempotent(ctx, dbgen.CapFindIdempotentParams{TenantID: p.TenantID, Capability: definition.Name, IdempotencyKey: key})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if row.AccountID != p.Account.ID || row.IdentityBindingID != p.IdentityBindingID ||
		row.MachineCredentialID != p.MachineCredentialID() || row.AgentDelegationID != p.AgentDelegationID() || row.InputDigest != digest || row.CapabilityVersion != definition.Version {
		return nil, false, contracts.Problem("conflict")
	}
	if row.MembershipRevokedSinceInvocation {
		return nil, true, contracts.Problem("forbidden")
	}
	result := &contracts.Result{
		InvocationID: row.InvocationID, TraceID: row.TraceID, ApprovalID: row.ApprovalID,
		Status: contracts.Status(row.Status), Replayed: true,
	}
	switch contracts.Status(row.Status) {
	case contracts.StatusSucceeded:
		stored := storedResult{}
		if len(row.Result) > 0 {
			_ = json.Unmarshal(row.Result, &stored)
		}
		if !definition.TransientResult {
			result.Data, err = projectOutput(p, definition, stored.Data)
			if err != nil {
				return nil, false, err
			}
		}
		result.EventIDs = stored.EventIDs
	case contracts.StatusFailed:
		result.ErrorCode = row.ErrorCode
	case contracts.StatusAwaitingApproval:
		result.ApprovalID = row.ApprovalID
	case contracts.StatusExecuting, contracts.StatusRequested:
		run, e := dbgen.New(g.pool).ExecutionRunForInvocation(ctx, dbgen.ExecutionRunForInvocationParams{TenantID: p.TenantID, AccountID: p.Account.ID, InvocationID: row.InvocationID})
		if e != nil {
			return nil, true, e
		}
		if run.Valid {
			return result, true, nil
		}
		return nil, true, contracts.Problem("conflict")
	}
	return result, true, nil
}

// replayWait tolerates a concurrent in-flight winner: the unique index makes one
// caller the owner, the other waits briefly and returns the stored outcome.
func (g *Gateway) replayWait(ctx context.Context, p *identity.Principal, definition contracts.Definition, key string, digest string) (*contracts.Result, bool, error) {
	deadline := g.now().Add(time.Second)
	for {
		result, found, err := g.replay(ctx, p, definition, key, digest)
		if err == nil || !isConflict(err) {
			return result, found, err
		}
		if g.now().After(deadline) {
			return nil, false, err
		}
		select {
		case <-ctx.Done():
			return nil, false, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func isConflict(err error) bool {
	var coded *contracts.Error
	return errors.As(err, &coded) && coded.Code == "conflict"
}

// requestApproval books the invocation and the confirmation card in one
// transaction, so a pending call never exists without its approval.
func (g *Gateway) requestApproval(ctx context.Context, p *identity.Principal, entry registry.Entry, request Request, invocationID, traceID, digest string, redacted json.RawMessage, kind contracts.Approval) (*contracts.Result, error) {
	arguments, err := g.approvalArguments(p.TenantID, invocationID, entry.Definition, request.Input)
	if err != nil {
		return nil, err
	}
	tx, err := g.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	q := dbgen.New(tx)
	if p.MachineCredentialID() != "" {
		p, err = g.identity.WithReadQueries(q).RefreshMachine(ctx, p, true)
		if err != nil {
			return nil, err
		}
		if err = checkInputAccess(p, entry.Definition, request.Input); err != nil {
			return nil, err
		}
	}
	if err = g.checkEntry(ctx, q, p, entry, request, true); err != nil {
		return nil, err
	}
	if _, err = q.CapCreateInvocation(ctx, dbgen.CapCreateInvocationParams{
		ActingOrganizationID: p.ActingOrganization.OrganizationID, ActorPortal: p.ActingOrganization.Portal,
		TenantID: p.TenantID, AccountID: p.Account.ID,
		IdentityBindingID:   p.IdentityBindingID,
		MachineCredentialID: p.MachineCredentialID(),
		AgentDelegationID:   p.AgentDelegationID(),
		InvocationID:        invocationID, Capability: entry.Definition.Name, CapabilityVersion: entry.Definition.Version,
		Origin: string(request.Origin), IdempotencyKey: request.IdempotencyKey,
		Status: string(contracts.StatusAwaitingApproval), Input: redacted, InputDigest: digest,
		CorrelationID: traceID, CausationID: request.CausationID, TriggerDepth: int32(request.TriggerDepth),
		TraceID: traceID, ExecutionBinding: bindingJSON(request.executionBinding),
	}); err != nil {
		if isUnique(err) && request.IdempotencyKey != "" {
			if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
				return nil, rollbackErr
			}
			if replay, found, replayErr := g.replayWait(ctx, p, entry.Definition, request.IdempotencyKey, digest); replayErr != nil {
				return nil, replayErr
			} else if found {
				return replay, nil
			}
		}
		return nil, err
	}
	refs, sourceErr := entrySources(entry, request.Input)
	if sourceErr != nil {
		return nil, sourceErr
	}
	for _, ref := range refs {
		if err = q.RuntimeBindInvocation(ctx, dbgen.RuntimeBindInvocationParams{TenantID: p.TenantID, InvocationID: invocationID, PluginID: ref.Installation}); err != nil {
			return nil, err
		}
	}
	approvalID, err := g.approvals.Create(ctx, q, approval.Approval{
		TenantID: p.TenantID, AccountID: p.Account.ID,
		IdentityBindingID: p.IdentityBindingID,
		Capability:        entry.Definition.Name, Version: entry.Definition.Version, InvocationID: invocationID,
		Risk: string(entry.Definition.Risk), Kind: string(kind), Arguments: arguments, Digest: digest, ActionKey: approvalActionKey(request),
	}, ApprovalTTL)
	if err != nil {
		return nil, err
	}
	if err = q.CapSetInvocationStatus(ctx, dbgen.CapSetInvocationStatusParams{
		TenantID: p.TenantID, InvocationID: invocationID,
		Status: string(contracts.StatusAwaitingApproval), ApprovalID: approvalID,
	}); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &contracts.Result{
		InvocationID: invocationID, Status: contracts.StatusAwaitingApproval,
		ApprovalID: approvalID, TraceID: traceID, EventIDs: []string{},
	}, nil
}

// Decide approves or rejects a pending card and, on approval, resumes the exact
// payload under the requester's re-resolved authority.
func (g *Gateway) Decide(ctx context.Context, id int64, approve bool, note string) (*contracts.Result, error) {
	p, err := identity.Current(ctx)
	if err != nil {
		return nil, contracts.Problem("unauthenticated")
	}
	if p.MachineCredentialID() != "" {
		return nil, contracts.Problem("forbidden")
	}
	if p.TokenHash != "" {
		p, err = g.identity.PrincipalForSessionInvocation(ctx, p, false)
		if err != nil {
			return nil, err
		}
	}
	card, err := g.approvals.Get(ctx, p.TenantID, id)
	if err != nil {
		return nil, err
	}
	if card.Status != approval.Pending {
		return nil, contracts.Problem("conflict")
	}
	other := card.AccountID != p.Account.ID
	if !other && (card.IdentityBindingID != p.IdentityBindingID) {
		return nil, contracts.Problem("forbidden")
	}
	if other && !tenantApprovalReviewer(p) {
		return nil, contracts.Problem("forbidden")
	}
	if card.Kind == string(contracts.ApprovalFourEyes) && !other {
		return nil, contracts.Problem("forbidden")
	}
	if entry, ok := g.registry.Resolve(card.Capability, card.Version); ok && slices.Contains(entry.Definition.Tags, contracts.TagSeparatePrincipalApproval) {
		requester, lookupErr := dbgen.New(g.pool).IAMActiveAccountSubject(ctx, dbgen.IAMActiveAccountSubjectParams{TenantID: p.TenantID, AccountID: card.AccountID})
		if lookupErr != nil || p.SubjectID == "" || requester.PrincipalID.String() == p.SubjectID {
			return nil, contracts.Problem("forbidden")
		}
	}
	if !card.ExpiresAt.IsZero() && g.now().After(card.ExpiresAt) {
		_ = g.approvals.Expire(ctx, p.TenantID)
		return nil, contracts.Problem("conflict")
	}
	decided, err := g.approvals.Decide(ctx, p.TenantID, id, p.Account.ID, approve, strings.TrimSpace(note))
	if err != nil {
		return nil, err
	}
	if !decided {
		return nil, contracts.Problem("conflict")
	}
	trace, err := dbgen.New(g.pool).CapInvocationTrace(ctx, dbgen.CapInvocationTraceParams{TenantID: p.TenantID, InvocationID: card.InvocationID})
	if err != nil {
		return nil, err
	}
	if !approve {
		_ = g.finish(ctx, p.TenantID, card.InvocationID, contracts.StatusCancelled, nil, "rejected", 0)
		_ = g.approvals.Complete(ctx, p.TenantID, id, approval.Rejected, "rejected")
		return &contracts.Result{InvocationID: card.InvocationID, Status: contracts.StatusCancelled, ErrorCode: "rejected", TraceID: trace.TraceID, EventIDs: []string{}}, nil
	}
	entry, ok := g.registry.Resolve(card.Capability, card.Version)
	if !ok {
		_ = g.approvals.Complete(ctx, p.TenantID, id, approval.Failed, "unavailable")
		return nil, contracts.Problem("unavailable")
	}
	if entry.Definition.TransientResult && !entry.Definition.ReadOnly {
		_ = g.finish(ctx, p.TenantID, card.InvocationID, contracts.StatusFailed, nil, "forbidden", 0)
		_ = g.approvals.Complete(ctx, p.TenantID, id, approval.Failed, "forbidden")
		return nil, contracts.Problem("forbidden")
	}
	input, err := g.openApprovalArguments(card, entry.Definition)
	if err != nil || entry.Definition.Input.Validate(input) != nil {
		_ = g.approvals.Complete(ctx, p.TenantID, id, approval.Failed, "invalid")
		_ = g.finish(ctx, p.TenantID, card.InvocationID, contracts.StatusFailed, nil, "invalid", 0)
		return nil, contracts.Problem("invalid")
	}
	var requester *identity.Principal
	if trace.MachineCredentialID != "" {
		requester, err = g.identity.PrincipalForMachineInvocation(ctx, p.TenantID, card.AccountID, trace.MachineCredentialID, trace.AgentDelegationID)
	} else {
		requester, err = g.identity.PrincipalForBoundTask(ctx, p.TenantID, card.AccountID, card.IdentityBindingID)
	}
	if err != nil {
		_ = g.approvals.Complete(ctx, p.TenantID, id, approval.Failed, "forbidden")
		_ = g.finish(ctx, p.TenantID, card.InvocationID, contracts.StatusFailed, nil, "forbidden", 0)
		return nil, contracts.Problem("forbidden")
	}
	resumed := requestFrom(card, trace, input)
	if err = g.checkEntry(ctx, dbgen.New(g.pool), requester, entry, resumed, false); err != nil {
		_ = g.approvals.Complete(ctx, p.TenantID, id, approval.Failed, contracts.Code(err))
		_ = g.finish(ctx, p.TenantID, card.InvocationID, contracts.StatusFailed, nil, contracts.Code(err), 0)
		return nil, err
	}
	if err = checkInputAccess(requester, entry.Definition, input); err != nil {
		_ = g.approvals.Complete(ctx, p.TenantID, id, approval.Failed, "forbidden")
		_ = g.finish(ctx, p.TenantID, card.InvocationID, contracts.StatusFailed, nil, "forbidden", 0)
		return nil, err
	}
	if (entry.Definition.Permission != "" && !requester.Can(entry.Definition.Permission)) || !callerOf(requester).Allowed(entry.Definition.Permission) {
		_ = g.approvals.Complete(ctx, p.TenantID, id, approval.Failed, "forbidden")
		_ = g.finish(ctx, p.TenantID, card.InvocationID, contracts.StatusFailed, nil, "forbidden", 0)
		return nil, contracts.Problem("forbidden")
	}
	// A workflow cancelled while this card waited must not execute afterwards.
	if runStatus, e := dbgen.New(g.pool).OrchRunForInvocation(ctx, dbgen.OrchRunForInvocationParams{TenantID: p.TenantID, AccountID: card.AccountID, InvocationID: card.InvocationID}); e == nil {
		if runStatus != "running" && runStatus != "awaiting_approval" {
			return nil, contracts.Problem("conflict")
		}
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return nil, e
	}
	result, err := g.run(identity.WithPrincipal(ctx, requester), requester, entry, resumed, card.InvocationID, trace.TraceID, card.Digest, nil, true)
	if err != nil {
		_ = g.approvals.Complete(ctx, p.TenantID, id, approval.Failed, contracts.Code(err))
		return nil, err
	}
	status := approval.Executed
	if result.Status != contracts.StatusSucceeded && result.Status != contracts.StatusExecuting {
		status = approval.Failed
	}
	_ = g.approvals.Complete(ctx, p.TenantID, id, status, result.ErrorCode)
	if trace.MachineCredentialID != "" {
		result.Data = nil
	}
	return result, nil
}

func requestFrom(card *approval.Approval, trace dbgen.CapInvocationTraceRow, input contracts.Input) Request {
	return Request{
		Name: card.Capability, Version: card.Version, Input: input,
		Origin: contracts.OriginApproval, CorrelationID: trace.TraceID,
		authorizationOrigin: contracts.Origin(trace.Origin),
		CausationID:         trace.CausationID, TriggerDepth: int(trace.TriggerDepth),
	}
}

// Definitions is the catalogs a caller may use.
func (g *Gateway) Definitions(ctx context.Context) ([]contracts.Definition, error) {
	p, err := identity.Current(ctx)
	if err != nil {
		return nil, contracts.Problem("unauthenticated")
	}
	disabled, err := g.disabledResources(ctx, p)
	if err != nil {
		return nil, err
	}
	out := []contracts.Definition{}
	for _, d := range g.registry.Discover(callerOf(p)) {
		if !disabled[d.Entity] && g.available(ctx, dbgen.New(g.pool), p, d, false) == nil {
			d.Input = contracts.AuthorizedSchema(d.Input, d.InputAccess, p.Can)
			if d.Input == nil {
				continue
			}
			d.Output = contracts.AuthorizedSchema(d.Output, d.OutputAccess, p.Can)
			if d.Output == nil {
				continue
			}
			out = append(out, d)
		}
	}
	return out, nil
}

// AutomatedDefinitions returns the same eligible capability catalog used by the AI assistant.
func (g *Gateway) AutomatedDefinitions(ctx context.Context) ([]contracts.Definition, error) {
	p, err := identity.Current(ctx)
	if err != nil {
		return nil, contracts.Problem("unauthenticated")
	}
	disabled, err := g.disabledResources(ctx, p)
	if err != nil {
		return nil, err
	}
	discovered := g.registry.DiscoverAutomated(callerOf(p))
	return slices.DeleteFunc(discovered, func(d contracts.Definition) bool {
		return disabled[d.Entity] || (p.ActingOrganization.OrganizationID != 0 && !slices.Contains(d.Tags, contracts.OrganizationScopedTag))
	}), nil
}

// Entities is the metadata projection for the caller.
func (g *Gateway) Entities(ctx context.Context) ([]contracts.Entity, error) {
	allowed := map[string]bool{}
	definitions, err := g.Definitions(ctx)
	if err != nil {
		return nil, err
	}
	for _, definition := range definitions {
		for _, name := range definition.EntityNames() {
			allowed[name] = true
		}
	}
	out := []contracts.Entity{}
	for _, entity := range g.registry.Entities() {
		if allowed[entity.Name] {
			out = append(out, entity)
		}
	}
	return out, nil
}

// Invocations lists the caller's own audit trail.
func (g *Gateway) Invocations(ctx context.Context, limit int) ([]audit.Invocation, error) {
	p, err := identity.Current(ctx)
	if err != nil {
		return nil, contracts.Problem("unauthenticated")
	}
	return g.audit.List(ctx, p.TenantID, p.Account.ID, p.IdentityBindingID, limit)
}

// Invocation returns one audited call.
func (g *Gateway) Invocation(ctx context.Context, invocationID string) (*audit.Invocation, error) {
	p, err := identity.Current(ctx)
	if err != nil {
		return nil, contracts.Problem("unauthenticated")
	}
	return g.audit.Get(ctx, p.TenantID, p.Account.ID, p.IdentityBindingID, invocationID)
}

// Approvals lists pending confirmations for the caller or, for reviewers, all.
func (g *Gateway) Approvals(ctx context.Context) ([]approval.Approval, error) {
	p, err := identity.Current(ctx)
	if err != nil {
		return nil, contracts.Problem("unauthenticated")
	}
	if err = g.approvals.Expire(ctx, p.TenantID); err != nil {
		return nil, err
	}
	return g.approvals.List(ctx, p.TenantID, p.Account.ID, p.IdentityBindingID, tenantApprovalReviewer(p))
}

// Events lists business events the caller may see, with their AI trigger status.
func (g *Gateway) Events(ctx context.Context, limit int) ([]eventhub.Event, error) {
	p, err := identity.Current(ctx)
	if err != nil {
		return nil, contracts.Problem("unauthenticated")
	}
	return g.events.List(ctx, p.TenantID, p.Account.ID, p.IdentityBindingID, tenantApprovalReviewer(p), limit)
}

func tenantApprovalReviewer(p *identity.Principal) bool {
	return p.IdentityBindingID == "" && p.MachineCredentialID() == "" && p.HasTenantWideGrant("capability:approve")
}

func callerOf(p *identity.Principal) contracts.Caller {
	return contracts.Caller{
		TenantID: p.TenantID, AccountID: p.Account.ID,
		Admin: p.Account.IsAdmin, RestrictAdmin: true, Grants: p.Effective(),
	}
}

func identifier(prefix string) string {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return prefix + "unknown"
	}
	return prefix + hex.EncodeToString(raw[:])
}

func approvalActionKey(r Request) string {
	if strings.HasPrefix(r.IdempotencyKey, "orch:") || strings.HasPrefix(r.Name, "orchestration.workflow.") {
		return r.IdempotencyKey
	}
	return ""
}
