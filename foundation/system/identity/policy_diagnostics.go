package identity

import (
	"context"
	"slices"
	"strings"

	"horizon/foundation/kernel/capability/contracts"
)

type policyIssue struct {
	Rule int    `json:"rule"`
	Code string `json:"code"`
}

func policyIssuesSchema() *contracts.Schema {
	return contracts.Array(contracts.Object(map[string]*contracts.Schema{
		"rule": contracts.Int(0, 100), "code": contracts.Str(1, 100),
	}, "rule", "code"), 0, 100)
}

func (s *Service) checkPolicy(ctx context.Context, input contracts.Input) (contracts.Output, error) {
	if _, err := s.workspaceIAMAdmin(ctx, s.repo.q); err != nil {
		return contracts.Output{}, err
	}
	var candidate struct{ Rules []authorizationRule }
	if err := contracts.Bind(input, &candidate); err != nil {
		return contracts.Output{}, problem("invalid")
	}
	return contracts.Output{Data: map[string]any{"issues": s.policyIssues(candidate.Rules)}}, nil
}

func (s *Service) policyIssues(rules []authorizationRule) []policyIssue {
	if rule, code := authorizationRuleIssue(rules); code != "" {
		return []policyIssue{{Rule: rule, Code: code}}
	}
	issues := make([]policyIssue, 0)
	for index, rule := range rules {
		if code := s.policyDeploymentIssue(rule); code != "" {
			issues = append(issues, policyIssue{Rule: index + 1, Code: code})
		}
	}
	return issues
}

func (s *Service) policyDeploymentIssue(rule authorizationRule) string {
	entry, ok := s.authorizationCatalog.Resolve(rule.Action, "")
	if !ok || entry.Definition.Entity != rule.Resource {
		return "unknown_action_resource"
	}
	switch rule.Effect {
	case "filter_row":
		if !slices.Contains(entry.Definition.RowFilterAttributes, strings.TrimPrefix(rule.Attribute, "resource.")) {
			return "row_filter_unavailable"
		}
	case "mask_output":
		if !entry.Definition.ReadOnly || !entry.Definition.TransientResult ||
			!slices.ContainsFunc(entry.Definition.OutputAccess, func(field contracts.FieldAccess) bool { return field.Path == rule.Attribute }) {
			return "output_mask_unavailable"
		}
	case "require_attribute":
		if strings.HasPrefix(rule.Attribute, "resource.") &&
			(s.resourceAttributes[rule.Resource] == nil || (entry.Definition.ResourceAttributeID == "" && entry.Definition.DelegationResourceID == "")) {
			return "attribute_unavailable"
		}
	}
	return ""
}
