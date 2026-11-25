package identity

import "horizon/foundation/kernel/capability/contracts"

func policyVersionEntity() contracts.Entity {
	return contracts.Entity{
		Name: "authorization_policy_version", Domain: "identity", Table: "iam_policy_versions", Key: "id",
		Description: "Reviewable version of tenant authorization restrictions.",
		Attributes: []contracts.Attribute{
			{Name: "id", Kind: contracts.AttributeSystem, Type: contracts.TypeInteger, Required: true},
			{Name: "tenantId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "authorId", Kind: contracts.AttributeSystem, Type: contracts.TypeInteger, Required: true},
			{Name: "authorPrincipalId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Nullable: true},
			{Name: "baseVersion", Kind: contracts.AttributeSystem, Type: contracts.TypeInteger, Required: true},
			{Name: "rules", Kind: contracts.AttributeBusiness, Type: contracts.TypeJSON, Shape: "array", Required: true},
			{Name: "reason", Kind: contracts.AttributeBusiness, Type: contracts.TypeString, Required: true},
			{Name: "publishedAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Nullable: true},
			{Name: "reviewerId", Kind: contracts.AttributeSystem, Type: contracts.TypeInteger, Nullable: true},
			{Name: "reviewerPrincipalId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Nullable: true},
		},
		Relations: []contracts.Relation{
			{Name: "tenant", Target: "tenant", Kind: "one", Via: "tenant_id", Required: true},
			{Name: "author", Target: "security_principal", Kind: "one", Via: "author_principal_id"},
			{Name: "reviewer", Target: "security_principal", Kind: "one", Via: "reviewer_principal_id"},
		},
	}
}

func policyHeadEntity() contracts.Entity {
	return contracts.Entity{
		Name: "authorization_policy_head", Domain: "identity", Table: "iam_policy_heads", Key: "tenantId",
		Description: "Atomically selected published authorization version for a tenant.",
		Attributes: []contracts.Attribute{
			{Name: "tenantId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "versionId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
		},
		Relations: []contracts.Relation{
			{Name: "tenant", Target: "tenant", Kind: "one", Via: "tenant_id", Required: true},
			{Name: "version", Target: "authorization_policy_version", Kind: "one", Via: "version_id", Required: true},
		},
	}
}
