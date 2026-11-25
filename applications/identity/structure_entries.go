package identityapp

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/capability/registry"
	iam "horizon/foundation/system/identity"
	"horizon/internal/repository/dbgen"
)

// Entries assembles the identity application's directory contracts.
func (p Plugin) Entries() []registry.Entry {
	entries := append(p.IAM.AdministrationEntries(),
		registry.Entry{Definition: organizationsReadDefinition(), Handler: contracts.Handler{Execute: p.organizationsRead}},
		registry.Entry{Definition: departmentsReadDefinition(), Handler: contracts.Handler{Execute: p.departmentsRead}},
		registry.Entry{Definition: accountsAdminReadDefinition(), Handler: contracts.Handler{Execute: p.accountsAdminRead}},
	)
	for _, operation := range []struct {
		definition contracts.Definition
		execute    func(*structureService, context.Context, contracts.Input) (contracts.Output, error)
	}{
		{organizationUpdateDefinition(), (*structureService).updateOrganization},
		{departmentCreateDefinition(), (*structureService).createDepartment},
		{departmentUpdateDefinition(), (*structureService).updateDepartment},
		{departmentDeleteDefinition(), (*structureService).deleteDepartment},
	} {
		entries = append(entries, registry.Entry{
			Definition: operation.definition,
			Handler: contracts.Handler{ExecuteTx: p.IAM.AuthorizedWrite(func(iamService *iam.Service, ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
				service := &structureService{iam: iamService, repo: &structureRepository{q: dbgen.New(tx)}}
				return operation.execute(service, ctx, input)
			})},
		})
	}
	return entries
}

func deleteSchema() *contracts.Schema {
	return contracts.Object(map[string]*contracts.Schema{"id": contracts.Int(1, 1<<40)}, "id")
}

func structureWrite(name, title, description, entity, permission string, events []string, input *contracts.Schema, loggable ...string) contracts.Definition {
	risk := contracts.RiskHigh
	approval := contracts.ApprovalConfirm
	return contracts.Definition{Layer: contracts.LayerAtomic,
		Name: name, Version: "1.0.0", Title: title, Description: description,
		Domain: "identity", Entity: entity, Permission: permission,
		Risk: risk, Approval: approval,
		Idempotency:   contracts.Idempotency{Required: true, Scope: "tenant+operation+key"},
		SuccessEvents: events, Executor: "go:" + name, Tags: []string{"identity", "write"},
		Input: input, Output: contracts.Object(map[string]*contracts.Schema{"id": {Type: "integer"}}),
		Loggable: loggable,
	}
}

func organizationSchema() *contracts.Schema {
	properties := map[string]*contracts.Schema{
		"name": contracts.Str(1, 100), "code": contracts.Str(2, 40),
		"kind": contracts.Enum("enterprise", "company"), "parentId": contracts.Int(0, 1<<40),
		"status": contracts.Enum("active", "inactive"), "description": contracts.Str(0, 500),
	}
	properties["id"] = contracts.Int(1, 1<<40)
	return contracts.Object(properties, "id", "name", "code", "kind", "parentId", "status", "description")
}

func departmentSchema(requireID bool) *contracts.Schema {
	properties := map[string]*contracts.Schema{
		"name": contracts.Str(1, 100), "code": contracts.Str(2, 40),
		"organizationId": contracts.Int(1, 1<<40), "parentId": contracts.Int(0, 1<<40),
		"manager": contracts.Str(0, 100), "status": contracts.Enum("active", "inactive"),
		"description": contracts.Str(0, 500),
	}
	required := []string{"name", "code", "parentId", "manager", "status", "description"}
	if requireID {
		properties["id"] = contracts.Int(1, 1<<40)
		required = append(required, "id", "organizationId")
	}
	return contracts.Object(properties, required...)
}

