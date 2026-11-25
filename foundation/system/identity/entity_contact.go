package identity

import "horizon/foundation/kernel/capability/contracts"

func verifiedContactEntity() contracts.Entity {
	return contracts.Entity{
		Name: "verified_login_contact", Domain: "identity", Table: "iam_verified_contacts", Key: "loginAccountId",
		Description: "A verified email address or phone number for one login account.",
		Attributes: []contracts.Attribute{
			{Name: "loginAccountId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "channel", Kind: contracts.AttributeSystem, Type: contracts.TypeEnum, Enum: []string{"email", "sms"}, Required: true},
			{Name: "destination", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true, Sensitive: true},
			{Name: "verifiedAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Required: true},
		},
		Relations: []contracts.Relation{{Name: "loginAccount", Target: "login_account", Kind: "one", Via: "login_account_id", Required: true}},
	}
}

func contactChallengeEntity() contracts.Entity {
	return contracts.Entity{
		Name: "iam_contact_challenge", Domain: "identity", Table: "iam_contact_challenges", Key: "id",
		Description: "An expiring one-use challenge with sealed delivery data.",
		Attributes: []contracts.Attribute{
			{Name: "id", Kind: contracts.AttributeSystem, Type: contracts.TypeUUID, Required: true},
			{Name: "loginAccountId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "channel", Kind: contracts.AttributeSystem, Type: contracts.TypeEnum, Enum: []string{"email", "sms"}, Required: true},
			{Name: "destination", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true, Sensitive: true},
			{Name: "purpose", Kind: contracts.AttributeSystem, Type: contracts.TypeEnum, Enum: []string{"verify", "password_reset", "registration"}, Required: true},
			{Name: "tokenHash", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true, Sensitive: true},
			{Name: "sessionHash", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true, Sensitive: true},
			{Name: "contextHash", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true, Sensitive: true},
			{Name: "sealedToken", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true, Sensitive: true},
			{Name: "expiresAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Required: true},
			{Name: "deliveryDueAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Required: true},
			{Name: "deliveryAttempts", Kind: contracts.AttributeSystem, Type: contracts.TypeInteger, Required: true},
			{Name: "deliveredAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Nullable: true},
			{Name: "consumedAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Nullable: true},
		},
		Relations: []contracts.Relation{{Name: "loginAccount", Target: "login_account", Kind: "one", Via: "login_account_id", Required: true}},
	}
}

func pendingRegistrationEntity() contracts.Entity {
	return contracts.Entity{
		Name: "pending_registration", Domain: "identity", Table: "iam_pending_registrations", Key: "loginAccountId",
		Description: "A requested workspace registration awaiting contact verification.",
		Attributes: []contracts.Attribute{
			{Name: "loginAccountId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "tenantId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "channel", Kind: contracts.AttributeSystem, Type: contracts.TypeEnum, Required: true, Enum: []string{"email", "sms"}},
			{Name: "expiresAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Required: true},
		},
		Relations: []contracts.Relation{
			{Name: "loginAccount", Target: "login_account", Kind: "one", Via: "login_account_id", Required: true},
			{Name: "tenant", Target: "tenant", Kind: "one", Via: "tenant_id", Required: true},
		},
	}
}
