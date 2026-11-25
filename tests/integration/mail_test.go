//go:build integration

package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"horizon/applications/mail"
	genidentity "horizon/foundation/gen/identity"
	"horizon/foundation/kernel/approval"
	"horizon/foundation/kernel/audit"
	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/capability/gateway"
	"horizon/foundation/kernel/capability/registry"
	"horizon/foundation/kernel/eventhub"
	"horizon/foundation/system/files"
	"horizon/foundation/system/governance"
	"horizon/foundation/system/identity"
	"horizon/internal/repository/dbgen"
	"horizon/internal/repository/dbgen/maildb"
)

type mailHarness struct {
	*projectHarness
	core *governance.Core
}

func TestMailRuntimeMailboxPageExecutesDeclaredAction(t *testing.T) {
	handler, pool, root := iamFixture(t)
	client := loginIAM(t, handler, root.Username)
	app := checkIAMInvocation(t, client.invoke(t, "application.runtime.resolve", map[string]any{"applicationId": "mail"}, true))
	protocol := app["protocol"].(map[string]any)
	if protocol["version"] != "1.0.10" {
		t.Fatalf("unexpected published mail Runtime version: %v", protocol["version"])
	}
	before := checkIAMInvocation(t, client.invoke(t, "mail.mailbox.list", map[string]any{}, true))
	if len(before["items"].([]any)) != 0 {
		t.Fatal("expected a fresh mailbox configuration")
	}

	result := checkIAMInvocation(t, client.request(t, "POST", "/api/capabilities/mail.mailbox.onboard/invocations", map[string]any{
		"version":        "1.0.0",
		"idempotencyKey": "mail-runtime-onboard",
		"input": map[string]any{
			"name": "Runtime test mailbox", "emailAddress": "owner@example.test", "password": "test-only-password",
			"smtpHost": "smtp.example.test", "smtpPort": 587, "smtpTls": "starttls",
			"pop3Host": "pop.example.test", "pop3Port": 995,
		},
		"runtime": map[string]any{
			"applicationId": "mail", "pageId": "mail.mailbox-page",
			"applicationVersion": app["applicationVersion"], "catalogRevision": app["catalogRevision"],
			"definitionDigest": app["definitionDigest"], "installationRevision": app["installationRevision"],
			"metadataVersion": app["metadataVersion"],
		},
	}, true))
	if result["status"] != "unverified" {
		t.Fatalf("mailbox should be saved after the disallowed test connection: %+v", result)
	}
	after := checkIAMInvocation(t, client.invoke(t, "mail.mailbox.list", map[string]any{}, true))
	if items := after["items"].([]any); len(items) != 1 || items[0].(map[string]any)["id"] != result["id"] {
		t.Fatal("the authorized Provider did not return the Runtime-created mailbox")
	}
	const originalHTML = `<table width="600"><tbody><tr><td style="color:#333"><a href="https://example.test/details"><strong>Message body</strong></a><img src="https://example.test/hero.png" width="240" alt="Hero"></td></tr></tbody></table>`
	message, err := dbgen.New(pool).MailMessageCreate(t.Context(), dbgen.MailMessageCreateParams{
		TenantID: "tenant-a", MailboxID: int64(result["id"].(float64)), OwnerAccountID: root.ID,
		Direction: "inbound", Folder: "inbox", ProviderUid: "encoded-subject",
		Subject: "=?GB18030?B?xOO6ww==?=", FromAddress: "sender@example.test",
		BodyText: "Message body", BodyHtml: originalHTML,
		OccurredAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	listed := checkIAMInvocation(t, client.invoke(t, "mail.message.list", map[string]any{"folder": "inbox"}, true))
	items := listed["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["subject"] != "你好" || items[0].(map[string]any)["from"] != "sender@example.test" {
		t.Fatal("inbox Provider did not return the decoded subject and sender")
	}
	detail := checkIAMInvocation(t, client.invoke(t, "mail.message.get", map[string]any{"id": message.ID}, true))
	if detail["subject"] != "你好" || detail["from"] != "sender@example.test" ||
		detail["bodyText"] != "Message body" || detail["bodyHtml"] != originalHTML {
		t.Fatal("detail Provider did not return the decoded headers and body")
	}

	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct{ Role, Content string }
			Tools    []json.RawMessage
		}
		if r.URL.Path != "/v1/chat/completions" || json.NewDecoder(r.Body).Decode(&request) != nil ||
			len(request.Messages) != 2 || request.Messages[0].Role != "system" ||
			request.Messages[1].Role != "user" || len(request.Tools) != 0 {
			t.Error("unexpected email model request")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		evidence := request.Messages[1].Content
		content := `{"items":[{"number":1,"reason":"Review the update."}]}`
		if strings.Contains(evidence, `"writingBrief":`) {
			if !strings.Contains(evidence, "Request draft review") || strings.Contains(evidence, "sender@example.test") {
				t.Error("expected only the writing brief in the draft request")
			}
			content = `{"suggestions":[{"field":"subject","value":"Draft review"},{"field":"body","value":"Please review the draft."},{"field":"to","value":"owner@example.test, ner@example.test, invented@example.test"},{"field":"cc","value":"reviewer@example.test; reviewer@example.test, second@example.test"}]}`
		}
		if strings.Contains(evidence, `"body":`) {
			if !strings.Contains(evidence, "Message body") || strings.Contains(evidence, "bodyHtml") {
				t.Error("expected only plain-text content in the analysis request")
			}
			content = `{"summary":"Message summary","keyPoints":[],"actionItems":[{"task":"Review","deadline":"Friday"}],"deadlines":["Friday"]}`
		}
		if strings.Contains(request.Messages[0].Content, "Translate the email subject and ordered text segments") {
			if !strings.Contains(request.Messages[0].Content, "Simplified Chinese") ||
				!strings.Contains(evidence, `"segments":["Message body"]`) || strings.Contains(evidence, "example.test") {
				t.Error("translation should receive text segments without mail headers, links or image sources")
			}
			content = `{"subject":"翻译的主题","segments":["翻译的正文 <b>literal</b>"]}`
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{
			"message":       map[string]string{"role": "assistant", "content": content},
			"finish_reason": "stop",
		}}})
	}))
	defer model.Close()
	saveAIConfig(t, client, 0, map[string]any{
		"aiModels.custom-mail.type": "text", "aiModels.custom-mail.displayName": "Mail test",
		"aiModels.custom-mail.baseUrl": model.URL + "/v1", "aiModels.custom-mail.providerId": "test",
		"aiModels.custom-mail.model": "mail-test", "aiModels.custom-mail.token": "test-token",
		"aiModels.default": "custom-mail", "agent.enabled": true,
	})
	invokeAnalysis := func(name, page string, input map[string]any) map[string]any {
		t.Helper()
		return checkIAMInvocation(t, client.request(t, "POST", "/api/capabilities/"+name+"/invocations", map[string]any{
			"version": "1.0.0", "idempotencyKey": page, "input": input,
			"runtime": map[string]any{
				"applicationId": "mail", "pageId": page,
				"applicationVersion": app["applicationVersion"], "catalogRevision": app["catalogRevision"],
				"definitionDigest": app["definitionDigest"], "installationRevision": app["installationRevision"],
				"metadataVersion": app["metadataVersion"],
			},
		}, true))
	}
	ranked := invokeAnalysis("mail.inbox.recommend", "mail.inbox", map[string]any{"locale": "en"})["items"].([]any)
	if len(ranked) != 1 || ranked[0].(map[string]any)["id"] != float64(message.ID) {
		t.Fatalf("recommendation did not resolve to the original message: %v", ranked)
	}
	analysis := invokeAnalysis("mail.message.analyze", "mail.detail-page", map[string]any{"id": message.ID, "locale": "en"})
	if analysis["summary"] != "Message summary" || analysis["truncated"] != false {
		t.Fatalf("unexpected message analysis: %v", analysis)
	}
	translation := invokeAnalysis("mail.message.translate", "mail.detail-page", map[string]any{"id": message.ID, "locale": "zh-CN"})
	if translation["subject"] != "翻译的主题" || translation["body"] != "翻译的正文 <b>literal</b>" || translation["truncated"] != false {
		t.Fatalf("unexpected message translation: %v", translation)
	}
	translatedHTML := translation["bodyHtml"].(string)
	for _, expected := range []string{`<table width="600">`, `style="color: #333"`, `href="https://example.test/details"`,
		`<img src="https://example.test/hero.png" width="240" alt="Hero">`, `<strong>翻译的正文 &lt;b&gt;literal&lt;/b&gt;</strong>`} {
		if !strings.Contains(translatedHTML, expected) {
			t.Fatalf("translation changed the original layout or markup: missing %q in %s", expected, translatedHTML)
		}
	}
	draft := invokeAnalysis("mail.message.suggest-draft", "mail.compose-page", map[string]any{
		"locale": "en", "sourceText": "Request draft review. To: owner@example.test; CC: Reviewer <reviewer@example.test>、 second@example.test。",
		"includeSubject": true, "includeTo": true, "includeCc": true,
	})
	suggestions := map[string]string{}
	for _, item := range draft["suggestions"].([]any) {
		suggestion := item.(map[string]any)
		suggestions[suggestion["field"].(string)] = suggestion["value"].(string)
	}
	if len(suggestions) != 4 || suggestions["subject"] != "Draft review" ||
		suggestions["to"] != "owner@example.test" || suggestions["cc"] != "reviewer@example.test, second@example.test" {
		t.Fatalf("unexpected draft suggestions: %v", suggestions)
	}
	var leaked int
	err = pool.QueryRow(t.Context(), `SELECT count(*) FROM cap_invocations
		WHERE capability IN ('mail.inbox.recommend','mail.message.analyze','mail.message.get','mail.message.suggest-draft','mail.message.translate')
		AND (result::text LIKE '%Message summary%' OR result::text LIKE '%Review the update.%' OR result::text LIKE '%Message body%'
		OR result::text LIKE '%翻译的正文%' OR result::text LIKE '%Please review the draft.%' OR input::text LIKE '%Request draft review%')`).Scan(&leaked)
	if err != nil || leaked != 0 {
		t.Fatalf("email content leaked into invocation history: count=%d err=%v", leaked, err)
	}
}

