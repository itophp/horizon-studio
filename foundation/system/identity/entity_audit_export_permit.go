package identity

import "horizon/foundation/kernel/capability/contracts"

func auditExportPermitModel() contracts.Entity {
	return contracts.Entity{
		Name: auditExportPermitEntity, Domain: "identity", Table: "iam_audit_export_permits", Key: "requestId",
		Description: "Expiring, single-use approval bound to an audit page and work context.",
		Attributes: []contracts.Attribute{
			{Name: "tenantId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "accountId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "membershipId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "requestId", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true},
			{Name: "bindingId", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true},
			{Name: "filters", Kind: contracts.AttributeSystem, Type: contracts.TypeJSON, Shape: "object", Required: true, Sensitive: true},
			{Name: "filterDigest", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true},
			{Name: "expiresAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Required: true},
		},
		Relations: []contracts.Relation{
			{Name: "tenant", Target: "tenant", Kind: "one", Via: "tenant_id", Required: true},
			{Name: "account", Target: "account", Kind: "one", Via: "account_id", Required: true},
			{Name: "membership", Target: "tenant_membership", Kind: "one", Via: "membership_id", Required: true},
		},
	}
}
