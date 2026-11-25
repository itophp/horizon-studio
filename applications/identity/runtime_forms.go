package identityapp

import ar "horizon/foundation/kernel/applicationruntime"

func protocolField(name, labelKey, typ string) ar.FieldV2 {
	return ar.FieldV2{Name: name, LabelKey: labelKey, Type: typ, Component: typ, ComponentVersion: "2.0"}
}

func protocolReference(name, labelKey, provider string) ar.FieldV2 {
	field := protocolField(name, labelKey, "reference")
	field.Reference = provider
	field.SearchParam = "keyword"
	return field
}

func protocolEnum(name, labelKey, prefix string, values ...string) ar.FieldV2 {
	field := protocolField(name, labelKey, "enum")
	field.Enum = values
	field.ValueLabelPrefix = prefix
	if len(values) > 0 {
		field.Default = ar.Literal(`"` + values[0] + `"`)
	}
	return field
}

func accountRuntimeForm() ar.FormV2 {
	fields := []ar.FieldV2{
		protocolField("fullName", "admin:fields.fullName", "string"),
		protocolField("username", "admin:fields.username", "string"),
		protocolField("email", "admin:fields.email", "string"),
		protocolEnum("status", "admin:fields.status", "admin:status.", "active", "inactive"),
		protocolField("password", "admin:accounts.newPassword", "string"),
	}
	fields[0].Required, fields[1].Required, fields[3].Required = true, true, true
	fields[3].DisabledWhen = ar.Condition("eq", ar.Path("record.isAdmin"), ar.Literal(`true`))
	create := append([]ar.FieldV2(nil), fields...)
	create[3].Name = "initialStatus"
	create[4].Required = true
	form := ar.FormV2{ID: "account.form", LabelKey: "admin:accounts.title", Tags: []string{"identity"}}
	form.Create = ar.Command{ActionType: "capability", Capability: ar.Ref("identity.account.create"), Fields: create}
	form.Edit = ar.Command{ActionType: "capability", Capability: ar.Ref("identity.account.update"), Fields: fields}
	for _, field := range create {
		form.Create.Input = append(form.Create.Input, ar.Bind(field.Name, "form."+field.Name))
	}
	for _, field := range fields {
		form.Edit.Input = append(form.Edit.Input, ar.Bind(field.Name, "form."+field.Name))
	}
	form.Edit.Input = append(form.Edit.Input, ar.Bind("id", "record.id"))
	return ar.Canonical(form)
}

func organizationRuntimeForm() ar.FormV2 {
	fields := []ar.FieldV2{
		protocolField("name", "admin:fields.name", "string"),
		protocolField("code", "admin:fields.code", "string"),
		{LabelKey: "admin:fields.description", Name: "description", Type: "string", Component: "textarea", ComponentVersion: "2.0"},
	}
	form := ar.FormV2{ID: "organization.form", LabelKey: "admin:organizations.title", Tags: []string{"identity"}}
	form.Edit = ar.Command{ActionType: "capability", Capability: ar.Ref("identity.organization.update"), Fields: fields}
	for _, field := range fields {
		form.Edit.Input = append(form.Edit.Input, ar.Bind(field.Name, "form."+field.Name))
	}
	for _, field := range []string{"id", "kind", "parentId", "status"} {
		form.Edit.Input = append(form.Edit.Input, ar.Bind(field, "record."+field))
	}
	return ar.Canonical(form)
}

func departmentRuntimeForm() ar.FormV2 {
	fields := []ar.FieldV2{
		protocolField("name", "admin:fields.name", "string"),
		protocolField("code", "admin:fields.code", "string"),
		protocolReference("parentId", "admin:fields.parent", "protocol.department.options"),
		protocolField("manager", "admin:departments.manager", "string"),
		protocolEnum("status", "admin:fields.status", "admin:status.", "active", "inactive"),
		{LabelKey: "admin:fields.description", Name: "description", Type: "string", Component: "textarea", ComponentVersion: "2.0"},
	}
	form := ar.FormV2{ID: "department.form", LabelKey: "admin:departments.title", Tags: []string{"identity"}}
	form.Create = ar.Command{ActionType: "capability", Capability: ar.Ref("identity.department.create"), Fields: fields}
	form.Edit = ar.Command{ActionType: "capability", Capability: ar.Ref("identity.department.update"), Fields: append([]ar.FieldV2{protocolField("id", "admin:fields.name", "integer")}, fields...)}
	for _, field := range fields {
		form.Create.Input = append(form.Create.Input, ar.Bind(field.Name, "form."+field.Name))
		form.Edit.Input = append(form.Edit.Input, ar.Bind(field.Name, "form."+field.Name))
	}
	form.Edit.Input = append(form.Edit.Input, ar.Bind("id", "record.id"), ar.Bind("organizationId", "record.organizationId"))
	form.ReferenceFilters = map[string][]ar.ReferenceFilter{"parentId": {{OptionField: "status", Value: "active"}}}
	form.ReferencePaths = map[string]string{"protocol.department.options": "_departments.items"}
	return ar.Canonical(form)
}

