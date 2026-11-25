package identity

import (
	"sync"

	ar "horizon/foundation/kernel/applicationruntime"
)

var governanceUIOnce sync.Once

// Policy review combines immutable versions, structured rules and a trusted
// current-subject simulation. Its renderer invokes the existing policy Entries;
// definitions and browser state never confer publishing authority.
func GovernanceRuntimeApplication() ar.ApplicationV2 {
	governanceUIOnce.Do(func() {
		ar.RegisterUIContracts(ar.UIContract{
			Name: "identity-governance-panel", Version: "1.0", Category: "element",
			Description: "Authorization policy history, structured draft, diff, current-subject simulation and separate-administrator publication through Gateway.",
			PropsSchema: `{"type":"object","properties":{},"additionalProperties":false}`,
			Events:      []string{}, Slots: []string{}, ReadOnly: "Preview performs no policy queries or mutations.",
			Example: `{"type":"component","id":"governance","component":{"kind":"identity-governance-panel","version":"1.0","props":{}}}`,
		})
	})
	protocol := &ar.ApplicationV3{
		SchemaVersion: "3.0", ID: "iam-governance", Version: "1.0.0", Source: "builtin:iam-governance",
		RuntimeCompatibility: "3.0", LabelKey: "admin:policy.title", Permission: "access:manage", Route: "/iam-governance",
		Pages: []ar.PageV3{{
			SchemaVersion: "3.0", ID: "iam-governance.policies", Route: "/iam-governance", TitleKey: "admin:policy.title",
			SubtitleKey: "admin:policy.subtitle", Permission: "access:manage",
			Root: ar.Node{Type: "layout", Layout: &ar.LayoutSpec{ID: "governance-root", Kind: "stack", Props: ar.LayoutProps{Gap: "20px", Width: "full"}, Children: []ar.Node{
				{Type: "layout", Layout: &ar.LayoutSpec{ID: "heading-layout", Kind: "grid", Children: []ar.Node{
					{Type: "component", Component: &ar.ComponentSpec{ID: "heading", Kind: "page-heading", Version: "1.0", Props: map[string]any{"titleKey": "admin:policy.title", "subtitleKey": "admin:policy.subtitle"}}},
				}}},
				{Type: "layout", Layout: &ar.LayoutSpec{ID: "governance-layout", Kind: "grid", Children: []ar.Node{
					{Type: "component", Component: &ar.ComponentSpec{ID: "governance", Kind: "identity-governance-panel", Version: "1.0", Props: map[string]any{}}},
				}}},
			}}},
		}},
	}
	return ar.ApplicationV2{Protocol: protocol, SchemaVersion: "3.0", ID: protocol.ID, Version: protocol.Version,
		PluginVersion: "1.0.0", MetadataVersion: "1", Source: protocol.Source, RuntimeCompatibility: "3.0",
		LabelKey: protocol.LabelKey, Permission: protocol.Permission, Route: protocol.Route}
}
