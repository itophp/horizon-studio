package studio

import ar "horizon/foundation/kernel/applicationruntime"

const ApplicationID = "studio"

func RuntimeApplication() ar.ApplicationV2 {
	const version = "1.0.0"
	const source = "com.horizon.studio"
	const route = "/studio"
	const label = "studio:title"
	page := ar.PageV3{
		SchemaVersion: "3.0", ID: "studio.projects", Route: route, TitleKey: label, Permission: ReadPermission,
		Root: ar.Node{Type: "layout", Layout: &ar.LayoutSpec{
			ID: "studio-projects-root", Kind: "stack",
			Children: []ar.Node{{Type: "component", Component: &ar.ComponentSpec{
				ID: "studio-projects-heading", Kind: "page-heading", Version: "1.0", Props: map[string]any{"titleKey": label},
			}}},
		}},
	}
	protocol := &ar.ApplicationV3{
		SchemaVersion: "3.0", RuntimeCompatibility: "3.0", ID: ApplicationID, Version: version,
		Source: source, Route: route, LabelKey: label, Permission: ReadPermission,
		Pages: []ar.PageV3{page}, Providers: []ar.Provider{},
		Navigation: []ar.Navigation{{PageID: page.ID, LabelKey: label, Icon: "boxes"}},
	}
	return ar.ApplicationV2{
		SchemaVersion: "3.0", RuntimeCompatibility: "3.0", MetadataVersion: "1", ID: ApplicationID,
		Version: version, PluginVersion: version, Source: source, Route: route,
		LabelKey: label, Permission: ReadPermission, Protocol: protocol,
	}
}
