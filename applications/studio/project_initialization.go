package studio

import (
	"context"
	"encoding/json"

	"horizon/foundation/kernel/capability/contracts"
)

type initializationCheck struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Required bool   `json:"required"`
}

func requiredEnvironment(target string) []string {
	switch target {
	case "backend":
		return []string{"go", "api_address", "postgresql"}
	case "frontend":
		return []string{"node", "web_dependencies", "web_port"}
	default:
		return []string{"go", "api_address", "postgresql", "node", "web_dependencies", "web_port"}
	}
}

func normalizeInitialization(raw json.RawMessage, target string) ([]initializationCheck, bool) {
	var checks []initializationCheck
	if err := json.Unmarshal(raw, &checks); err != nil {
		return []initializationCheck{{Name: "environment", Status: "unavailable", Required: true}}, false
	}
	required := make(map[string]bool)
	for _, name := range requiredEnvironment(target) {
		required[name] = true
	}
	seen := make(map[string]bool)
	ready := true
	for index := range checks {
		check := &checks[index]
		check.Required = check.Required || required[check.Name]
		if seen[check.Name] {
			ready = false
		}
		seen[check.Name] = true
		if check.Required && check.Status != "ok" {
			ready = false
		}
	}
	for _, name := range requiredEnvironment(target) {
		if !seen[name] {
			checks = append(checks, initializationCheck{Name: name, Status: "unavailable", Required: true})
			ready = false
		}
	}
	return checks, ready
}

func inspectInitialization(ctx context.Context, project projectRef, target string) ([]initializationCheck, bool) {
	if _, _, err := projectCLI(project.Root); err != nil {
		return []initializationCheck{{Name: "horizon_cli", Status: "unavailable", Required: true}}, false
	}
	raw, err := environment(ctx, project, target)
	if err != nil {
		return []initializationCheck{{Name: "environment", Status: "unavailable", Required: true}}, false
	}
	return normalizeInitialization(raw, target)
}

func validateServiceStart(ctx context.Context, project projectRef, action string) error {
	if action != "start" && action != "restart" {
		return nil
	}
	if _, ready := inspectInitialization(ctx, project, "all"); !ready {
		return contracts.Problem("initialization_required")
	}
	return nil
}
