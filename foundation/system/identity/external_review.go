package identity

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/capability/registry"
	"horizon/internal/repository/dbgen"
)

const (
	externalReviewEntity    = "external_identity_review"
	externalReviewConfirmed = "identity.externalReview.confirmed"
	externalReviewDecided   = "identity.externalReview.decided"
)

func (s *Service) externalReviewEntries() []registry.Entry {
	id := contracts.Str(36, 36)
	text := &contracts.Schema{Type: "string"}
	item := contracts.Object(map[string]*contracts.Schema{
		"id": id, "status": text, "providerId": text, "createdAt": text, "expiresAt": text, "appliedAt": text,
		"reason": text, "originalOwner": {Type: "boolean"}, "requestedOwner": {Type: "boolean"},
	}, "id", "status", "providerId", "createdAt", "expiresAt", "appliedAt", "reason", "originalOwner", "requestedOwner")
	list := readDefinition("identity.externalReview.list", "List external identity reviews", "List current tenant reviews within the current login's authority.",
		externalReviewEntity, "", contracts.Object(map[string]*contracts.Schema{
			"items": contracts.Array(item, 0, 50), "nextAfter": contracts.Str(0, 36),
		}, "items", "nextAfter"))
	list.Input = contracts.Object(map[string]*contracts.Schema{"after": id, "reviewer": {Type: "boolean"}})
	confirm := identityWrite("identity.externalReview.confirm", "Confirm external identity transfer",
		"Confirm a transfer after signing in again using the original external identity.",
		externalReviewEntity, "", []string{externalReviewConfirmed}, contracts.Object(map[string]*contracts.Schema{"id": id}, "id"), "id")
	confirm.Output = contracts.Object(map[string]*contracts.Schema{"id": id, "status": text}, "id", "status")
	for _, definition := range []*contracts.Definition{&list, &confirm} {
		definition.Tags = []string{"identity", "manual-only", contracts.AuthenticatedActionTag}
		definition.AllowedOrigins = []contracts.Origin{contracts.OriginHumanUI}
	}
	decide := identityWrite("identity.externalReview.decide", "Decide external identity transfer",
		"An independent workspace reviewer may apply, reject or roll back the confirmed mapping change.",
		externalReviewEntity, "access:manage", []string{externalReviewDecided},
		contracts.Object(map[string]*contracts.Schema{
			"id": id, "decision": contracts.Enum("apply", "reject", "rollback"), "reason": contracts.Str(5, 500),
		}, "id", "decision", "reason"), "id", "decision", "reason")
	decide.Entities = []string{externalReviewEntity, "external_identity"}
	decide.Output = contracts.Object(map[string]*contracts.Schema{"id": id, "status": text}, "id", "status")
	decide.Tags = []string{"identity", "manual-only"}
	decide.AllowedOrigins = []contracts.Origin{contracts.OriginHumanUI}
	return []registry.Entry{
		{Definition: list, Handler: contracts.Handler{Execute: s.listExternalReviews}},
		{Definition: confirm, Handler: contracts.Handler{ExecuteTx: s.confirmExternalReview}},
		{Definition: decide, Handler: contracts.Handler{ExecuteTx: s.decideExternalReview}},
	}
}

func (s *Service) listExternalReviews(ctx context.Context, input contracts.Input) (contracts.Output, error) {
	var request struct {
		After    string
		Reviewer bool
	}
	var after pgtype.UUID
	if err := contracts.Bind(input, &request); err != nil || request.After != "" && after.Scan(request.After) != nil {
		return contracts.Output{}, problem("invalid")
	}
	actor, err := Current(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	if request.Reviewer {
		actor, err = s.workspaceIAMAdmin(ctx, s.repo.q)
		if err != nil {
			return contracts.Output{}, err
		}
	}
	login, _, err := sessionLoginSubject(actor)
	if err != nil || actor.IdentityBindingID != "" {
		return contracts.Output{}, problem("forbidden")
	}
	rows, err := s.repo.q.IAMListExternalReviews(ctx, dbgen.IAMListExternalReviewsParams{
		TenantID: actor.TenantID, LoginID: login, Reviewer: request.Reviewer, AfterID: after,
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
			"id": row.ID.String(), "status": row.Status, "providerId": row.ProviderID, "reason": row.Reason,
			"createdAt": timeValue(row.CreatedAt), "expiresAt": timeValue(row.ExpiresAt), "appliedAt": timeValue(row.AppliedAt),
			"originalOwner": row.OriginalOwner, "requestedOwner": row.RequestedOwner,
		})
	}
	return contracts.Output{Data: map[string]any{"items": items, "nextAfter": next}}, nil
}

