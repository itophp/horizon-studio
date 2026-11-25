//go:build integration

package tests

import (
	"testing"

	"horizon/foundation/system/identity"
)

func TestIAMRuntimeProvisioningWithoutRestart(t *testing.T) {
	handler, pool, account := iamFixture(t)
	current := loginTenantIAM(t, handler, account.Username, "tenant-a")
	// The server has already registered deployment snapshots at this point.
	// The next Bootstrap must provision published heads without another startup.
	fresh, err := identity.Bootstrap(t.Context(), pool, "iam-runtime-new", "iam-runtime-new-admin", "TestOnly!2026Secure")
	if err != nil {
		t.Fatal(err)
	}
	newcomer := loginTenantIAM(t, handler, fresh.Username, "iam-runtime-new")
	for _, client := range []iamClient{current, newcomer} {
		for _, id := range []string{"account-security", "iam-governance"} {
			data := checkIAMInvocation(t, client.invoke(t, "application.runtime.resolve", map[string]any{"applicationId": id}, true))
			protocol := data["protocol"].(map[string]any)
			if protocol["route"] != "/"+id || data["applicationVersion"] != "1.0.0.0" {
				t.Fatal("IAM application was not immediately published", id, data)
			}
		}
	}
	var count int
	if err = pool.QueryRow(t.Context(), `SELECT count(*) FROM runtime_installation_templates
WHERE plugin_id IN ('builtin:account-security','builtin:iam-governance') AND version='1.0.0' AND enabled`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("baseline templates: count=%d err=%v", count, err)
	}
}
