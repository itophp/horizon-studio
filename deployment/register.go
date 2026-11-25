// Package deployment composes the deployment baseline from builtin plugins.
// The plugin manager owns capability, entity, application, permission and
// worker contributions; this package only constructs plugins and adapts the
// runtime registration.
package deployment

import (
	"context"
	"io/fs"

	"github.com/jackc/pgx/v5"

	"horizon/applications/example"
	"horizon/applications/identity"
	"horizon/applications/mail"
	"horizon/applications/projects"
	"horizon/applications/studio"
	ar "horizon/foundation/kernel/applicationruntime"
	"horizon/foundation/kernel/capability/gateway"
	"horizon/foundation/kernel/metadata"
	"horizon/foundation/kernel/plugin"
	workflow "horizon/foundation/kernel/workflow/spec"
	"horizon/foundation/runtime/app"
	"horizon/foundation/system/governance"
	"horizon/foundation/system/identity"
	"horizon/internal/repository/dbgen"
)

// LoadSchema includes the schema owned by the builtin Studio application.
func LoadSchema() ([]*metadata.Table, error) {
	migrations, err := fs.Sub(studio.Migrations, "migrations")
	if err != nil {
		return nil, err
	}
	return metadata.Load(migrations)
}

// Register composes the deployment from the builtin plugin catalog.
func Register(core *governance.Core) app.Registration {
	return WithPlugins(core, nil)
}

// WithPlugins builds the runtime registration around the plugin manager.
// Capability, entity, application and permission contributions all flow
// through the plugin catalog; the deployment only adds the platform module
// application and adapts plugin workers.
func WithPlugins(core *governance.Core, plugins *plugin.Manager) app.Registration {
	if core == nil {
		core = governance.New(nil, identity.New(nil, false), governance.Options{})
	}
	if plugins == nil {
		plugins = BuiltinPlugins(core)
	}
	signIns, defaultSignIn := applicationSignIns(plugins)
	return app.Registration{
		Plugins:            plugins,
		Applications:       []ar.ApplicationV2{app.CapabilityRuntimeApplication()},
		SignInApplications: signIns,
		DefaultSignIn:      defaultSignIn,
		Authentication:     identityapp.Authentication{IAM: core.Identity},
		// Platform workflow definitions, compiled graphs and plugin gateway
		// hooks stay deployment composition: plugins contribute permissions,
		// entries, entities and applications through the catalog, while these
		// cross-cutting assets attach through the registration.
		// The Registration accessor appends the platform runtime designer, so
		// the field itself carries only plugin-contributed workflows.
		Workflows:     PluginWorkflows(plugins),
		Graphs:        PluginGraphs(plugins),
		AttachGateway: PluginGatewayHooks(plugins),
		Workers:       PluginWorkers(plugins),

		// Application-domain wiring for platform services; see the
		// Registration field docs. The names are deployment composition data.
		MemoryActivityPages:    []string{"projects"},
		MemoryProjectWrite:     "projects:write",
		MemoryProjectAuthority: "project.get@1.0.0",
		MemoryEntityScopes: []app.MemoryEntityScope{{
			Resource: "example_record", Permission: "example:read", AggregateType: "example_record", Authority: "example.record.get@1.0.0",
		}},
	}
}

// BuiltinPlugins constructs and registers every builtin application plugin.
// Business limits resolve from deployment configuration into plugin options.
func BuiltinPlugins(core *governance.Core) *plugin.Manager {
	mailOptions, err := mail.OptionsFromEnv()
	if err != nil {
		panic(err)
	}
	exampleModule := example.New(core.Pool)
	core.Identity.ConfigureTenantProvisioning([]string{identityapp.AccountProfileEntity, "organization"}, ProvisionIdentityTenant)
	core.Identity.ConfigureApplicationDirectoryReader(identityapp.ReadDirectoryStructure)
	core.Identity.ConfigureAccountProfileWriter(identityapp.StoreAccountProfile)
	manager := plugin.New(
		identityapp.Plugin{IAM: core.Identity},
		projects.Plugin{Module: projects.New(core.Pool)},
		mail.Plugin{Module: mail.New(core, mailOptions)},
		example.Plugin{Module: exampleModule},
		studio.Plugin{Module: studio.New(core.Pool)},
	)
	if err := manager.Register(context.Background()); err != nil {
		panic(err)
	}
	if err := identity.New(nil, false).ConfigurePluginPermissions(manager.Catalog.Permissions); err != nil {
		panic(err)
	}
	governance.RegisterDefinitions(PluginSettings(manager)...)
	return manager
}

