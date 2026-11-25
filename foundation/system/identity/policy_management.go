package identity

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/capability/registry"
	"horizon/internal/repository/dbgen"
)

const (
	policyDrafted   = "identity.policy.drafted"
	policyPublished = "identity.policy.published"
)

func (s *Service) policyEntries() []registry.Entry {
	policyID := contracts.Int(1, 1<<53-1)
	version := contracts.Int(0, 1<<53-1)
	reason := contracts.Str(5, 500)
	maxRules := 100
	rules := &contracts.Schema{Type: "array", MaxItems: &maxRules, Items: contracts.Object(map[string]*contracts.Schema{
		"action": contracts.Str(1, 200), "resource": contracts.Str(1, 200), "effect": {Type: "string", Enum: []string{"deny", "require_binding", "require_attribute", "filter_row", "mask_output"}},
		"bindingType": contracts.Str(1, 200), "attribute": contracts.Str(1, 200), "equals": contracts.Str(1, 256),
	}, "action", "resource", "effect")}
	draft := contracts.Object(map[string]*contracts.Schema{"baseVersion": version, "reason": reason, "rules": rules}, "baseVersion", "reason", "rules")
	copyVersion := contracts.Object(map[string]*contracts.Schema{"id": policyID, "baseVersion": version, "reason": reason}, "id", "baseVersion", "reason")
	selectVersion := contracts.Object(map[string]*contracts.Schema{"id": policyID}, "id")
	read := func(action, title string, input, output *contracts.Schema, executor contracts.Executor) registry.Entry {
		return registry.Entry{Definition: contracts.Definition{
			Name: "identity.policy." + action, Version: identityVersion, Title: title, Description: title,
			Domain: "identity", Entity: "authorization_policy_version", Layer: contracts.LayerAtomic, Permission: "access:manage",
			ReadOnly: true, TransientResult: true, Risk: contracts.RiskLow, Approval: contracts.ApprovalAuto,
			AllowedOrigins: []contracts.Origin{contracts.OriginHumanUI}, Tags: []string{"identity", "manual-only"},
			Executor: "go:identity.policy." + action, Input: input, Output: output,
		}, Handler: contracts.Handler{Execute: executor}}
	}
	write := func(action, title, event string, input *contracts.Schema, executor contracts.TxExecutor) registry.Entry {
		definition := identityWrite("identity.policy."+action, title, title, "authorization_policy_version", "access:manage", []string{event}, input)
		definition.Entities = []string{"authorization_policy_version"}
		if action == "publish" {
			definition.Entities = append(definition.Entities, "authorization_policy_head")
		}
		definition.Output = contracts.Object(map[string]*contracts.Schema{"id": policyID}, "id")
		definition.AllowedOrigins = []contracts.Origin{contracts.OriginHumanUI}
		definition.Tags = append(definition.Tags, "manual-only")
		return registry.Entry{Definition: definition, Handler: contracts.Handler{ExecuteTx: executor}}
	}
	history := contracts.Object(map[string]*contracts.Schema{
		"version": version,
		"items": contracts.Array(contracts.Object(map[string]*contracts.Schema{
			"id": policyID, "baseVersion": version, "reason": reason,
			"published": {Type: "boolean"}, "authorId": policyID, "authoredByMe": {Type: "boolean"},
		}, "id", "baseVersion", "reason", "published", "authorId", "authoredByMe"), 0, 50),
	}, "version", "items")
	preview := contracts.Object(map[string]*contracts.Schema{
		"currentVersion": version, "proposedVersion": policyID, "stale": {Type: "boolean"},
		"baseVersion": version, "reason": reason,
		"published": {Type: "boolean"}, "authoredByMe": {Type: "boolean"},
		"additions": rules, "removals": rules, "proposedRules": rules,
		"authorName": {Type: "string"}, "reviewerName": {Type: "string"}, "createdAt": {Type: "string"}, "publishedAt": {Type: "string"},
		"issues": policyIssuesSchema(),
	}, "currentVersion", "proposedVersion", "baseVersion", "reason", "stale", "published", "authoredByMe", "additions", "removals", "proposedRules")
	decision := contracts.Object(map[string]*contracts.Schema{
		"requiresBinding": {Type: "boolean"},
		"maskedFields":    {Type: "array", Items: &contracts.Schema{Type: "string"}},
		"rowPredicates": {Type: "array", Items: contracts.Object(map[string]*contracts.Schema{
			"attribute": {Type: "string"}, "equals": {Type: "string"},
		}, "attribute", "equals")},
	}, "requiresBinding", "maskedFields", "rowPredicates")
	simulation := contracts.Object(map[string]*contracts.Schema{
		"currentVersion": version, "proposedVersion": policyID,
		"currentAllowed": {Type: "boolean"}, "proposedAllowed": {Type: "boolean"},
		"currentReason": contracts.Str(1, 100), "proposedReason": contracts.Str(1, 100),
		"currentConstraints": decision, "proposedConstraints": decision,
	}, "currentVersion", "proposedVersion", "currentAllowed", "proposedAllowed", "currentReason", "proposedReason")
	return []registry.Entry{
		read("history", "View authorization policy versions", contracts.Object(nil), history, s.policyHistory),
		read("preview", "Compare authorization policy versions", selectVersion, preview, s.policyPreview),
		read("check", "Check authorization policy rules", contracts.Object(map[string]*contracts.Schema{"rules": rules}, "rules"),
			contracts.Object(map[string]*contracts.Schema{"issues": policyIssuesSchema()}, "issues"), s.checkPolicy),
		read("searchContexts", "Search eligible simulation contexts",
			contracts.Object(map[string]*contracts.Schema{"keyword": contracts.Str(1, 100)}, "keyword"),
			contracts.Object(map[string]*contracts.Schema{"items": contracts.Array(contracts.Object(map[string]*contracts.Schema{
				"accountId": policyID, "fullName": {Type: "string"}, "username": {Type: "string"},
			}, "accountId", "fullName", "username"), 0, 20)}, "items"), s.searchPolicyContexts),
		read("simulate", "Evaluate a draft against a current trusted identity", contracts.Object(map[string]*contracts.Schema{
			"id": policyID, "accountId": policyID,
			"bindingId": contracts.Str(36, 36), "action": contracts.Str(1, 200), "resource": contracts.Str(1, 200),
			"permission": contracts.Str(1, 200), "capabilityVersion": contracts.Str(1, 100),
			"resourceId": contracts.Int(1, 1<<53-1),
		}, "id", "accountId", "action", "resource"), simulation, s.simulatePolicy),
		write("draft", "Draft an authorization policy", policyDrafted, draft, s.createPolicyDraft(false)),
		write("rollback", "Draft a rollback to a previously published authorization policy", policyDrafted, copyVersion, s.createPolicyDraft(true)),
		write("publish", "Approve and publish another administrator's authorization policy draft", policyPublished, selectVersion, s.publishPolicy),
	}
}

