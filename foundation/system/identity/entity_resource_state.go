package identity

import "horizon/foundation/kernel/capability/contracts"

func resourceStateEntity() contracts.Entity {
	return contracts.Entity{
		Name: "authorization_resource_state", Domain: "identity", Table: "iam_disabled_resources", Key: "resource",
		Description: "Workspace disable switch for a deployed authorization resource.",
		Attributes: []contracts.Attribute{
			{Name: "tenantId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "resource", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true},
		},
		Relations: []contracts.Relation{{Name: "tenant", Target: "tenant", Kind: "one", Via: "tenant_id", Required: true}},
	}
}
