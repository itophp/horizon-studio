package studio

import (
	"context"
	"encoding/json"
	"strconv"

	"horizon/foundation/kernel/capability/contracts"
)

func projectID(in contracts.Input) (int64, bool) {
	value, ok := number(in["project"])
	if !ok || value < 1 {
		return 0, false
	}
	return int64(value), true
}

func projectControlDefinition() contracts.Definition {
	return contracts.Definition{Layer: contracts.LayerAtomic,
		Name: "studio.project.control", Version: "1.0.0", Title: "Control project dev services",
		SuccessEvents: []string{"studio.project.controlled"},
		Description:   "Start, stop, restart or query the development services of a managed project through its own horizon-cli. Affects local processes of the project directory only.",
		Domain:        "studio", Entity: "studio_project", Permission: ControlPermission,
		Risk: contracts.RiskMedium, Approval: contracts.ApprovalAuto,
		Idempotency: contracts.Idempotency{Required: true, Scope: "tenant+operation+key"},
		Executor:    "go:studio.project.control", Tags: []string{"studio", "control"},
		Input: contracts.Object(map[string]*contracts.Schema{
			"project": contracts.Int(1, 1<<53),
			"action":  {Type: "string", Enum: []string{"start", "stop", "restart", "status"}},
			"target":  {Type: "string", Enum: []string{"all", "backend", "frontend"}},
		}, "project", "action"),
		Output:   projectControlOutput(),
		Loggable: []string{"project", "action", "target"},
	}
}

func projectControlOutput() *contracts.Schema {
	output := projectServicesOutput()
	output.Properties["state"] = &contracts.Schema{Type: "string"}
	output.Required = append(output.Required, "state")
	return output
}

func (m *Module) projectControl(ctx context.Context, in contracts.Input) (contracts.Output, error) {
	p, project, err := m.ownedProject(ctx, in)
	if err != nil {
		return contracts.Output{}, err
	}
	if !p.Can(ControlPermission) {
		return contracts.Output{}, contracts.Problem("forbidden")
	}
	action := stringField(in, "action")
	target := stringField(in, "target")
	if target == "" {
		target = "all"
	}
	projectRef := ref(project)
	if err := validateServiceStart(ctx, projectRef, action); err != nil {
		return contracts.Output{}, err
	}
	services, err := serviceStatuses(ctx, projectRef, action, target)
	if err != nil {
		return contracts.Output{}, err
	}
	return contracts.Output{Data: map[string]any{"state": "cli", "services": services}, Summary: "control=" + action, Events: []contracts.Event{{
		Type: "studio.project.controlled", AggregateType: "studio_project", AggregateID: strconv.FormatInt(project.ID, 10),
	}}}, nil
}

func projectEnvironmentDefinition() contracts.Definition {
	return contracts.Definition{Layer: contracts.LayerAtomic,
		Name: "studio.project.environment", Version: "1.0.0", Title: "Check project environment",
		Description: "Run the project CLI environment check for backend, frontend or all services.",
		Domain:      "studio", Entity: "studio_project", Permission: ReadPermission,
		Risk: contracts.RiskLow, Approval: contracts.ApprovalAuto, ReadOnly: true, TransientResult: true,
		Executor: "go:studio.project.environment", Tags: []string{"studio", "read"},
		Input: contracts.Object(map[string]*contracts.Schema{
			"project": contracts.Int(1, 1<<53),
			"target":  {Type: "string", Enum: []string{"all", "backend", "frontend"}},
		}, "project"),
		Output: contracts.Object(map[string]*contracts.Schema{
			"result": {Type: "array", Items: contracts.Object(map[string]*contracts.Schema{
				"name": {Type: "string"}, "status": {Type: "string"}, "required": {Type: "boolean"},
			}, "name", "status", "required")},
			"ready": {Type: "boolean"},
		}, "result", "ready"),
	}
}