func (s *Service) workspaceIAMAdmin(ctx context.Context, queries *dbgen.Queries) (*Principal, error) {
	principal, err := Current(ctx)
	if err != nil {
		return nil, err
	}
	bound := s.WithReadQueries(queries)
	principal, err = bound.PrincipalForSessionInvocation(ctx, principal, false)
	if err != nil || principal.IdentityBindingID != "" {
		return nil, contracts.Problem("forbidden")
	}
	scope, err := bound.ScopeFor(ctx, principal, "access:manage", true)
	if err != nil || !scope.All {
		return nil, contracts.Problem("forbidden")
	}
	return principal, nil
}

func (s *Service) createPolicyDraft(rollback bool) contracts.TxExecutor {
	return func(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
		queries := dbgen.New(tx)
		actor, err := s.workspaceIAMAdmin(ctx, queries)
		if err != nil {
			return contracts.Output{}, err
		}
		_, principalID, err := sessionLoginSubject(actor)
		if err != nil {
			return contracts.Output{}, err
		}
		var request struct {
			ID          int64               `json:"id"`
			BaseVersion int64               `json:"baseVersion"`
			Reason      string              `json:"reason"`
			Rules       []authorizationRule `json:"rules"`
		}
		if err = contracts.Bind(input, &request); err != nil {
			return contracts.Output{}, contracts.Problem("invalid")
		}
		if err = queries.IAMPolicyLock(ctx, actor.TenantID); err != nil {
			return contracts.Output{}, err
		}
		current, err := queries.IAMPolicyCurrentID(ctx, actor.TenantID)
		if err != nil {
			return contracts.Output{}, err
		}
		if current != request.BaseVersion {
			return contracts.Output{}, contracts.Problem("conflict")
		}
		if rollback {
			source, sourceErr := queries.IAMPolicyVersion(ctx, dbgen.IAMPolicyVersionParams{TenantID: actor.TenantID, ID: request.ID})
			if errors.Is(sourceErr, pgx.ErrNoRows) || sourceErr == nil && !source.PublishedAt.Valid {
				return contracts.Output{}, contracts.Problem("not_found")
			}
			if sourceErr != nil {
				return contracts.Output{}, sourceErr
			}
			if json.Unmarshal(source.Rules, &request.Rules) != nil {
				return contracts.Output{}, contracts.Problem("unavailable")
			}
		}
		if err = validateAuthorizationRules(request.Rules); err != nil {
			return contracts.Output{}, err
		}
		rules, err := json.Marshal(request.Rules)
		if err != nil {
			return contracts.Output{}, err
		}
		id, err := queries.IAMPolicyDraft(ctx, dbgen.IAMPolicyDraftParams{
			TenantID: actor.TenantID, AuthorID: actor.Account.ID, AuthorPrincipalID: principalID, BaseVersion: current, Rules: rules, Reason: request.Reason,
		})
		if err != nil {
			return contracts.Output{}, err
		}
		return policyOutput(id, policyDrafted), nil
	}
}