func organizationUpdateDefinition() contracts.Definition {
	return structureWrite("identity.organization.update", "Update organization", "Update the tenant organization's name, code and description.",
		"organization", "organizations:update", []string{"identity.organization.updated"}, organizationSchema(), "id", "name", "code", "status")
}

func departmentCreateDefinition() contracts.Definition {
	return structureWrite("identity.department.create", "Create department", "Create one department in the current tenant’s organization.",
		"department", "departments:create", []string{"identity.department.created"}, departmentSchema(false), "name", "code", "organizationId", "parentId")
}

func departmentUpdateDefinition() contracts.Definition {
	return structureWrite("identity.department.update", "Update department", "Update one department, its parent and manager.",
		"department", "departments:update", []string{"identity.department.updated"}, departmentSchema(true), "id", "name", "code", "parentId", "manager")
}

func departmentDeleteDefinition() contracts.Definition {
	return structureWrite("identity.department.delete", "Delete department", "Delete one department when it holds no accounts.",
		"department", "departments:delete", []string{"identity.department.deleted"}, deleteSchema(), "id")
}

func (s *structureService) updateOrganization(ctx context.Context, in contracts.Input) (contracts.Output, error) {
	payload := organizationInput{}
	if err := contracts.Bind(in, &payload); err != nil {
		return contracts.Output{}, err
	}
	out, err := s.UpdateOrganization(ctx, &payload)
	if err != nil {
		return contracts.Output{}, err
	}
	return contracts.Output{Data: out, Summary: fmt.Sprintf("organization=%d", out.ID), Events: []contracts.Event{{
		Type: "identity.organization.updated", AggregateType: "organization", AggregateID: fmt.Sprintf("%d", out.ID),
		Payload: map[string]any{"organizationId": out.ID, "name": out.Name, "status": out.Status},
	}}}, nil
}

func (s *structureService) createDepartment(ctx context.Context, in contracts.Input) (contracts.Output, error) {
	payload := departmentInput{}
	if err := contracts.Bind(in, &payload); err != nil {
		return contracts.Output{}, err
	}
	out, err := s.CreateDepartment(ctx, &payload)
	if err != nil {
		return contracts.Output{}, err
	}
	return contracts.Output{Data: out, Summary: fmt.Sprintf("department=%d", out.ID), Events: []contracts.Event{{
		Type: "identity.department.created", AggregateType: "department", AggregateID: fmt.Sprintf("%d", out.ID),
		Payload: map[string]any{"departmentId": out.ID, "name": out.Name, "organizationId": out.OrganizationID},
	}}}, nil
}

func (s *structureService) updateDepartment(ctx context.Context, in contracts.Input) (contracts.Output, error) {
	payload := departmentInput{}
	if err := contracts.Bind(in, &payload); err != nil {
		return contracts.Output{}, err
	}
	out, err := s.UpdateDepartment(ctx, &payload)
	if err != nil {
		return contracts.Output{}, err
	}
	return contracts.Output{Data: out, Summary: fmt.Sprintf("department=%d", out.ID), Events: []contracts.Event{{
		Type: "identity.department.updated", AggregateType: "department", AggregateID: fmt.Sprintf("%d", out.ID),
		Payload: map[string]any{"departmentId": out.ID, "name": out.Name, "status": out.Status},
	}}}, nil
}

func (s *structureService) deleteDepartment(ctx context.Context, in contracts.Input) (contracts.Output, error) {
	payload := structureDeleteInput{}
	if err := contracts.Bind(in, &payload); err != nil {
		return contracts.Output{}, err
	}
	if err := s.DeleteDepartment(ctx, &payload); err != nil {
		return contracts.Output{}, err
	}
	return contracts.Output{Data: map[string]any{"id": payload.ID}, Summary: fmt.Sprintf("department=%d", payload.ID), Events: []contracts.Event{{
		Type: "identity.department.deleted", AggregateType: "department", AggregateID: fmt.Sprintf("%d", payload.ID),
		Payload: map[string]any{"departmentId": payload.ID},
	}}}, nil
}
