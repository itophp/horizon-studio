package identity

import "horizon/foundation/kernel/capability/contracts"

func contextRoleGrantEntity() contracts.Entity {
	return contracts.Entity{
		Name: "context_role_grant", Domain: "identity", Table: "iam_context_role_grants", Key: "id",
		Description: "Time-limited assignment of a role to a tenant member or one of their verified identity bindings.",
		Attributes: []contracts.Attribute{
			{Name: "id", Kind: contracts.AttributeSystem, Type: contracts.TypeUUID, Required: true},
			{Name: "tenantId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "principalId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "identityBindingId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Nullable: true},
			{Name: "roleId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "grantedBy", Kind: contracts.AttributeSystem, Type: contracts.TypeInteger, Required: true},
			{Name: "reason", Kind: contracts.AttributeBusiness, Type: contracts.TypeString, Required: true},
			{Name: "createdAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Required: true},
			{Name: "expiresAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Required: true},
			{Name: "revokedAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Nullable: true},
		},
		Relations: []contracts.Relation{
			{Name: "role", Kind: "one", Target: "role", Via: "role_id", Required: true},
			{Name: "identityBinding", Kind: "one", Target: "identity_binding", Via: "identity_binding_id"},
		},
	}
}
