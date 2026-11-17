package studio

import "horizon/foundation/kernel/capability/contracts"

func studioRunEntity() contracts.Entity {
	maxKey := 180
	return contracts.Entity{
		Name:        "studio_run",
		Domain:      "studio",
		Table:       "studio_runs",
		Description: "Gateway invocation record against a managed project, keyed by idempotency key.",
		Key:         "id",
		Attributes: []contracts.Attribute{
			{Name: "id", Kind: contracts.AttributeSystem, Type: contracts.TypeInteger, Required: true},
			{Name: "projectId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "mode", Kind: contracts.AttributeBusiness, Type: contracts.TypeString, Required: true,
				Description: "Invocation mode: direct, workflow, adk, graph or plan."},
			{Name: "capability", Kind: contracts.AttributeBusiness, Type: contracts.TypeString, Required: true},
			{Name: "version", Kind: contracts.AttributeBusiness, Type: contracts.TypeString, Required: true},
			{Name: "requestKey", Kind: contracts.AttributeBusiness, Type: contracts.TypeString, Required: true,
				MaxLength: &maxKey, Description: "Caller supplied idempotency key; unique per project."},
			{Name: "status", Kind: contracts.AttributeBusiness, Type: contracts.TypeString,
				Description: "Last observed invocation status."},
			{Name: "invocationId", Kind: contracts.AttributeBusiness, Type: contracts.TypeString,
				Description: "Invocation identifier returned by the project gateway."},
			{Name: "approvalId", Kind: contracts.AttributeBusiness, Type: contracts.TypeInteger,
				Description: "Pending approval identifier, zero when none."},
			{Name: "runId", Kind: contracts.AttributeBusiness, Type: contracts.TypeInteger,
				Description: "Workflow run identifier, zero when none."},
		},
		Relations: []contracts.Relation{
			{Name: "project", Target: "studio_project", Kind: "one", Via: "project_id", Required: true},
		},
	}
}
