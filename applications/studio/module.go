// Package studio is the HorizonStudio workbench backend: it manages external
// Horizon projects (link or create a local checkout, run its dev services,
// inspect its definition catalogue), collects requirements and work items
// pinned to fixed definition versions, and proxies Capability Gateway
// invocations against the managed project. The DSH web shell stays the UI; the
// Horizon base and the Capability Gateway are the only contract.
package studio

import (
	"context"
	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/capability/gateway"
	"horizon/foundation/kernel/capability/registry"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Permissions used by the module.
const (
	ReadPermission    = "studio:read"
	WritePermission   = "studio:write"
	ControlPermission = "studio:control"
	InvokePermission  = "studio:invoke"
)

// Module is the HorizonStudio workbench module.
type Module struct {
	pool       *pgxpool.Pool
	gateway    *gateway.Gateway
	generateAI func(context.Context, string, string) (string, error)
}

func (m *Module) AttachGateway(g *gateway.Gateway) { m.gateway = g }

// New builds the module.
func New(pool *pgxpool.Pool) *Module { return &Module{pool: pool} }

// Entries is the capability catalog contributed by the module.
func (m *Module) Entries() []registry.Entry {
	return []registry.Entry{
		{Definition: projectLinkDefinition(), Handler: contracts.Handler{ExecuteTx: m.projectLink}},
		{Definition: projectCreateDefinition(), Handler: contracts.Handler{Execute: m.projectCreate}},
		{Definition: projectListDefinition(), Handler: contracts.Handler{Execute: m.projectList}},
		{Definition: projectGetDefinition(), Handler: contracts.Handler{Execute: m.projectGet}},
		{Definition: projectAnalyzeDefinition(), Handler: contracts.Handler{ExecuteTx: m.projectAnalyze}},
		{Definition: projectControlDefinition(), Handler: contracts.Handler{Execute: m.projectControl}},
		{Definition: projectStatusDefinition(), Handler: contracts.Handler{Execute: m.projectStatus}},
		{Definition: projectEnvironmentDefinition(), Handler: contracts.Handler{Execute: m.projectEnvironment}},
		{Definition: projectLogDefinition(), Handler: contracts.Handler{Execute: m.projectLog}},
		{Definition: catalogInspectDefinition(), Handler: contracts.Handler{Execute: m.catalogInspect}},
		{Definition: requirementCreateDefinition(), Handler: contracts.Handler{ExecuteTx: m.requirementCreate}},
		{Definition: requirementUpdateDefinition(), Handler: contracts.Handler{ExecuteTx: m.requirementUpdate}},
		{Definition: requirementArchiveDefinition(), Handler: contracts.Handler{ExecuteTx: m.requirementArchive}},
		{Definition: requirementListDefinition(), Handler: contracts.Handler{Execute: m.requirementList}},
		{Definition: requirementSearchDefinition(), Handler: contracts.Handler{Execute: m.requirementSearch}},
		{Definition: requirementAttachmentListDefinition(), Handler: contracts.Handler{Execute: m.requirementAttachmentList}},
		{Definition: requirementAttachmentAddDefinition(), Handler: contracts.Handler{ExecuteTx: m.requirementAttachmentAdd}},
		{Definition: requirementAttachmentRemoveDefinition(), Handler: contracts.Handler{ExecuteTx: m.requirementAttachmentRemove}},
		{Definition: itemCreateDefinition(), Handler: contracts.Handler{ExecuteTx: m.itemCreate}},
		{Definition: itemBindDefinition(), Handler: contracts.Handler{ExecuteTx: m.itemBind}},
		{Definition: itemTransitionDefinition(), Handler: contracts.Handler{ExecuteTx: m.itemTransition}},
		{Definition: itemListDefinition(), Handler: contracts.Handler{Execute: m.itemList}},
		{Definition: itemPinDefinition(), Handler: contracts.Handler{ExecuteTx: m.itemPin}},
		{Definition: itemArchiveDefinition(), Handler: contracts.Handler{ExecuteTx: m.itemArchive}},
		{Definition: itemLinkSessionDefinition(), Handler: contracts.Handler{ExecuteTx: m.itemLinkSession}},
		{Definition: sessionContextDefinition(), Handler: contracts.Handler{Execute: m.sessionContext}},
		{Definition: gatewayLoginDefinition(), Handler: contracts.Handler{Execute: m.gatewayLogin}},
		{Definition: gatewayLogoutDefinition(), Handler: contracts.Handler{Execute: m.gatewayLogout}},
		{Definition: gatewayInvokeDefinition(), Handler: contracts.Handler{Execute: m.gatewayInvoke}},
		{Definition: runListDefinition(), Handler: contracts.Handler{Execute: m.runList}},
		{Definition: summaryDefinition(), Handler: contracts.Handler{Execute: m.summary}},
	}
}
