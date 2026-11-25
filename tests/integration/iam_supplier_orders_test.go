//go:build integration

package tests

import (
	"testing"

	genidentity "horizon/foundation/gen/identity"
	"horizon/foundation/kernel/capability/contracts"
)

func TestIAMSupplierOrderIsolation(t *testing.T) {
	handler, _, account := iamFixture(t)
	admin := loginIAM(t, handler, account.Username)
	grants := []map[string]any{}
	for _, permission := range []string{"example:read", "example:create", "example-orders:read", "example-orders:write"} {
		grants = append(grants, map[string]any{"permission": permission, "effect": "allow", "scope": "ALL"})
	}
	role := checkIAMInvocation(t, admin.invoke(t, "identity.role.create", map[string]any{
		"name": "Supplier", "code": "supplier", "description": "", "status": "active", "grants": grants,
	}, true))
	grantRole := func(accountID, bindingID any) {
		t.Helper()
		input := map[string]any{
			"accountId": accountID, "roleId": role["id"], "minutes": 30, "reason": "Verify supplier order isolation",
		}
		if bindingID != nil {
			input["bindingId"] = bindingID
		}
		checkIAMInvocation(t, admin.invoke(t, "identity.roleGrant.create", input, true))
	}
	newSupplier := func(username string) (iamClient, any) {
		t.Helper()
		supplier := checkIAMInvocation(t, admin.invoke(t, "identity.account.create", map[string]any{
			"username": username, "fullName": username, "initialStatus": "active", "password": "TestOnly!2026Secure",
		}, true))
		grantRole(supplier["id"], nil)
		client := loginIAM(t, handler, username)
		record := checkIAMInvocation(t, client.invoke(t, "example.record.create", map[string]any{"title": username}, true))
		binding := checkIAMInvocation(t, client.invoke(t, "identity.binding.claim", map[string]any{
			"identityType": "com.horizon.example.supplier-contact", "version": "1.0.0", "entityId": record["id"],
		}, true))
		grantRole(supplier["id"], binding["id"])
		selected := client.request(t, "POST", "/api/auth/identity", map[string]any{
			"tenantId": "tenant-a", "identityBindingId": binding["id"],
		}, true)
		checkStatus(t, selected, 200)
		client.csrf = decodeBody[genidentity.SessionProfile](t, selected).CsrfToken
		return client, record["id"]
	}
	first, firstReference := newSupplier("supplier-a")
	second, secondReference := newSupplier("supplier-b")
	create := func(reference any, title string) any {
		t.Helper()
		return checkIAMInvocation(t, admin.invoke(t, "example.order.create", map[string]any{"supplierId": reference, "title": title}, true))["id"]
	}
	firstOrder := create(firstReference, "First supplier order")
	secondOrder := create(secondReference, "Second supplier order")
	// The caller cannot supply its authorization context, including through
	// alternate field spelling. None of these rejected requests may write.
	for _, operation := range []struct {
		name  string
		input map[string]any
	}{
		{"list", map[string]any{}}, {"export", map[string]any{}},
		{"get", map[string]any{"id": secondOrder}},
		{"update", map[string]any{"id": firstOrder, "title": "Forged context write"}},
		{"batchUpdate", map[string]any{"ids": []any{firstOrder}, "title": "Forged context batch"}},
	} {
		for _, forged := range []struct {
			field string
			value any
		}{{"tenant_id", "tenant-b"}, {"tenantId", "tenant-b"}, {"supplier_id", secondReference}, {"supplierId", secondReference}, {"actingOrganizationId", 1}} {
			input := make(map[string]any, len(operation.input)+1)
			for key, value := range operation.input {
				input[key] = value
			}
			input[forged.field] = forged.value
			t.Run(operation.name+"/"+forged.field, func(t *testing.T) {
				checkStatus(t, first.invoke(t, "example.order."+operation.name, input, true), 400)
			})
		}
	}
	for _, operation := range []string{"list", "export"} {
		page := checkIAMInvocation(t, first.invoke(t, "example.order."+operation, map[string]any{}, true))
		items := page["items"].([]any)
		if page["total"] != float64(1) || len(items) != 1 || items[0].(map[string]any)["id"] != firstOrder {
			t.Fatalf("%s crossed the supplier boundary: %+v", operation, page)
		}
	}
	denied := func(operation string, input map[string]any) {
		t.Helper()
		response := first.invoke(t, "example.order."+operation, input, true)
		checkStatus(t, response, 200)
		result := decodeBody[contracts.Result](t, response)
		if result.Status != contracts.StatusFailed || result.ErrorCode != "not_found" {
			t.Fatalf("%s crossed the supplier boundary: %s", operation, response.Body.String())
		}
	}
	denied("get", map[string]any{"id": secondOrder})
	denied("update", map[string]any{"id": secondOrder, "title": "Foreign write"})
	denied("batchUpdate", map[string]any{"ids": []any{firstOrder, secondOrder}, "title": "Partial write"})
	lookup := func(client iamClient, id any) any {
		t.Helper()
		return checkIAMInvocation(t, client.invoke(t, "example.order.get", map[string]any{"id": id}, true))["title"]
	}
	if lookup(first, firstOrder) != "First supplier order" || lookup(second, secondOrder) != "Second supplier order" {
		t.Fatal("rejected batch changed an order")
	}
	checkIAMInvocation(t, first.invoke(t, "example.order.update", map[string]any{"id": firstOrder, "title": "Own write"}, true))
	if lookup(first, firstOrder) != "Own write" {
		t.Fatal("supplier could not update an in-scope order")
	}
	checkIAMInvocation(t, admin.invoke(t, "example.record.archive", map[string]any{"id": firstReference}, true))
	checkStatus(t, first.request(t, "GET", "/api/auth/me", nil, false), 401)
	if lookup(second, secondOrder) != "Second supplier order" {
		t.Fatal("one supplier binding revoked another supplier")
	}
}