func applicationSignIns(manager *plugin.Manager) ([]app.SignInApplication, string) {
	var entries []app.SignInApplication
	var defaultID string
	for _, instance := range manager.Plugins() {
		entry := instance.Manifest().SignIn
		if entry == nil {
			continue
		}
		signIn := app.SignInApplication{ID: entry.ID, Namespace: entry.Namespace}
		if _, identityApplication := instance.(identityapp.Plugin); identityApplication {
			signIn.Aliases = []string{"administration"}
		}
		entries = append(entries, signIn)
		if entry.Default {
			if defaultID != "" {
				panic("multiple default application authentication entries")
			}
			defaultID = entry.ID
		}
	}
	return entries, defaultID
}

// PluginSettings converts plugin-declared configuration keys into the
// governance registry grammar. Keys, groups and defaults are manifest data.
func PluginSettings(manager *plugin.Manager) []governance.Definition {
	var definitions []governance.Definition
	for _, setting := range manager.Catalog.Settings {
		definitions = append(definitions, governance.Definition{
			Key: setting.Key, Group: setting.Group, Kind: setting.Kind, Default: setting.Default,
			Scopes: setting.Scopes, Choices: setting.Choices,
			Sensitive: setting.Sensitive, Secret: setting.Secret, Min: setting.Min, Max: setting.Max, UI: setting.UI,
		})
	}
	return definitions
}

// PluginWorkers adapts plugin-declared workers to the runtime worker type.
// Workers run under the platform worker supervision with the gateway attached.
func PluginWorkers(manager *plugin.Manager) []app.Worker {
	var workers []app.Worker
	for _, instance := range manager.Plugins() {
		if source, ok := instance.(interface{ Workers() []app.Worker }); ok {
			workers = append(workers, source.Workers()...)
		}
	}
	return workers
}

// PluginGatewayHooks collects plugin hooks that need the assembled gateway,
// mirroring the worker adaptation above.
func PluginGatewayHooks(manager *plugin.Manager) []func(*gateway.Gateway) {
	var hooks []func(*gateway.Gateway)
	for _, instance := range manager.Plugins() {
		if source, ok := instance.(interface{ AttachGateway(*gateway.Gateway) }); ok {
			hooks = append(hooks, source.AttachGateway)
		}
	}
	return hooks
}

// PluginWorkflows aggregates workflow definitions contributed by plugins.
// Business workflow assets travel with their application, never the kernel.
func PluginWorkflows(manager *plugin.Manager) []workflow.Definition {
	var definitions []workflow.Definition
	for _, instance := range manager.Plugins() {
		if source, ok := instance.(interface{ Workflows() []workflow.Definition }); ok {
			definitions = append(definitions, source.Workflows()...)
		}
	}
	return definitions
}

// PluginGraphs aggregates graph contracts contributed by plugins.
func PluginGraphs(manager *plugin.Manager) []workflow.GraphDefinition {
	var graphs []workflow.GraphDefinition
	for _, instance := range manager.Plugins() {
		if source, ok := instance.(interface {
			Graphs() []workflow.GraphDefinition
		}); ok {
			graphs = append(graphs, source.Graphs()...)
		}
	}
	return graphs
}

// WorkflowDefinitions is the shared deployment catalog for offline generators:
// plugin workflow assets plus the platform runtime designer.
func WorkflowDefinitions() []workflow.Definition {
	return append(append(projects.Workflows(), example.Workflows()...), app.WorkflowDefinitions()...)
}

// GraphDefinitions is the shared deployment graph catalog for offline tools.
func GraphDefinitions() []workflow.GraphDefinition { return example.Graphs() }

// ProvisionIdentityTenant initializes the identity application's records within
// IAM's tenant provisioning transaction. The schema provisions the organization
// when the tenant row is inserted.
func ProvisionIdentityTenant(ctx context.Context, tx pgx.Tx, tenant string, accountID int64) error {
	account, err := dbgen.New(tx).IAMGetAuthorityAccount(ctx, dbgen.IAMGetAuthorityAccountParams{TenantID: tenant, ID: accountID})
	if err != nil {
		return err
	}
	return identityapp.StoreAccountProfile(ctx, tx, tenant, accountID, account.Username, "")
}
