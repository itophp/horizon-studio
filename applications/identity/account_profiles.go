package identityapp

import (
	"context"

	"horizon/foundation/kernel/capability/contracts"
	"horizon/internal/repository/dbgen"
)

const AccountProfileEntity = "identity_account_profile"

func accountProfileEntity() contracts.Entity {
	return contracts.Entity{
		Name: AccountProfileEntity, Domain: "identity", Table: "identity_account_profiles", Key: "id",
		Description: "Application-owned display and contact profile for a human account.",
		Attributes: []contracts.Attribute{
			{Name: "id", Kind: contracts.AttributeSystem, Type: contracts.TypeInteger, Required: true},
			{Name: "tenantId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "accountId", Kind: contracts.AttributeReference, Type: contracts.TypeReference, Required: true},
			{Name: "displayName", Kind: contracts.AttributeBusiness, Type: contracts.TypeString, Required: true},
			{Name: "email", Kind: contracts.AttributeBusiness, Type: contracts.TypeString, Required: true},
		},
		Relations: []contracts.Relation{
			{Name: "tenant", Target: "tenant", Kind: "one", Via: "tenant_id", Required: true},
			{Name: "account", Target: "account", Kind: "one", Via: "account_id", Required: true},
		},
	}
}

// StoreAccountProfile writes profile data using the account mutation's transaction.
func StoreAccountProfile(ctx context.Context, conn dbgen.DBTX, tenant string, accountID int64, name, email string) error {
	_, err := conn.Exec(ctx, `
		INSERT INTO identity_account_profiles (tenant_id, account_id, display_name, email)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (tenant_id, account_id) DO UPDATE
		SET display_name = EXCLUDED.display_name, email = EXCLUDED.email
	`, tenant, accountID, name, email)
	return err
}
