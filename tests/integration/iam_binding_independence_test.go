//go:build integration

package tests

import (
	"testing"

	genidentity "horizon/foundation/gen/identity"
)

// A business record remains a plugin-owned reference in this example. The two
// bindings deliberately belong to the SAME login/principal, unlike the supplier
// isolation fixture, which uses two people.
func TestIAMSamePersonBindingIndependence(t *testing.T) {
	handler, pool, account := iamFixture(t)
	owner := loginIAM(t, handler, account.Username)
	newBinding := func(identityType, title string) (any, any) {
		t.Helper()
		reference := checkIAMInvocation(t, owner.invoke(t, "example.record.create", map[string]any{"title": title}, true))
		binding := checkIAMInvocation(t, owner.invoke(t, "identity.binding.claim", map[string]any{
			"identityType": identityType, "version": "1.0.0", "entityId": reference["id"],
		}, true))
		return reference["id"], binding["id"]
	}
	employeeRecord, employeeID := newBinding("com.horizon.example.employee", "Employee reference")
	customerRecord, customerID := newBinding("com.horizon.example.customer-contact", "Customer contact reference")
	selectBinding := func(client iamClient, binding any) iamClient {
		t.Helper()
		selected := client.request(t, "POST", "/api/auth/identity", map[string]any{
			"tenantId": "tenant-a", "departmentId": account.DepartmentID, "identityBindingId": binding,
		}, true)
		checkStatus(t, selected, 200)
		profile := decodeBody[genidentity.SessionProfile](t, selected)
		if profile.Account.ID != account.ID || profile.IdentityBindingID == nil || *profile.IdentityBindingID != binding {
			t.Fatal("binding selection changed the person's account or binding")
		}
		client.csrf = profile.CsrfToken
		return client
	}
	employee := selectBinding(loginIAM(t, handler, account.Username), employeeID)
	customer := selectBinding(loginIAM(t, handler, account.Username), customerID)
	var principals int
	if err := pool.QueryRow(t.Context(), `SELECT count(DISTINCT principal_id) FROM iam_identity_bindings
WHERE id IN ($1::uuid, $2::uuid)`, employeeID, customerID).Scan(&principals); err != nil || principals != 1 {
		t.Fatalf("fixture is not the same principal: %d %v", principals, err)
	}
	checkIAMInvocation(t, owner.invoke(t, "example.record.archive", map[string]any{"id": employeeRecord}, true))
	checkStatus(t, employee.request(t, "GET", "/api/auth/me", nil, false), 401)
	checkStatus(t, customer.request(t, "GET", "/api/auth/me", nil, false), 200)
	checkIAMInvocation(t, customer.invoke(t, "example.record.get", map[string]any{"id": customerRecord}, true))
	// Even an existing customer session cannot switch back to the invalid binding.
	checkStatus(t, customer.request(t, "POST", "/api/auth/identity", map[string]any{
		"tenantId": "tenant-a", "departmentId": account.DepartmentID, "identityBindingId": employeeID,
	}, true), 403)
	// A new login still has the valid customer identity, not just a cached session.
	fresh := selectBinding(loginIAM(t, handler, account.Username), customerID)
	checkIAMInvocation(t, fresh.invoke(t, "example.record.get", map[string]any{"id": customerRecord}, true))
}
