package identity

import "horizon/foundation/kernel/capability/contracts"

func externalIdentityProofEntity() contracts.Entity {
	return contracts.Entity{Name: "external_identity_proof", Domain: "identity", Table: "iam_external_identity_proofs", Key: "proofHash",
		Description: "A short-lived single-use proof of external account ownership; local ownership is independently verified when linking.",
		Attributes: []contracts.Attribute{
			{Name: "proofHash", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true, Sensitive: true},
			{Name: "providerId", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true},
			{Name: "issuer", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true},
			{Name: "subject", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true, Sensitive: true},
			{Name: "purpose", Kind: contracts.AttributeSystem, Type: contracts.TypeEnum, Required: true, Enum: []string{"login", "link"}},
			{Name: "completion", Kind: contracts.AttributeSystem, Type: contracts.TypeBoolean, Required: true},
			{Name: "expiresAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Required: true},
			{Name: "consumedAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Nullable: true},
		},
	}
}
