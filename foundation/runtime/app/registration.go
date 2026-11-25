package app

import (
	"context"

	"horizon/foundation/ai/automation"
	"horizon/foundation/ai/harness"
	genidentity "horizon/foundation/gen/identity"
	ar "horizon/foundation/kernel/applicationruntime"
	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/capability/gateway"
	"horizon/foundation/kernel/capability/registry"
	"horizon/foundation/kernel/plugin"
	workflow "horizon/foundation/kernel/workflow/spec"
	"horizon/foundation/system/governance"
	"horizon/foundation/system/identity"
)

type Worker func(context.Context, *gateway.Gateway, *harness.Service)

// Registration holds application contributions for one deployment instance.
type Registration struct {
	Plugins       *plugin.Manager
	Entries       []registry.Entry
	Entities      []contracts.Entity
	Applications  []ar.ApplicationV2
	AttachGateway []func(*gateway.Gateway)
	Workers       []Worker
	Workflows     []workflow.Definition
	Graphs        []workflow.GraphDefinition
	// ResourceAttributes are registered before Gateway begins accepting calls.
	ResourceAttributes map[string]identity.ResourceAttributeProvider
	SignInApplications []SignInApplication
	DefaultSignIn      string
	Authentication     ApplicationAuthentication

	// Application-domain wiring declared by the deployment for platform
	// services. Values are deployment composition data; the kernel only
	// carries the references and never hardcodes business names.
	MemoryActivityPages    []string
	MemoryProjectWrite     string
	MemoryProjectAuthority string

	// MemoryEntityScopes declares the application resource types memory may
	// reference, with the guarding permission and existence capability.
	MemoryEntityScopes []MemoryEntityScope
}

// SignInApplication is a deployment-owned mapping from an application route
// to the login-account namespace used by the credential store.
type SignInApplication struct {
	ID        string
	Namespace string
	Aliases   []string
}

// ApplicationAuthentication handles public authentication requests on behalf of
// the registered applications. Credential checks are provided by IAM.
type ApplicationAuthentication interface {
	Login(context.Context, *genidentity.LoginPayload) (*genidentity.LoginResult, error)
	RequestRegistration(context.Context, *genidentity.RequestRegistrationPayload) error
	CompleteRegistration(context.Context, *genidentity.CompleteRegistrationPayload) error
	RequestPasswordReset(context.Context, *genidentity.RequestPasswordResetPayload) error
	CompletePasswordReset(context.Context, *genidentity.CompletePasswordResetPayload) error
}

// MemoryEntityScope is deployment composition data for one resource type.
type MemoryEntityScope struct {
	Resource      string
	Permission    string
	AggregateType string
	Authority     string
}

type Register func(*governance.Core) Registration

// PlatformWorkers registers background services owned by the foundation itself.
func PlatformWorkers(core *governance.Core) []Worker {
	return []Worker{func(ctx context.Context, dispatcher *gateway.Gateway, launcher *harness.Service) {
		worker := automation.NewWorker(core.Pool, launcher, core.Identity)
		worker.Core, worker.Gateway = core, dispatcher
		worker.Run(ctx)
	}}
}

func (r Registration) WorkflowDefinitions() []workflow.Definition {
	return append(append([]workflow.Definition(nil), r.Workflows...), workflow.RuntimeDesigner())
}
