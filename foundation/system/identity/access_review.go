package identity

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	genidentity "horizon/foundation/gen/identity"
	"horizon/foundation/kernel/audit"
	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/capability/registry"
	"horizon/internal/repository/dbgen"
)

const (
	reviewEntity              = "iam_access_review"
	reviewSnapshotEvent       = "identity.review.snapshot.created"
	reviewScheduledCapability = "identity.review.runDue"
)

func (s *Service) accessReviewEntries() []registry.Entry {
	id := contracts.Int(1, 1<<53-1)
	date := contracts.Str(1, 40)
	item := contracts.Object(map[string]*contracts.Schema{
		"id": id, "principalId": contracts.Str(36, 36), "kind": contracts.Enum("membership", "context_role"),
		"targetId": contracts.Str(36, 36), "roleId": {Type: "integer"}, "targetExpiresAt": {Type: "string"},
		"principalName": {Type: "string"}, "roleName": {Type: "string"},
		"username": {Type: "string"},
		"status":   {Type: "string"}, "createdAt": date, "reviewedAt": {Type: "string"},
		"reviewerId": {Type: "integer"}, "reason": {Type: "string"},
	}, "id", "principalId", "kind", "targetId", "status", "createdAt")
	list := readDefinition("identity.review.list", "List access reviews", "Read the current workspace's review queue and completed decisions.",
		reviewEntity, "access:manage", contracts.Object(map[string]*contracts.Schema{
			"items": contracts.Array(item, 0, 50), "nextAfter": contracts.Int(0, 1<<53-1),
		}, "items", "nextAfter"))
	list.Input = contracts.Object(map[string]*contracts.Schema{
		"after": id, "status": {Type: "string", Enum: []string{"pending", "retained", "revoked", "superseded"}},
	})
	list.TransientResult, list.Tags, list.AllowedOrigins = true, []string{"identity", "manual-only"}, []contracts.Origin{contracts.OriginHumanUI}

	write := func(name, title, event, entity string, input, output *contracts.Schema, loggable ...string) contracts.Definition {
		def := identityWrite(name, title, title, entity, "access:manage", []string{event}, input, loggable...)
		def.Output, def.Tags, def.AllowedOrigins = output, []string{"identity", "manual-only"}, []contracts.Origin{contracts.OriginHumanUI}
		return def
	}
	configure := write("identity.review.configure", "Set access review interval", "identity.review.schedule.changed", "tenant",
		contracts.Object(map[string]*contracts.Schema{"intervalDays": contracts.Int(1, 365)}, "intervalDays"),
		contracts.Object(map[string]*contracts.Schema{"nextAt": date}, "nextAt"), "intervalDays")
	start := write("identity.review.start", "Create access review snapshot", reviewSnapshotEvent, reviewEntity, contracts.Object(nil),
		contracts.Object(map[string]*contracts.Schema{"created": contracts.Int(0, 1<<53-1)}, "created"))
	start.Entities = []string{reviewEntity, "tenant"}
	scheduled := start
	scheduled.Name, scheduled.Title, scheduled.Executor = reviewScheduledCapability, "Create due access review snapshot", "go:"+reviewScheduledCapability
	scheduled.Input = contracts.Object(map[string]*contracts.Schema{"expectedDueAt": date}, "expectedDueAt")
	scheduled.Permission = ""
	scheduled.Tags, scheduled.AllowedOrigins = []string{"system-only", "durable-system"}, []contracts.Origin{contracts.OriginEvent}
	scheduled.Risk, scheduled.Approval = contracts.RiskLow, contracts.ApprovalAuto
	decision := write("identity.review.decide", "Decide access review", "identity.review.decided", reviewEntity,
		contracts.Object(map[string]*contracts.Schema{"id": id, "decision": {Type: "string", Enum: []string{"retain", "revoke"}}, "reason": contracts.Str(5, 500)}, "id", "decision", "reason"),
		contracts.Object(map[string]*contracts.Schema{"id": id, "status": {Type: "string"}}, "id", "status"),
		"id", "decision", "reason")
	decision.Entities = []string{reviewEntity, "tenant_membership", "context_role_grant"}
	return []registry.Entry{
		{Definition: list, Handler: contracts.Handler{Execute: s.listAccessReviews}},
		{Definition: configure, Handler: contracts.Handler{ExecuteTx: s.configureAccessReview}},
		{Definition: start, Handler: contracts.Handler{ExecuteTx: s.snapshotAccessReview(false)}},
		{Definition: scheduled, Handler: contracts.Handler{ExecuteTx: s.snapshotAccessReview(true)}},
		{Definition: decision, Handler: contracts.Handler{ExecuteTx: s.decideAccessReview}},
	}
}

