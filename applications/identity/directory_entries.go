package identityapp

import (
	"context"
	"fmt"

	genidentity "horizon/foundation/gen/identity"
	"horizon/foundation/kernel/capability/contracts"
	iam "horizon/foundation/system/identity"
)

func readDefinition(name, title, description, entity, permission string, output *contracts.Schema) contracts.Definition {
	return contracts.Definition{Layer: contracts.LayerAtomic,
		Name: name, Version: "1.0.0", Title: title, Description: description,
		Domain: "identity", Entity: entity, Entities: []string{entity},
		Permission: permission, Risk: contracts.RiskLow, Approval: contracts.ApprovalAuto,
		ReadOnly: true, TransientResult: true,
		Executor: "go:" + name, Tags: []string{"identity", "read"},
		Input:  contracts.Object(map[string]*contracts.Schema{}),
		Output: output,
	}
}

func organizationsReadDefinition() contracts.Definition {
	return readDefinition("identity.organizations.read", "Read organizations",
		"Read the authorized organization structure for administration and Runtime providers.",
		"organization", "organizations:read", contracts.Object(map[string]*contracts.Schema{
			"items": {Type: "array", Items: contracts.Object(map[string]*contracts.Schema{
				"id": {Type: "integer"}, "name": {Type: "string"}, "code": {Type: "string"},
				"kind": {Type: "string"}, "parentId": {Type: "integer"}, "status": {Type: "string"},
				"description": {Type: "string"}, "createdAt": {Type: "string"},
			}, "id", "name", "code", "kind", "parentId", "status")},
			"total": {Type: "integer"},
		}, "items", "total"))
}

func departmentsReadDefinition() contracts.Definition {
	return readDefinition("identity.departments.read", "Read departments",
		"Read authorized departments with their companies and account membership summaries.",
		"department", "departments:read", contracts.Object(map[string]*contracts.Schema{
			"items": {Type: "array", Items: contracts.Object(map[string]*contracts.Schema{
				"id": {Type: "integer"}, "name": {Type: "string"}, "code": {Type: "string"},
				"organizationId": {Type: "integer"}, "parentId": {Type: "integer"},
				"manager": {Type: "string"}, "status": {Type: "string"}, "description": {Type: "string"},
				"createdAt": {Type: "string"},
			}, "id", "name", "code", "organizationId", "parentId", "status")},
			"organizations": {Type: "array", Items: contracts.Object(map[string]*contracts.Schema{
				"id": {Type: "integer"}, "name": {Type: "string"}, "kind": {Type: "string"},
			}, "id", "name", "kind")},
			"accounts": {Type: "array", Items: contracts.Object(map[string]*contracts.Schema{
				"id": {Type: "integer"},
				"memberships": {Type: "array", Items: contracts.Object(map[string]*contracts.Schema{
					"departmentId": {Type: "integer"},
				}, "departmentId")},
			}, "id", "memberships")},
			"total": {Type: "integer"},
		}, "items", "organizations", "total"))
}

func accountsAdminReadDefinition() contracts.Definition {
	return readDefinition("identity.accounts.admin.read", "Read account directory",
		"Read authorized accounts with memberships, structure options, roles and the permission catalog for administration.",
		"account", "accounts:read", contracts.Object(map[string]*contracts.Schema{
			"items": {Type: "array", Items: contracts.Object(map[string]*contracts.Schema{
				"id": {Type: "integer"}, "username": {Type: "string"}, "fullName": {Type: "string"},
				"email": {Type: "string"}, "status": {Type: "string"}, "isAdmin": {Type: "boolean"}, "createdAt": {Type: "string"},
				"organizationId": {Type: "integer"}, "departmentId": {Type: "integer"},
				"roleIds":   {Type: "array", Items: contracts.Int(0, 1<<40)},
				"overrides": {Type: "array", Items: contracts.FreeObject()},
				"memberships": {Type: "array", Items: contracts.Object(map[string]*contracts.Schema{
					"organizationId": {Type: "integer"}, "departmentId": {Type: "integer"},
					"departmentName": {Type: "string"}, "roleIds": {Type: "array", Items: contracts.Int(0, 1<<40)},
					"overrides": {Type: "array", Items: contracts.FreeObject()},
				}, "organizationId", "departmentId", "roleIds", "overrides")},
			}, "id", "username", "fullName", "status", "memberships")},
			"organizations": {Type: "array", Items: contracts.Object(map[string]*contracts.Schema{
				"id": {Type: "integer"}, "name": {Type: "string"}, "code": {Type: "string"},
				"kind": {Type: "string"}, "status": {Type: "string"},
			}, "id", "name", "kind", "status")},
			"departments": {Type: "array", Items: contracts.Object(map[string]*contracts.Schema{
				"id": {Type: "integer"}, "name": {Type: "string"}, "code": {Type: "string"},
				"organizationId": {Type: "integer"}, "parentId": {Type: "integer"},
				"manager": {Type: "string"}, "status": {Type: "string"},
			}, "id", "name", "organizationId", "status")},
			"roles": {Type: "array", Items: contracts.Object(map[string]*contracts.Schema{
				"id": {Type: "integer"}, "name": {Type: "string"}, "code": {Type: "string"},
				"status": {Type: "string"}, "grants": {Type: "array", Items: contracts.FreeObject()},
			}, "id", "name", "grants")},
			"catalog": {Type: "array", Items: contracts.FreeObject()},
			"total":   {Type: "integer"},
		}, "items", "organizations", "departments", "roles", "total"))
}