func (s *Service) confirmExternalReview(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
	var id pgtype.UUID
	if err := id.Scan(input["id"]); err != nil || !id.Valid {
		return contracts.Output{}, problem("invalid")
	}
	q := dbgen.New(tx)
	actor, login, err := s.lockMFAAccount(ctx, q)
	if err != nil || actor.IdentityBindingID != "" {
		return contracts.Output{}, problem("forbidden")
	}
	if _, err = q.IAMConfirmExternalReview(ctx, dbgen.IAMConfirmExternalReviewParams{
		TenantID: actor.TenantID, ID: id, LoginID: login, SessionHash: actor.TokenHash, CsrfToken: actor.CSRF,
	}); errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, problem("forbidden")
	} else if err != nil {
		return contracts.Output{}, safe(err)
	}
	return externalReviewOutput(id, "confirmed", externalReviewConfirmed), nil
}

func (s *Service) decideExternalReview(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
	var request struct{ ID, Decision, Reason string }
	var id pgtype.UUID
	if err := contracts.Bind(input, &request); err != nil || id.Scan(request.ID) != nil || !id.Valid || len(strings.TrimSpace(request.Reason)) < 5 {
		return contracts.Output{}, problem("invalid")
	}
	q := dbgen.New(tx)
	actor, err := s.workspaceIAMAdmin(ctx, q)
	if err != nil {
		return contracts.Output{}, err
	}
	login, _, err := sessionLoginSubject(actor)
	if err != nil {
		return contracts.Output{}, err
	}
	row, err := q.IAMLockExternalReview(ctx, dbgen.IAMLockExternalReviewParams{TenantID: actor.TenantID, ID: id, ReviewerLoginID: login})
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, problem("forbidden")
	}
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	var applied pgtype.Timestamptz
	status := "rejected"
	switch request.Decision {
	case "reject":
		if row.Status != "pending" && row.Status != "confirmed" {
			return contracts.Output{}, problem("conflict")
		}
	case "apply", "rollback":
		if request.Decision == "apply" && row.Status != "confirmed" || request.Decision == "rollback" && row.Status != "applied" {
			return contracts.Output{}, problem("conflict")
		}
		from, to, expected := row.OriginalLoginID, row.RequestedLoginID, row.OriginalLinkedAt
		status = "applied"
		if request.Decision == "rollback" {
			from, to, expected = to, from, row.AppliedAt
			status = "rolled_back"
		}
		applied, err = q.IAMTransferExternalIdentity(ctx, dbgen.IAMTransferExternalIdentityParams{
			TenantID: actor.TenantID, ExternalID: row.ExternalIdentityID, PreviousLoginID: from, NextLoginID: to, ExpectedLinkedAt: expected,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return contracts.Output{}, problem("conflict")
		}
		if err != nil {
			return contracts.Output{}, safe(err)
		}
		if err = q.IAMRevokeExternalSessions(ctx, row.ExternalIdentityID); err != nil {
			return contracts.Output{}, safe(err)
		}
	}
	if _, err = q.IAMDecideExternalReview(ctx, dbgen.IAMDecideExternalReviewParams{
		TenantID: actor.TenantID, ID: id, ReviewerLoginID: login, Status: status, Reason: strings.TrimSpace(request.Reason), AppliedAt: applied,
	}); err != nil {
		return contracts.Output{}, safe(err)
	}
	return externalReviewOutput(id, status, externalReviewDecided), nil
}

func externalReviewOutput(id pgtype.UUID, status, event string) contracts.Output {
	return contracts.Output{Data: map[string]any{"id": id.String(), "status": status}, Events: []contracts.Event{{
		Type: event, AggregateType: externalReviewEntity, AggregateID: id.String(), Payload: map[string]any{"status": status},
	}}}
}
