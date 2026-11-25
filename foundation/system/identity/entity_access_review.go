package identity

import "horizon/foundation/kernel/capability/contracts"

func accessReviewEntity() contracts.Entity {
	return contracts.Entity{
		Name: reviewEntity, Domain: "identity", Table: "iam_access_reviews", Key: "id",
		Description: "A tenant membership or contextual role assignment captured for access review.",
		Attributes: []contracts.Attribute{
			{Name: "id", Kind: contracts.AttributeSystem, Type: contracts.TypeInteger, Required: true},
			{Name: "tenantId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "principalId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "kind", Kind: contracts.AttributeSystem, Type: contracts.TypeEnum, Enum: []string{"membership", "context_role"}, Required: true},
			{Name: "targetId", Kind: contracts.AttributeSystem, Type: contracts.TypeUUID, Required: true},
			{Name: "roleId", Kind: contracts.AttributeSystem, Type: contracts.TypeInteger, Nullable: true},
			{Name: "targetExpiresAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Nullable: true},
			{Name: "targetRevokedAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Nullable: true},
			{Name: "status", Kind: contracts.AttributeState, Type: contracts.TypeEnum, Enum: []string{"pending", "retained", "revoked", "superseded"}, Required: true},
			{Name: "reviewerId", Kind: contracts.AttributeSystem, Type: contracts.TypeInteger, Nullable: true},
			{Name: "reviewedAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Nullable: true},
			{Name: "reason", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Nullable: true},
		},
		Relations: []contracts.Relation{
			{Name: "tenant", Target: "tenant", Kind: "one", Via: "tenant_id", Required: true},
			{Name: "principal", Target: "security_principal", Kind: "one", Via: "principal_id", Required: true},
		},
		States: []contracts.State{{Name: "pending"}, {Name: "retained", Terminal: true}, {Name: "revoked", Terminal: true}, {Name: "superseded", Terminal: true}},
		Transitions: []contracts.Transition{
			{From: []string{"pending"}, To: "retained", Capability: "identity.review.decide"},
			{From: []string{"pending"}, To: "revoked", Capability: "identity.review.decide"},
			{From: []string{"pending"}, To: "superseded", Capability: "identity.review.decide"},
		},
	}
}