func policyIDFrom(input contracts.Input) (int64, error) {
	var request struct {
		ID int64 `json:"id"`
	}
	if err := contracts.Bind(input, &request); err != nil || request.ID <= 0 {
		return 0, contracts.Problem("invalid")
	}
	return request.ID, nil
}

func (s *Service) publishPolicy(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
	queries := dbgen.New(tx)
	actor, err := s.workspaceIAMAdmin(ctx, queries)
	if err != nil {
		return contracts.Output{}, err
	}
	_, reviewer, err := sessionLoginSubject(actor)
	if err != nil {
		return contracts.Output{}, err
	}
	id, err := policyIDFrom(input)
	if err != nil {
		return contracts.Output{}, err
	}
	if err = queries.IAMPolicyLock(ctx, actor.TenantID); err != nil {
		return contracts.Output{}, err
	}
	current, err := queries.IAMPolicyCurrentID(ctx, actor.TenantID)
	if err != nil {
		return contracts.Output{}, err
	}
	candidate, err := queries.IAMPolicyVersion(ctx, dbgen.IAMPolicyVersionParams{TenantID: actor.TenantID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, problem("conflict")
	}
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	var rules []authorizationRule
	if json.Unmarshal(candidate.Rules, &rules) != nil || validateAuthorizationRules(rules) != nil {
		return contracts.Output{}, problem("invalid")
	}
	if _, err = queries.IAMPolicyPublish(ctx, dbgen.IAMPolicyPublishParams{ID: id, TenantID: actor.TenantID, ReviewerID: actor.Account.ID, ReviewerPrincipalID: reviewer, BaseVersion: current}); errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, contracts.Problem("conflict")
	} else if err != nil {
		return contracts.Output{}, err
	}
	if err = queries.IAMPolicyAdvance(ctx, dbgen.IAMPolicyAdvanceParams{TenantID: actor.TenantID, VersionID: id}); err != nil {
		return contracts.Output{}, err
	}
	return policyOutput(id, policyPublished), nil
}

