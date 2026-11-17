package studio

import "horizon/foundation/kernel/capability/contracts"

func studioItemEntity() contracts.Entity {
	maxTitle := 200
	maxText := 12000
	maxReference := 180
	maxSessionID := 256
	return contracts.Entity{
		Name:        "studio_item",
		Domain:      "studio",
		Table:       "studio_items",
		Description: "Work item pinned to a fixed definition version of the managed project.",
		Key:         "id",
		Attributes: []contracts.Attribute{
			{Name: "id", Kind: contracts.AttributeSystem, Type: contracts.TypeInteger, Required: true},
			{Name: "projectId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "requirementId", Kind: contracts.AttributeReference, Type: contracts.TypeReference,
				Description: "Optional owning requirement; cleared when the requirement is deleted."},
			{Name: "subjectItemId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Description: "Planned catalog item associated with an execution task."},
			{Name: "dshSessionId", Kind: contracts.AttributeSystem, Type: contracts.TypeString, MaxLength: &maxSessionID, Description: "DSH session linked to this execution task."},
			{Name: "catalogTarget", Kind: contracts.AttributeBusiness, Type: contracts.TypeString, MaxLength: &maxReference, Description: "Expected fixed version of a planned capability or workflow."},
			{Name: "kind", Kind: contracts.AttributeBusiness, Type: contracts.TypeEnum, Required: true,
				Enum:        []string{"application", "capability", "workflow", "plugin", "execution"},
				Description: "Catalogue type the item tracks."},
			{Name: "title", Kind: contracts.AttributeBusiness, Type: contracts.TypeString, Required: true,
				MaxLength: &maxTitle},
			{Name: "stage", Kind: contracts.AttributeState, Type: contracts.TypeEnum, Required: true,
				Enum:        []string{"planned", "developing", "verifying", "accepted"},
				Description: "Lifecycle state; acceptance requires recorded evidence."},
			{Name: "reference", Kind: contracts.AttributeBusiness, Type: contracts.TypeString,
				MaxLength: &maxReference, Description: "Pinned definition reference name@version."},
			{Name: "referenceKind", Kind: contracts.AttributeBusiness, Type: contracts.TypeString,
				Description: "Catalogue type of the pinned reference."},
			{Name: "checks", Kind: contracts.AttributeBusiness, Type: contracts.TypeString,
				MaxLength: &maxText, Description: "Acceptance conditions collected up front."},
			{Name: "pinned", Kind: contracts.AttributeBusiness, Type: contracts.TypeBoolean,
				Description: "Session kept at the top of its list; restored sessions start unpinned."},
			{Name: "archived", Kind: contracts.AttributeBusiness, Type: contracts.TypeBoolean,
				Description: "Archived sessions stay readable but leave the default session list."},
			{Name: "evidence", Kind: contracts.AttributeBusiness, Type: contracts.TypeJSON, Shape: "object",
				Description: "Verification evidence recorded at acceptance: source revision, dirty flag, notes."},
			{Name: "createdBy", Kind: contracts.AttributeSystem, Type: contracts.TypeReference, Required: true},
		},
		Relations: []contracts.Relation{
			{Name: "project", Target: "studio_project", Kind: "one", Via: "project_id", Required: true},
			{Name: "subjectItem", Target: "studio_item", Kind: "one", Via: "subject_item_id"},
			{Name: "requirement", Target: "studio_requirement", Kind: "one", Via: "requirement_id"},
		},
		States: []contracts.State{
			{Name: "planned"},
			{Name: "developing"},
			{Name: "verifying"},
			{Name: "accepted", Terminal: true},
		},
		Transitions: []contracts.Transition{
			{To: "planned", Capability: "studio.item.create"},
			{From: []string{"planned"}, To: "developing", Capability: "studio.item.transition"},
			{From: []string{"developing"}, To: "verifying", Capability: "studio.item.transition"},
			{From: []string{"verifying"}, To: "developing", Capability: "studio.item.transition"},
			{From: []string{"verifying"}, To: "accepted", Capability: "studio.item.transition"},
		},
	}
}
