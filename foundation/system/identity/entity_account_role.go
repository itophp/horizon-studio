package identity

import "horizon/foundation/kernel/capability/contracts"

func accountRoleEntity() contracts.Entity {
	return contracts.Entity{
		Name: "account_role", Domain: "identity", Table: "iam_account_roles", Key: "id",
		Description: "A workspace account role in the account's primary department.",
		Attributes: []contracts.Attribute{
			{Name: "id", Kind: contracts.AttributeSystem, Type: contracts.TypeUUID, Required: true},
			{Name: "tenantId", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true},
			{Name: "accountId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "roleId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
		},
		Relations: []contracts.Relation{
			{Name: "account", Target: "account", Kind: "one", Via: "account_id", Required: true},
			{Name: "role", Target: "role", Kind: "one", Via: "role_id", Required: true},
		},
	}
}
