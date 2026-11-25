//go:build integration

package tests

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"horizon/deployment/database"
	genidentity "horizon/foundation/gen/identity"
	"horizon/foundation/runtime/app"
	"horizon/foundation/system/governance"
	"horizon/foundation/system/identity"
)

func TestDatabaseInitializationAndDefaultAdministrator(t *testing.T) {
	ctx := t.Context()
	dsn := isolatedIAMDatabase(t)
	type result struct {
		created bool
		err     error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			created, err := database.Initialize(ctx, dsn, database.Options{})
			results <- result{created, err}
		}()
	}
	applied := 0
	for range 2 {
		outcome := <-results
		if outcome.err != nil {
			t.Errorf("initialize: %v", outcome.err)
		}
		if outcome.created {
			applied++
		}
	}
	if applied != 1 {
		t.Fatalf("expected one concurrent initializer to apply the schema, got %d", applied)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	if created, err := database.Initialize(ctx, dsn, database.Options{Username: "another-admin"}); err != nil || created {
		t.Fatalf("repeat initialization: created=%t, err=%v", created, err)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM iam_accounts").Scan(&count); err != nil || count != 1 {
		t.Fatalf("expected one administrator, got %d (%v)", count, err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM pg_views WHERE schemaname = current_schema() AND viewname IN
		('iam_organizations', 'iam_departments', 'iam_department_memberships', 'iam_department_membership_roles', 'iam_department_directory')
	`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("unexpected IAM compatibility views: %d (%v)", count, err)
	}

	iam := identity.New(pool, false)
	core := governance.New(pool, iam, governance.Options{
		EncryptionKey:   []byte("01234567890123456789012345678901"),
		StorageRoot:     t.TempDir(),
		PublicWorkspace: "workspace-default",
	})
	client := iamClient{handler: deploymentHandler(app.System{}, core, iam)}
	wrong := client.request(t, "POST", "/api/auth/login", map[string]string{"username": "admin", "password": "wrong-password"}, false)
	checkStatus(t, wrong, 401)
	login := client.request(t, "POST", "/api/auth/login", map[string]string{"username": "admin", "password": "111111"}, false)
	checkStatus(t, login, 200)
	client.cookie = login.Result().Cookies()[0]
	client.csrf = decodeBody[genidentity.LoginResult](t, login).Profile.CsrfToken
	profile := *decodeBody[genidentity.LoginResult](t, login).Profile
	if profile.TenantID != "workspace-default" {
		t.Fatalf("login did not establish the deployment tenant: %s", profile.TenantID)
	}
	if profile.Account == nil || profile.Account.Username != "admin" || !profile.Account.IsAdmin {
		t.Fatalf("unexpected administrator profile: %+v", profile.Account)
	}
	client.csrf = profile.CsrfToken
	checkSucceeded(t, client.invoke(t, "identity.accounts.admin.read", map[string]any{}, true))
}

func TestDatabaseInitializationProvisionsOnlyOneOrganization(t *testing.T) {
	ctx := t.Context()
	dsn := isolatedIAMDatabase(t)
	if _, err := database.Initialize(ctx, dsn, database.Options{}); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var organizationID int64
	if err := pool.QueryRow(ctx, `
		SELECT id FROM identity_organizations
		WHERE tenant_id = 'workspace-default' AND parent_id IS NULL AND kind = 'company'
	`).Scan(&organizationID); err != nil || organizationID < 1 {
		t.Fatalf("default tenant organization: %d (%v)", organizationID, err)
	}
	var absent bool
	if err := pool.QueryRow(ctx, `
		SELECT to_regclass(format('%I.org_registry_organizations', current_schema())) IS NULL
	`).Scan(&absent); err != nil || !absent {
		t.Fatalf("the organization registry table still exists: %v (%v)", !absent, err)
	}
	var duplicate *pgconn.PgError
	if _, err := pool.Exec(ctx, `
		INSERT INTO identity_organizations (tenant_id, name, code, kind, status)
		VALUES ('workspace-default', 'Second', 'second', 'company', 'active')
	`); !errors.As(err, &duplicate) || duplicate.Code != "23505" {
		t.Fatalf("the database did not reject a duplicate organization: %v", err)
	}
}

func TestDatabaseInitializationRejectsOldSchema(t *testing.T) {
	ctx := t.Context()
	dsn := isolatedIAMDatabase(t)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `CREATE TABLE iam_accounts (id bigint NOT NULL);
        INSERT INTO iam_accounts (id) VALUES (42)`); err != nil {
		t.Fatal(err)
	}
	if created, err := database.Initialize(ctx, dsn, database.Options{}); err == nil || created {
		t.Fatalf("old schema must be rejected: created=%t, err=%v", created, err)
	}
	var id int64
	if err := conn.QueryRow(ctx, "SELECT id FROM iam_accounts").Scan(&id); err != nil || id != 42 {
		t.Fatalf("existing database data was changed: %d (%v)", id, err)
	}
}