func (s *Service) policyHistory(ctx context.Context, _ contracts.Input) (contracts.Output, error) {
	actor, err := s.workspaceIAMAdmin(ctx, s.repo.q)
	if err != nil {
		return contracts.Output{}, err
	}
	version, err := s.repo.q.IAMPolicyCurrentID(ctx, actor.TenantID)
	if err != nil {
		return contracts.Output{}, err
	}
	rows, err := s.repo.q.IAMPolicyHistory(ctx, actor.TenantID)
	if err != nil {
		return contracts.Output{}, err
	}
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, map[string]any{"id": row.ID, "baseVersion": row.BaseVersion, "reason": row.Reason, "published": row.PublishedAt.Valid, "authorId": row.AuthorID, "authoredByMe": row.AuthorID == actor.Account.ID})
	}
	return contracts.Output{Data: map[string]any{"version": version, "items": items}}, nil
}

func (s *Service) policyPreview(ctx context.Context, input contracts.Input) (contracts.Output, error) {
	actor, err := s.workspaceIAMAdmin(ctx, s.repo.q)
	if err != nil {
		return contracts.Output{}, err
	}
	id, err := policyIDFrom(input)
	if err != nil {
		return contracts.Output{}, err
	}
	version, current, err := readAuthorizationPolicy(ctx, s.repo.q, actor.TenantID)
	if err != nil {
		return contracts.Output{}, err
	}
	proposed, err := s.repo.q.IAMPolicyVersion(ctx, dbgen.IAMPolicyVersionParams{TenantID: actor.TenantID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, contracts.Problem("not_found")
	}
	if err != nil {
		return contracts.Output{}, err
	}
	var draft []authorizationRule
	if json.Unmarshal(proposed.Rules, &draft) != nil || validateAuthorizationRules(draft) != nil {
		return contracts.Output{}, contracts.Problem("unavailable")
	}
	return contracts.Output{Data: map[string]any{
		"currentVersion": version, "proposedVersion": id, "stale": version != proposed.BaseVersion,
		"additions": policyDiff(draft, current), "removals": policyDiff(current, draft),
		"proposedRules": draft, "published": proposed.PublishedAt.Valid,
		"authoredByMe": proposed.AuthorID == actor.Account.ID || proposed.AuthorPrincipalID.String() == actor.SubjectID || proposed.CurrentAuthorPrincipalID.String() == actor.SubjectID,
		"baseVersion":  proposed.BaseVersion, "reason": proposed.Reason,
		"authorName": proposed.AuthorName, "reviewerName": proposed.ReviewerName, "createdAt": timeValue(proposed.CreatedAt), "publishedAt": timeValue(proposed.PublishedAt),
		"issues": s.policyIssues(draft),
	}}, nil
}

func policyDiff(from, excluding []authorizationRule) []authorizationRule {
	seen := make(map[authorizationRule]bool, len(excluding))
	for _, rule := range excluding {
		seen[rule] = true
	}
	items := []authorizationRule{}
	for _, rule := range from {
		if !seen[rule] {
			items = append(items, rule)
		}
	}
	return items
}

func (s *Service) searchPolicyContexts(ctx context.Context, input contracts.Input) (contracts.Output, error) {
	actor, err := s.workspaceIAMAdmin(ctx, s.repo.q)
	if err != nil {
		return contracts.Output{}, err
	}
	var request struct{ Keyword string }
	if err = contracts.Bind(input, &request); err != nil || strings.TrimSpace(request.Keyword) == "" {
		return contracts.Output{}, problem("invalid")
	}
	rows, err := s.repo.q.IAMPolicySearchContexts(ctx, dbgen.IAMPolicySearchContextsParams{TenantID: actor.TenantID, Keyword: strings.TrimSpace(request.Keyword)})
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, map[string]any{
			"accountId": row.AccountID, "fullName": row.FullName, "username": row.Username,
		})
	}
	return contracts.Output{Data: map[string]any{"items": items}}, nil
}

