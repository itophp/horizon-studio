package studio

import "horizon/foundation/kernel/capability/contracts"

// studioProjectEntity mirrors studio_projects. The canonical root is unique per
// tenant: linking through a symlink resolves to the same row, so the database
// is the deduplication boundary rather than a path comparison in code.
func studioProjectEntity() contracts.Entity {
	maxRoot := 1024
	maxOrigin := 200
	maxAnalysis := 4000
	return contracts.Entity{
		Name:        "studio_project",
		Domain:      "studio",
		Table:       "studio_projects",
		Description: "External Horizon project managed by the workbench; visible to the account that linked it.",
		Key:         "id",
		Attributes: []contracts.Attribute{
			{Name: "id", Kind: contracts.AttributeSystem, Type: contracts.TypeInteger, Required: true},
			{Name: "root", Kind: contracts.AttributeBusiness, Type: contracts.TypeString, Required: true,
				MaxLength: &maxRoot, Description: "Canonical absolute path of the managed Horizon project."},
			{Name: "apiOrigin", Kind: contracts.AttributeBusiness, Type: contracts.TypeString, Required: true,
				MaxLength: &maxOrigin, Description: "Legacy API origin retained for rollback compatibility."},
			{Name: "webOrigin", Kind: contracts.AttributeBusiness, Type: contracts.TypeString, Required: true,
				MaxLength: &maxOrigin, Description: "Legacy web origin retained for rollback compatibility."},
			{Name: "name", Kind: contracts.AttributeBusiness, Type: contracts.TypeString, Required: true, MaxLength: &maxOrigin},
			{Name: "developmentBrief", Kind: contracts.AttributeBusiness, Type: contracts.TypeString, Required: true, MaxLength: &maxAnalysis, Description: "Project background and development requirements supplied to new Agent sessions."},
			{Name: "aiAnalysis", Kind: contracts.AttributeBusiness, Type: contracts.TypeString, Required: true, MaxLength: &maxAnalysis},
			{Name: "aiAnalyzedAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Nullable: true},
			{Name: "createdBy", Kind: contracts.AttributeSystem, Type: contracts.TypeReference, Required: true,
				Description: "Account that linked the project; the visibility boundary."},
		},
	}
}
