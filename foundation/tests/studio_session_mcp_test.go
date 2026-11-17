//go:build integration

package tests

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	businessmcp "horizon/foundation/integrations/mcp"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestStudioSessionContextMCP(t *testing.T) {
	handler, _, account := iamFixture(t)
	owner := loginIAM(t, handler, "root-admin")
	root := t.TempDir()
	api := filepath.Join(root, "foundation", "cmd", "api")
	if err := os.MkdirAll(api, 0700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(root, "go.mod"), filepath.Join(api, "main.go")} {
		if err := os.WriteFile(path, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	project := studioCall(t, owner, "studio.project.link", map[string]any{
		"path": root, "name": "Studio MCP project", "developmentBrief": "Follow project rules.",
	})
	projectID := project.Data.(map[string]any)["id"]
	requirement := studioCall(t, owner, "studio.requirement.create", map[string]any{
		"project": projectID, "title": "Grouped requirement", "description": "Read this through MCP.",
	})
	requirementID := requirement.Data.(map[string]any)["id"]
	attachment := studioCall(t, owner, "file.upload", map[string]any{
		"name": "note.txt", "purpose": "attachment",
		"content": base64.StdEncoding.EncodeToString([]byte("hello attachment")),
	})
	attachmentID := attachment.Data.(map[string]any)["id"]
	studioCall(t, owner, "studio.requirement.attachment.add", map[string]any{
		"project": projectID, "requirement": requirementID, "file": attachmentID,
	})
	task := studioCall(t, owner, "studio.item.create", map[string]any{
		"project": projectID, "kind": "execution", "title": "First task", "requirementId": requirementID, "checks": "Verify the exact session.",
	})
	taskID := task.Data.(map[string]any)["id"]
	const sessionID = "dsh-test-session"
	link := studioCall(t, owner, "studio.item.link_session", map[string]any{"id": taskID, "sessionId": sessionID})
	if link.Status != "succeeded" || len(link.EventIds) != 1 {
		t.Fatalf("binding did not commit with its event: %+v", link)
	}
	retry := studioCall(t, owner, "studio.item.link_session", map[string]any{"id": taskID, "sessionId": "racing-session"})
	if retry.Status != "succeeded" || retry.Data.(map[string]any)["sessionId"] != sessionID {
		t.Fatalf("a later request changed the canonical binding: %+v", retry)
	}
	unlinked := studioCall(t, owner, "studio.item.create", map[string]any{"project": projectID, "kind": "execution", "title": "Standalone task"})
	unlinkedID := unlinked.Data.(map[string]any)["id"]
	duplicate := studioCall(t, owner, "studio.item.link_session", map[string]any{"id": unlinkedID, "sessionId": sessionID})
	if duplicate.ErrorCode != "conflict" {
		t.Fatalf("duplicate session ID accepted: %+v", duplicate)
	}
	studioCall(t, owner, "studio.item.link_session", map[string]any{"id": unlinkedID, "sessionId": "standalone-session"})
	standalone := studioCall(t, owner, "studio.session.context", map[string]any{"sessionId": "standalone-session"})
	if standalone.Status != "succeeded" || standalone.Data.(map[string]any)["requirement"] != nil {
		t.Fatalf("standalone session returned a requirement: %+v", standalone)
	}

	checkSucceeded(t, owner.invoke(t, "identity.account.create", map[string]any{
		"username": "studio-other", "fullName": "Other Studio account", "email": "", "status": "active",
		"organizationId": account.OrganizationID, "departmentId": account.DepartmentID,
		"roleIds": []int64{}, "overrides": []map[string]any{
			{"permission": "studio:read", "effect": "allow", "scope": "OWN"},
			{"permission": "studio:write", "effect": "allow", "scope": "OWN"},
		},
		"password": "TestOnly!2026Secure",
	}, true))
	other := loginIAM(t, handler, "studio-other")
	if blocked := studioCall(t, other, "studio.item.link_session", map[string]any{"id": taskID, "sessionId": "foreign"}); blocked.ErrorCode != "not_found" {
		t.Fatalf("foreign task link was not rejected: %+v", blocked)
	}
	makeToken := func(user iamClient) businessmcp.MCPTokenCreated {
		t.Helper()
		return decodeData[businessmcp.MCPTokenCreated](t, user.invoke(t, "mcp.token.create", map[string]any{
			"name": "Studio test", "password": "TestOnly!2026Secure", "days": 1, "departmentId": account.DepartmentID,
		}, true))
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	foreign, err := sdk.NewClient(&sdk.Implementation{Name: "studio-other", Version: "1"}, nil).Connect(ctx, &sdk.StreamableClientTransport{
		Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: workflowBearer{token: makeToken(other).Secret}},
		DisableStandaloneSSE: true, MaxRetries: -1,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer foreign.Close()
	read, err := foreign.CallTool(ctx, &sdk.CallToolParams{Name: "studio_session_context", Arguments: map[string]any{"sessionId": sessionID}})
	if err != nil || read.IsError {
		t.Fatalf("foreign lookup did not return a scoped capability result: %v %+v", err, read)
	}
	raw, err := json.Marshal(read.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var denied struct {
		ErrorCode string `json:"errorCode"`
		Data      any    `json:"data"`
	}
	if err := json.Unmarshal(raw, &denied); err != nil || denied.ErrorCode != "not_found" || denied.Data != nil {
		t.Fatalf("foreign lookup leaked data or had unexpected result: %+v %v", denied, err)
	}

	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	adapterRoot := t.TempDir()
	for _, subdir := range []string{"scripts", ".local/horizon-cli"} {
		if err := os.MkdirAll(filepath.Join(adapterRoot, subdir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(repo, "node_modules"), filepath.Join(adapterRoot, "node_modules")); err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile(filepath.Join(repo, "scripts", "studio-mcp.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(adapterRoot, "scripts", "studio-mcp.mjs"), script, 0600); err != nil {
		t.Fatal(err)
	}
	var firstCalls, secondCalls atomic.Int64
	startBackend := func(counter *atomic.Int64) *httptest.Server {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			counter.Add(1)
			handler.ServeHTTP(w, r)
		}))
		t.Cleanup(api.Close)
		return api
	}
	first := startBackend(&firstCalls)
	second := startBackend(&secondCalls)
	cmd := exec.CommandContext(ctx, "node", "--input-type=module", "-e", `
		import assert from 'node:assert/strict'
		import { writeFileSync } from 'node:fs'
		import { Client } from '@modelcontextprotocol/sdk/client/index.js'
		import { StdioClientTransport } from '@modelcontextprotocol/sdk/client/stdio.js'
		const setOrigin = origin => writeFileSync('.local/horizon-cli/backend.json', JSON.stringify({ origin, pid: process.pid }))
		setOrigin(process.env.STUDIO_TEST_FIRST_API)
		const client = new Client({ name: 'studio-integration', version: '1' })
		await client.connect(new StdioClientTransport({
		  command: process.execPath, args: ['scripts/studio-mcp.mjs'],
		  env: { HORIZON_STUDIO_MCP_TOKEN: process.env.HORIZON_STUDIO_MCP_TOKEN },
		}))
		try {
		  const { tools } = await client.listTools()
		  assert.deepEqual(tools.map(tool => tool.name), ['get_session_context', 'read_attachment'])
		  assert.equal(tools[1].annotations.readOnlyHint, true)
		  const result = await client.callTool({ name: 'get_session_context', arguments: { sessionId: 'dsh-test-session' } })
		  assert.equal(result.isError, undefined)
		  assert.equal(result.structuredContent.requirement.description, 'Read this through MCP.')
		  assert.equal(result.structuredContent.task.checks, 'Verify the exact session.')
		  assert.equal(result.structuredContent.project.developmentBrief, 'Follow project rules.')
		  const attachments = result.structuredContent.requirement.attachments
		  assert.equal(attachments.length, 1)
		  assert.equal(attachments[0].fileId, Number(process.env.STUDIO_TEST_ATTACHMENT_ID))
		  assert.equal(attachments[0].name, 'note.txt')
		  const attachment = await client.callTool({ name: 'read_attachment', arguments: { fileId: Number(process.env.STUDIO_TEST_ATTACHMENT_ID) } })
		  assert.equal(attachment.isError, undefined)
		  assert.equal(attachment.structuredContent.name, 'note.txt')
		  assert.equal(Buffer.from(attachment.structuredContent.content, 'base64').toString(), 'hello attachment')
		  const invalidAttachment = await client.callTool({ name: 'read_attachment', arguments: { fileId: 0 } })
		  assert.equal(invalidAttachment.isError, true)
		  setOrigin(process.env.STUDIO_TEST_SECOND_API)
		  const moved = await client.callTool({ name: 'get_session_context', arguments: { sessionId: 'dsh-test-session' } })
		  assert.deepEqual(moved.structuredContent, result.structuredContent)
		} finally {
		  await client.close()
		}
	`)
	cmd.Dir = adapterRoot
	cmd.Env = append(os.Environ(), "STUDIO_TEST_FIRST_API="+first.URL, "STUDIO_TEST_SECOND_API="+second.URL, "HORIZON_STUDIO_MCP_TOKEN="+makeToken(owner).Secret,
		"STUDIO_TEST_ATTACHMENT_ID="+fmt.Sprint(attachmentID))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Studio stdio MCP adapter did not read from the live Gateway: %v\n%s", err, out)
	}
	if firstCalls.Load() == 0 || secondCalls.Load() == 0 {
		t.Fatalf("adapter did not follow the backend port change: initial=%d changed=%d", firstCalls.Load(), secondCalls.Load())
	}
}
