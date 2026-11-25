package identity

import "horizon/foundation/kernel/capability/contracts"

func federationAssertionUseEntity() contracts.Entity {
	return contracts.Entity{Name: "federation_assertion_use", Domain: "identity", Table: "iam_federation_assertion_uses", Key: "assertionHash",
		Description: "Distributed single-use SAML assertion identifiers scoped to their configured identity provider.",
		Attributes: []contracts.Attribute{
			{Name: "providerId", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true},
			{Name: "assertionHash", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true, Sensitive: true},
			{Name: "expiresAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Required: true},
		},
	}
}