func (m *Module) projectEnvironment(ctx context.Context, in contracts.Input) (contracts.Output, error) {
	p, project, err := m.ownedProject(ctx, in)
	if err != nil {
		return contracts.Output{}, err
	}
	if !p.Can(ReadPermission) {
		return contracts.Output{}, contracts.Problem("forbidden")
	}
	target := stringField(in, "target")
	if target == "" {
		target = "all"
	}
	checks, ready := inspectInitialization(ctx, ref(project), target)
	return contracts.Output{Data: map[string]any{"result": checks, "ready": ready}}, nil
}

func projectLogDefinition() contracts.Definition {
	return contracts.Definition{Layer: contracts.LayerAtomic,
		Name: "studio.project.log", Version: "1.0.0", Title: "Tail project service log",
		Description: "Return the tail of the project CLI service log. The path is pinned inside the project log directory.",
		Domain:      "studio", Entity: "studio_project", Permission: ReadPermission,
		Risk: contracts.RiskLow, Approval: contracts.ApprovalAuto, ReadOnly: true, TransientResult: true,
		Executor: "go:studio.project.log", Tags: []string{"studio", "read"},
		Input: contracts.Object(map[string]*contracts.Schema{
			"project": contracts.Int(1, 1<<53),
			"service": {Type: "string", Enum: []string{"backend", "frontend"}},
		}, "project", "service"),
		Output:   contracts.Object(map[string]*contracts.Schema{"log": {Type: "string"}}, "log"),
		Loggable: []string{"project", "service"},
	}
}

func (m *Module) projectLog(ctx context.Context, in contracts.Input) (contracts.Output, error) {
	p, project, err := m.ownedProject(ctx, in)
	if err != nil {
		return contracts.Output{}, err
	}
	if !p.Can(ReadPermission) {
		return contracts.Output{}, contracts.Problem("forbidden")
	}
	log, err := readLog(ref(project), stringField(in, "service"))
	if err != nil {
		return contracts.Output{}, err
	}
	return contracts.Output{Data: map[string]any{"log": log}}, nil
}

func catalogInspectDefinition() contracts.Definition {
	return contracts.Definition{Layer: contracts.LayerAtomic,
		Name: "studio.catalog.inspect", Version: "1.0.0", Title: "Inspect project definition catalogue",
		Description: "Read the application, capability, workflow, graph or declared plugin catalogue of a managed project, or the schema of one fixed name@version reference.",
		Domain:      "studio", Entity: "studio_project", Permission: ReadPermission,
		Risk: contracts.RiskLow, Approval: contracts.ApprovalAuto, ReadOnly: true, TransientResult: true,
		Executor: "go:studio.catalog.inspect", Tags: []string{"studio", "read"},
		Input: contracts.Object(map[string]*contracts.Schema{
			"project":   contracts.Int(1, 1<<53),
			"kind":      {Type: "string", Enum: []string{"application", "capability", "workflow", "graph", "plugin", "registered-plugin"}},
			"reference": {Type: "string", MaxLength: intPtr(180)},
		}, "project", "kind"),
		Output:   contracts.Object(map[string]*contracts.Schema{"value": contracts.FreeObject()}, "value"),
		Loggable: []string{"project", "kind", "reference"},
	}
}

func (m *Module) catalogInspect(ctx context.Context, in contracts.Input) (contracts.Output, error) {
	p, project, err := m.ownedProject(ctx, in)
	if err != nil {
		return contracts.Output{}, err
	}
	if !p.Can(ReadPermission) {
		return contracts.Output{}, contracts.Problem("forbidden")
	}
	if stringField(in, "kind") == "plugin" {
		if stringField(in, "reference") != "" {
			return contracts.Output{}, contracts.Problem("invalid")
		}
		items, err := projectPlugins(ref(project))
		if err != nil {
			return contracts.Output{}, err
		}
		return contracts.Output{Data: map[string]any{"value": map[string]any{"items": items}}}, nil
	}
	kind := stringField(in, "kind")
	if kind == "registered-plugin" {
		kind = "plugin"
	}
	value, err := inspect(ctx, ref(project), kind, stringField(in, "reference"))
	if err != nil {
		return contracts.Output{}, err
	}
	var payload map[string]any
	_ = json.Unmarshal(value, &payload)
	return contracts.Output{Data: map[string]any{"value": payload}}, nil
}
