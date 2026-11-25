package identity

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	genidentity "horizon/foundation/gen/identity"
	"horizon/internal/repository/dbgen"
	"log/slog"
	"regexp"
	"strings"
)

var errTenantAlreadyBootstrapped = errors.New("workspace already has accounts; bootstrap refused")

// Bootstrap creates a workspace's first administrator under a transaction lock. It never replaces existing credentials.
func Bootstrap(ctx context.Context, pool *pgxpool.Pool, tenant, username, password string) (*genidentity.AccountRecord, error) {
	return bootstrap(ctx, pool, tenant, username, password, nil)
}

// BootstrapWithTenantProvisioning runs the deployment's initializer in the same
// transaction as the initial administrator and tenant records.
func BootstrapWithTenantProvisioning(ctx context.Context, pool *pgxpool.Pool, tenant, username, password string, provision func(context.Context, pgx.Tx, string, int64) error) (*genidentity.AccountRecord, error) {
	return bootstrap(ctx, pool, tenant, username, password, provision)
}

func bootstrap(ctx context.Context, pool *pgxpool.Pool, tenant, username, password string, provision func(context.Context, pgx.Tx, string, int64) error) (*genidentity.AccountRecord, error) {
	if tenant == "" || !regexp.MustCompile(`^[A-Za-z0-9_.@-]{3,80}$`).MatchString(username) {
		return nil, errors.New("invalid bootstrap identity")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			slog.Error("bootstrap rollback failed", "error", err)
		}
	}()
	id, err := bootstrapTenant(ctx, dbgen.New(tx), tenant, username, hash)
	if err != nil {
		return nil, err
	}
	if provision != nil {
		if err := provision(ctx, tx, tenant, id); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return newRepository(pool).authorityAccount(ctx, tenant, id)
}

func bootstrapTenant(ctx context.Context, q *dbgen.Queries, tenant, username, hash string) (int64, error) {
	if err := q.IAMBootstrapLock(ctx, tenant); err != nil {
		return 0, err
	}
	count, err := q.IAMCountAccounts(ctx, tenant)
	if err != nil {
		return 0, err
	}
	if count > 0 {
		return 0, errTenantAlreadyBootstrapped
	}
	if err := q.IAMEnsureBootstrapTenant(ctx, tenant); err != nil {
		return 0, err
	}
	all := []*genidentity.PermissionGrant{}
	for _, permission := range Permissions {
		if permission == "temporary:approve" || (permission != "access:manage" && !strings.HasPrefix(permission, "accounts:") && !strings.HasPrefix(permission, "organizations:") && !strings.HasPrefix(permission, "departments:") && !strings.HasPrefix(permission, "roles:")) {
			continue
		}
		g := &genidentity.PermissionGrant{Permission: permission, Effect: "allow", Scope: "ALL"}
		all = append(all, g)

	}
	allJSON, err := encodeTenantGrants(all)
	if err != nil {
		return 0, err
	}
	adminRole, err := q.IAMCreateRole(ctx, dbgen.IAMCreateRoleParams{TenantID: tenant, Name: "System administrator", Code: "system-admin", Status: "active", Description: "Full workspace administration", Grants: allJSON, TemplateKey: "tenant-admin"})
	if err != nil {
		return 0, err
	}
	id, err := q.IAMCreateAccount(ctx, dbgen.IAMCreateAccountParams{TenantID: tenant, Username: strings.ToLower(username), Status: "active", PasswordHash: hash})
	if err != nil {
		return 0, err
	}
	if err = q.IAMMarkAdmin(ctx, dbgen.IAMMarkAdminParams{TenantID: tenant, ID: id}); err != nil {
		return 0, err
	}
	if err = q.IAMAssignRole(ctx, dbgen.IAMAssignRoleParams{TenantID: tenant, AccountID: id, RoleID: adminRole}); err != nil {
		return 0, err
	}
	if err = q.RuntimeProvisionTenant(ctx, tenant); err != nil {
		return 0, err
	}
	return id, nil
}
