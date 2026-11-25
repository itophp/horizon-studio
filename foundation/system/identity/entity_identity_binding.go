package identity

import "horizon/foundation/kernel/capability/contracts"

func identityBindingEntity() contracts.Entity {
	return contracts.Entity{
		Name: "identity_binding", Domain: "identity", Table: "iam_identity_bindings", Key: "id",
		Description: "Verified link between a tenant member and a plugin-owned business entity.",
		Attributes: []contracts.Attribute{
			{Name: "id", Kind: contracts.AttributeSystem, Type: contracts.TypeUUID, Required: true},
			{Name: "tenantId", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true},
			{Name: "principalId", Kind: contracts.AttributeSystem, Type: contracts.TypeUUID, Required: true},
			{Name: "identityType", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true},
			{Name: "identityVersion", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true},
			{Name: "revocationEvent", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true},
			{Name: "entityId", Kind: contracts.AttributeSystem, Type: contracts.TypeInteger, Required: true},
			{Name: "status", Kind: contracts.AttributeState, Type: contracts.TypeEnum, Required: true, Enum: []string{"pending", "active", "suspended", "expired", "revoked"}},
			{Name: "verificationLevel", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true},
			{Name: "source", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true},
			{Name: "validUntil", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Nullable: true},
			{Name: "revokedAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Nullable: true},
		},
		States: []contracts.State{{Name: "pending"}, {Name: "active"}, {Name: "suspended"}, {Name: "expired"}, {Name: "revoked", Terminal: true}},
		Transitions: []contracts.Transition{
			{From: []string{"pending"}, To: "active", Capability: "identity.binding.approve"},
			{From: []string{"active"}, To: "suspended", Capability: "identity.binding.suspend"},
			{From: []string{"pending", "active", "suspended", "expired"}, To: "revoked", Capability: "identity.binding.remove"},
			{From: []string{"pending", "active", "suspended"}, To: "expired"},
		},
	}
}
