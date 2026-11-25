package identity

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"horizon/foundation/kernel/audit"
	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/capability/registry"
	"horizon/internal/repository/dbgen"
)

const loginMergeEntity = "login_merge_review"

func (s *Service) loginMergeEntries() []registry.Entry {
	id := contracts.Str(36, 36)
	text := &contracts.Schema{Type: "string"}
	item := contracts.Object(map[string]*contracts.Schema{
		"id": id, "primaryUsername": text, "secondaryUsername": text, "status": text, "reason": text, "decisionReason": text,
		"createdAt": text, "expiresAt": text, "canConfirm": {Type: "boolean"},
	}, "id", "primaryUsername", "secondaryUsername", "status", "reason", "decisionReason", "createdAt", "expiresAt", "canConfirm")
	list := readDefinition("identity.loginMerge.list", "List login merge reviews", "List reviews for the current login or authorized workspace reviewer.",
		loginMergeEntity, "", contracts.Object(map[string]*contracts.Schema{"items": contracts.Array(item, 0, 50), "nextBefore": contracts.Str(0, 36)}, "items", "nextBefore"))
	list.Tags, list.AllowedOrigins = []string{"identity", "manual-only"}, []contracts.Origin{contracts.OriginHumanUI}
	list.Input = contracts.Object(map[string]*contracts.Schema{"reviewer": {Type: "boolean"}, "before": id})
	entries := []registry.Entry{{Definition: list, Handler: contracts.Handler{Execute: s.listLoginMerges}}}
	for _, action := range []string{"request", "confirm", "decide"} {
		properties := map[string]*contracts.Schema{"password": contracts.Str(1, 128), "factor": contracts.Str(6, 32)}
		required := []string{"password"}
		permission := ""
		switch action {
		case "request":
			properties["username"], properties["realm"] = contracts.Str(3, 80), contracts.Str(1, 63)
			properties["reason"] = contracts.Str(5, 500)
			required = append(required, "username", "reason")
		case "confirm":
			properties["id"] = id
			required = append(required, "id")
		case "decide":
			properties["id"], properties["decision"], properties["reason"] = id, contracts.Enum("apply", "reject", "rollback"), contracts.Str(5, 500)
			required = append(required, "id", "decision", "reason")
			permission = "access:manage"
		}
		name := "identity.loginMerge." + action
		definition := identityWrite(name, "Review duplicate login accounts", "Verify or decide a reversible association between workspace login accounts.",
			loginMergeEntity, permission, []string{name + ".completed"}, contracts.Object(properties, required...), "id", "decision")
		definition.Tags, definition.AllowedOrigins = list.Tags, list.AllowedOrigins
		definition.Output = contracts.Object(map[string]*contracts.Schema{"id": id, "status": text}, "id", "status")
		definition.TransientResult = true
		definition.Entities = []string{loginMergeEntity, loginAssociationEntity, "login_account", "tenant_membership", "session"}
		entries = append(entries, registry.Entry{Definition: definition, Handler: contracts.Handler{ExecuteTx: s.changeLoginMerge(action)}})
	}
	return entries
}

func (s *Service) listLoginMerges(ctx context.Context, input contracts.Input) (contracts.Output, error) {
	var request struct {
		Reviewer bool
		Before   string
	}
	var before pgtype.UUID
	if contracts.Bind(input, &request) != nil || request.Before != "" && (before.Scan(request.Before) != nil || !before.Valid) {
		return contracts.Output{}, problem("invalid")
	}
	actor, err := Current(ctx)
	if err != nil || actor.IdentityBindingID != "" {
		return contracts.Output{}, problem("forbidden")
	}
	if request.Reviewer {
		actor, err = s.workspaceIAMAdmin(ctx, s.repo.q)
		if err != nil {
			return contracts.Output{}, err
		}
	}
	login, _, err := sessionLoginSubject(actor)
	if err != nil {
		return contracts.Output{}, err
	}
	rows, err := s.repo.q.IAMMergeList(ctx, dbgen.IAMMergeListParams{
		TenantID: actor.TenantID, LoginID: login, Reviewer: request.Reviewer, BeforeID: before,
	})
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	next := ""
	if len(rows) > 50 {
		rows = rows[:50]
		next = rows[49].ID.String()
	}
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, map[string]any{
			"id": row.ID.String(), "primaryUsername": row.PrimaryUsername, "secondaryUsername": row.SecondaryUsername, "decisionReason": row.DecisionReason,
			"status": row.Status, "reason": row.Reason, "createdAt": timeValue(row.CreatedAt), "expiresAt": timeValue(row.ExpiresAt), "canConfirm": row.CanConfirm,
		})
	}
	return contracts.Output{Data: map[string]any{"items": items, "nextBefore": next}}, nil
}

