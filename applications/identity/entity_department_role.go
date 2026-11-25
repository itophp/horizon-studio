package identityapp

import "horizon/foundation/kernel/capability/contracts"

func departmentRoleEntity() contracts.Entity {
	return contracts.Entity{
		Name: "department_role", Domain: "identity", Table: "identity_department_membership_roles", Key: "id",
		Description: "A workspace account role in an additional department.",
		Attributes: []contracts.Attribute{
			{Name: "id", Kind: contracts.AttributeSystem, Type: contracts.TypeUUID, Required: true},
			{Name: "tenantId", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true},
			{Name: "accountId", Kind: contracts.AttributeSystem, Type: contracts.TypeInteger, Required: true},
			{Name: "departmentId", Kind: contracts.AttributeSystem, Type: contracts.TypeInteger, Required: true},
			{Name: "roleId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
		},
		Relations: []contracts.Relation{
			{Name: "role", Target: "role", Kind: "one", Via: "role_id", Required: true},
		},
	}
}
