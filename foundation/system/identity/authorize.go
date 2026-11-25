package identity

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/capability/registry"
	"horizon/internal/repository/dbgen"
)

type AuthorizationInput struct {
	Principal           *Principal
	Action              string
	Permission          string
	Resource            string
	ResourceID          int64
	Environment         contracts.Origin
	RowFilterAttributes []string
	PolicyOutputFields  []contracts.FieldAccess
	OrganizationScoped  bool
}

type AuthorizationDecision struct {
	Allowed         bool
	Reason          string
	PolicyVersion   int64
	Constraint      Scope
	RequiresBinding bool
	RowPredicates   []contracts.RowPredicate
	MaskedOutput    []string
}

func (s *Service) BindAuthorizationCatalog(catalog *registry.Registry) {
	s.authorizationCatalog = catalog
}

type authorizationRule struct {
	Action      string `json:"action"`
	Resource    string `json:"resource"`
	Effect      string `json:"effect"`
	BindingType string `json:"bindingType,omitempty"`
	Attribute   string `json:"attribute,omitempty"`
	Equals      string `json:"equals,omitempty"`
}

var authorizationName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_.-]{0,199}$`)

func validateAuthorizationRules(rules []authorizationRule) error {
	_, code := authorizationRuleIssue(rules)
	if code != "" {
		return contracts.Problem(code)
	}
	return nil
}

func authorizationRuleIssue(rules []authorizationRule) (int, string) {
	if rules == nil || len(rules) > 100 {
		return 0, "invalid"
	}
	requirements := make(map[[2]string]string)
	attributes := make(map[[3]string]string)
	seen := make(map[authorizationRule]bool)
	for index, rule := range rules {
		if !authorizationName.MatchString(rule.Action) || !authorizationName.MatchString(rule.Resource) ||
			strings.HasPrefix(rule.Action, "identity.policy.") ||
			(rule.BindingType != "" && !authorizationName.MatchString(rule.BindingType)) {
			return index + 1, "invalid"
		}
		if seen[rule] {
			return index + 1, "conflict"
		}
		seen[rule] = true
		switch rule.Effect {
		case "deny":
			if rule.Attribute != "" || rule.Equals != "" {
				return index + 1, "invalid"
			}
		case "require_binding":
			key := [2]string{rule.Action, rule.Resource}
			if previous := requirements[key]; rule.BindingType == "" || rule.Attribute != "" || rule.Equals != "" || (previous != "" && previous != rule.BindingType) {
				return index + 1, "conflict"
			}
			requirements[key] = rule.BindingType
		case "require_attribute", "filter_row":
			key := [3]string{rule.Action, rule.Resource, rule.Attribute}
			if !validAuthorizationAttribute(rule.Attribute) || rule.Equals == "" || len(rule.Equals) > 256 || rule.BindingType != "" {
				return index + 1, "invalid"
			}
			if rule.Effect == "filter_row" && !strings.HasPrefix(rule.Attribute, "resource.") {
				return index + 1, "invalid"
			}
			if before := attributes[key]; before != "" && before != rule.Equals {
				return index + 1, "conflict"
			}
			attributes[key] = rule.Equals
		case "mask_output":
			if !contracts.ValidRowFilterName(rule.Attribute) || rule.Equals != "" {
				return index + 1, "invalid"
			}
		default:
			return index + 1, "invalid"
		}
	}
	return 0, ""
}

func decideWithAttributes(input AuthorizationInput, version int64, rules []authorizationRule, resolve func(string) (string, bool)) AuthorizationDecision {
	decision := AuthorizationDecision{PolicyVersion: version, Reason: "permission"}
	principal := input.Principal
	if principal == nil || principal.Account == nil || principal.TenantID == "" || (input.Permission != "" && !principal.Can(input.Permission)) {
		return decision
	}
	if principal.ActingOrganization.OrganizationID != 0 && !input.OrganizationScoped {
		decision.Reason = "organization_scope_unavailable"
		return decision
	}
	decision.Constraint.TenantID = principal.TenantID
	decision.Constraint.ActingOrganizationID = principal.ActingOrganization.OrganizationID
	requireBinding := false
	for _, rule := range rules {
		if rule.Action != input.Action || rule.Resource != input.Resource {
			continue
		}
		if rule.Effect == "deny" && (rule.BindingType == "" || principal.IdentityType == rule.BindingType) {
			decision.Reason = "policy_deny"
			return decision
		}
		if rule.Effect == "require_binding" {
			decision.RequiresBinding = true
			if principal.IdentityBindingID == "" || principal.IdentityType != rule.BindingType {
				requireBinding = true
			}
		}
	}
	if requireBinding {
		decision.Reason = "required_binding"
		return decision
	}
	for _, rule := range rules {
		if rule.Action != input.Action || rule.Resource != input.Resource {
			continue
		}
		if rule.Effect == "filter_row" {
			attribute := strings.TrimPrefix(rule.Attribute, "resource.")
			if !slices.Contains(input.RowFilterAttributes, attribute) {
				decision.Reason = "row_filter_unavailable"
				return decision
			}
			decision.RowPredicates = append(decision.RowPredicates, contracts.RowPredicate{Attribute: attribute, Equals: rule.Equals})
			continue
		}
		if rule.Effect == "mask_output" && (rule.BindingType == "" || rule.BindingType == principal.IdentityType) {
			if !slices.ContainsFunc(input.PolicyOutputFields, func(field contracts.FieldAccess) bool {
				return field.Path == rule.Attribute
			}) {
				decision.Reason = "output_mask_unavailable"
				return decision
			}
			decision.MaskedOutput = append(decision.MaskedOutput, rule.Attribute)
			continue
		}
		if rule.Effect != "require_attribute" {
			continue
		}
		value, available := authorizationAttribute(input, rule.Attribute, resolve)
		if !available {
			decision.Reason = "attribute_unavailable"
			return decision
		}
		if value != rule.Equals {
			decision.Reason = "attribute_mismatch"
			return decision
		}
	}
	decision.Allowed = true
	decision.Reason = "allowed"
	return decision
}

func (input *AuthorizationInput) ApplyCapabilityFields(definition contracts.Definition) {
	input.RowFilterAttributes = definition.RowFilterAttributes
	input.OrganizationScoped = slices.Contains(definition.Tags, contracts.OrganizationScopedTag)
	// Transient reads are evaluated on every request and their raw data never
	// enters persisted invocation results or the idempotency cache.
	if definition.ReadOnly && definition.TransientResult {
		input.PolicyOutputFields = definition.OutputAccess
	}
}

func (s *Service) AuthorizeAction(ctx context.Context, queries *dbgen.Queries, input AuthorizationInput) (AuthorizationDecision, error) {
	if input.Principal == nil || input.Principal.Account == nil {
		return AuthorizationDecision{Reason: "unauthenticated"}, nil
	}
	if queries == nil {
		queries = s.repo.q
	}
	version, rules, err := readAuthorizationPolicy(ctx, queries, input.Principal.TenantID)
	if err != nil {
		return AuthorizationDecision{Reason: "policy_unavailable"}, err
	}
	enabled, err := queries.IAMResourceEnabled(ctx, dbgen.IAMResourceEnabledParams{TenantID: input.Principal.TenantID, Resource: input.Resource})
	if err != nil {
		return AuthorizationDecision{PolicyVersion: version, Reason: "resource_unavailable"}, err
	}
	if !enabled.Valid || !enabled.Bool {
		return AuthorizationDecision{PolicyVersion: version, Reason: "resource_disabled"}, nil
	}
	return s.evaluateAuthorization(ctx, queries, input, version, rules, s.authorizationResourceResolver(ctx, queries, input))
}

func (s *Service) evaluateAuthorization(ctx context.Context, queries *dbgen.Queries, input AuthorizationInput, version int64, rules []authorizationRule, resolve func(string) (string, bool)) (AuthorizationDecision, error) {
	decision := decideWithAttributes(input, version, rules, resolve)
	if decision.Allowed && input.Permission != "" && input.Principal.Account.ID != 0 {
		scope, err := s.WithReadQueries(queries).scope(ctx, input.Principal, input.Permission)
		if err != nil {
			return AuthorizationDecision{PolicyVersion: version, Reason: "scope_unavailable"}, err
		}
		decision.Constraint = scope
	}
	return decision, nil
}

// ResourceAttributes is a trusted projection loaded for one tenant and one
// stored resource. A registration is deployment-owned, never request data.
type ResourceAttributes struct {
	Values     map[string]string
	ObservedAt time.Time
}

type ResourceAttributeProvider func(context.Context, *dbgen.Queries, string, int64) (ResourceAttributes, error)

func (s *Service) RegisterResourceAttributeProvider(resource string, provider ResourceAttributeProvider) error {
	if !authorizationName.MatchString(resource) || provider == nil || s.resourceAttributes[resource] != nil {
		return errors.New("invalid or duplicate authorization attribute provider")
	}
	if s.resourceAttributes == nil {
		s.resourceAttributes = map[string]ResourceAttributeProvider{}
	}
	s.resourceAttributes[resource] = provider
	return nil
}

func (s *Service) authorizationResourceResolver(ctx context.Context, q *dbgen.Queries, input AuthorizationInput) func(string) (string, bool) {
	var values map[string]string
	loaded := false
	return func(attribute string) (string, bool) {
		if !loaded {
			loaded = true
			values = s.resourceAttributeValues(ctx, q, input.Principal.TenantID, input.Resource, input.ResourceID)
		}
		value, ok := values[attribute]
		return value, ok
	}
}

func (s *Service) resourceAttributeValues(ctx context.Context, q *dbgen.Queries, tenant, resource string, id int64) map[string]string {
	provider := s.resourceAttributes[resource]
	if provider == nil || id <= 0 {
		return nil
	}
	providerCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()
	result, err := provider(providerCtx, q, tenant, id)
	elapsed := time.Since(result.ObservedAt)
	if err != nil || providerCtx.Err() != nil || elapsed < 0 || elapsed > 30*time.Second {
		return nil
	}
	return result.Values
}

func validAuthorizationAttribute(attribute string) bool {
	switch attribute {
	case "subject.kind", "subject.identityType", "environment.origin":
		return true
	}
	return strings.HasPrefix(attribute, "resource.") && authorizationName.MatchString(strings.TrimPrefix(attribute, "resource."))
}

func authorizationAttribute(input AuthorizationInput, name string, resource func(string) (string, bool)) (string, bool) {
	switch name {
	case "subject.kind":
		if input.Principal.machine != nil {
			return input.Principal.machine.kind, true
		}
		if input.Principal.LoginAccountID != "" {
			return "human", true
		}
	case "subject.identityType":
		return input.Principal.IdentityType, input.Principal.IdentityBindingID != ""
	case "environment.origin":
		return string(input.Environment), input.Environment != ""
	default:
		if resource != nil && strings.HasPrefix(name, "resource.") {
			return resource(strings.TrimPrefix(name, "resource."))
		}
	}
	return "", false
}

func readAuthorizationPolicy(ctx context.Context, queries *dbgen.Queries, tenant string) (int64, []authorizationRule, error) {
	row, err := queries.IAMPolicyCurrent(ctx, tenant)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil, nil
	}
	if err != nil {
		return 0, nil, err
	}
	var rules []authorizationRule
	if json.Unmarshal(row.Rules, &rules) != nil || validateAuthorizationRules(rules) != nil {
		return 0, nil, contracts.Problem("unavailable")
	}
	return row.ID, rules, nil
}
