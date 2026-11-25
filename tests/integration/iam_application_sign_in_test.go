//go:build integration

package tests

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"horizon/deployment"
	genidentity "horizon/foundation/gen/identity"
	"horizon/foundation/runtime/app"
	"horizon/foundation/runtime/transport"
	"horizon/foundation/system/governance"
	"horizon/foundation/system/identity"
)

func TestIAMApplicationLoginKeepsSameNamedAccountsSeparate(t *testing.T) {
	var iam *identity.Service
	var code string
	_, pool, root := iamFixture(t, func(service *identity.Service, _ *pgxpool.Pool, _ *genidentity.AccountRecord) {
		iam = service
		iam.ConfigureContactDelivery(func(_ context.Context, _, purpose, value, _ string) error {
			if purpose == "registration" || purpose == "password_reset" {
				code = value
			}
			return nil
		}, nil)
	})
	core := governance.New(pool, iam, governance.Options{EncryptionKey: []byte("01234567890123456789012345678901"), PublicWorkspace: "tenant-a"})
	registration := deployment.Register(core)
	for _, id := range []string{"first-portal", "second-portal"} {
		registration.SignInApplications = append(registration.SignInApplications, app.SignInApplication{ID: id, Namespace: id})
	}
	handler, _ := transport.HandlerWithRegistration(app.System{}, core, registration, iam)
	admin := loginTenantIAM(t, handler, root.Username, "tenant-a")
	checkIAMInvocation(t, admin.invoke(t, "identity.tenant.registration.configure", map[string]any{
		"channel": "email",
	}, true))
	anonymous := iamClient{handler: handler}
	endpoint := func(application, action string) string {
		return "/api/auth/applications/" + application + "/" + action
	}
	for _, appID := range []string{"identity", "administration"} {
		checkStatus(t, anonymous.request(t, "POST", endpoint(appID, "login"), map[string]any{
			"username": root.Username, "password": "TestOnly!2026Secure",
		}, false), 200)
	}
	passwords := map[string]string{"first-portal": "Different!First2026", "second-portal": "Different!Second2026"}
	var accounts []int64
	for _, id := range []string{"first-portal", "second-portal"} {
		register := map[string]string{
			"tenantId": "foreign-tenant", "username": "same-user", "displayName": id,
			"password": passwords[id], "destination": fmt.Sprintf("%s@example.test", id),
		}
		checkStatus(t, anonymous.request(t, "POST", endpoint(id, "registration/request"), register, false), 204)
		if sent, err := iam.ProcessContactDelivery(t.Context()); err != nil || !sent || len(code) != 64 {
			t.Fatalf("registration challenge missing: %v %v", sent, err)
		}
		verify := map[string]string{"token": code, "password": passwords[id]}
		checkStatus(t, anonymous.request(t, "POST", "/api/auth/registration/complete", verify, false), 403)
		checkStatus(t, anonymous.request(t, "POST", endpoint(id, "registration/complete"), verify, false), 204)
		login := anonymous.request(t, "POST", endpoint(id, "login"), map[string]string{
			"username": "same-user", "password": passwords[id],
		}, false)
		checkStatus(t, login, 200)
		profile := decodeBody[genidentity.LoginResult](t, login).Profile
		if profile.Account != nil || profile.TenantID != "" {
			t.Fatalf("registration conferred tenant access without an explicit role: %+v", profile)
		}
		var accountID int64
		if err := pool.QueryRow(t.Context(), `SELECT a.id FROM iam_accounts a
			JOIN iam_login_accounts l ON l.id = a.login_account_id
			JOIN identity_account_profiles p ON p.tenant_id = a.tenant_id AND p.account_id = a.id
			WHERE a.tenant_id = 'tenant-a' AND p.display_name = $1 AND l.realm = $1`, id).Scan(&accountID); err != nil {
			t.Fatalf("application namespace or account profile was not preserved: %v", err)
		}
		accounts = append(accounts, accountID)
	}
	if accounts[0] == accounts[1] {
		t.Fatal("same-name application logins share an IAM account")
	}
	const duplicatePassword = "Different!Duplicate2026"
	checkStatus(t, anonymous.request(t, "POST", endpoint("first-portal", "registration/request"), map[string]string{
		"tenantId": "tenant-a", "username": "Same-User", "displayName": "Duplicate",
		"password": duplicatePassword, "destination": "duplicate@example.test",
	}, false), 204)
	if sent, err := iam.ProcessContactDelivery(t.Context()); err != nil || sent {
		t.Fatalf("duplicate username should not produce a registration challenge: sent=%v err=%v", sent, err)
	}
	checkStatus(t, anonymous.request(t, "POST", endpoint("first-portal", "login"), map[string]string{
		"username": "same-user", "password": duplicatePassword,
	}, false), 401)
	checkStatus(t, anonymous.request(t, "POST", endpoint("first-portal", "login"), map[string]string{
		"username": "same-user", "password": passwords["second-portal"], "realm": "second-portal",
	}, false), 401)
	checkStatus(t, anonymous.request(t, "POST", endpoint("not-registered", "login"), map[string]string{
		"username": "same-user", "password": passwords["second-portal"],
	}, false), 404)
	checkStatus(t, anonymous.request(t, "POST", endpoint("second-portal", "password-reset/request"), map[string]string{
		"username": "same-user", "channel": "email",
	}, false), 204)
	if sent, err := iam.ProcessContactDelivery(t.Context()); err != nil || !sent {
		t.Fatalf("password reset challenge missing: %v %v", sent, err)
	}
	reset := map[string]string{"token": code, "newPassword": "Replacement!Password2026"}
	checkStatus(t, anonymous.request(t, "POST", endpoint("first-portal", "password-reset/complete"), reset, false), 403)
	checkStatus(t, anonymous.request(t, "POST", endpoint("second-portal", "password-reset/complete"), reset, false), 204)
}
