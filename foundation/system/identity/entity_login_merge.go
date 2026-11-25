package identity

import "horizon/foundation/kernel/capability/contracts"

func loginMergeEntityModel() contracts.Entity {
	statuses := []string{"pending", "confirmed", "applied", "rejected", "rolled_back"}
	return contracts.Entity{
		Name: loginMergeEntity, Domain: "identity", Table: "iam_login_merge_reviews", Key: "id",
		Description: "Two separately verified workspace logins awaiting an independent decision on a reversible association.",
		Attributes: []contracts.Attribute{
			{Name: "id", Kind: contracts.AttributeSystem, Type: contracts.TypeUUID, Required: true},
			{Name: "tenantId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "primaryLoginId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "secondaryLoginId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "primaryPrincipalId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "secondaryPrincipalId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "status", Kind: contracts.AttributeState, Type: contracts.TypeEnum, Required: true, Enum: statuses},
			{Name: "createdAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Required: true},
			{Name: "expiresAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Required: true},
			{Name: "primaryDeviceId", Kind: contracts.AttributeSystem, Type: contracts.TypeUUID, Required: true, Sensitive: true},
			{Name: "secondaryDeviceId", Kind: contracts.AttributeSystem, Type: contracts.TypeUUID, Nullable: true, Sensitive: true},
			{Name: "associationId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Nullable: true},
			{Name: "reason", Kind: contracts.AttributeBusiness, Type: contracts.TypeString, Required: true},
			{Name: "decisionReason", Kind: contracts.AttributeBusiness, Type: contracts.TypeString, Nullable: true},
		},
		Relations: []contracts.Relation{
			{Name: "tenant", Target: "tenant", Kind: "one", Via: "tenant_id", Required: true},
			{Name: "primaryLogin", Target: "login_account", Kind: "one", Via: "primary_login_id", Required: true},
			{Name: "secondaryLogin", Target: "login_account", Kind: "one", Via: "secondary_login_id", Required: true},
			{Name: "primaryPrincipal", Target: "security_principal", Kind: "one", Via: "primary_principal_id", Required: true},
			{Name: "secondaryPrincipal", Target: "security_principal", Kind: "one", Via: "secondary_principal_id", Required: true},
			{Name: "association", Target: loginAssociationEntity, Kind: "one", Via: "association_id"},
		},
		States: []contracts.State{{Name: "pending"}, {Name: "confirmed"}, {Name: "applied"}, {Name: "rejected", Terminal: true}, {Name: "rolled_back", Terminal: true}},
		Transitions: []contracts.Transition{
			{To: "pending", Capability: "identity.loginMerge.request"},
			{From: []string{"pending"}, To: "confirmed", Capability: "identity.loginMerge.confirm"},
			{From: []string{"confirmed"}, To: "applied", Capability: "identity.loginMerge.decide"},
			{From: []string{"pending", "confirmed"}, To: "rejected", Capability: "identity.loginMerge.decide"},
			{From: []string{"applied"}, To: "rolled_back", Capability: "identity.loginMerge.decide"},
		},
	}
}
