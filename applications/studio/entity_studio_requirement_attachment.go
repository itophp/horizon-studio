package studio

import "horizon/foundation/kernel/capability/contracts"

func studioRequirementAttachmentEntity() contracts.Entity {
	return contracts.Entity{
		Name:        "studio_requirement_attachment",
		Domain:      "studio",
		Table:       "studio_requirement_files",
		Description: "File bound to one requirement. Removing the binding keeps the file in the shared file service.",
		Key:         "id",
		Attributes: []contracts.Attribute{
			{Name: "id", Kind: contracts.AttributeSystem, Type: contracts.TypeInteger, Required: true},
			{Name: "requirementId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "fileId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true,
				Description: "File owned by the uploader with purpose attachment."},
			{Name: "createdBy", Kind: contracts.AttributeSystem, Type: contracts.TypeReference, Required: true},
			{Name: "createdAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Required: true},
		},
		Relations: []contracts.Relation{
			{Name: "requirement", Target: "studio_requirement", Kind: "one", Via: "requirement_id", Required: true},
			{Name: "file", Target: "file", Kind: "one", Via: "file_id", Required: true},
		},
	}
}