var accountAdminFields = []string{"id", "username", "fullName", "email", "status", "isAdmin", "createdAt", "organizationId", "departmentId", "roleIds", "overrides", "memberships"}
var membershipFields = []string{"organizationId", "departmentId", "departmentName", "roleIds", "overrides"}

func projectAccounts(items []any) []any {
	out := make([]any, 0, len(items))
	for _, item := range projectList(items, accountAdminFields) {
		account, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if memberships, ok := account["memberships"].([]any); ok {
			account["memberships"] = projectList(memberships, membershipFields)
		}
		out = append(out, account)
	}
	return out
}

func (p Plugin) accountsAdminRead(ctx context.Context, _ contracts.Input) (contracts.Output, error) {
	snapshot, err := p.IAM.Snapshot(ctx, &genidentity.SnapshotPayload{})
	if err != nil {
		return contracts.Output{}, err
	}
	data, _ := iam.CapabilityData(snapshot).(map[string]any)
	accounts, _ := data["accounts"].([]any)
	organizations, _ := data["organizations"].([]any)
	departments, _ := data["departments"].([]any)
	roles, _ := data["roles"].([]any)
	projectedAccounts := projectAccounts(accounts)
	return contracts.Output{
		Data: map[string]any{
			"items":         projectedAccounts,
			"organizations": projectList(organizations, []string{"id", "name", "code", "kind", "status"}),
			"departments":   projectList(departments, []string{"id", "name", "code", "organizationId", "parentId", "manager", "status"}),
			"roles":         projectList(roles, []string{"id", "name", "code", "status", "grants"}),
			"catalog":       data["catalog"],
			"total":         len(projectedAccounts),
		},
		Summary: fmt.Sprintf("accounts=%d", len(projectedAccounts)),
	}, nil
}

var organizationReadFields = []string{"id", "name", "code", "kind", "parentId", "status", "description", "createdAt"}
var departmentReadFields = []string{"id", "name", "code", "organizationId", "parentId", "manager", "status", "description", "createdAt"}

func projectFields(item any, fields []string) any {
	record, ok := item.(map[string]any)
	if !ok {
		return nil
	}
	row := map[string]any{}
	for _, field := range fields {
		if value, exists := record[field]; exists {
			row[field] = value
		}
	}
	return row
}

func projectList(items []any, fields []string) []any {
	out := make([]any, 0, len(items))
	for _, item := range items {
		if row := projectFields(item, fields); row != nil {
			out = append(out, row)
		}
	}
	return out
}

func (p Plugin) organizationsRead(ctx context.Context, _ contracts.Input) (contracts.Output, error) {
	snapshot, err := p.IAM.Snapshot(ctx, &genidentity.SnapshotPayload{})
	if err != nil {
		return contracts.Output{}, err
	}
	data, _ := iam.CapabilityData(snapshot).(map[string]any)
	items, _ := data["organizations"].([]any)
	projectedItems := projectList(items, organizationReadFields)
	return contracts.Output{
		Data:    map[string]any{"items": projectedItems, "total": len(projectedItems)},
		Summary: fmt.Sprintf("organizations=%d", len(projectedItems)),
	}, nil
}

func (p Plugin) departmentsRead(ctx context.Context, _ contracts.Input) (contracts.Output, error) {
	snapshot, err := p.IAM.Snapshot(ctx, &genidentity.SnapshotPayload{})
	if err != nil {
		return contracts.Output{}, err
	}
	data, _ := iam.CapabilityData(snapshot).(map[string]any)
	departments, _ := data["departments"].([]any)
	organizations, _ := data["organizations"].([]any)
	accounts := []any{}
	if p, err := iam.Current(ctx); err == nil && p.Can("accounts:read") {
		if raw, ok := data["accounts"].([]any); ok {
			for _, item := range projectList(raw, []string{"id", "memberships"}) {
				account, ok := item.(map[string]any)
				if !ok {
					continue
				}
				if memberships, ok := account["memberships"].([]any); ok {
					projected := make([]any, 0, len(memberships))
					for _, membership := range memberships {
						if row := projectFields(membership, []string{"departmentId"}); row != nil {
							projected = append(projected, row)
						}
					}
					account["memberships"] = projected
				}
				accounts = append(accounts, account)
			}
		}
	}
	projectedDepartments := projectList(departments, departmentReadFields)
	projectedOrganizations := projectList(organizations, []string{"id", "name", "kind"})
	return contracts.Output{
		Data: map[string]any{
			"items":         projectedDepartments,
			"organizations": projectedOrganizations,
			"accounts":      accounts,
			"total":         len(projectedDepartments),
		},
		Summary: fmt.Sprintf("departments=%d", len(projectedDepartments)),
	}, nil
}
