package identity

import "horizon/foundation/kernel/capability/contracts"

func securityPrincipalEntity() contracts.Entity {
	return contracts.Entity{
		Name: "security_principal", Domain: "identity", Table: "iam_principals", Key: "id",
		Description: "Stable subject for authentication and authorization.",
		Attributes: []contracts.Attribute{
			{Name: "id", Kind: contracts.AttributeSystem, Type: contracts.TypeUUID, Required: true},
			{Name: "kind", Kind: contracts.AttributeSystem, Type: contracts.TypeEnum, Required: true, Enum: []string{"human", "agent", "service", "application"}},
			{Name: "status", Kind: contracts.AttributeState, Type: contracts.TypeEnum, Required: true, Enum: []string{"pending", "active", "suspended", "inactive"}},
			{Name: "displayName", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true},
			{Name: "creationSource", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true},
			{Name: "securityNotes", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true, Sensitive: true},
			{Name: "riskLevel", Kind: contracts.AttributeSystem, Type: contracts.TypeEnum, Required: true, Enum: []string{"none", "elevated", "high"}},
			{Name: "createdInTenant", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Nullable: true},
		},
		Relations: []contracts.Relation{{Name: "creationTenant", Target: "tenant", Kind: "one", Via: "created_in_tenant"}},
		States:    []contracts.State{{Name: "pending"}, {Name: "active"}, {Name: "suspended"}, {Name: "inactive"}},
		Transitions: []contracts.Transition{
			{From: []string{"active"}, To: "suspended", Capability: "identity.principal.suspend"},
			{From: []string{"active", "pending", "suspended"}, To: "inactive", Capability: "identity.principal.disable"},
			{From: []string{"pending", "suspended", "inactive"}, To: "active", Capability: "identity.principal.restore"},
		},
	}
}

func loginAccountEntity() contracts.Entity {
	return contracts.Entity{
		Name: "login_account", Domain: "identity", Table: "iam_login_accounts", Key: "id",
		Description: "Authentication account associated with a security principal.",
		Attributes: []contracts.Attribute{
			{Name: "id", Kind: contracts.AttributeSystem, Type: contracts.TypeUUID, Required: true},
			{Name: "principalId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "username", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true},
			{Name: "realm", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true},
			{Name: "status", Kind: contracts.AttributeState, Type: contracts.TypeEnum, Required: true, Enum: []string{"active", "frozen", "revoked"}},
		},
		Relations: []contracts.Relation{
			{Name: "principal", Target: "security_principal", Kind: "one", Via: "principal_id", Required: true},
		},
		States: []contracts.State{{Name: "active"}, {Name: "frozen"}, {Name: "revoked", Terminal: true}},
		Transitions: []contracts.Transition{
			{From: []string{"active"}, To: "frozen", Capability: "identity.login.freeze"},
			{From: []string{"frozen"}, To: "active", Capability: "identity.login.restore"},
		},
	}
}

func passwordCredentialEntity() contracts.Entity {
	return contracts.Entity{
		Name: "password_credential", Domain: "identity", Table: "iam_password_credentials", Key: "loginAccountId",
		Description: "Argon2id credential retained separately from the login account and tenant placement.",
		Attributes: []contracts.Attribute{
			{Name: "loginAccountId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "passwordHash", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true, Sensitive: true},
			{Name: "updatedAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Required: true},
		},
		Relations: []contracts.Relation{
			{Name: "loginAccount", Target: "login_account", Kind: "one", Via: "login_account_id", Required: true},
		},
	}
}

func tenantEntity() contracts.Entity {
	return contracts.Entity{
		Name: "tenant", Domain: "identity", Table: "iam_tenants", Key: "id",
		Description: "Workspace security boundary independent of business organizations.",
		Attributes: []contracts.Attribute{
			{Name: "id", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true},
			{Name: "status", Kind: contracts.AttributeState, Type: contracts.TypeEnum, Required: true, Enum: []string{"active", "inactive"}},
			{Name: "registrationChannel", Kind: contracts.AttributeSystem, Type: contracts.TypeEnum, Required: true, Enum: []string{"invite", "email", "sms"}},
			{Name: "reviewIntervalDays", Kind: contracts.AttributeSystem, Type: contracts.TypeInteger, Required: true},
			{Name: "resourceRevision", Kind: contracts.AttributeSystem, Type: contracts.TypeInteger, Required: true},
			{Name: "auditExportApproval", Kind: contracts.AttributeSystem, Type: contracts.TypeBoolean, Required: true},
			{Name: "nextReviewAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Required: true},
		},
		States: []contracts.State{{Name: "active"}, {Name: "inactive"}},
		Transitions: []contracts.Transition{
			{From: []string{"active"}, To: "inactive", Capability: "identity.tenant.deactivate"},
			{From: []string{"inactive"}, To: "active", Capability: "identity.tenant.restore"},
		},
	}
}

func tenantMembershipEntity() contracts.Entity {
	return contracts.Entity{
		Name: "tenant_membership", Domain: "identity", Table: "iam_tenant_memberships", Key: "id",
		Description: "A principal's time-limited eligibility to access a tenant.",
		Attributes: []contracts.Attribute{
			{Name: "id", Kind: contracts.AttributeSystem, Type: contracts.TypeUUID, Required: true},
			{Name: "tenantId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "principalId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "status", Kind: contracts.AttributeState, Type: contracts.TypeEnum, Required: true, Enum: []string{"pending", "active", "suspended", "revoked"}},
			{Name: "expiresAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Nullable: true},
			{Name: "accessRevokedAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Nullable: true},
		},
		Relations: []contracts.Relation{
			{Name: "tenant", Target: "tenant", Kind: "one", Via: "tenant_id", Required: true},
			{Name: "principal", Target: "security_principal", Kind: "one", Via: "principal_id", Required: true},
		},
		States: []contracts.State{{Name: "pending"}, {Name: "active"}, {Name: "suspended"}, {Name: "revoked", Terminal: true}},
		Transitions: []contracts.Transition{
			{From: []string{"active"}, To: "suspended", Capability: "identity.membership.suspend"},
			{From: []string{"suspended"}, To: "active", Capability: "identity.membership.restore"},
			{From: []string{"active", "suspended", "pending"}, To: "revoked", Capability: "identity.membership.revoke"},
		},
	}
}

func loginSessionEntity() contracts.Entity {
	return contracts.Entity{
		Name: "login_session", Domain: "identity", Table: "iam_login_sessions", Key: "tokenHash",
		Description: "Short-lived authentication before a tenant work context is selected.",
		Attributes: []contracts.Attribute{
			{Name: "tokenHash", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Sensitive: true, Required: true},
			{Name: "loginAccountId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "csrfToken", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Sensitive: true, Required: true},
			{Name: "deviceLabel", Kind: contracts.AttributeSystem, Type: contracts.TypeString, Required: true},
			{Name: "expiresAt", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Required: true},
			{Name: "externalIdentityId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Nullable: true},
		},
		Relations: []contracts.Relation{
			{Name: "loginAccount", Target: "login_account", Via: "login_account_id", Kind: "one", Required: true},
			{Name: "externalIdentity", Target: "external_identity", Via: "external_identity_id", Kind: "one"},
		},
	}
}