func (s *Service) configureAccessReview(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
	q := dbgen.New(tx)
	actor, err := s.workspaceIAMAdmin(ctx, q)
	if err != nil {
		return contracts.Output{}, err
	}
	var request struct{ IntervalDays int32 }
	if err = contracts.Bind(input, &request); err != nil {
		return contracts.Output{}, problem("invalid")
	}
	next, err := q.IAMReviewConfigure(ctx, dbgen.IAMReviewConfigureParams{TenantID: actor.TenantID, IntervalDays: request.IntervalDays})
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	return contracts.Output{Data: map[string]any{"nextAt": timeValue(next)}, Events: []contracts.Event{{
		Type: "identity.review.schedule.changed", AggregateType: "tenant", AggregateID: actor.TenantID,
		Payload: map[string]any{"intervalDays": request.IntervalDays},
	}}}, nil
}

func (s *Service) snapshotAccessReview(scheduled bool) contracts.TxExecutor {
	return func(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
		q := dbgen.New(tx)
		actor, err := Current(ctx)
		if err != nil {
			return contracts.Output{}, err
		}
		var request struct{ ExpectedDueAt string }
		if scheduled {
			if !actor.AllowsSystemCapability(reviewScheduledCapability+"@"+identityVersion) ||
				contracts.Bind(input, &request) != nil || request.ExpectedDueAt == "" {
				return contracts.Output{}, problem("forbidden")
			}
			subject, lookupErr := q.IAMReviewService(ctx, actor.TenantID)
			if lookupErr != nil || subject.String() != actor.SubjectID {
				return contracts.Output{}, problem("forbidden")
			}
		} else if actor, err = s.workspaceIAMAdmin(ctx, q); err != nil {
			return contracts.Output{}, err
		}
		due, err := auditTime(request.ExpectedDueAt)
		if err != nil {
			return contracts.Output{}, problem("invalid")
		}
		created, err := q.IAMReviewSnapshot(ctx, dbgen.IAMReviewSnapshotParams{TenantID: actor.TenantID, ExpectedDueAt: due})
		if err != nil {
			return contracts.Output{}, safe(err)
		}
		if scheduled {
			authority, _ := json.Marshal(map[string]string{"principalId": actor.SubjectID, "origin": string(contracts.OriginEvent)})
			if err := q.SecurityAuditAppend(ctx, dbgen.SecurityAuditAppendParams{
				TenantID: actor.TenantID, Action: reviewScheduledCapability, Outcome: "allowed", CorrelationID: audit.Correlation(ctx),
				ResourceType: "tenant", ResourceID: actor.TenantID, Authority: authority,
			}); err != nil {
				return contracts.Output{}, safe(err)
			}
		}
		return contracts.Output{Data: map[string]any{"created": created}, Events: []contracts.Event{{
			Type: reviewSnapshotEvent, AggregateType: "tenant", AggregateID: actor.TenantID, Payload: map[string]any{"created": created},
		}}}, nil
	}
}

func (s *Service) listAccessReviews(ctx context.Context, input contracts.Input) (contracts.Output, error) {
	actor, err := s.workspaceIAMAdmin(ctx, s.repo.q)
	if err != nil {
		return contracts.Output{}, err
	}
	var request struct {
		After  int64
		Status string
	}
	if err = contracts.Bind(input, &request); err != nil {
		return contracts.Output{}, problem("invalid")
	}
	rows, err := s.repo.q.IAMReviewList(ctx, dbgen.IAMReviewListParams{TenantID: actor.TenantID, AfterID: request.After, Status: request.Status})
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	next := int64(0)
	if len(rows) > 50 {
		rows = rows[:50]
		next = rows[49].ID
	}
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, map[string]any{"id": row.ID, "principalId": row.PrincipalID.String(), "kind": row.Kind, "targetId": row.TargetID.String(),
			"roleId": row.RoleID.Int64, "targetExpiresAt": timeValue(row.TargetExpiresAt), "status": row.Status, "createdAt": timeValue(row.CreatedAt),
			"principalName": row.PrincipalName, "roleName": row.RoleName,
			"username":   row.Username,
			"reviewerId": row.ReviewerID.Int64, "reviewedAt": timeValue(row.ReviewedAt), "reason": row.Reason.String})
	}
	return contracts.Output{Data: map[string]any{"items": items, "nextAfter": next}}, nil
}