func (s *Service) simulatePolicy(ctx context.Context, input contracts.Input) (contracts.Output, error) {
	tx, err := s.repo.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	defer tx.Rollback(ctx)
	queries := dbgen.New(tx)
	actor, err := s.workspaceIAMAdmin(ctx, queries)
	if err != nil {
		return contracts.Output{}, err
	}
	var request struct {
		ID                int64  `json:"id"`
		AccountID         int64  `json:"accountId"`
		BindingID         string `json:"bindingId"`
		Action            string `json:"action"`
		Resource          string `json:"resource"`
		Permission        string `json:"permission"`
		ResourceID        int64  `json:"resourceId"`
		CapabilityVersion string `json:"capabilityVersion"`
	}
	if err = contracts.Bind(input, &request); err != nil {
		return contracts.Output{}, problem("invalid")
	}
	entry, ok := s.authorizationCatalog.Resolve(request.Action, request.CapabilityVersion)
	definition := entry.Definition
	if !ok || definition.Entity != request.Resource ||
		(request.Permission != "" && request.Permission != definition.Permission) ||
		(request.ResourceID != 0 && definition.ResourceAttributeID == "" && definition.DelegationResourceID == "") {
		return contracts.Output{}, problem("invalid")
	}
	subject, err := s.WithReadQueries(queries).PrincipalForBoundTask(ctx, actor.TenantID, request.AccountID, request.BindingID)
	if err != nil {
		return contracts.Output{}, problem("forbidden")
	}
	proposed, err := queries.IAMPolicyVersion(ctx, dbgen.IAMPolicyVersionParams{TenantID: actor.TenantID, ID: request.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, problem("not_found")
	}
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	var draft []authorizationRule
	if json.Unmarshal(proposed.Rules, &draft) != nil || validateAuthorizationRules(draft) != nil {
		return contracts.Output{}, problem("unavailable")
	}
	version, active, err := readAuthorizationPolicy(ctx, queries, actor.TenantID)
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	enabled, err := queries.IAMResourceEnabled(ctx, dbgen.IAMResourceEnabledParams{TenantID: actor.TenantID, Resource: definition.Entity})
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	authorization := AuthorizationInput{Principal: subject, Action: definition.Name, Permission: definition.Permission, Resource: definition.Entity,
		ResourceID: request.ResourceID, Environment: contracts.OriginHumanUI}
	authorization.ApplyCapabilityFields(definition)
	resolve := s.authorizationResourceResolver(ctx, queries, authorization)
	evaluate := func(version int64, rules []authorizationRule) (AuthorizationDecision, error) {
		switch {
		case !enabled.Valid || !enabled.Bool:
			return AuthorizationDecision{PolicyVersion: version, Reason: "resource_disabled"}, nil
		case !definition.AllowsOrigin(authorization.Environment):
			return AuthorizationDecision{PolicyVersion: version, Reason: "origin_not_allowed"}, nil
		default:
			return s.evaluateAuthorization(ctx, queries, authorization, version, rules, resolve)
		}
	}
	current, err := evaluate(version, active)
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	decision, err := evaluate(proposed.ID, draft)
	if err != nil {
		return contracts.Output{}, safe(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return contracts.Output{}, safe(err)
	}
	return contracts.Output{Data: map[string]any{
		"currentVersion": current.PolicyVersion, "currentAllowed": current.Allowed, "currentReason": current.Reason,
		"proposedVersion": decision.PolicyVersion, "proposedAllowed": decision.Allowed, "proposedReason": decision.Reason,
		"currentConstraints": policySimulationConstraints(current), "proposedConstraints": policySimulationConstraints(decision),
	}}, nil
}

func policySimulationConstraints(decision AuthorizationDecision) map[string]any {
	result := map[string]any{"requiresBinding": decision.RequiresBinding, "maskedFields": []string{}, "rowPredicates": []contracts.RowPredicate{}}
	if decision.Allowed {
		result["maskedFields"] = append([]string{}, decision.MaskedOutput...)
		result["rowPredicates"] = append([]contracts.RowPredicate{}, decision.RowPredicates...)
	}
	return result
}

func policyOutput(id int64, event string) contracts.Output {
	return contracts.Output{Data: map[string]any{"id": id}, Events: []contracts.Event{{
		Type: event, AggregateType: "authorization_policy_version", AggregateID: strconv.FormatInt(id, 10),
	}}}
}