func newMailHarness(t *testing.T) *mailHarness {
	t.Helper()
	h := newProjectHarness(t)
	core := governance.New(h.pool, h.iam, governance.Options{
		EncryptionKey: []byte("01234567890123456789012345678901"),
		StorageRoot:   t.TempDir(),
	})
	files.New(core)
	module := mail.New(core, mail.Options{})
	catalog := registry.New(module.Entries())
	if err := catalog.Err(); err != nil {
		t.Fatal(err)
	}
	h.gw = gateway.New(gateway.Options{
		Registry: catalog, Identity: h.iam, Pool: h.pool,
		Approvals: approval.NewStore(h.pool), Audit: audit.NewStore(h.pool), Events: eventhub.NewStore(h.pool),
	})
	(mail.Plugin{Module: module}).AttachGateway(h.gw)
	return &mailHarness{projectHarness: h, core: core}
}

func (h *mailHarness) mailbox(t *testing.T, actor projectActor, address string, defaultSend bool) int64 {
	t.Helper()
	ctx := context.Background()
	q := dbgen.New(h.pool)
	id, err := q.MailMailboxCreate(ctx, dbgen.MailMailboxCreateParams{
		TenantID: "tenant-a", OwnerAccountID: actor.id, Name: address,
		EmailAddress: address, DefaultSend: defaultSend,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, purpose := range []string{"smtp", "pop3"} {
		ciphertext, err := h.core.Seal("tenant-a", fmt.Sprintf("mail_mailbox:%d", id), purpose+":v1", "app-password")
		if err != nil {
			t.Fatal(err)
		}
		credential, err := q.MailCredentialCreate(ctx, dbgen.MailCredentialCreateParams{
			TenantID: "tenant-a", MailboxID: id, Purpose: purpose,
			Ciphertext: ciphertext, Fingerprint: governance.Hash(ciphertext)[:16],
		})
		if err != nil {
			t.Fatal(err)
		}
		direction, protocol, host, port, tlsMode := "send", "smtp", "smtp.example.test", 587, "starttls"
		if purpose == "pop3" {
			direction, protocol, host, port, tlsMode = "receive", "pop3", "pop.example.test", 995, "tls"
		}
		if _, err = q.MailChannelCreate(ctx, dbgen.MailChannelCreateParams{
			TenantID: "tenant-a", MailboxID: id, Direction: direction, Protocol: protocol,
			Host: host, Port: int32(port), TlsMode: tlsMode, Username: address, CredentialID: credential.ID,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = q.MailMailboxStatusUpdate(ctx, dbgen.MailMailboxStatusUpdateParams{
		Status: "active", TenantID: "tenant-a", ID: id, OwnerAccountID: actor.id,
		Column5: []string{"unverified"},
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestMailUpdateDefaultMailboxPreservesRevisionCheck(t *testing.T) {
	h := newMailHarness(t)
	owner := h.account(t, "mail-owner", "mail:configure")
	first := h.mailbox(t, owner, "first@example.test", true)
	second := h.mailbox(t, owner, "second@example.test", false)
	type mailboxState struct {
		Revision    int64
		DefaultSend bool
		Channels    []struct {
			Direction string
			Port      int
		}
	}
	get := func(id int64) mailboxState {
		return decodePayload[mailboxState](t, h.ok(t, owner, "mail.mailbox.get", map[string]any{"id": id}).Data)
	}
	update := func(id, revision int64, address string) map[string]any {
		return map[string]any{
			"id": id, "revision": revision, "name": "Updated mailbox", "emailAddress": address, "defaultSend": true,
			"smtpHost": "smtp.example.test", "smtpPort": 2465, "smtpTls": "tls", "smtpUsername": address,
			"pop3Host": "pop.example.test", "pop3Port": 995, "pop3Username": address,
		}
	}

	before := get(first)
	h.ok(t, owner, "mail.mailbox.update", update(first, before.Revision, "first@example.test"))
	saved := get(first)
	if !saved.DefaultSend || saved.Revision != before.Revision+1 {
		t.Fatalf("editing the default mailbox should keep it default and increment the revision once: %+v", saved)
	}
	if len(saved.Channels) != 2 || saved.Channels[1].Direction != "send" || saved.Channels[1].Port != 2465 {
		t.Fatalf("saved channels should include SMTP on port 2465: %+v", saved.Channels)
	}

	h.ok(t, owner, "mail.mailbox.update", update(second, get(second).Revision, "second@example.test"))
	previous, current := get(first), get(second)
	if previous.DefaultSend || !current.DefaultSend {
		t.Fatalf("switching the default mailbox failed: previous=%+v current=%+v", previous, current)
	}
	result, err := h.call(t, owner, "mail.mailbox.update", update(first, saved.Revision, "first@example.test"))
	rejected(t, result, err, "conflict")
	if after := get(second); !after.DefaultSend || after.Revision != current.Revision {
		t.Fatalf("stale update changed the default mailbox: before=%+v after=%+v", current, after)
	}
}

func TestMailIMAPConfigurationAndWorker(t *testing.T) {
	h := newMailHarness(t)
	owner := h.account(t, "mail-imap-owner", "mail:configure")
	ctx := t.Context()
	input := map[string]any{
		"name": "IMAP mailbox", "emailAddress": "imap@example.test", "password": "app-password",
		"smtpHost": "smtp.example.test", "smtpPort": 587, "smtpTls": "starttls",
		"receiveProtocol": "imap", "receiveHost": "imap.example.test", "receivePort": 1143,
		"receiveTls": "starttls", "receiveUsername": "imap-login",
	}
	created := decodePayload[struct{ ID int64 }](t, h.ok(t, owner, "mail.mailbox.onboard", input).Data)
	get := func() dbgen.MailMailboxGetRow {
		t.Helper()
		row, err := dbgen.New(h.pool).MailMailboxGet(ctx, dbgen.MailMailboxGetParams{
			TenantID: "tenant-a", OwnerAccountID: owner.id, ID: created.ID,
		})
		if err != nil {
			t.Fatal(err)
		}
		return row
	}
	if row := get(); row.Status != "unverified" || !row.DefaultSend {
		t.Fatalf("disallowed connections should preserve an unverified IMAP mailbox: %+v", row)
	}
	test := decodePayload[struct{ Status string }](t, h.ok(t, owner, "mail.mailbox.connection.test", map[string]any{
		"mailboxId": created.ID, "direction": "receive",
	}).Data)
	if test.Status != "failed" {
		t.Fatalf("IMAP connection test must reject an unlisted endpoint, got %q", test.Status)
	}
	h.ok(t, owner, "mail.mailbox.enable", map[string]any{"id": created.ID})
	q := maildb.New(h.pool)
	due, err := q.ReceiveDueIDs(ctx, "tenant-a")
	if err != nil || len(due) != 1 {
		t.Fatalf("IMAP worker discovery: channels=%v err=%v", due, err)
	}
	load := func() maildb.ReceiveDueRow {
		t.Helper()
		row, err := q.ReceiveDue(ctx, maildb.ReceiveDueParams{TenantID: "tenant-a", ID: due[0].ID, Protocol: "imap"})
		if err != nil {
			t.Fatal(err)
		}
		return row
	}
	if row := load(); row.Protocol != "imap" || row.Port != 1143 || row.TlsMode != "starttls" || row.Username != "imap-login" {
		t.Fatalf("unexpected persisted IMAP connection: protocol=%s port=%d TLS=%s username=%s", row.Protocol, row.Port, row.TlsMode, row.Username)
	}
	worker, err := identity.NewPrincipal(&genidentity.AccountRecord{ID: 0, Status: "active"}, "tenant-a",
		[]*genidentity.PermissionGrant{{Permission: "mail:sync", Effect: "allow", Scope: "ALL"}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := h.callWith(t, projectActor{ctx: identity.WithPrincipal(ctx, worker)}, "mail.inbox.imap.sync",
		map[string]any{"channelId": due[0].ID}, "imap-worker-endpoint-denied", contracts.OriginEvent)
	rejected(t, result, err, "unavailable")
	if err := q.IMAPCursorSave(ctx, maildb.IMAPCursorSaveParams{TenantID: "tenant-a", ID: due[0].ID, ImapUidValidity: 41, ImapLastUid: 89}); err != nil {
		t.Fatal(err)
	}
	delete(input, "password")
	input["id"], input["defaultSend"], input["revision"] = created.ID, true, get().Revision
	input["name"] = "Renamed IMAP mailbox"
	h.ok(t, owner, "mail.mailbox.update", input)
	if row := load(); row.ImapUidValidity != 41 || row.ImapLastUid != 89 {
		t.Fatal("editing the display name reset the IMAP cursor")
	}
	input["revision"], input["receivePort"] = get().Revision, 993
	input["receiveTls"] = "tls"
	h.ok(t, owner, "mail.mailbox.update", input)
	if row := load(); row.ImapUidValidity != 0 || row.ImapLastUid != 0 || row.Port != 993 || row.TlsMode != "tls" {
		t.Fatal("changing IMAP endpoint did not save the new configuration and reset the cursor")
	}
}

func TestMailIMAPUpgradePreservesPOP3Mailbox(t *testing.T) {
	h := newMailHarness(t)
	owner := h.account(t, "mail-upgrade-owner", "mail:configure")
	id := h.mailbox(t, owner, "legacy@example.test", true)
	ctx := t.Context()
	_, err := h.pool.Exec(ctx, `ALTER TABLE mail_channels
		DROP CONSTRAINT mail_channels_check, DROP CONSTRAINT mail_channels_protocol_check,
		DROP COLUMN imap_uid_validity, DROP COLUMN imap_last_uid,
		ADD CONSTRAINT mail_channels_protocol_check CHECK (protocol IN ('smtp', 'pop3')),
		ADD CONSTRAINT mail_channels_check CHECK ((direction='send' AND protocol='smtp') OR
			(direction='receive' AND protocol='pop3' AND tls_mode='tls'))`)
	if err != nil {
		t.Fatal(err)
	}
	plugin := mail.Plugin{Module: mail.New(h.core, mail.Options{})}
	for range 2 {
		if err := plugin.Start(ctx); err != nil {
			t.Fatal(err)
		}
	}
	channels, err := maildb.New(h.pool).ReceiveDueIDs(ctx, "tenant-a")
	if err != nil || len(channels) != 1 || channels[0].Protocol != "pop3" {
		t.Fatalf("POP3 mailbox after upgrade: channels=%v err=%v", channels, err)
	}
	channel, err := maildb.New(h.pool).ReceiveDue(ctx, maildb.ReceiveDueParams{
		TenantID: "tenant-a", ID: channels[0].ID, Protocol: "pop3",
	})
	if err != nil || channel.MailboxID != id || channel.Port != 995 || channel.ImapUidValidity != 0 || channel.ImapLastUid != 0 {
		t.Fatalf("POP3 settings or cursor changed after upgrade: mailbox=%d port=%d err=%v", channel.MailboxID, channel.Port, err)
	}
}

func TestMailWorkersProcessSubmittedDeliveryAndCopyPOP3(t *testing.T) {
	h := newMailHarness(t)
	owner := h.account(t, "mail-worker-owner", "mail:send", "mail:sync")
	ctx := t.Context()
	mailboxID := h.mailbox(t, owner, "owner@example.test", true)
	submission := map[string]any{
		"clientKey": "mail-human-submission", "mailboxId": mailboxID,
		"to":      []any{map[string]any{"address": "recipient@example.test"}},
		"subject": "Worker test", "html": `<p><strong>Isolated test message</strong><script>alert(1)</script><img src="https://example.test/pixel"></p>`,
	}
	delivery := decodePayload[struct{ DeliveryID int64 }](t, h.ok(t, owner, "mail.message.send", submission).Data)
	var bodyText, bodyHTML string
	if err := h.pool.QueryRow(ctx, `SELECT m.body_text, m.body_html
		FROM mail_outbound_deliveries d JOIN mail_messages m ON m.tenant_id=d.tenant_id AND m.id=d.message_id
		WHERE d.tenant_id='tenant-a' AND d.id=$1`, delivery.DeliveryID).Scan(&bodyText, &bodyHTML); err != nil ||
		bodyText != "Isolated test message" || bodyHTML != "<p><strong>Isolated test message</strong></p>" {
		t.Fatalf("formatted mail was not stored with a safe HTML body and alternative text: text=%q html=%q err=%v", bodyText, bodyHTML, err)
	}
	worker, err := identity.NewPrincipal(&genidentity.AccountRecord{ID: 0, Status: "active"}, "tenant-a",
		[]*genidentity.PermissionGrant{{Permission: "mail:sync", Effect: "allow", Scope: "ALL"}})
	if err != nil {
		t.Fatal(err)
	}
	workerActor := projectActor{ctx: identity.WithPrincipal(ctx, worker)}
	process := map[string]any{"id": delivery.DeliveryID}
	result, err := h.call(t, owner, "mail.outbound.process", process)
	rejected(t, result, err, "forbidden")
	result, err = h.callWith(t, workerActor, "mail.outbound.process", process, "mail-worker-delivery", contracts.OriginEvent)
	succeeded(t, result, err)
	var attempts int
	var code string
	if err := h.pool.QueryRow(ctx, `SELECT attempts, error_code FROM mail_outbound_deliveries WHERE id=$1`, delivery.DeliveryID).Scan(&attempts, &code); err != nil || attempts != 1 || code != "unavailable" {
		t.Fatalf("worker should attempt the queued delivery once and record its unlisted endpoint: attempts=%d code=%q err=%v", attempts, code, err)
	}
	submission["clientKey"] = "mail-agent-submission"
	result, err = h.gw.Invoke(owner.ctx, gateway.Request{
		Name: "mail.message.send", Version: "1.0.0", Origin: contracts.OriginAgent,
		Input: contracts.Input(submission), IdempotencyKey: "mail-agent-submission",
	})
	if err != nil || result == nil || result.Status != contracts.StatusAwaitingApproval {
		t.Fatalf("agent submission should require confirmation: result=%+v err=%v", result, err)
	}
	q := maildb.New(h.pool)
	channels, err := q.ReceiveDueIDs(ctx, "tenant-a")
	if err != nil || len(channels) != 1 || channels[0].Protocol != "pop3" {
		t.Fatalf("expected one due POP3 channel: channels=%+v err=%v", channels, err)
	}
	result, err = h.callWith(t, workerActor, "mail.inbox.sync", map[string]any{"channelId": channels[0].ID},
		"mail-worker-pop3", contracts.OriginEvent)
	rejected(t, result, err, "unavailable") // The system action reached the endpoint allowlist.
	if _, err := dbgen.New(h.pool).MailMessageCreate(ctx, dbgen.MailMessageCreateParams{
		TenantID: "tenant-a", MailboxID: mailboxID, OwnerAccountID: owner.id,
		Direction: "inbound", Folder: "inbox", ProviderUid: "known-uid",
		OccurredAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}); err != nil {
		t.Fatal(err)
	}
	pending, err := q.PendingPOP3UIDs(ctx, maildb.PendingPOP3UIDsParams{
		TenantID: "tenant-a", MailboxID: mailboxID,
		Uids: []string{"known-uid", "new-uid", "later-uid"}, MaxRows: 1,
	})
	if err != nil || !slices.Equal(pending, []string{"new-uid"}) {
		t.Fatalf("existing remote messages should not occupy the POP3 batch: pending=%v err=%v", pending, err)
	}
}

func TestMailMailboxesAndMessagesAreAccountPrivate(t *testing.T) {
	h := newMailHarness(t)
	owner := h.account(t, "mail-owner", "mail:read", "mail:send", "mail:configure")
	other := h.account(t, "mail-other", "mail:read", "mail:send", "mail:configure")
	onboarding := h.account(t, "mail-onboarding", "mail:configure")
	empty := h.ok(t, onboarding, "mail.mailbox.list", nil)
	if payload := decodePayload[struct{ Items []json.RawMessage }](t, empty.Data); payload.Items == nil || len(payload.Items) != 0 {
		t.Fatalf("unconfigured account must return an empty mailbox list: %+v", payload)
	}
	first := h.mailbox(t, owner, "first@example.test", true)
	second := h.mailbox(t, owner, "second@example.test", false)
	h.mailbox(t, other, "other@example.test", true)
	unverified := h.ok(t, onboarding, "mail.mailbox.onboard", map[string]any{
		"name": "Unverified", "emailAddress": "unverified@example.test", "password": "app-password",
		"smtpHost": "smtp.example.test", "smtpPort": 587, "smtpTls": "starttls",
		"pop3Host": "pop.example.test", "pop3Port": 995,
	})
	unverifiedPayload := decodePayload[struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}](t, unverified.Data)
	if unverifiedPayload.Status != "unverified" {
		t.Fatalf("disallowed endpoint did not preserve unverified mailbox: %+v", unverifiedPayload)
	}
	var failedChannels int
	if err := h.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM mail_channels WHERE tenant_id='tenant-a' AND mailbox_id=$1 AND verification_status='failed'`,
		unverifiedPayload.ID).Scan(&failedChannels); err != nil || failedChannels != 2 {
		t.Fatalf("failed channels=%d err=%v", failedChannels, err)
	}

	ownerList := decodePayload[struct {
		Items []struct {
			ID           int64  `json:"id"`
			EmailAddress string `json:"emailAddress"`
		} `json:"items"`
	}](t, h.ok(t, owner, "mail.mailbox.list", nil).Data)
	if len(ownerList.Items) != 2 || ownerList.Items[0].ID != first || ownerList.Items[1].ID != second {
		t.Fatalf("owner mailboxes = %+v", ownerList.Items)
	}

	sendResult := h.ok(t, owner, "mail.message.send", map[string]any{
		"clientKey": "mail-integration-key", "mailboxId": first,
		"to":      []any{map[string]any{"address": "recipient@example.test"}},
		"subject": "Integration", "markdown": "**private integration body**",
	})
	if sendResult.Status != contracts.StatusSucceeded {
		t.Fatalf("send status=%s code=%s", sendResult.Status, sendResult.ErrorCode)
	}
	approved := sendResult
	payload := decodePayload[struct {
		MessageID  int64 `json:"messageId"`
		DeliveryID int64 `json:"deliveryId"`
	}](t, approved.Data)
	if payload.MessageID == 0 || payload.DeliveryID == 0 {
		t.Fatalf("missing ids: %+v", payload)
	}
	_, duplicateErr := h.callWith(t, owner, "mail.message.send", map[string]any{
		"clientKey": "mail-integration-key", "mailboxId": first,
		"to":      []any{map[string]any{"address": "recipient@example.test"}},
		"subject": "Integration", "markdown": "**private integration body**",
	}, "mail-duplicate-send", contracts.OriginHumanUI)
	rejected(t, nil, duplicateErr, "conflict")

	var message, recipient, delivery, event int
	ctx := context.Background()
	if err := h.pool.QueryRow(ctx, `SELECT
	  (SELECT count(*) FROM mail_messages WHERE tenant_id='tenant-a' AND id=$1 AND owner_account_id=$2),
	  (SELECT count(*) FROM mail_message_recipients WHERE tenant_id='tenant-a' AND message_id=$1),
	  (SELECT count(*) FROM mail_outbound_deliveries WHERE tenant_id='tenant-a' AND message_id=$1 AND status='queued'),
	  (SELECT count(*) FROM cap_outbox WHERE tenant_id='tenant-a' AND event_type='mail.message.submitted')`,
		payload.MessageID, owner.id).Scan(&message, &recipient, &delivery, &event); err != nil {
		t.Fatal(err)
	}
	if message != 1 || recipient != 1 || delivery != 1 || event != 1 {
		t.Fatalf("message=%d recipient=%d delivery=%d event=%d", message, recipient, delivery, event)
	}

	_, otherErr := h.call(t, other, "mail.message.get", map[string]any{"id": payload.MessageID})
	rejected(t, nil, otherErr, "not_found")
	draftInput := map[string]any{"sourceText": "Request draft review", "includeSubject": true, "locale": "en"}
	res, err := h.call(t, onboarding, "mail.message.suggest-draft", draftInput)
	rejected(t, res, err, "forbidden")
	res, err = h.callWith(t, owner, "mail.message.suggest-draft", draftInput, "", contracts.OriginAgent)
	rejected(t, res, err, "forbidden")
	analysisInput := map[string]any{"id": payload.MessageID, "locale": "en"}
	res, err = h.call(t, other, "mail.message.analyze", analysisInput)
	rejected(t, res, err, "not_found")
	res, err = h.call(t, other, "mail.message.translate", analysisInput)
	rejected(t, res, err, "not_found")
	res, err = h.callWith(t, owner, "mail.message.translate", analysisInput, "", contracts.OriginAgent)
	rejected(t, res, err, "forbidden")
	res, err = h.callWith(t, owner, "mail.message.analyze", analysisInput, "", contracts.OriginAgent)
	rejected(t, res, err, "forbidden")
	res, err = h.call(t, onboarding, "mail.inbox.recommend", map[string]any{"locale": "en"})
	rejected(t, res, err, "forbidden")
	recent := h.ok(t, other, "mail.inbox.recommend", map[string]any{"locale": "en"})
	if items := decodePayload[struct{ Items []json.RawMessage }](t, recent.Data).Items; len(items) != 0 {
		t.Fatalf("unexpected recommendations for empty inbox: %v", items)
	}
	var leaks int
	if err := h.pool.QueryRow(ctx,
		`SELECT count(*) FROM cap_invocations WHERE capability='mail.message.send' AND (input::text LIKE '%private integration body%' OR result::text LIKE '%private integration body%')`,
	).Scan(&leaks); err != nil || leaks != 0 {
		t.Fatalf("body leaked into invocation audit: count=%d err=%v", leaks, err)
	}
	var credentialProjection []byte
	if err := h.pool.QueryRow(ctx, `SELECT input FROM cap_invocations WHERE capability='mail.mailbox.list' ORDER BY id DESC LIMIT 1`).Scan(&credentialProjection); err != nil {
		t.Fatal(err)
	}
	var invocationInput map[string]any
	if err := json.Unmarshal(credentialProjection, &invocationInput); err != nil {
		t.Fatal(err)
	}
	if _, exists := invocationInput["password"]; exists {
		t.Fatal("credential field appeared in audited input")
	}
}