func (s *Service) decideAccessReview(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
	q := dbgen.New(tx)
	initial, err := Current(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	if err = q.IAMGrantWriteLock(ctx, initial.TenantID); err != nil {
		return contracts.Output{}, safe(err)
	}
	actor, err := s.workspaceIAMAdmin(ctx, q)
	if err != nil {
		return contracts.Output{}, err
	}
	_, principalID, err := sessionLoginSubject(actor)
	if err != nil {
		return contracts.Output{}, err
	}
	var request struct {
		ID               int64
		Decision, Reason string
	}
	if err = contracts.Bind(input, &request); err != nil || len(strings.TrimSpace(request.Reason)) < 5 {
		return contracts.Output{}, problem("invalid")
	}
	row, err := q.IAMReviewLock(ctx, dbgen.IAMReviewLockParams{TenantID: actor.TenantID, ID: request.ID, ReviewerPrincipal: principalID})
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, problem("forbidden")
	}
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	status := "retained"
	if request.Decision == "revoke" {
		status = "revoked"
	}
	switch row.Kind {
	case "membership":
		var checked dbgen.IAMReviewCheckMembershipRow
		checked, err = q.IAMReviewCheckMembership(ctx, dbgen.IAMReviewCheckMembershipParams{
			TenantID: actor.TenantID, ID: row.TargetID, ExpiresAt: row.TargetExpiresAt, RevokedAt: row.TargetRevokedAt,
		})
		if err == nil && status == "revoked" {
			if checked.Protected {
				return contracts.Output{}, problem("forbidden")
			}
			err = q.IAMReviewSuspendMembership(ctx, dbgen.IAMReviewSuspendMembershipParams{TenantID: actor.TenantID, ID: row.TargetID})
		}
	case "context_role":
		_, err = q.IAMReviewCheckContextRole(ctx, dbgen.IAMReviewCheckContextRoleParams{TenantID: actor.TenantID, ID: row.TargetID})
		if err == nil && status == "revoked" {
			_, err = q.IAMRevokeContextRoleGrant(ctx, dbgen.IAMRevokeContextRoleGrantParams{TenantID: actor.TenantID, ID: row.TargetID})
		}
	default:
		return contracts.Output{}, problem("unavailable")
	}
	if errors.Is(err, pgx.ErrNoRows) {
		status = "superseded"
	} else if err != nil {
		return contracts.Output{}, safe(err)
	}
	_, err = q.IAMReviewDecide(ctx, dbgen.IAMReviewDecideParams{TenantID: actor.TenantID, ID: row.ID,
		NextStatus: status, ReviewerID: actor.Account.ID, Reason: strings.TrimSpace(request.Reason)})
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	return contracts.Output{Data: map[string]any{"id": row.ID, "status": status}, Events: []contracts.Event{{
		Type: "identity.review.decided", AggregateType: reviewEntity, AggregateID: strconv.FormatInt(row.ID, 10),
		Payload: map[string]any{"status": status, "kind": row.Kind, "targetId": row.TargetID.String()},
	}}}, nil
}

type scheduledReviewInvoker func(context.Context, string, string, contracts.Input, string) (*contracts.Result, error)

func (s *Service) RunAccessReviews(ctx context.Context, invoke scheduledReviewInvoker) {
	if s.repo == nil || invoke == nil {
		return
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if err := s.processAccessReviews(ctx, invoke); err != nil && ctx.Err() == nil {
			slog.Error("access review scheduler failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) processAccessReviews(ctx context.Context, invoke scheduledReviewInvoker) error {
	due, err := s.repo.q.IAMReviewsDueTenants(ctx)
	if err != nil {
		return safe(err)
	}
	var failures error
	for _, item := range due {
		system, err := s.reviewServicePrincipal(ctx, item.ID)
		if err == nil {
			timestamp := item.NextReviewAt.Time.UTC().Format(time.RFC3339Nano)
			var result *contracts.Result
			result, err = invoke(WithPrincipal(ctx, system), reviewScheduledCapability, identityVersion, contracts.Input{"expectedDueAt": timestamp}, "access-review:"+timestamp)
			if err == nil && (result == nil || result.Status != contracts.StatusSucceeded) {
				err = problem("unavailable")
			}
		}
		failures = errors.Join(failures, err)
	}
	return failures
}

func (s *Service) reviewServicePrincipal(ctx context.Context, tenant string) (*Principal, error) {
	if err := s.repo.q.IAMProvisionReviewService(ctx, tenant); err != nil {
		return nil, safe(err)
	}
	subject, err := s.repo.q.IAMReviewService(ctx, tenant)
	if err != nil {
		return nil, safe(err)
	}
	principal, err := newPrincipal(&genidentity.AccountRecord{ID: 0}, tenant, subject.String(), nil, s.permissionCodes())
	if err != nil {
		return nil, safe(err)
	}
	principal.systemCapability = reviewScheduledCapability + "@" + identityVersion
	return principal, nil
}
