package studio

import "horizon/foundation/kernel/capability/contracts"

// Entities lists the business objects of this module.
func Entities() []contracts.Entity {
	return []contracts.Entity{
		studioProjectEntity(),
		studioRequirementEntity(),
		studioRequirementAttachmentEntity(),
		studioItemEntity(),
		studioRunEntity(),
	}
}
