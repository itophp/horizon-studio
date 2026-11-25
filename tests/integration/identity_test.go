//go:build integration

package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"horizon/deployment"
	"horizon/deployment/database"
	genidentity "horizon/foundation/gen/identity"
	"horizon/foundation/runtime/app"
	"horizon/foundation/system/governance"
	"horizon/foundation/system/identity"
	"horizon/internal/repository/dbgen"
)

type iamClient struct {
	handler http.Handler
	cookie  *http.Cookie
	csrf    string
}

func (c iamClient) request(t *testing.T, method, path string, payload any, csrf bool) *httptest.ResponseRecorder {
	t.Helper()
	body := ""
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = string(raw)
	}
	r := httptest.NewRequest(method, "http://console.test"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://console.test")
	r.Header.Set("X-Tenant-ID", "attacker-controlled")
	if c.cookie != nil {
		r.AddCookie(c.cookie)
	}
	if csrf {
		r.Header.Set("X-CSRF-Token", c.csrf)
	}
	w := httptest.NewRecorder()
	c.handler.ServeHTTP(w, r)
	return w
}
func checkStatus(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("HTTP got %d, want %d: %s", w.Code, want, w.Body.String())
	}
}
func decodeBody[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// invoke runs one capability through the gateway. Every module test uses it, so
// the console and the tests exercise the same request shape.
func (c iamClient) invoke(t *testing.T, name string, input any, csrf bool) *httptest.ResponseRecorder {
	t.Helper()
	body := map[string]any{
		"input":          input,
		"idempotencyKey": fmt.Sprintf("test-%d-%s", time.Now().UnixNano(), name),
	}
	return c.request(t, "POST", "/api/capabilities/"+name+"/invocations", body, csrf)
}

func loginIAM(t *testing.T, handler http.Handler, username string) iamClient {
	t.Helper()
	c := iamClient{handler: handler}
	w := c.request(t, "POST", "/api/auth/login", map[string]string{"username": username, "password": "TestOnly!2026Secure"}, false)
	checkStatus(t, w, 200)
	result := decodeBody[genidentity.LoginResult](t, w)
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode || len(cookies[0].Value) != 64 {
		t.Fatal("unsafe or missing session cookie")
	}
	if strings.Contains(w.Body.String(), "password") || strings.Contains(w.Body.String(), "Set-Cookie") {
		t.Fatal("credential leaked into response")
	}
	return iamClient{handler: handler, cookie: cookies[0], csrf: result.Profile.CsrfToken}
}

type iamFixtureDeployment struct {
	http.Handler
	forTenant func(string) http.Handler
}

func deploymentForTenant(handler http.Handler, tenant string) http.Handler {
	if deployment, ok := handler.(*iamFixtureDeployment); ok {
		return deployment.forTenant(tenant)
	}
	return handler
}

func loginTenantIAM(t *testing.T, handler http.Handler, username, tenant string) iamClient {
	t.Helper()
	client := loginIAM(t, deploymentForTenant(handler, tenant), username)
	response := client.request(t, "GET", "/api/auth/me", nil, false)
	checkStatus(t, response, 200)
	profile := decodeBody[genidentity.SessionProfile](t, response)
	if profile.TenantID != tenant || profile.Account == nil {
		t.Fatalf("expected account in deployment tenant %s, got tenant %s", tenant, profile.TenantID)
	}
	return client
}

func seedTenantRole(t *testing.T, pool *pgxpool.Pool, tenant, code string, grantorID, accountID int64, grants []*genidentity.PermissionGrant) {
	t.Helper()
	encoded, err := json.Marshal(grants)
	if err != nil {
		t.Fatal(err)
	}
	q := dbgen.New(pool)
	roleID, err := q.IAMCreateRole(t.Context(), dbgen.IAMCreateRoleParams{
		TenantID: tenant, Name: code, Code: code, Status: "active", TemplateKey: "custom", Grants: encoded,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.IAMCreateContextRoleGrant(t.Context(), dbgen.IAMCreateContextRoleGrantParams{
		TenantID: tenant, AccountID: accountID, RoleID: roleID, GrantedBy: grantorID,
		Reason: "Integration test fixture", ValidMinutes: 120,
	}); err != nil {
		t.Fatal(err)
	}
}

func isolatedIAMDatabase(t *testing.T) string {
	t.Helper()
	source := os.Getenv("TEST_DATABASE_URL")
	if source == "" {
		t.Fatal("TEST_DATABASE_URL is required")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("iam_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Error(err)
		}
		if err := admin.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	parsed, err := url.Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	// Tables stay isolated in the fixture schema. public is kept on the path so
	// shared extension objects (for example the pg_trgm operator class used by
	// knowledge retrieval) remain resolvable.
	query.Set("search_path", schema+",public")
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func iamFixture(t *testing.T, configure ...func(*identity.Service, *pgxpool.Pool, *genidentity.AccountRecord)) (http.Handler, *pgxpool.Pool, *genidentity.AccountRecord) {
	t.Helper()
	ctx := t.Context()
	dsn := isolatedIAMDatabase(t)
	if _, err := database.Initialize(ctx, dsn, database.Options{SkipAdministrator: true}); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	root, err := identity.BootstrapWithTenantProvisioning(ctx, pool, "tenant-a", "root-admin", "TestOnly!2026Secure", deployment.ProvisionIdentityTenant)
	if err != nil {
		t.Fatal(err)
	}
	iam := identity.New(pool, false)
	for _, configureIAM := range configure {
		configureIAM(iam, pool, root)
	}
	core := governance.New(pool, iam, governance.Options{
		EncryptionKey:   []byte("01234567890123456789012345678901"),
		StorageRoot:     t.TempDir(),
		PublicWorkspace: "tenant-a",
	})
	handler := deploymentHandler(app.System{}, core, iam)
	deployments := map[string]http.Handler{"tenant-a": handler}
	return &iamFixtureDeployment{Handler: handler, forTenant: func(tenant string) http.Handler {
		if existing := deployments[tenant]; existing != nil {
			return existing
		}
		options := core.Options
		options.PublicWorkspace = tenant
		tenantIAM := identity.New(pool, false)
		next := deploymentHandler(app.System{}, governance.New(pool, tenantIAM, options), tenantIAM)
		deployments[tenant] = next
		return next
	}}, pool, root
}
func TestPersistentIdentityAuthorization(t *testing.T) {
	handler, pool, _ := iamFixture(t)
	anonymous := iamClient{handler: handler}
	checkStatus(t, anonymous.request(t, "GET", "/api/admin/snapshot", nil, false), 401)
	checkStatus(t, anonymous.request(t, "POST", "/api/auth/login", map[string]string{"username": "root-admin", "password": "incorrect"}, false), 401)
	root := loginTenantIAM(t, handler, "root-admin", "tenant-a")
	initial := decodeBody[genidentity.AdminSnapshot](t, root.request(t, "GET", "/api/admin/snapshot", nil, false))
	if len(initial.Accounts) != 1 || len(initial.Organizations) != 1 || len(initial.Departments) != 0 || len(initial.Roles) != 1 {
		t.Fatal("bootstrap did not persist expected records")
	}

	t.Run("origin and CSRF enforce write boundaries", func(t *testing.T) {
		input := map[string]any{"id": initial.Organizations[0].ID}
		checkStatus(t, root.invoke(t, "identity.organization.update", input, false), 403)
		r := httptest.NewRequest("POST", "http://console.test/api/capabilities/identity.organization.update/invocations", strings.NewReader(`{"input":{}}`))
		r.Header.Set("Origin", "https://attacker.test")
		r.Header.Set("X-CSRF-Token", root.csrf)
		r.AddCookie(root.cookie)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		checkStatus(t, w, 403)
	})

	for _, capability := range []string{
		"organization.registry.create", "organization.registry.list", "organization.unit.create",
		"organization.membership.confirm", "identity.organizationAccess.bind",
		"identity.organization.create", "identity.organization.delete", "example.supplier.organization.bind",
	} {
		checkStatus(t, root.invoke(t, capability, map[string]any{}, true), 404)
	}

	profile := initial.Organizations[0]
	renamed := decodeData[genidentity.OrganizationRecord](t, root.invoke(t, "identity.organization.update", map[string]any{
		"id": profile.ID, "name": "Renamed organization", "code": profile.Code, "kind": profile.Kind,
		"parentId": profile.ParentID, "status": profile.Status, "description": profile.Description,
	}, true))
	if renamed.ID != profile.ID || renamed.Name != "Renamed organization" {
		t.Fatalf("unexpected organization after renaming: %+v", renamed)
	}

	grants := []map[string]any{{"permission": "accounts:read", "effect": "allow", "scope": "ALL"}}
	response := root.invoke(t, "identity.role.create", map[string]any{
		"name": "Scoped manager", "code": "scoped-manager", "description": "", "status": "active", "grants": grants,
	}, true)
	checkSucceeded(t, response)
	role := decodeData[genidentity.RoleRecord](t, response)

	response = root.invoke(t, "identity.account.create", map[string]any{
		"username": "scoped-manager", "fullName": "Scoped manager", "email": "manager@example.test", "initialStatus": "active",
		"password": "TestOnly!2026Secure",
	}, true)
	account := decodeData[genidentity.AccountRecord](t, response)

	var profileName, profileEmail string
	if err := pool.QueryRow(t.Context(), `SELECT display_name, email FROM identity_account_profiles
	WHERE tenant_id = $1 AND account_id = $2`, "tenant-a", account.ID).Scan(&profileName, &profileEmail); err != nil ||
		profileName != "Scoped manager" || profileEmail != "manager@example.test" {
		t.Fatalf("identity application profile was not preserved: %q %q %v", profileName, profileEmail, err)
	}
	var retiredColumns int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM information_schema.columns
	WHERE table_schema = current_schema() AND table_name = 'iam_accounts'
	AND column_name IN ('full_name', 'email', 'organization_id', 'department_id')`).Scan(&retiredColumns); err != nil || retiredColumns != 0 {
		t.Fatalf("IAM accounts retained application fields: %d %v", retiredColumns, err)
	}
	checkIAMInvocation(t, root.invoke(t, "identity.roleGrant.create", map[string]any{
		"accountId": account.ID, "roleId": role.ID, "minutes": 30, "reason": "Tenant access test",
	}, true))
	manager := loginIAM(t, handler, "scoped-manager")
	selected := manager.request(t, "POST", "/api/auth/identity", map[string]any{"tenantId": "tenant-a"}, true)
	checkStatus(t, selected, 200)
	manager.csrf = decodeBody[genidentity.SessionProfile](t, selected).CsrfToken
	checkStatus(t, manager.invoke(t, "identity.account.create", map[string]any{
		"username": "sneaky", "fullName": "Sneaky", "initialStatus": "active",
		"password": "TestOnly!2026Secure",
	}, true), 403)
	checkStatus(t, manager.request(t, "GET", "/api/admin/snapshot", nil, false), 200)
}

// decodeData unwraps a capability result and decodes its data projection.
func decodeData[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var envelope struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Status != "succeeded" {
		t.Fatalf("capability failed: %s", w.Body.String())
	}
	var out T
	if err := json.Unmarshal(envelope.Data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// checkSucceeded asserts the business outcome, not just the transport status.
func checkSucceeded(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("HTTP got %d: %s", w.Code, w.Body.String())
	}
	var envelope struct {
		Status    string `json:"status"`
		ErrorCode string `json:"errorCode"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Status != "succeeded" {
		t.Fatalf("capability status=%s code=%s body=%s", envelope.Status, envelope.ErrorCode, w.Body.String())
	}
}
