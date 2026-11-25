//go:build integration

package tests

import (
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"horizon/deployment"
	genidentity "horizon/foundation/gen/identity"
	"horizon/foundation/system/identity"
)

func TestSingleOrganizationProvisionedWithTenant(t *testing.T) {
	var identityService *identity.Service
	handler, pool, root := iamFixture(t, func(iam *identity.Service, pool *pgxpool.Pool, root *genidentity.AccountRecord) {
		identityService = iam
		var subject string
		if err := pool.QueryRow(t.Context(), "SELECT principal_id::text FROM iam_active_account_subjects WHERE tenant_id = 'tenant-a' AND account_id = $1", root.ID).Scan(&subject); err != nil {
			t.Fatal(err)
		}
		iam.ConfigurePlatformOperators([]string{subject})
	})
	check := func(tenant string) {
		t.Helper()
		var count int
		if err := pool.QueryRow(t.Context(), `
			SELECT count(*) FROM identity_organizations
			WHERE tenant_id = $1 AND parent_id IS NULL AND kind = 'company' AND status = 'active'
		`, tenant).Scan(&count); err != nil || count != 1 {
			t.Fatalf("expected one organization for %s, got %d (%v)", tenant, count, err)
		}
		_, err := pool.Exec(t.Context(), `
			INSERT INTO identity_organizations (tenant_id, name, code, kind, status)
			VALUES ($1, 'Extra', 'extra', 'company', 'active')
		`, tenant)
		if pgErr, ok := err.(*pgconn.PgError); !ok || pgErr.Code != "23505" {
			t.Fatalf("duplicate organization was not rejected: %v", err)
		}
	}
	check("tenant-a")
	operator := loginTenantIAM(t, handler, root.Username, "tenant-a")
	request := map[string]any{"targetTenantId": "tenant-owner-new", "target": "owner-admin", "reason": "Verify atomic tenant organization provisioning"}
	issueProof := func() any {
		t.Helper()
		return checkIAMInvocation(t, operator.invoke(t, "identity.principal.reauthenticate", map[string]any{
			"targetTenantId": request["targetTenantId"], "target": request["target"], "reason": request["reason"],
			"operation": "tenant.create", "password": "TestOnly!2026Secure",
		}, true))["proof"]
	}
	request["proof"] = issueProof()
	request["initialPassword"] = "TestOnly!2026Secure"
	reselected := operator.request(t, "POST", "/api/auth/identity", map[string]any{"tenantId": "tenant-a"}, true)
	checkStatus(t, reselected, 200)
	operator.csrf = decodeBody[genidentity.SessionProfile](t, reselected).CsrfToken
	checkStatus(t, operator.invoke(t, "identity.tenant.create", request, true), 403)
	request["proof"] = issueProof()
	checkIAMInvocation(t, operator.invoke(t, "identity.tenant.create", request, true))
	check("tenant-owner-new")

	if _, err := identity.BootstrapWithTenantProvisioning(t.Context(), pool, "tenant-owner-cli", "owner-cli", "TestOnly!2026Secure", deployment.ProvisionIdentityTenant); err != nil {
		t.Fatal(err)
	}
	check("tenant-owner-cli")

	administrator := loginTenantIAM(t, handler, "owner-cli", "tenant-owner-cli")
	contexts := administrator.request(t, "GET", "/api/auth/identities", nil, false)
	checkStatus(t, contexts, 200)
	options := decodeBody[[]*genidentity.SignInIdentity](t, contexts)
	if len(options) != 1 || options[0].IdentityBindingID != nil {
		t.Fatalf("unexpected tenant administrator contexts: %s", contexts.Body.String())
	}
	selected := administrator.request(t, "POST", "/api/auth/identity", map[string]any{"tenantId": "tenant-owner-cli"}, true)
	checkStatus(t, selected, 200)
	profile := decodeBody[genidentity.SessionProfile](t, selected)
	administrator.csrf = profile.CsrfToken
	checkIAMInvocation(t, administrator.invoke(t, "identity.review.list", map[string]any{}, true))
	checkIAMInvocation(t, administrator.invoke(t, "identity.review.start", map[string]any{}, true))
	invoke := func(name string, input map[string]any) map[string]any {
		t.Helper()
		return checkIAMInvocation(t, administrator.invoke(t, name, input, true))
	}
	checkStatus(t, administrator.invoke(t, "identity.account.create", map[string]any{
		"username": "department-payload", "fullName": "Rejected payload", "initialStatus": "active",
		"password": "TestOnly!2026Secure", "departmentId": 1,
	}, true), 400)
	testAccount := invoke("identity.account.create", map[string]any{
		"username": "tenant-base-reader", "fullName": "Tenant reader", "initialStatus": "active", "password": "TestOnly!2026Secure",
	})
	updated := invoke("identity.account.update", map[string]any{
		"id": testAccount["id"], "username": "tenant-base-reader", "fullName": "Updated tenant reader",
		"email": "reader@example.test", "status": "active",
	})
	if updated["fullName"] != "Updated tenant reader" || updated["email"] != "reader@example.test" {
		t.Fatalf("profile update did not return application data: %+v", updated)
	}
	reader := loginIAM(t, deploymentForTenant(handler, "tenant-owner-cli"), "tenant-base-reader")
	contextInput := map[string]any{"tenantId": "tenant-owner-cli"}
	checkStatus(t, reader.request(t, "POST", "/api/auth/identity", contextInput, true), 403)
	role := invoke("identity.role.create", map[string]any{
		"name": "Tenant reader", "code": "tenant-base-reader", "description": "", "status": "active",
		"grants": []map[string]any{{"permission": "accounts:read", "effect": "allow", "scope": "OWN"}},
	})
	grant := invoke("identity.roleGrant.create", map[string]any{
		"accountId": testAccount["id"], "roleId": role["id"], "minutes": 30, "reason": "Tenant authorization regression coverage",
	})
	entered := reader.request(t, "POST", "/api/auth/identity", contextInput, true)
	checkStatus(t, entered, 200)
	reader.csrf = decodeBody[genidentity.SessionProfile](t, entered).CsrfToken
	checkIAMInvocation(t, reader.invoke(t, "identity.accounts.read", map[string]any{}, true))
	invoke("identity.roleGrant.revoke", grant)
	checkStatus(t, reader.invoke(t, "identity.accounts.read", map[string]any{}, true), 403)
	inviter := loginTenantIAM(t, handler, "owner-admin", "tenant-owner-new")
	invitation := checkIAMInvocation(t, inviter.invoke(t, "identity.membership.invite", map[string]any{"username": "tenant-base-reader"}, true))
	reader = loginIAM(t, deploymentForTenant(handler, "tenant-owner-new"), "tenant-base-reader")
	join := reader.request(t, "POST", "/api/auth/invitation", map[string]any{"code": invitation["code"]}, true)
	checkStatus(t, join, 204)
	checkStatus(t, reader.request(t, "POST", "/api/auth/identity", map[string]any{"tenantId": "tenant-owner-new"}, true), 403)
	machine := invoke("identity.machine.create", map[string]any{
		"kind": "service", "name": "Tenant service", "password": "TestOnly!2026Secure",
		"grants": []map[string]any{{"permission": "example:create", "effect": "allow", "scope": "OWN"}},
	})
	credential := invoke("identity.machine.credential.rotate", map[string]any{
		"accountId": machine["id"], "audience": "horizon-capability", "minutes": 30,
		"capabilities": []string{"example.record.create@1.0.0"}, "password": "TestOnly!2026Secure",
	})
	var departmentColumns int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name IN ('iam_machine_credentials', 'iam_agent_delegations')
		  AND column_name IN ('department_id', 'issued_for_department_id', 'delegator_department_id')`).Scan(&departmentColumns); err != nil || departmentColumns != 0 {
		t.Fatalf("tenant credentials retained department columns: %d (%v)", departmentColumns, err)
	}
	secret := credential["secret"].(string)
	current, err := identityService.AuthenticateMachine(t.Context(), secret, "")
	if err != nil || current.TenantID != "tenant-owner-cli" || current.Account.DepartmentID != 0 || !current.Can("example:create") {
		t.Fatalf("tenant machine authority did not restore: %v", err)
	}
	invoke("identity.machine.credential.revoke", map[string]any{"accountId": machine["id"], "password": "TestOnly!2026Secure"})
	if _, err := identityService.AuthenticateMachine(t.Context(), secret, ""); err == nil {
		t.Fatal("revoked machine credential restored tenant authority")
	}

	var departments, profiles int
	if err := pool.QueryRow(t.Context(), `SELECT
	    (SELECT count(*) FROM identity_departments WHERE tenant_id = 'tenant-owner-cli'),
	    (SELECT count(*) FROM identity_account_profiles p JOIN iam_active_account_subjects a USING (tenant_id, account_id)
	      WHERE p.tenant_id = 'tenant-owner-cli' AND a.account_id = $1)`, profile.Account.ID).Scan(&departments, &profiles); err != nil || departments != 0 || profiles != 1 {
		t.Fatalf("expected no department and one application profile, got %d departments and %d profiles (%v)", departments, profiles, err)
	}
}
