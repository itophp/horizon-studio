//go:build integration

package tests

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	genidentity "horizon/foundation/gen/identity"
	"horizon/foundation/kernel/audit"
)

func TestIAMSecurityAuditCheckpoint(t *testing.T) {
	handler, pool, account := iamFixture(t)
	client := loginTenantIAM(t, handler, account.Username, "tenant-a")
	checkIAMInvocation(t, client.invoke(t, "example.record.create", map[string]any{"title": "Redacted order title"}, true))
	denied := iamClient{handler: handler}
	checkStatus(t, denied.request(t, "POST", "/api/auth/login", map[string]string{"username": account.Username, "password": "must-not-be-audit-data"}, false), 401)
	response := client.invoke(t, "identity.audit.export", map[string]any{"workspace": true, "limit": 50}, true)
	items := checkIAMInvocation(t, response)["items"].([]any)
	if bytes.Contains(response.Body.Bytes(), []byte("Redacted order title")) || bytes.Contains(response.Body.Bytes(), []byte("must-not-be-audit-data")) {
		t.Fatal("audit projection included business input or credentials")
	}
	var linked, session bool
	var principalID string
	for _, item := range items {
		entry := item.(map[string]any)
		switch entry["action"] {
		case "example.record.created":
			linked = entry["resourceType"] == "example_record" && entry["resourceId"] != "" && entry["correlationId"] != ""
		case "example.record.create":
			if entry["resourceType"] == "invocation" && entry["origin"] == "human-ui" && entry["capabilityVersion"] == "1.0.0" {
				principalID, _ = entry["principalId"].(string)
			}
		case "identity.session.start":
			session = entry["outcome"] == "allowed" && entry["correlationId"] != ""
		}
	}
	if !linked || !session {
		t.Fatalf("missing correlated audit events: linked=%v session=%v", linked, session)
	}
	var anonymousDenials int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM iam_security_events
	WHERE tenant_id = '' AND account_id = 0 AND action = 'identity.login'
	AND outcome = 'denied' AND reason = 'credentials' AND correlation_id <> ''`).Scan(&anonymousDenials); err != nil || anonymousDenials != 1 {
		t.Fatalf("unscoped login denial was not recorded: %d %v", anonymousDenials, err)
	}
	if principalID == "" {
		t.Fatal("invocation audit is missing its trusted authority")
	}
	filtered := checkIAMInvocation(t, client.invoke(t, "identity.audit.list", map[string]any{
		"workspace": true, "principalId": principalID, "origin": "human-ui", "action": "example.record.create",
	}, true))["items"].([]any)
	if len(filtered) == 0 {
		t.Fatal("trusted authority was not searchable")
	}
	log := audit.NewSecurityLog(pool, bytes.Repeat([]byte{42}, 32))
	checkpoint, err := log.Checkpoint(t.Context())
	if err != nil || checkpoint == 0 {
		t.Fatalf("checkpoint=%d err=%v", checkpoint, err)
	}
	if err = log.Verify(t.Context(), checkpoint); err != nil {
		t.Fatal(err)
	}
	previousKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{42}, 32)).Public().(ed25519.PublicKey)
	rotated := audit.NewSecurityLog(pool, bytes.Repeat([]byte{43}, 32), previousKey)
	loginIAM(t, handler, account.Username)
	next, err := rotated.Checkpoint(t.Context())
	if err != nil || next <= checkpoint {
		t.Fatalf("rotated checkpoint=%d previous=%d err=%v", next, checkpoint, err)
	}
	if err = rotated.Verify(t.Context(), next); err != nil {
		t.Fatal(err)
	}
	missingHistory := audit.NewSecurityLog(pool, bytes.Repeat([]byte{43}, 32))
	if err = missingHistory.Verify(t.Context(), next); !errors.Is(err, audit.ErrAuditIntegrity) {
		t.Fatalf("checkpoint verification accepted a missing historical key: %v", err)
	}
	if moved, err := rotated.ArchiveBefore(t.Context(), time.Now().Add(time.Hour)); err != nil || moved < 1 {
		t.Fatalf("verified checkpoint was not archived: %d %v", moved, err)
	}
	if err = rotated.Verify(t.Context(), checkpoint); err != nil {
		t.Fatalf("archiving changed the signed checkpoint: %v", err)
	}
	archived := checkIAMInvocation(t, client.invoke(t, "identity.audit.list",
		map[string]any{"workspace": true, "action": "example.record.created", "outcome": "committed"}, true))["items"].([]any)
	if len(archived) != 1 {
		t.Fatalf("archived event disappeared from scoped audit search: %v", archived)
	}
	if _, err = pool.Exec(t.Context(), `UPDATE iam_security_event_archive SET outcome='edited' WHERE id=$1`, checkpoint); err == nil {
		t.Fatal("audit row could be edited through regular DML")
	}
	if _, err = pool.Exec(t.Context(), `ALTER TABLE iam_security_event_archive DISABLE TRIGGER iam_security_archive_append_only`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(t.Context(), `UPDATE iam_security_event_archive SET authority='{"origin":"edited"}'::jsonb WHERE id=$1`, checkpoint); err != nil {
		t.Fatal(err)
	}
	if err = log.Verify(t.Context(), checkpoint); !errors.Is(err, audit.ErrAuditIntegrity) {
		t.Fatalf("privileged tampering was not detected: %v", err)
	}
}

func TestIAMSecurityAuditRetentionPreservesSignedEvidence(t *testing.T) {
	handler, pool, account := iamFixture(t)
	loginIAM(t, handler, account.Username)
	ctx := t.Context()
	log := audit.NewSecurityLog(pool, bytes.Repeat([]byte{42}, 32))
	first, err := log.Checkpoint(ctx)
	if err != nil || first == 0 {
		t.Fatalf("initial checkpoint: %d %v", first, err)
	}
	cutoff := time.Now()
	if moved, err := log.ArchiveBefore(ctx, cutoff); moved == 0 || err != nil {
		t.Fatalf("archive: %d %v", moved, err)
	}
	if _, err = pool.Exec(ctx, `DELETE FROM iam_security_event_archive WHERE id=$1`, first); err == nil {
		t.Fatal("ordinary deletion bypassed audit retention")
	}
	if err = log.ExpireBefore(ctx, cutoff.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err = log.Verify(ctx, first); err != nil {
		t.Fatalf("records inside retention window were removed: %v", err)
	}
	if err = log.ExpireBefore(ctx, cutoff); err != nil {
		t.Fatal(err)
	}
	if err = log.Verify(ctx, first); !errors.Is(err, audit.ErrAuditExpired) {
		t.Fatalf("expired checkpoint lost its signed receipt: %v", err)
	}
	if _, err = pool.Exec(ctx, `DELETE FROM iam_security_retention_receipts WHERE checkpoint_id=$1`, first); err == nil {
		t.Fatal("retention evidence was deletable")
	}
	loginIAM(t, handler, account.Username)
	second, err := log.Checkpoint(ctx)
	if err != nil || second <= first {
		t.Fatalf("chain stopped after expiry: %d %v", second, err)
	}
	if err = log.Verify(ctx, second); err != nil {
		t.Fatalf("new checkpoint after expiry did not verify: %v", err)
	}
	if _, err = pool.Exec(ctx, `ALTER TABLE iam_security_retention_receipts DISABLE TRIGGER iam_security_receipt_append_only`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE iam_security_retention_receipts SET cutoff=cutoff+interval '1 second' WHERE checkpoint_id=$1`, first); err != nil {
		t.Fatal(err)
	}
	if err = log.Verify(ctx, first); !errors.Is(err, audit.ErrAuditIntegrity) {
		t.Fatalf("edited retention receipt was treated as legitimate expiry: %v", err)
	}
}