func grantFields() []ar.FieldV2 {
	fields := []ar.FieldV2{
		protocolField("permission", "admin:permissions.permission", "string"),
		protocolEnum("effect", "admin:permissions.effect", "admin:permissions.", "allow"),
		protocolEnum("scope", "admin:permissions.dataScope", "admin:permissions.", "ALL", "OWN"),
	}
	for i := range fields {
		fields[i].Required = true
	}
	return fields
}

func roleRuntimeForm() ar.FormV2 {
	grants := protocolField("grants", "admin:permissions.grants", "array")
	grants.Component = "permission-matrix"
	grants.Children = grantFields()
	fields := []ar.FieldV2{
		protocolField("name", "admin:fields.name", "string"),
		protocolField("code", "admin:fields.code", "string"),
		{LabelKey: "admin:fields.description", Name: "description", Type: "string", Component: "textarea", ComponentVersion: "2.0"},
		protocolEnum("status", "admin:fields.status", "admin:status.", "active", "inactive"),
		grants,
	}
	form := ar.FormV2{ID: "role.form", LabelKey: "admin:roles.title", Tags: []string{"identity"}}
	form.Create = ar.Command{ActionType: "capability", Capability: ar.Ref("identity.role.create"), Fields: fields}
	form.Edit = ar.Command{ActionType: "capability", Capability: ar.Ref("identity.role.update"), Fields: append([]ar.FieldV2{protocolField("id", "admin:fields.name", "integer")}, fields...)}
	for _, field := range fields {
		form.Create.Input = append(form.Create.Input, ar.Bind(field.Name, "form."+field.Name))
		form.Edit.Input = append(form.Edit.Input, ar.Bind(field.Name, "form."+field.Name))
	}
	form.Edit.Input = append(form.Edit.Input, ar.Bind("id", "record.id"))
	form.ReferencePaths = map[string]string{
		"protocol.department.options": "departments",
		"catalog":                     "roles.catalog",
		"departments":                 "roles.departments",
	}
	return ar.Canonical(form)
}

func accountAccessRuntimeForm() ar.FormV2 {
	fields := []ar.FieldV2{
		protocolReference("roleId", "admin:fields.role", "protocol.role.options"),
		protocolField("minutes", "admin:machine.minutes", "integer"),
		protocolField("reason", "admin:policy.reason", "string"),
	}
	for i := range fields {
		fields[i].Required = true
	}
	form := ar.FormV2{
		ID: "account.access.form", LabelKey: "admin:actions.manageAccess", Tags: []string{"identity"},
		ReferencePaths: map[string]string{"protocol.role.options": "_accounts.roles"},
	}
	form.Edit = ar.Command{ActionType: "capability", Capability: ar.Ref("identity.roleGrant.create"), Fields: fields}
	for _, field := range fields {
		form.Edit.Input = append(form.Edit.Input, ar.Bind(field.Name, "form."+field.Name))
	}
	form.Edit.Input = append(form.Edit.Input, ar.Bind("accountId", "record.id"))
	return ar.Canonical(form)
}

func roleAccessRuntimeForm() ar.FormV2 {
	roleForm := roleRuntimeForm()
	fields := make([]ar.FieldV2, 0, len(roleForm.Edit.Fields))
	for _, field := range roleForm.Edit.Fields {
		if field.Name != "grants" {
			field.Hidden = true
			field.ReadOnly = true
		}
		fields = append(fields, field)
	}
	form := ar.FormV2{
		ID: "role.access.form", LabelKey: "admin:permissions.matrix", Tags: []string{"identity"},
		ReferencePaths: map[string]string{
			"catalog":     "roles.catalog",
			"departments": "roles.departments",
		},
	}
	form.Edit = ar.Command{
		ActionType: "capability", Capability: ar.Ref("identity.role.update"),
		Input: roleForm.Edit.Input, Fields: fields,
	}
	return ar.Canonical(form)
}
