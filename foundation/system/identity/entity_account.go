package identity

import "horizon/foundation/kernel/capability/contracts"

// accountEntity mirrors iam_accounts.
func accountEntity() contracts.Entity {
	return contracts.Entity{
		Name:        "account",
		Domain:      "identity",
		Table:       "iam_accounts",
		Description: "A tenant-local account for ownership and authorization.",
		Key:         "id",
		Attributes: []contracts.Attribute{
			{Name: "id", Kind: contracts.AttributeSystem, Type: contracts.TypeInteger, Required: true},
			{Name: "username", Kind: contracts.AttributeBusiness, Type: contracts.TypeString, Required: true,
				Description: "Unique sign-in name."},

			{Name: "loginAccountId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Nullable: true},
			{Name: "machinePrincipalId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Nullable: true},
			// Lifecycle flag, not a state machine: identity.account.update may set
			// either value in one call, so there is no legal transition graph to
			// declare yet.
			{Name: "status", Kind: contracts.AttributeBusiness, Type: contracts.TypeEnum, Required: true,
				Enum: []string{"active", "inactive"}},

			{Name: "overrides", Kind: contracts.AttributeSystem, Type: contracts.TypeJSON, Shape: "array", Required: true,
				Description: "Per-account grants that replace the role grant for the same permission."},
			{Name: "isAdmin", Kind: contracts.AttributeSystem, Type: contracts.TypeBoolean, Required: true,
				Description: "Built-in administrator; bypasses grant evaluation."},
			{Name: "lastLogin", Kind: contracts.AttributeSystem, Type: contracts.TypeTimestamp, Nullable: true},
		},
		Relations: []contracts.Relation{
			{Name: "loginAccount", Target: "login_account", Kind: "one", Via: "login_account_id"},
			{Name: "machinePrincipal", Target: "security_principal", Kind: "one", Via: "machine_principal_id"},
		},
	}
}
