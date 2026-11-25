//go:build integration

package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"horizon/applications/articles"
	gen "horizon/foundation/gen/identity"
	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/capability/gateway"
	"horizon/foundation/kernel/capability/registry"
	"horizon/foundation/runtime/app"
	"horizon/foundation/system/governance"
	"horizon/foundation/system/identity"
	"testing"
)

func TestRetiredArticleApplicationRejectsOldEntrypoints(t *testing.T) {
	handler, pool, root := articleFixture(t)
	before := loginIAM(t, handler, root.Username)
	checkIAMInvocation(t, before.invoke(t, "application.runtime.resolve", map[string]any{"applicationId": "articles"}, true))
	article := checkIAMInvocation(t, before.invoke(t, "article.create", map[string]any{
		"title": "Historical article", "body": "Preserved article body",
	}, true))

	iam := identity.New(pool, false)
	core := governance.New(pool, iam, governance.Options{PublicWorkspace: "tenant-a", StorageRoot: t.TempDir()})
	current := before
	current.handler = deploymentHandler(app.System{}, core, iam)
	response := current.invoke(t, "application.runtime.resolve", map[string]any{"applicationId": "articles"}, true)
	checkStatus(t, response, 200)
	if result := decodeBody[contracts.Result](t, response); result.ErrorCode != "not_found" {
		t.Fatalf("old article Runtime definition still resolves: %+v", result)
	}
	checkStatus(t, current.invoke(t, "article.list", map[string]any{}, true), 404)
	catalog := checkIAMInvocation(t, current.invoke(t, "application.runtime.list", map[string]any{}, true))
	for _, item := range catalog["items"].([]any) {
		if item.(map[string]any)["id"] == "articles" {
			t.Fatal("retired app still appears in the navigation catalog")
		}
	}
	var title string
	if err := pool.QueryRow(t.Context(), "SELECT title FROM articles WHERE tenant_id=$1 AND id=$2", "tenant-a", int64(article["id"].(float64))).Scan(&title); err != nil || title != "Historical article" {
		t.Fatalf("article data was not preserved: %q (%v)", title, err)
	}
}

func TestApplicationRuntimeArticleQueries(t *testing.T) {
	handler, pool, root := articleFixture(t)
	client := loginTenantIAM(t, handler, "root-admin", "tenant-a")
	response := invokeCapability(t, client, "application.runtime.resolve", map[string]any{"input": map[string]any{"applicationId": "articles"}})
	if response.Status != "succeeded" {
		t.Fatal(response)
	}
	raw, _ := json.Marshal(response.Data)
	if string(raw) == "null" {
		t.Fatal("missing metadata")
	}
	ctx := context.Background()
	// More than the former 100-row cutoff, plus records outside both scope boundaries.
	_, err := pool.Exec(ctx, `INSERT INTO articles(tenant_id,created_by,title,body) SELECT 'tenant-a',$1,'Page '||lpad(i::text,3,'0'),'private' FROM generate_series(1,105)i`, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO articles(tenant_id,created_by,title,body) VALUES('tenant-b',$1,'Page foreign','secret'),('tenant-a',$2,'Page other','secret')`, root.ID, root.ID+100000)
	if err != nil {
		t.Fatal(err)
	}
	p, err := pluginIAM(t, pool, (articles.Plugin{}).Manifest().Permissions...).NewPrincipal(&gen.AccountRecord{ID: root.ID}, "tenant-a", []*gen.PermissionGrant{{Permission: "articles:read", Effect: "allow"}})
	if err != nil {
		t.Fatal(err)
	}
	ctx = identity.WithPrincipal(ctx, p)
	g := gateway.New(gateway.Options{Registry: registry.New((&articles.Module{Pool: pool}).Entries()), Pool: pool})
	result, err := g.Invoke(ctx, gateway.Request{Name: "article.list", Input: contracts.Input{"page": 6, "pageSize": 20, "sort": "title:asc", "keyword": "Page"}, Origin: contracts.OriginHumanUI})
	if err != nil || result.Status != contracts.StatusSucceeded {
		t.Fatal(result, err)
	}
	data := result.Data.(map[string]any)
	if fmt.Sprint(data["total"]) != "105" {
		t.Fatal(data)
	}
	items := data["items"].([]map[string]any)
	if len(items) != 5 || items[0]["title"] != "Page 101" {
		t.Fatal(items)
	}
	for _, item := range items {
		if _, ok := item["body"]; ok {
			t.Fatal("body exposed")
		}
	}
	for _, input := range []contracts.Input{{"pageSize": 101}, {"sort": "body:asc"}, {"select": []any{"body"}}, {"tenantId": "tenant-b"}} {
		result, err = g.Invoke(ctx, gateway.Request{Name: "article.list", Input: input, Origin: contracts.OriginHumanUI})
		if err == nil && result.Status == contracts.StatusSucceeded {
			t.Fatal("unsafe query accepted", input)
		}
	}
}