type loginMergeInput struct {
	ID, Username, Password, Decision, Reason string
	Realm, Factor                            *string
}

func (s *Service) changeLoginMerge(action string) contracts.TxExecutor {
	return func(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
		var request loginMergeInput
		if contracts.Bind(input, &request) != nil {
			return contracts.Output{}, problem("invalid")
		}
		request.Reason = strings.TrimSpace(request.Reason)
		q := dbgen.New(tx)
		// Serialize merge decisions before locking any participating login.
		if err := q.RuntimeTenantLock(ctx, "iam:login-merge"); err != nil {
			return contracts.Output{}, safe(err)
		}
		actor, login, subject, err := s.associationOwner(ctx, q, request.Password, request.Factor)
		if err != nil {
			return contracts.Output{}, err
		}
		if action == "request" {
			return s.requestLoginMerge(ctx, q, actor, login, subject, request)
		}
		var id pgtype.UUID
		if id.Scan(request.ID) != nil || !id.Valid {
			return contracts.Output{}, problem("invalid")
		}
		review, err := q.IAMMergeReviewLock(ctx, dbgen.IAMMergeReviewLockParams{ID: id, TenantID: actor.TenantID})
		if errors.Is(err, pgx.ErrNoRows) {
			return contracts.Output{}, problem("forbidden")
		}
		if err != nil {
			return contracts.Output{}, safe(err)
		}
		if action == "confirm" {
			if review.SecondaryLoginID != login || review.SecondaryPrincipalID != subject || review.Status != "pending" || !review.ExpiresAt.Time.After(time.Now()) {
				return contracts.Output{}, problem("forbidden")
			}
			device, err := mergeProofDevice(ctx, q, actor, login)
			if err != nil {
				return contracts.Output{}, err
			}
			if err = q.IAMMergeConfirm(ctx, dbgen.IAMMergeConfirmParams{ID: id, SecondaryDeviceID: device}); err != nil {
				return contracts.Output{}, safe(err)
			}
			return loginMergeOutput(id, "confirmed", action), nil
		}
		if _, err = s.workspaceIAMAdmin(ctx, q); err != nil || login == review.PrimaryLoginID || login == review.SecondaryLoginID ||
			subject == review.PrimaryPrincipalID || subject == review.SecondaryPrincipalID {
			return contracts.Output{}, problem("forbidden")
		}
		if len(request.Reason) < 5 {
			return contracts.Output{}, problem("invalid")
		}
		return s.decideLoginMerge(ctx, q, actor, review, request)
	}
}

