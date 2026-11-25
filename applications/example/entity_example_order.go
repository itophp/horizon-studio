package example

import "horizon/foundation/kernel/capability/contracts"

func exampleOrderEntity() contracts.Entity {
	maxTitle := 200
	return contracts.Entity{
		Name: "example_order", Domain: "example", Table: "example_orders", Key: "id",
		Description: "An order assigned to a supplier contact in the same tenant.",
		Attributes: []contracts.Attribute{
			{Name: "id", Kind: contracts.AttributeSystem, Type: contracts.TypeInteger, Required: true},
			{Name: "tenantId", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true},
			{Name: "supplierId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "title", Kind: contracts.AttributeBusiness, Type: contracts.TypeString, Required: true, MaxLength: &maxTitle},
		},
		Relations: []contracts.Relation{
			{Name: "supplier", Target: "example_record", Kind: "one", Via: "supplier_id", Required: true},
		},
	}
}
