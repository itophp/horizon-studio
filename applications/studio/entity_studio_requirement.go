package studio

import "horizon/foundation/kernel/capability/contracts"

func studioRequirementEntity() contracts.Entity {
	maxTitle := 200
	maxText := 12000
	maxSource := 200
	return contracts.Entity{
		Name:        "studio_requirement",
		Domain:      "studio",
		Table:       "studio_requirements",
		Description: "Requirement collected for one managed project. Archiving keeps history readable.",
		Key:         "id",
		Attributes: []contracts.Attribute{
			{Name: "id", Kind: contracts.AttributeSystem, Type: contracts.TypeInteger, Required: true},
			{Name: "projectId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "parentId", Kind: contracts.AttributeReference, Type: contracts.TypeReference,
				Description: "Optional top-level requirement this node belongs to. A requirement tree has at most two levels."},
			{Name: "title", Kind: contracts.AttributeBusiness, Type: contracts.TypeString, Required: true,
				MaxLength: &maxTitle, Description: "Short human readable title."},
			{Name: "description", Kind: contracts.AttributeBusiness, Type: contracts.TypeString,
				MaxLength: &maxText, Description: "Collected requirement body."},
			{Name: "source", Kind: contracts.AttributeBusiness, Type: contracts.TypeString,
				MaxLength: &maxSource, Description: "Where the requirement came from."},
			{Name: "archived", Kind: contracts.AttributeBusiness, Type: contracts.TypeBoolean,
				Description: "Archived requirements stay readable but are out of the active set."},
			{Name: "createdBy", Kind: contracts.AttributeSystem, Type: contracts.TypeReference, Required: true},
		},
		Relations: []contracts.Relation{
			{Name: "project", Target: "studio_project", Kind: "one", Via: "project_id", Required: true},
			{Name: "parent", Target: "studio_requirement", Kind: "one", Via: "parent_id"},
		},
	}
}
