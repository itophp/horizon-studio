//go:build integration

package tests

import (
	"context"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"horizon/applications/articles"
	"horizon/deployment"
	genidentity "horizon/foundation/gen/identity"
	"horizon/foundation/gen/system"
	"horizon/foundation/kernel/plugin"
	"horizon/foundation/runtime/app"
	"horizon/foundation/runtime/transport"
	"horizon/foundation/system/governance"
	"horizon/foundation/system/identity"
)

func deploymentHandler(service system.Service, core *governance.Core, identities ...*identity.Service) http.Handler {
	handler, _ := deploymentHandlerWithRuntime(service, core, identities...)
	return handler
}

func deploymentHandlerWithRuntime(service system.Service, core *governance.Core, identities ...*identity.Service) (http.Handler, func(context.Context)) {
	// The HTTP harness assembles the full deployment, including plugin
	// contributions, so managed applications reconcile like production.
	manager := deployment.BuiltinPlugins(core)
	for _, iam := range identities {
		_ = iam.ConfigurePluginPermissions(manager.Catalog.Permissions)
	}
	return transport.HandlerWithRegistration(service, core, deployment.WithPlugins(core, manager), identities...)
}

// pluginIAM returns an identity service whose catalog carries the declarative
// permissions contributed by the builtin application plugins, mirroring the
// production assembly where the transport layer merges plugin permissions into
// the identity instance before any principal is constructed.
func pluginIAM(t *testing.T, pool *pgxpool.Pool, extraPermissions ...plugin.Permission) *identity.Service {
	t.Helper()
	iam := identity.New(pool, false)
	core := governance.New(pool, iam, governance.Options{
		EncryptionKey: []byte("01234567890123456789012345678901"),
		StorageRoot:   t.TempDir(),
	})
	manager := deployment.BuiltinPlugins(core)
	permissions := append(manager.Catalog.Permissions, extraPermissions...)
	if err := iam.ConfigurePluginPermissions(permissions); err != nil {
		t.Fatal(err)
	}
	return iam
}

// articleFixture explicitly loads the retired plugin in an isolated tenant for
// archived capability coverage and upgrade regressions.
func articleFixture(t *testing.T) (http.Handler, *pgxpool.Pool, *genidentity.AccountRecord) {
	t.Helper()
	_, pool, root := iamFixture(t)
	if _, err := pool.Exec(t.Context(), `INSERT INTO runtime_installations(tenant_id,plugin_id,version,status)
		VALUES ('tenant-a','builtin:articles','2.0.0','enabled')`); err != nil {
		t.Fatal(err)
	}
	iam := identity.New(pool, false)
	core := governance.New(pool, iam, governance.Options{
		PublicWorkspace: "tenant-a",
		EncryptionKey:   []byte("01234567890123456789012345678901"),
		StorageRoot:     t.TempDir(),
	})
	builtins := deployment.BuiltinPlugins(core)
	manager := plugin.New(append(builtins.Plugins(), articles.Plugin{Module: articles.New(core)})...)
	if err := manager.Register(t.Context()); err != nil {
		t.Fatal(err)
	}
	handler, _ := transport.HandlerWithRegistration(app.System{}, core, deployment.WithPlugins(core, manager), iam)
	return handler, pool, root
}
