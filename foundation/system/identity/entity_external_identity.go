package identity

import "horizon/foundation/kernel/capability/contracts"

func externalIdentityEntity() contracts.Entity {
	return contracts.Entity{Name: "external_identity", Domain: "identity", Table: "iam_external_identities", Key: "id",
		Description: "A verified stable external subject associated with a local login account; revocation retains its ownership mapping.",
		Attributes: []contracts.Attribute{
			{Name: "id", Kind: contracts.AttributeSystem, Type: contracts.TypeUUID, Required: true},
			{Name: "providerId", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true},
			{Name: "issuer", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true},
			{Name: "subject", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true, Sensitive: true},
			{Name: "loginAccountId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "linkedAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Required: true},
			{Name: "revokedAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Nullable: true},
		}, Relations: []contracts.Relation{{Name: "loginAccount", Target: "login_account", Kind: "one", Via: "login_account_id", Required: true}},
	}
}
