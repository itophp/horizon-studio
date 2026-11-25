package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	genidentity "horizon/foundation/gen/identity"
	"horizon/foundation/kernel/capability/contracts"
	"horizon/internal/repository/dbgen"
)

func TestAuthorizationRequiresFreshResourceAttributes(t *testing.T) {
	p, err := NewPrincipal(&genidentity.AccountRecord{ID: 5}, "tenant-test", []*genidentity.PermissionGrant{
		{Permission: "accounts:read", Effect: "allow", Scope: "ALL"},
	})
	if err != nil {
		t.Fatal(err)
	}
	base := AuthorizationInput{Principal: p, Action: "identity.resource.read", Permission: "accounts:read", Resource: "account", ResourceID: 8, Environment: contracts.OriginHumanUI}
	rules := []authorizationRule{
		{Action: base.Action, Resource: base.Resource, Effect: "require_attribute", Attribute: "environment.origin", Equals: "human-ui"},
		{Action: base.Action, Resource: base.Resource, Effect: "require_attribute", Attribute: "resource.classification", Equals: "public"},
	}
	for _, scenario := range []struct {
		name, value, reason string
		age                 time.Duration
		err                 error
		origin              contracts.Origin
	}{
		{name: "allowed", value: "public", reason: "allowed"},
		{name: "mismatch", value: "restricted", reason: "attribute_mismatch"},
		{name: "stale", value: "public", age: time.Minute, reason: "attribute_unavailable"},
		{name: "failure", value: "public", err: errors.New("not reachable"), reason: "attribute_unavailable"},
		{name: "different origin", value: "public", origin: contracts.OriginAgent, reason: "attribute_mismatch"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			service := &Service{}
			if err := service.RegisterResourceAttributeProvider("account", func(ctx context.Context, _ *dbgen.Queries, tenant string, resource int64) (ResourceAttributes, error) {
				if tenant != "tenant-test" || resource != 8 {
					t.Fatalf("untrusted provider context: %s/%d", tenant, resource)
				}
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("provider deadline is missing")
				}
				return ResourceAttributes{ObservedAt: time.Now().Add(-scenario.age), Values: map[string]string{"classification": scenario.value}}, scenario.err
			}); err != nil {
				t.Fatal(err)
			}
			input := base
			if scenario.origin != "" {
				input.Environment = scenario.origin
			}
			decision := decideWithAttributes(input, 7, rules, service.authorizationResourceResolver(t.Context(), nil, input))
			if decision.Reason != scenario.reason || decision.Allowed != (scenario.reason == "allowed") || decision.PolicyVersion != 7 {
				t.Fatalf("unexpected authorization decision: %+v", decision)
			}
		})
	}
	denied := append(append([]authorizationRule{}, rules...), authorizationRule{Action: base.Action, Resource: base.Resource, Effect: "deny"})
	service := &Service{}
	if got := decideWithAttributes(base, 7, denied, service.authorizationResourceResolver(t.Context(), nil, base)); got.Allowed || got.Reason != "policy_deny" {
		t.Fatalf("explicit deny failed to take precedence: %+v", got)
	}
}
