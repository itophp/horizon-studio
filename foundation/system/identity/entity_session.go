package identity

import "horizon/foundation/kernel/capability/contracts"

// sessionEntity mirrors iam_sessions. Sessions are business objects because
// publishing a security setting clamps or expires them; they are never editable
// from a form, which is why every attribute is system or reference.
func sessionEntity() contracts.Entity {
	return contracts.Entity{
		Name:        "session",
		Domain:      "identity",
		Table:       "iam_sessions",
		Description: "A browser session bound to one tenant, account and optional identity binding.",
		Key:         "tokenHash",
		Attributes: []contracts.Attribute{
			{Name: "tokenHash", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true,
				Sensitive: true, Description: "SHA-256 of the cookie token; the raw token never leaves the client."},
			{Name: "accountId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "csrfToken", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true,
				Sensitive: true, Description: "Nonce required on unsafe methods; held in memory by the console."},
			{Name: "expiresAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Required: true},
			{Name: "publicId", Kind: contracts.AttributeSystem, Type: contracts.TypeUUID, Required: true},
			{Name: "deviceLabel", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true},
			{Name: "lastSeenAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Required: true},
			{Name: "refreshedAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Nullable: true},
			{Name: "identityBindingId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Nullable: true},
			{Name: "externalIdentityId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Nullable: true},
		},
		Relations: []contracts.Relation{
			{Name: "account", Target: "account", Kind: "one", Via: "account_id", Required: true},
			{Name: "identityBinding", Target: "identity_binding", Kind: "one", Via: "identity_binding_id"},
			{Name: "externalIdentity", Target: "external_identity", Kind: "one", Via: "external_identity_id"},
		},
	}
}
