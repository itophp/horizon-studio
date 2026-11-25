package identity

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/capability/registry"
	"horizon/internal/repository/dbgen"
)

func (s *Service) externalIdentityEntries() []registry.Entry {
	item := contracts.Object(map[string]*contracts.Schema{
		"id": {Type: "string"}, "providerId": {Type: "string"}, "linkedAt": {Type: "string"}, "revokedAt": {Type: "string"},
	}, "id", "providerId", "linkedAt", "revokedAt")
	list := contracts.Definition{
		Name: "identity.externalIdentity.list", Version: identityVersion, Title: "List external logins",
		Description: "List providers associated with the current login account without disclosing external subject identifiers.",
		Domain:      "identity", Entity: "external_identity", Layer: contracts.LayerAtomic, Executor: "go:identity.externalIdentity.list",
		Risk: contracts.RiskLow, Approval: contracts.ApprovalAuto, ReadOnly: true, TransientResult: true,
		Input: contracts.Object(nil), Output: contracts.Object(map[string]*contracts.Schema{"items": {Type: "array", Items: item}}, "items"),
		Tags: []string{"identity", "manual-only", contracts.AuthenticatedActionTag}, AllowedOrigins: []contracts.Origin{contracts.OriginHumanUI},
	}
	link := identityWrite("identity.externalIdentity.link", "Link an external login", "Verify local ownership and consume a short-lived proof from a completed external authentication. Conflicts are audited for manual review.",
		"external_identity", "", []string{"identity.externalIdentity.linked", "identity.externalIdentity.conflict"},
		contracts.Object(map[string]*contracts.Schema{"proof": contracts.Str(64, 64), "password": contracts.Str(1, 128)}, "proof", "password"))
	link.Entities = []string{"external_identity", externalReviewEntity}
	link.Output = contracts.Object(map[string]*contracts.Schema{"id": {Type: "string"}, "linked": {Type: "boolean"}, "reviewRequired": {Type: "boolean"}, "reviewId": contracts.Str(36, 36)}, "linked", "reviewRequired")
	unlink := identityWrite("identity.externalIdentity.unlink", "Unlink an external login", "Reauthenticate and revoke an external login mapping while retaining the account's local password credential.",
		"external_identity", "", []string{"identity.externalIdentity.unlinked"},
		contracts.Object(map[string]*contracts.Schema{"id": contracts.Str(36, 36), "password": contracts.Str(1, 128)}, "id", "password"), "id")
	unlink.Output = contracts.Object(map[string]*contracts.Schema{"id": {Type: "string"}}, "id")
	externalOnly := identityWrite("identity.externalIdentity.requireExternal", "Require external sign-in",
		"Remove the password after a fresh sign-in with an already linked identity provider.",
		"password_credential", "", []string{"identity.externalIdentity.passwordRemoved"},
		contracts.Object(map[string]*contracts.Schema{"confirm": {Type: "boolean"}}, "confirm"))
	externalOnly.Output = contracts.Object(map[string]*contracts.Schema{"enabled": {Type: "boolean"}}, "enabled")
	for _, definition := range []*contracts.Definition{&link, &unlink, &externalOnly} {
		definition.Tags = append(definition.Tags, "manual-only", contracts.AuthenticatedActionTag)
		definition.AllowedOrigins = []contracts.Origin{contracts.OriginHumanUI}
	}
	return []registry.Entry{
		{Definition: list, Handler: contracts.Handler{Execute: s.listExternalIdentities}},
		{Definition: link, Handler: contracts.Handler{ExecuteTx: s.linkExternalIdentity}},
		{Definition: unlink, Handler: contracts.Handler{ExecuteTx: s.unlinkExternalIdentity}},
		{Definition: externalOnly, Handler: contracts.Handler{ExecuteTx: s.requireExternalLogin}},
	}
}