func mergeProofDevice(ctx context.Context, q *dbgen.Queries, actor *Principal, login pgtype.UUID) (pgtype.UUID, error) {
	device, err := q.IAMMergeProofDevice(ctx, dbgen.IAMMergeProofDeviceParams{
		TokenHash: actor.TokenHash, CsrfToken: actor.CSRF, LoginID: login,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return device, problem("forbidden")
	}
	return device, safe(err)
}

func (s *Service) requestLoginMerge(ctx context.Context, q *dbgen.Queries, actor *Principal, login, subject pgtype.UUID, request loginMergeInput) (contracts.Output, error) {
	username, realm := strings.ToLower(strings.TrimSpace(request.Username)), authenticationRealm(request.Realm)
	if !invitationUsername.MatchString(username) || !loginRealmPattern.MatchString(realm) || len(request.Reason) < 5 {
		return contracts.Output{}, problem("invalid")
	}
	other, err := q.IAMMergeLookupLogin(ctx, dbgen.IAMMergeLookupLoginParams{TenantID: actor.TenantID, Username: username, Realm: realm})
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (other.PrincipalID == subject || s.platformOperators[subject.String()] ||
		s.platformOperators[other.PrincipalID.String()] || actor.Account.IsAdmin) {
		return contracts.Output{}, problem("forbidden")
	}
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	device, err := mergeProofDevice(ctx, q, actor, login)
	if err != nil {
		return contracts.Output{}, err
	}
	id, err := q.IAMMergeCreate(ctx, dbgen.IAMMergeCreateParams{
		TenantID: actor.TenantID, PrimaryLoginID: login, PrimaryPrincipalID: subject,
		SecondaryLoginID: other.ID, SecondaryPrincipalID: other.PrincipalID, PrimaryDeviceID: device, Reason: request.Reason,
	})
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	return loginMergeOutput(id, "pending", "request"), nil
}

func (s *Service) decideLoginMerge(ctx context.Context, q *dbgen.Queries, actor *Principal, review dbgen.IamLoginMergeReview, request loginMergeInput) (contracts.Output, error) {
	status, association := "rejected", review.AssociationID
	var err error
	switch request.Decision {
	case "reject":
		if review.Status != "pending" && review.Status != "confirmed" {
			return contracts.Output{}, problem("conflict")
		}
	case "apply":
		if review.Status != "confirmed" || !review.ExpiresAt.Time.After(time.Now()) {
			return contracts.Output{}, problem("conflict")
		}
		association, err = s.applyLoginMerge(ctx, q, review, false)
		status = "applied"
	case "rollback":
		if review.Status != "applied" {
			return contracts.Output{}, problem("conflict")
		}
		association, err = s.applyLoginMerge(ctx, q, review, true)
		status = "rolled_back"
	}
	if err != nil {
		return contracts.Output{}, err
	}
	if err = q.IAMMergeComplete(ctx, dbgen.IAMMergeCompleteParams{
		ID: review.ID, Status: status, DecisionReason: pgtype.Text{String: request.Reason, Valid: true}, AssociationID: association,
	}); err != nil {
		return contracts.Output{}, safe(err)
	}
	if err = q.SecurityAuditAppend(ctx, dbgen.SecurityAuditAppendParams{
		TenantID: actor.TenantID, AccountID: actor.Account.ID,
		Action: "identity.loginMerge." + request.Decision, Outcome: "allowed", Reason: request.Reason,
		CorrelationID: audit.Correlation(ctx), ResourceType: loginMergeEntity, ResourceID: review.ID.String(),
	}); err != nil {
		return contracts.Output{}, safe(err)
	}
	return loginMergeOutput(review.ID, status, "decide"), nil
}

func (s *Service) applyLoginMerge(ctx context.Context, q *dbgen.Queries, review dbgen.IamLoginMergeReview, rollback bool) (pgtype.UUID, error) {
	primary, secondary := review.PrimaryPrincipalID, review.SecondaryPrincipalID
	if s.platformOperators[primary.String()] || s.platformOperators[secondary.String()] {
		return pgtype.UUID{}, problem("forbidden")
	}
	logins, err := q.IAMMergeLockLogins(ctx, dbgen.IAMMergeLockLoginsParams{PrimaryID: review.PrimaryLoginID, SecondaryID: review.SecondaryLoginID})
	if err != nil || len(logins) != 2 {
		return pgtype.UUID{}, problem("conflict")
	}
	principals, err := q.IAMMergeLockPrincipals(ctx, dbgen.IAMMergeLockPrincipalsParams{PrimaryID: primary, SecondaryID: secondary})
	if err != nil || len(principals) != 2 {
		return pgtype.UUID{}, problem("conflict")
	}
	if _, err = q.IAMMergeLockMemberships(ctx, dbgen.IAMMergeLockMembershipsParams{PrimaryID: primary, SecondaryID: secondary}); err != nil {
		return pgtype.UUID{}, safe(err)
	}
	compatible, err := q.IAMMergeCompatible(ctx, dbgen.IAMMergeCompatibleParams{ID: review.ID, Rollback: rollback})
	if err != nil {
		return pgtype.UUID{}, safe(err)
	}
	if !compatible {
		return pgtype.UUID{}, problem("conflict")
	}
	if err = q.IAMMergeRevokeRuntime(ctx, dbgen.IAMMergeRevokeRuntimeParams{
		PrimaryLogin: review.PrimaryLoginID, SecondaryLogin: review.SecondaryLoginID,
		PrimaryPrincipal: primary, SecondaryPrincipal: secondary,
	}); err != nil {
		return pgtype.UUID{}, safe(err)
	}
	from, to := secondary, primary
	if rollback {
		from, to = to, from
	}
	if _, err = q.IAMMergeMoveLogin(ctx, dbgen.IAMMergeMoveLoginParams{FromID: from, ToID: to, LoginID: review.SecondaryLoginID}); err != nil {
		return pgtype.UUID{}, safe(err)
	}
	if rollback {
		return review.AssociationID, safe(q.IAMMarkLoginAssociationRevoked(ctx, review.AssociationID))
	}
	id, err := q.IAMRecordLoginAssociation(ctx, dbgen.IAMRecordLoginAssociationParams{
		PrincipalID: primary, OriginalPrincipalID: secondary, PrimaryLoginID: review.PrimaryLoginID, AssociatedLoginID: review.SecondaryLoginID,
	})
	return id, safe(err)
}

func loginMergeOutput(id pgtype.UUID, status, action string) contracts.Output {
	return contracts.Output{Data: map[string]any{"id": id.String(), "status": status}, Events: []contracts.Event{{
		Type: "identity.loginMerge." + action + ".completed", AggregateType: loginMergeEntity, AggregateID: id.String(),
		Payload: map[string]any{"status": status},
	}}}
}
