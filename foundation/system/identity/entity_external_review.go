package identity

import "horizon/foundation/kernel/capability/contracts"

func externalIdentityReviewEntity() contracts.Entity {
	return contracts.Entity{
		Name: externalReviewEntity, Domain: "identity", Table: "iam_external_identity_reviews", Key: "id",
		Description: "Reviewed reassignment of one verified external identity.",
		Attributes: []contracts.Attribute{
			{Name: "id", Kind: contracts.AttributeSystem, Type: contracts.TypeUUID, Required: true},
			{Name: "tenantId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "externalIdentityId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "originalLoginId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "requestedLoginId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "decidedBy", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Nullable: true},
			{Name: "status", Kind: contracts.AttributeState, Type: contracts.TypeEnum, Required: true,
				Enum: []string{"pending", "confirmed", "applied", "rejected", "rolled_back"}},
			{Name: "reason", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Nullable: true},
			{Name: "appliedAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Nullable: true},
			{Name: "originalLinkedAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Required: true},
			{Name: "expiresAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Required: true},
		},
		Relations: []contracts.Relation{
			{Name: "tenant", Target: "tenant", Kind: "one", Via: "tenant_id", Required: true},
			{Name: "externalIdentity", Target: "external_identity", Kind: "one", Via: "external_identity_id", Required: true},
			{Name: "originalLogin", Target: "login_account", Kind: "one", Via: "original_login_id", Required: true},
			{Name: "requester", Target: "login_account", Kind: "one", Via: "requested_login_id", Required: true},
			{Name: "reviewer", Target: "login_account", Kind: "one", Via: "decided_by"},
		},
		States: []contracts.State{{Name: "pending"}, {Name: "confirmed"}, {Name: "applied"}, {Name: "rejected", Terminal: true}, {Name: "rolled_back", Terminal: true}},
		Transitions: []contracts.Transition{
			{From: []string{"pending"}, To: "confirmed", Capability: "identity.externalReview.confirm"},
			{From: []string{"confirmed"}, To: "applied", Capability: "identity.externalReview.decide"},
			{From: []string{"pending", "confirmed"}, To: "rejected", Capability: "identity.externalReview.decide"},
			{From: []string{"applied"}, To: "rolled_back", Capability: "identity.externalReview.decide"},
		},
	}
}