func (s *Service) requireExternalLogin(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
	if input["confirm"] != true {
		return contracts.Output{}, problem("invalid")
	}
	present, err := Current(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	q := dbgen.New(tx)
	actor, err := s.WithReadQueries(q).PrincipalForSessionInvocation(ctx, present, false)
	if err != nil || actor.IdentityBindingID != "" {
		return contracts.Output{}, problem("forbidden")
	}
	login, _, err := sessionLoginSubject(actor)
	if err != nil {
		return contracts.Output{}, err
	}
	if _, err = q.IAMLockActiveLogin(ctx, login); err != nil {
		return contracts.Output{}, problem("forbidden")
	}
	id, err := q.IAMRemovePasswordForExternalLogin(ctx, dbgen.IAMRemovePasswordForExternalLoginParams{
		LoginID: login, SessionHash: actor.TokenHash, CsrfToken: actor.CSRF,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, problem("forbidden")
	}
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	return contracts.Output{Data: map[string]any{"enabled": true}, Events: []contracts.Event{{
		Type: "identity.externalIdentity.passwordRemoved", AggregateType: "login_account", AggregateID: id.String(),
	}}}, nil
}

func (s *Service) listExternalIdentities(ctx context.Context, _ contracts.Input) (contracts.Output, error) {
	principal, err := Current(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	loginID, _, err := sessionLoginSubject(principal)
	if err != nil {
		return contracts.Output{}, err
	}
	rows, err := s.repo.q.IAMListExternalIdentities(ctx, loginID)
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, map[string]any{"id": row.ID.String(), "providerId": row.ProviderID, "linkedAt": timeValue(row.LinkedAt), "revokedAt": timeValue(row.RevokedAt)})
	}
	return contracts.Output{Data: map[string]any{"items": items}}, nil
}

func (s *Service) linkExternalIdentity(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
	var request struct{ Proof, Password string }
	if err := contracts.Bind(input, &request); err != nil {
		return contracts.Output{}, problem("invalid")
	}
	queries := dbgen.New(tx)
	actor, err := s.delegationOwner(ctx, queries, request.Password)
	if err != nil {
		return contracts.Output{}, err
	}
	loginID, _, err := sessionLoginSubject(actor)
	if err != nil {
		return contracts.Output{}, err
	}
	proof, err := queries.IAMConsumeExternalProof(ctx, tokenHash(request.Proof))
	if err != nil {
		return contracts.Output{}, problem("forbidden")
	}
	if s.externalIssuer(proof.ProviderID) == "" || s.externalIssuer(proof.ProviderID) != proof.Issuer {
		return contracts.Output{}, problem("forbidden")
	}
	id, err := queries.IAMLinkExternalIdentity(ctx, dbgen.IAMLinkExternalIdentityParams{
		ProviderID: proof.ProviderID, Issuer: proof.Issuer, Subject: proof.Subject, LoginAccountID: loginID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		data := map[string]any{"linked": false, "reviewRequired": true}
		review, reviewErr := queries.IAMOpenExternalReview(ctx, dbgen.IAMOpenExternalReviewParams{
			TenantID: actor.TenantID, RequestedLoginID: loginID, ProviderID: proof.ProviderID, Issuer: proof.Issuer, Subject: proof.Subject,
		})
		if reviewErr == nil {
			data["reviewId"] = review.String()
		} else if !errors.Is(reviewErr, pgx.ErrNoRows) {
			return contracts.Output{}, safe(reviewErr)
		}
		return contracts.Output{Data: data, Events: []contracts.Event{{
			Type: "identity.externalIdentity.conflict", AggregateType: "login_account", AggregateID: loginID.String(), Payload: map[string]any{"providerId": proof.ProviderID},
		}}}, nil
	}
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	return contracts.Output{Data: map[string]any{"id": id.String(), "linked": true, "reviewRequired": false}, Events: []contracts.Event{{
		Type: "identity.externalIdentity.linked", AggregateType: "external_identity", AggregateID: id.String(), Payload: map[string]any{"providerId": proof.ProviderID},
	}}}, nil
}

func (s *Service) unlinkExternalIdentity(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
	var request struct{ ID, Password string }
	var id pgtype.UUID
	if err := contracts.Bind(input, &request); err != nil || id.Scan(request.ID) != nil || !id.Valid {
		return contracts.Output{}, problem("invalid")
	}
	queries := dbgen.New(tx)
	actor, err := s.delegationOwner(ctx, queries, request.Password)
	if err != nil {
		return contracts.Output{}, err
	}
	loginID, _, err := sessionLoginSubject(actor)
	if err != nil {
		return contracts.Output{}, err
	}
	if _, err = queries.IAMUnlinkExternalIdentity(ctx, dbgen.IAMUnlinkExternalIdentityParams{ID: id, LoginAccountID: loginID}); err != nil {
		return contracts.Output{}, safe(err)
	}
	// The mapping row remains locked until commit. A separate statement sees
	// sessions whose insert completed while that lock was being acquired;
	// deleting them prevents an old session reviving after a later relink.
	if err = queries.IAMRevokeExternalSessions(ctx, id); err != nil {
		return contracts.Output{}, safe(err)
	}
	return contracts.Output{Data: map[string]any{"id": id.String()}, Events: []contracts.Event{{
		Type: "identity.externalIdentity.unlinked", AggregateType: "external_identity", AggregateID: id.String(),
	}}}, nil
}