func TestIAMSecurityAuditCursorAndScope(t *testing.T) {
	handler, pool, account := iamFixture(t)
	admin := loginTenantIAM(t, handler, account.Username, "tenant-a")
	created := checkIAMInvocation(t, admin.invoke(t, "identity.account.create", map[string]any{
		"username": "audit-reader", "fullName": "Audit reader", "initialStatus": "active", "password": "TestOnly!2026Secure",
	}, true))
	readerID := int64(created["id"].(float64))
	seedTenantRole(t, pool, "tenant-a", "audit-reader", account.ID, readerID,
		[]*genidentity.PermissionGrant{{Permission: "accounts:read", Effect: "allow", Scope: "OWN"}})
	reader := loginTenantIAM(t, handler, "audit-reader", "tenant-a")
	// Insert adversarial projections to exercise the SQL scope independently of
	// the producers: other tenant/account/binding must all be hidden.
	for index := range 60 {
		if _, err := pool.Exec(t.Context(), `INSERT INTO iam_security_events
   (tenant_id, account_id, action, outcome, reason, correlation_id, resource_type, resource_id)
   VALUES ('tenant-a', $1, 'audit-fixture.own', 'allowed', 'test', $2, 'example_record', 'test')`, readerID, fmt.Sprintf("own-%d", index)); err != nil {
			t.Fatal(err)
		}
	}
	for _, other := range []struct {
		tenant  string
		actor   int64
		binding string
	}{
		{"tenant-b", readerID, ""},
		{"tenant-a", account.ID, ""},
		{"tenant-a", readerID, "11111111-1111-4111-8111-111111111111"},
	} {
		if _, err := pool.Exec(t.Context(), `INSERT INTO iam_security_events
   (tenant_id, account_id, binding_id, action, outcome, reason, correlation_id, resource_type, resource_id)
   VALUES ($1, $2, NULLIF($3, '')::uuid, 'audit-fixture.foreign', 'allowed', 'test', $1, 'example_record', 'test')`, other.tenant, other.actor, other.binding); err != nil {
			t.Fatal(err)
		}
	}
	live := checkIAMInvocation(t, reader.invoke(t, "identity.audit.list", map[string]any{"limit": 30}, true))["items"].([]any)
	for _, raw := range live {
		row := raw.(map[string]any)
		if row["action"] == "audit-fixture.foreign" || int64(row["accountId"].(float64)) != readerID {
			t.Fatal("live audit scope leaked", row)
		}
	}
	// Auditing a query appends its own invocation event. Pin fixture pages so
	// their expected boundaries remain independent of these newer audit writes.
	var fixtureTop int64
	if err := pool.QueryRow(t.Context(), `SELECT max(id) FROM iam_security_events WHERE action='audit-fixture.own'`).Scan(&fixtureTop); err != nil {
		t.Fatal(err)
	}
	first := checkIAMInvocation(t, reader.invoke(t, "identity.audit.list", map[string]any{"limit": 30, "beforeId": fixtureTop + 1}, true))["items"].([]any)
	if len(first) != 30 {
		t.Fatalf("page size=%d", len(first))
	}
	for _, raw := range first {
		row := raw.(map[string]any)
		if row["action"] != "audit-fixture.own" || int64(row["accountId"].(float64)) != readerID {
			t.Fatal("audit scope leaked", row)
		}
	}
	boundary := first[len(first)-1].(map[string]any)["id"]
	next := checkIAMInvocation(t, reader.invoke(t, "identity.audit.list", map[string]any{"limit": 30, "beforeId": boundary}, true))["items"].([]any)
	if len(next) != 30 {
		t.Fatalf("next page size=%d", len(next))
	}
	for _, raw := range next {
		if raw.(map[string]any)["id"].(float64) >= boundary.(float64) {
			t.Fatal("cursor repeated an event")
		}
	}
	pinned := map[string]any{"beforeId": first[0].(map[string]any)["id"].(float64) + 1, "limit": 30}
	exported := checkIAMInvocation(t, reader.invoke(t, "identity.audit.export", pinned, true))["items"].([]any)
	if !reflect.DeepEqual(exported, first) {
		t.Fatal("pinned export differs from authorized page")
	}
	for _, operation := range []string{"list", "export"} {
		checkStatus(t, reader.invoke(t, "identity.audit."+operation, map[string]any{"workspace": true}, true), 403)
	}
	workspace := checkIAMInvocation(t, admin.invoke(t, "identity.audit.list", map[string]any{"workspace": true, "limit": 100}, true))["items"].([]any)
	sawOther := false
	for _, raw := range workspace {
		row := raw.(map[string]any)
		if row["action"] == "audit-fixture.foreign" {
			if row["correlationId"] == "tenant-b" {
				t.Fatal("workspace audit crossed tenant boundary")
			}
			sawOther = true
		}
	}
	if !sawOther {
		t.Fatal("authorized administrator could not see workspace events")
	}
}
