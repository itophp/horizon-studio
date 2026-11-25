package identityapp

import ar "horizon/foundation/kernel/applicationruntime"

func OrganizationsRuntimeApplication() ar.ApplicationV2 {
	organizations := ar.Provider{
		ID: "protocol.organizations", Capability: ar.Ref("identity.organizations.read"),
		KeyField: "id", LabelField: "name", Pagination: "none", Tags: []string{"identity"},
	}
	departments := ar.Provider{
		ID: "protocol.departments", Capability: ar.Ref("identity.departments.read"),
		KeyField: "id", LabelField: "name", Pagination: "none", Tags: []string{"identity"},
	}
	departmentOptions := ar.Provider{
		ID: "protocol.department.options", Capability: ar.Ref("identity.departments.read"),
		ResultPath: "items", KeyField: "id", LabelField: "name", Pagination: "none", Tags: []string{"identity"},
	}
	organizationForm := organizationRuntimeForm()
	departmentForm := departmentRuntimeForm()
	departmentColumns := []map[string]any{
		{"name": "name", "labelKey": "admin:fields.name", "display": "tree", "subtitleField": "code", "icon": "network"},
		{"name": "manager", "labelKey": "admin:departments.manager", "display": "fallback", "emptyKey": "admin:fields.unassigned"},
		{"name": "memberCount", "labelKey": "admin:fields.members", "display": "membershipCount", "reference": "accounts", "field": "departmentId"},
		{"name": "status", "labelKey": "admin:fields.status", "display": "status"},
	}
	departmentActions := []map[string]any{
		{"labelKey": "admin:actions.edit", "icon": "pencil", "mode": "edit", "permission": "departments:update"},
		{"labelKey": "admin:actions.delete", "icon": "trash", "mode": "delete", "permission": "departments:delete"},
	}
	organizationPanel := map[string]any{
		"type": "table", "rowsPath": "organizations.items", "keyField": "id", "selectable": false, "search": false,
		"columns": []map[string]any{
			{"name": "name", "labelKey": "admin:fields.name", "subtitleField": "code"},
			{"name": "description", "labelKey": "admin:fields.description"},
		},
		"rowActions": []map[string]any{
			{"labelKey": "admin:actions.edit", "icon": "pencil", "mode": "edit", "permission": "organizations:update"},
		},
		"forms":        map[string]any{"edit": organizationForm},
		"dialogTitles": map[string]any{"edit": map[string]any{"titleKey": "admin:organizations.edit", "descriptionKey": "admin:organizations.description"}},
	}
	departmentPanel := map[string]any{
		"type": "table", "rowsPath": "_departments.items", "keyField": "id", "selectable": false, "totalKey": "admin:departments.total", "panelHeading": map[string]any{"titleKey": "admin:departments.title", "icon": "network"},
		"searchFields": []string{"name", "code"}, "references": map[string]string{"accounts": "_departments.accounts"},
		"hierarchical": true, "parentField": "parentId",
		"columns": departmentColumns, "rowActions": departmentActions,
		"createKey": "admin:departments.create", "createPermission": "departments:create", "createIcon": "plus",
		"forms": map[string]any{"create": departmentForm, "edit": departmentForm, "view": departmentForm},
		"dialogTitles": map[string]any{
			"create": map[string]any{"titleKey": "admin:departments.create", "descriptionKey": "admin:departments.description"},
			"edit":   map[string]any{"titleKey": "admin:departments.edit", "descriptionKey": "admin:departments.description"},
			"view":   map[string]any{"titleKey": "admin:departments.title", "descriptionKey": "admin:departments.description"},
		},
		"delete": map[string]any{"capability": "identity.department.delete", "version": "1.0.0"}, "deleteLabelField": "name",
	}
	protocol := &ar.ApplicationV3{
		SchemaVersion: "3.0", ID: "organizations", Version: "2.3.0", Source: installation,
		RuntimeCompatibility: "3.0", LabelKey: "admin:organizations.title", Permission: "organizations:read", Route: "/organizations",
		Pages: []ar.PageV3{{
			SchemaVersion: "3.0", ID: "organizations.console", Route: "/organizations", TitleKey: "admin:organizations.title",
			SubtitleKey: "admin:organizations.subtitle", Permission: "organizations:read",
			Root: ar.Node{Type: "layout", Layout: &ar.LayoutSpec{
				ID: "organizations-root", Kind: "stack", Props: ar.LayoutProps{Gap: "20px", Width: "full"},
				Data: []ar.DataSpec{{ID: "organizations", Provider: organizations.ID}, {ID: "_departments", Provider: departments.ID}},
				Children: []ar.Node{
					{Type: "layout", Layout: &ar.LayoutSpec{ID: "heading", Kind: "grid", Props: ar.LayoutProps{Columns: "minmax(0, 1fr)"}, Children: []ar.Node{
						{Type: "component", Component: &ar.ComponentSpec{ID: "heading", Kind: "page-heading", Version: "1.0", Props: map[string]any{"titleKey": "admin:organizations.title", "subtitleKey": "admin:organizations.subtitle"}}},
					}}},
					{Type: "layout", Layout: &ar.LayoutSpec{ID: "tabs", Kind: "grid", Props: ar.LayoutProps{Columns: "minmax(0, 1fr)"}, Children: []ar.Node{
						{Type: "component", Component: &ar.ComponentSpec{ID: "views", Kind: "page-tabs", Version: "1.0", Props: map[string]any{
							"param": "tab", "panels": []map[string]any{
								protocolMergePanel(map[string]any{"id": "organizations", "labelKey": "admin:organizations.tabOrganizations", "icon": "building2", "permission": "organizations:read"}, organizationPanel),
								protocolMergePanel(map[string]any{"id": "departments", "labelKey": "admin:organizations.tabDepartments", "icon": "network", "permission": "departments:read"}, departmentPanel),
							},
						}}},
					}}},
				},
			}},
		}},
		Providers:  []ar.Provider{organizations, departments, departmentOptions},
		Navigation: []ar.Navigation{{PageID: "organizations.console", LabelKey: "admin:nav.organizations", Icon: "network"}},
	}
	return ar.ApplicationV2{
		Protocol: protocol, SchemaVersion: "3.0", ID: protocol.ID, Version: protocol.Version,
		PluginVersion: "2.3.0", MetadataVersion: "1", Source: protocol.Source,
		RuntimeCompatibility: "3.0", LabelKey: protocol.LabelKey, Permission: protocol.Permission, Route: protocol.Route,
	}
}

func protocolMergePanel(metadata, panel map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range metadata {
		out[key] = value
	}
	for key, value := range panel {
		out[key] = value
	}
	return out
}
