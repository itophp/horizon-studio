package identity

import "horizon/foundation/kernel/capability/contracts"

func federationFlowEntity() contracts.Entity {
	return contracts.Entity{Name: "federation_flow", Domain: "identity", Table: "iam_federation_flows", Key: "stateHash",
		Description: "A single-use browser-bound external authentication transaction with an encrypted PKCE verifier or SAML request identifier.",
		Attributes: []contracts.Attribute{
			{Name: "stateHash", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true, Sensitive: true},
			{Name: "browserHash", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true, Sensitive: true},
			{Name: "providerId", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true},
			{Name: "issuer", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true},
			{Name: "purpose", Kind: contracts.AttributeSystem, Type: contracts.TypeEnum, Required: true, Enum: []string{"login", "link"}},
			{Name: "nonceHash", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true, Sensitive: true},
			{Name: "sealedVerifier", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true, Sensitive: true},
			{Name: "expiresAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Required: true},
			{Name: "consumedAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Nullable: true},
		},
	}
}
