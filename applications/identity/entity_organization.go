package identityapp

import "horizon/foundation/kernel/capability/contracts"

// organizationEntity mirrors the tenant organization profile.
func organizationEntity() contracts.Entity {
	return contracts.Entity{
		Name:        "organization",
		Domain:      "identity",
		Table:       "identity_organizations",
		Description: "The single organization provisioned for a tenant.",
		Key:         "id",
		Attributes: []contracts.Attribute{
			{Name: "id", Kind: contracts.AttributeSystem, Type: contracts.TypeInteger, Required: true},
			{Name: "tenantId", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true},
			{Name: "name", Kind: contracts.AttributeBusiness, Type: contracts.TypeString, Required: true},
			{Name: "code", Kind: contracts.AttributeBusiness, Type: contracts.TypeString, Required: true},
			{Name: "kind", Kind: contracts.AttributeBusiness, Type: contracts.TypeEnum, Required: true,
				Enum: []string{"company"}},
			{Name: "parentId", Kind: contracts.AttributeSystem, Type: contracts.TypeInteger, Nullable: true},
			{Name: "status", Kind: contracts.AttributeBusiness, Type: contracts.TypeEnum, Required: true,
				Enum: []string{"active", "inactive"}},
			{Name: "description", Kind: contracts.AttributeBusiness, Type: contracts.TypeString, Required: true},
		},
		Relations: []contracts.Relation{
			{Name: "tenant", Target: "tenant", Kind: "one", Via: "tenant_id", Required: true},
		},
	}
}
