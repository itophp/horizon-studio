package identity

import "horizon/foundation/kernel/capability/contracts"

func membershipInvitationEntity() contracts.Entity {
	return contracts.Entity{
		Name: "membership_invitation", Domain: "identity", Table: "iam_membership_invitations", Key: "id",
		Description: "Single-use invitation to join a tenant using an existing verified login.",
		Attributes: []contracts.Attribute{
			{Name: "id", Kind: contracts.AttributeSystem, Type: contracts.TypeUUID, Required: true},
			{Name: "tenantId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "inviterAccountId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "username", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true, Sensitive: true},
			{Name: "recipientRealm", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true},
			{Name: "tokenHash", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true, Sensitive: true},
			{Name: "expiresAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Required: true},
			{Name: "acceptedAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Nullable: true},
		},
		Relations: []contracts.Relation{
			{Name: "tenant", Target: "tenant", Kind: "one", Via: "tenant_id", Required: true},
			{Name: "inviter", Target: "account", Kind: "one", Via: "inviter_account_id", Required: true},
		},
	}
}
