package studio

import (
	"context"
	"encoding/json"

	"horizon/foundation/kernel/capability/contracts"
)

type projectService struct {
	Service   string `json:"service"`
	Status    string `json:"status"`
	PID       int    `json:"pid,omitempty"`
	Log       string `json:"log,omitempty"`
	StartedAt string `json:"startedAt,omitempty"`
	Reason    string `json:"reason,omitempty"`
	Origin    string `json:"origin,omitempty"`
}

func serviceStatuses(ctx context.Context, project projectRef, action, target string) ([]projectService, error) {
	result, err := control(ctx, project, action, target)
	if err != nil {
		return nil, err
	}
	services := []projectService{}
	if err := json.Unmarshal(result, &services); err != nil || services == nil {
		return nil, contracts.Problem("unavailable")
	}
	for index := range services {
		service := &services[index]
		origin, err := loopbackOrigin(service.Origin)
		if err != nil || service.Status != "running" || !logServices[service.Service] {
			service.Origin = ""
		} else {
			service.Origin = origin
		}
	}
	return services, nil
}

func projectServicesOutput() *contracts.Schema {
	return contracts.Object(map[string]*contracts.Schema{
		"services": {Type: "array", Items: contracts.Object(map[string]*contracts.Schema{
			"service": {Type: "string"}, "status": {Type: "string"}, "pid": {Type: "integer"},
			"log": {Type: "string"}, "origin": {Type: "string"}, "startedAt": {Type: "string"}, "reason": {Type: "string"},
		}, "service", "status")},
	}, "services")
}

func projectStatusDefinition() contracts.Definition {
	return contracts.Definition{Layer: contracts.LayerAtomic,
		Name: "studio.project.status", Version: "1.0.0", Title: "Read project service status",
		Description: "Read running services and their local development URLs from the project's launcher.",
		Domain:      "studio", Entity: "studio_project", Permission: ReadPermission,
		Risk: contracts.RiskLow, Approval: contracts.ApprovalAuto, ReadOnly: true, TransientResult: true,
		Executor: "go:studio.project.status", Tags: []string{"studio", "read"},
		Input: contracts.Object(map[string]*contracts.Schema{
			"project": contracts.Int(1, 1<<53),
			"target":  {Type: "string", Enum: []string{"all", "backend", "frontend"}},
		}, "project"),
		Output:   projectServicesOutput(),
		Loggable: []string{"project", "target"},
	}
}

func (m *Module) projectStatus(ctx context.Context, in contracts.Input) (contracts.Output, error) {
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
	services, err := serviceStatuses(ctx, ref(project), "status", target)
	if err != nil {
		return contracts.Output{}, err
	}
	return contracts.Output{Data: map[string]any{"services": services}}, nil
}

func runningBackendOrigin(ctx context.Context, project projectRef) (string, error) {
	services, err := serviceStatuses(ctx, project, "status", "backend")
	if err != nil {
		return "", err
	}
	for _, service := range services {
		if service.Service == "backend" && service.Origin != "" {
			return service.Origin, nil
		}
	}
	return "", contracts.Problem("unavailable")
}
