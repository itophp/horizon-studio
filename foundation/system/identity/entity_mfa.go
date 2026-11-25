package identity

import "horizon/foundation/kernel/capability/contracts"

func totpCredentialEntity() contracts.Entity {
	return contracts.Entity{Name: "totp_credential", Domain: "identity", Table: "iam_totp_credentials", Key: "loginAccountId",
		Description: "Encrypted time-based authenticator enrollment and replay counter.",
		Attributes: []contracts.Attribute{
			{Name: "loginAccountId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "sealedSecret", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true, Sensitive: true},
			{Name: "enrollmentContextHash", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true, Sensitive: true},
			{Name: "activatedAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Nullable: true},
			{Name: "enrollExpiresAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Required: true},
			{Name: "replacementSecret", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Nullable: true, Sensitive: true},
			{Name: "replacementContextHash", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Nullable: true, Sensitive: true},
			{Name: "replacementExpiresAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Nullable: true},
			{Name: "lastCounter", Kind: contracts.AttributeSystem, Type: contracts.TypeInteger, Required: true},
		}, Relations: []contracts.Relation{{Name: "loginAccount", Target: "login_account", Kind: "one", Via: "login_account_id", Required: true}}}
}

func recoveryCodeEntity() contracts.Entity {
	return contracts.Entity{Name: "mfa_recovery_code", Domain: "identity", Table: "iam_recovery_codes", Key: "codeHash",
		Description: "A hashed single-use recovery credential for an enrolled login account.",
		Attributes: []contracts.Attribute{
			{Name: "loginAccountId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "codeHash", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true, Sensitive: true},
			{Name: "usedAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Nullable: true},
		}, Relations: []contracts.Relation{{Name: "credential", Target: "totp_credential", Kind: "one", Via: "login_account_id", Required: true}}}
}
