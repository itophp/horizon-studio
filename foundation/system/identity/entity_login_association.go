package identity

import "horizon/foundation/kernel/capability/contracts"

func loginAssociationEntityModel() contracts.Entity {
	return contracts.Entity{
		Name: loginAssociationEntity, Domain: "identity", Table: "iam_login_associations", Key: "id",
		Description: "An independently verified login attached to a primary Human principal with a reversible mapping.",
		Attributes: []contracts.Attribute{
			{Name: "id", Kind: contracts.AttributeSystem, Type: contracts.TypeUUID, Required: true},
			{Name: "principalId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "originalPrincipalId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "primaryLoginId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "associatedLoginId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "revokedAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Nullable: true},
		},
		Relations: []contracts.Relation{
			{Name: "principal", Target: "security_principal", Kind: "one", Via: "principal_id", Required: true},
			{Name: "originalPrincipal", Target: "security_principal", Kind: "one", Via: "original_principal_id", Required: true},
			{Name: "primaryLogin", Target: "login_account", Kind: "one", Via: "primary_login_id", Required: true},
			{Name: "associatedLogin", Target: "login_account", Kind: "one", Via: "associated_login_id", Required: true},
		},
	}
}
