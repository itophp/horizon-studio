package identity

import (
	"sync"

	ar "horizon/foundation/kernel/applicationruntime"
)

var securityUIOnce sync.Once

// Account security requires short-lived secret disclosure and multi-step
// reauthentication, and scoped audit browsing. The trusted renderer keeps this state out of Providers,
// persisted page state and model/page-tool snapshots.
func SecurityRuntimeApplication() ar.ApplicationV2 {
	securityUIOnce.Do(func() {
		ar.RegisterUIContracts(ar.UIContract{
			Name: "identity-security-panel", Version: "1.0", Category: "element",
			Description: "Personal account security, scoped audit browsing and export, with in-memory credential disclosure and Gateway reauthentication.",
			PropsSchema: `{"type":"object","properties":{},"additionalProperties":false}`,
			Events:      []string{}, Slots: []string{}, ReadOnly: "Preview shows no credentials or personal data and performs no calls.",
			Example: `{"type":"component","id":"security","component":{"kind":"identity-security-panel","version":"1.0","props":{}}}`,
		})
	})
	protocol := &ar.ApplicationV3{
		SchemaVersion: "3.0", ID: "account-security", Version: "1.0.0", Source: "builtin:account-security",
		RuntimeCompatibility: "3.0", LabelKey: "admin:security.title", Permission: ar.AuthenticatedPermission, Route: "/account-security",
		Pages: []ar.PageV3{{
			SchemaVersion: "3.0", ID: "account-security.home", Route: "/account-security", TitleKey: "admin:security.title",
			SubtitleKey: "admin:security.subtitle", Permission: ar.AuthenticatedPermission,
			Root: ar.Node{Type: "layout", Layout: &ar.LayoutSpec{ID: "security-root", Kind: "stack", Props: ar.LayoutProps{Gap: "20px", Width: "full"}, Children: []ar.Node{
				{Type: "layout", Layout: &ar.LayoutSpec{ID: "heading-layout", Kind: "grid", Children: []ar.Node{
					{Type: "component", Component: &ar.ComponentSpec{ID: "heading", Kind: "page-heading", Version: "1.0", Props: map[string]any{"titleKey": "admin:security.title", "subtitleKey": "admin:security.subtitle"}}},
				}}},
				{Type: "layout", Layout: &ar.LayoutSpec{ID: "security-layout", Kind: "grid", Children: []ar.Node{
					{Type: "component", Component: &ar.ComponentSpec{ID: "security", Kind: "identity-security-panel", Version: "1.0", Props: map[string]any{}}},
				}}},
			}}},
		}},
	}
	return ar.ApplicationV2{Protocol: protocol, SchemaVersion: "3.0", ID: protocol.ID, Version: protocol.Version,
		PluginVersion: "1.0.0", MetadataVersion: "1", Source: protocol.Source, RuntimeCompatibility: "3.0",
		LabelKey: protocol.LabelKey, Permission: protocol.Permission, Route: protocol.Route}
}
