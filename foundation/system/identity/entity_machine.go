package identity

import "horizon/foundation/kernel/capability/contracts"

func machineCredentialEntity() contracts.Entity {
	return contracts.Entity{
		Name: "machine_credential", Domain: "identity", Table: "iam_machine_credentials", Key: "id",
		Description: "Hashed, audience-bound credential issued to a non-human principal.",
		Attributes: []contracts.Attribute{
			{Name: "id", Kind: contracts.AttributeSystem, Type: contracts.TypeUUID, Required: true},
			{Name: "tenantId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "accountId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "issuerId", Kind: contracts.AttributeSystem, Type: contracts.TypeInteger, Required: true},
			{Name: "capabilities", Kind: contracts.AttributeSystem, Type: contracts.TypeJSON, Shape: "array", Required: true},
			{Name: "tokenHash", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true, Sensitive: true},
			{Name: "audience", Kind: contracts.AttributeSystem, Type: contracts.TypeEnum, Required: true, Enum: []string{CapabilityAudience, AgentAudience}},
			{Name: "createdAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Required: true},
			{Name: "expiresAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Required: true},
			{Name: "revokedAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Nullable: true},
		},
		Relations: []contracts.Relation{{Name: "account", Target: "account", Kind: "one", Via: "account_id", Required: true}},
	}
}
