package studio

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/system/identity"
	"horizon/internal/repository/dbgen/studiodb"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func requirementCreateDefinition() contracts.Definition {
	return contracts.Definition{Layer: contracts.LayerAtomic,
		Name: "studio.requirement.create", Version: "1.0.0", Title: "Collect a requirement",
		SuccessEvents: []string{"studio.requirement.created"},
		Description:   "Record one requirement for a managed project, optionally as a sub-requirement of one top-level requirement. Requirements stay readable after archiving.",
		Domain:        "studio", Entity: "studio_requirement", Permission: WritePermission,
		Risk: contracts.RiskLow, Approval: contracts.ApprovalAuto,
		Idempotency: contracts.Idempotency{Required: true, Scope: "tenant+operation+key"},
		Executor:    "go:studio.requirement.create", Tags: []string{"studio", "write"},
		Input: contracts.Object(map[string]*contracts.Schema{
			"project": contracts.Int(1, 1<<53), "title": text(1, 200),
			"description": {Type: "string", MaxLength: intPtr(12000)},
			"source":      {Type: "string", MaxLength: intPtr(200)},
			"parent":      contracts.Int(1, 1<<53),
		}, "project", "title"),
		Output: contracts.Object(map[string]*contracts.Schema{
			"id": contracts.Int(1, 1<<53), "parentId": contracts.Int(0, 1<<53),
			"title": {Type: "string"}, "archived": {Type: "boolean"},
		}, "id", "parentId", "title", "archived"),
		Loggable: []string{"project", "parent", "title", "source"},
	}
}

func (m *Module) requirementCreate(ctx context.Context, tx pgx.Tx, in contracts.Input) (contracts.Output, error) {
	p, err := identity.Current(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	if !p.Can(WritePermission) {
		return contracts.Output{}, contracts.Problem("forbidden")
	}
	projectID, ok := projectID(in)
	if !ok {
		return contracts.Output{}, contracts.Problem("invalid")
	}
	title := strings.TrimSpace(stringField(in, "title"))
	if title == "" || utf8.RuneCountInString(title) > 200 {
		return contracts.Output{}, contracts.Problem("invalid")
	}
	var parentID pgtype.Int8
	if raw, supplied := number(in["parent"]); supplied && raw >= 1 {
		parent, err := studiodb.New(tx).StudioRequirementGet(ctx, studiodb.StudioRequirementGetParams{
			TenantID: p.TenantID, ID: int64(raw), CreatedBy: p.Account.ID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return contracts.Output{}, contracts.Problem("invalid")
		}
		if err != nil {
			return contracts.Output{}, err
		}
		if parent.ProjectID != projectID || parent.Archived || parent.ParentID.Valid {
			return contracts.Output{}, contracts.Problem("invalid")
		}
		parentID = pgtype.Int8{Int64: int64(raw), Valid: true}
	}
	row, err := studiodb.New(tx).StudioRequirementCreate(ctx, studiodb.StudioRequirementCreateParams{
		TenantID: p.TenantID, ID: projectID, Title: title, ParentID: parentID,
		Description: stringField(in, "description"), Source: stringField(in, "source"), CreatedBy: p.Account.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, contracts.Problem("not_found")
	}
	if err != nil {
		return contracts.Output{}, err
	}
	return contracts.Output{
		Data:    map[string]any{"id": row.ID, "parentId": requirementParentID(row.ParentID), "title": row.Title, "archived": row.Archived},
		Summary: "requirement=" + strconv.FormatInt(row.ID, 10),
		Events: []contracts.Event{{
			Type: "studio.requirement.created", AggregateType: "studio_requirement",
			AggregateID: strconv.FormatInt(row.ID, 10),
		}},
	}, nil
}

func requirementArchiveDefinition() contracts.Definition {
	return contracts.Definition{Layer: contracts.LayerAtomic,
		Name: "studio.requirement.archive", Version: "1.0.0", Title: "Archive a requirement",
		SuccessEvents: []string{"studio.requirement.archived"},
		Description:   "Archive one collected requirement and every active sub-requirement of a managed project in one transaction. Archiving keeps them readable.",
		Domain:        "studio", Entity: "studio_requirement", Permission: WritePermission,
		Risk: contracts.RiskLow, Approval: contracts.ApprovalAuto,
		Idempotency: contracts.Idempotency{Required: true, Scope: "tenant+operation+key"},
		Executor:    "go:studio.requirement.archive", Tags: []string{"studio", "write"},
		Input: contracts.Object(map[string]*contracts.Schema{
			"project": contracts.Int(1, 1<<53), "id": contracts.Int(1, 1<<53),
		}, "project", "id"),
		Output:   contracts.Object(map[string]*contracts.Schema{"id": contracts.Int(1, 1<<53)}, "id"),
		Loggable: []string{"project", "id"},
	}
}

func (m *Module) requirementArchive(ctx context.Context, tx pgx.Tx, in contracts.Input) (contracts.Output, error) {
	p, err := identity.Current(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	if !p.Can(WritePermission) {
		return contracts.Output{}, contracts.Problem("forbidden")
	}
	id, okID := number(in["id"])
	if !okID || id < 1 {
		return contracts.Output{}, contracts.Problem("invalid")
	}
	archived, err := studiodb.New(tx).StudioRequirementArchiveTree(ctx, studiodb.StudioRequirementArchiveTreeParams{
		TenantID: p.TenantID, ID: int64(id), CreatedBy: p.Account.ID,
	})
	if err != nil {
		return contracts.Output{}, err
	}
	if len(archived) == 0 {
		return contracts.Output{}, contracts.Problem("conflict")
	}
	events := make([]contracts.Event, 0, len(archived))
	for _, archivedID := range archived {
		events = append(events, contracts.Event{
			Type: "studio.requirement.archived", AggregateType: "studio_requirement",
			AggregateID: strconv.FormatInt(archivedID, 10), Payload: map[string]any{},
		})
	}
	return contracts.Output{
		Data:    map[string]any{"id": int64(id)},
		Summary: "requirement.archived=" + itoa(len(archived)),
		Events:  events,
	}, nil
}

func requirementUpdateDefinition() contracts.Definition {
	return contracts.Definition{Layer: contracts.LayerAtomic,
		Name: "studio.requirement.update", Version: "1.0.0", Title: "Edit a requirement",
		SuccessEvents: []string{"studio.requirement.updated"},
		Description:   "Edit the title, description and source of one collected requirement. Archived requirements stay read-only.",
		Domain:        "studio", Entity: "studio_requirement", Permission: WritePermission,
		Risk: contracts.RiskLow, Approval: contracts.ApprovalAuto,
		Idempotency: contracts.Idempotency{Required: true, Scope: "tenant+operation+key"},
		Executor:    "go:studio.requirement.update", Tags: []string{"studio", "write"},
		Input: contracts.Object(map[string]*contracts.Schema{
			"project": contracts.Int(1, 1<<53), "id": contracts.Int(1, 1<<53),
			"title":       text(1, 200),
			"description": {Type: "string", MaxLength: intPtr(12000)},
			"source":      {Type: "string", MaxLength: intPtr(200)},
			"parent":      contracts.Int(0, 1<<53),
		}, "project", "id", "title"),
		Output: contracts.Object(map[string]*contracts.Schema{
			"id": contracts.Int(1, 1<<53), "parentId": contracts.Int(0, 1<<53),
			"title": {Type: "string"}, "archived": {Type: "boolean"},
		}, "id", "parentId", "title", "archived"),
		Loggable: []string{"project", "id", "parent", "title", "source"},
	}
}

func (m *Module) requirementUpdate(ctx context.Context, tx pgx.Tx, in contracts.Input) (contracts.Output, error) {
	p, err := identity.Current(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	if !p.Can(WritePermission) {
		return contracts.Output{}, contracts.Problem("forbidden")
	}
	projectID, ok := projectID(in)
	if !ok {
		return contracts.Output{}, contracts.Problem("invalid")
	}
	id, okID := number(in["id"])
	if !okID || id < 1 {
		return contracts.Output{}, contracts.Problem("invalid")
	}
	title := strings.TrimSpace(stringField(in, "title"))
	if title == "" || utf8.RuneCountInString(title) > 200 {
		return contracts.Output{}, contracts.Problem("invalid")
	}
	q := studiodb.New(tx)
	current, err := q.StudioRequirementGet(ctx, studiodb.StudioRequirementGetParams{
		TenantID: p.TenantID, ID: int64(id), CreatedBy: p.Account.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, contracts.Problem("not_found")
	}
	if err != nil {
		return contracts.Output{}, err
	}
	if current.ProjectID != projectID {
		return contracts.Output{}, contracts.Problem("not_found")
	}
	if current.Archived {
		return contracts.Output{}, contracts.Problem("conflict")
	}
	var parentID pgtype.Int8
	if raw, supplied := in["parent"]; supplied {
		value, ok := number(raw)
		if !ok || value < 0 {
			return contracts.Output{}, contracts.Problem("invalid")
		}
		parentID = pgtype.Int8{Int64: int64(value), Valid: true}
		if parentID.Int64 > 0 {
			if parentID.Int64 == current.ID {
				return contracts.Output{}, contracts.Problem("invalid")
			}
			target, err := q.StudioRequirementGet(ctx, studiodb.StudioRequirementGetParams{
				TenantID: p.TenantID, ID: parentID.Int64, CreatedBy: p.Account.ID,
			})
			if errors.Is(err, pgx.ErrNoRows) {
				return contracts.Output{}, contracts.Problem("invalid")
			}
			if err != nil {
				return contracts.Output{}, err
			}
			if target.ProjectID != current.ProjectID || target.Archived || target.ParentID.Valid {
				return contracts.Output{}, contracts.Problem("invalid")
			}
			children, err := q.StudioRequirementChildCount(ctx, studiodb.StudioRequirementChildCountParams{
				TenantID: p.TenantID, ParentID: current.ID,
			})
			if err != nil {
				return contracts.Output{}, err
			}
			if children > 0 {
				return contracts.Output{}, contracts.Problem("conflict")
			}
		}
	}
	row, err := q.StudioRequirementUpdate(ctx, studiodb.StudioRequirementUpdateParams{
		TenantID: p.TenantID, ID: int64(id), ProjectID: projectID, CreatedBy: p.Account.ID,
		Title: title, Description: optionalText(in, "description"), Source: optionalText(in, "source"),
		ParentID: parentID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, contracts.Problem("conflict")
	}
	if err != nil {
		return contracts.Output{}, err
	}
	return contracts.Output{
		Data:    map[string]any{"id": row.ID, "parentId": requirementParentID(row.ParentID), "title": row.Title, "archived": row.Archived},
		Summary: "requirement=" + strconv.FormatInt(row.ID, 10),
		Events: []contracts.Event{{
			Type: "studio.requirement.updated", AggregateType: "studio_requirement",
			AggregateID: strconv.FormatInt(row.ID, 10), Payload: map[string]any{},
		}},
	}, nil
}

// optionalText tells an omitted field from an explicitly empty one.
func optionalText(in contracts.Input, key string) pgtype.Text {
	value, supplied := in[key].(string)
	return pgtype.Text{String: value, Valid: supplied}
}

func requirementListDefinition() contracts.Definition {
	return contracts.Definition{Layer: contracts.LayerAtomic,
		Name: "studio.requirement.list", Version: "1.0.0", Title: "List collected requirements",
		Description: "List the requirements collected for one managed project, newest first.",
		Domain:      "studio", Entity: "studio_requirement", Permission: ReadPermission,
		Risk: contracts.RiskLow, Approval: contracts.ApprovalAuto, ReadOnly: true,
		Executor: "go:studio.requirement.list", Tags: []string{"studio", "read"},
		Input: contracts.Object(map[string]*contracts.Schema{"project": contracts.Int(1, 1<<53)}, "project"),
		Output: contracts.Object(map[string]*contracts.Schema{
			"requirements": {Type: "array", Items: contracts.Object(map[string]*contracts.Schema{
				"id": contracts.Int(1, 1<<53), "projectId": contracts.Int(1, 1<<53),
				"parentId": contracts.Int(0, 1<<53),
				"title":    {Type: "string"}, "description": {Type: "string"}, "source": {Type: "string"},
				"archived": {Type: "boolean"},
			})},
		}, "requirements"),
	}
}

func (m *Module) requirementList(ctx context.Context, in contracts.Input) (contracts.Output, error) {
	p, project, err := m.ownedProject(ctx, in)
	if err != nil {
		return contracts.Output{}, err
	}
	if !p.Can(ReadPermission) {
		return contracts.Output{}, contracts.Problem("forbidden")
	}
	rows, err := studiodb.New(m.pool).StudioRequirementList(ctx, studiodb.StudioRequirementListParams{
		TenantID: p.TenantID, ProjectID: project.ID, CreatedBy: p.Account.ID,
	})
	if err != nil {
		return contracts.Output{}, err
	}
	requirements := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		requirements = append(requirements, map[string]any{
			"id": row.ID, "projectId": row.ProjectID, "parentId": requirementParentID(row.ParentID),
			"title": row.Title, "description": row.Description, "source": row.Source, "archived": row.Archived,
		})
	}
	return contracts.Output{Data: map[string]any{"requirements": requirements}, Summary: "requirements=" + itoa(len(rows))}, nil
}

// requirementParentID normalizes an optional parent reference for the wire
// contract: top-level requirements report 0 instead of a missing value.
func requirementParentID(value pgtype.Int8) int64 {
	if !value.Valid {
		return 0
	}
	return value.Int64
}

func requirementSearchDefinition() contracts.Definition {
	return contracts.Definition{Layer: contracts.LayerAtomic,
		Name: "studio.requirement.search", Version: "1.0.0", Title: "Search top-level requirements",
		Description: "Search the top-level requirements of one managed project for a parent selector. Sub-requirements and archived requirements are excluded.",
		Domain:      "studio", Entity: "studio_requirement", Permission: ReadPermission,
		Risk: contracts.RiskLow, Approval: contracts.ApprovalAuto, ReadOnly: true,
		Executor: "go:studio.requirement.search", Tags: []string{"studio", "read"},
		Input: contracts.Object(map[string]*contracts.Schema{
			"project": contracts.Int(1, 1<<53),
			"keyword": {Type: "string", MaxLength: intPtr(200)},
			"exclude": contracts.Int(0, 1<<53),
		}, "project"),
		Output: contracts.Object(map[string]*contracts.Schema{
			"requirements": {Type: "array", Items: contracts.Object(map[string]*contracts.Schema{
				"id": contracts.Int(1, 1<<53), "title": {Type: "string"},
			})},
		}, "requirements"),
	}
}

func (m *Module) requirementSearch(ctx context.Context, in contracts.Input) (contracts.Output, error) {
	p, project, err := m.ownedProject(ctx, in)
	if err != nil {
		return contracts.Output{}, err
	}
	if !p.Can(ReadPermission) {
		return contracts.Output{}, contracts.Problem("forbidden")
	}
	keyword := strings.TrimSpace(stringField(in, "keyword"))
	if utf8.RuneCountInString(keyword) > 200 {
		return contracts.Output{}, contracts.Problem("invalid")
	}
	var exclude int64
	if raw, ok := number(in["exclude"]); ok && raw >= 0 {
		exclude = int64(raw)
	}
	rows, err := studiodb.New(m.pool).StudioRequirementSearch(ctx, studiodb.StudioRequirementSearchParams{
		TenantID: p.TenantID, ProjectID: project.ID, CreatedBy: p.Account.ID,
		Keyword: keyword, Exclude: exclude,
	})
	if err != nil {
		return contracts.Output{}, err
	}
	requirements := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		requirements = append(requirements, map[string]any{"id": row.ID, "title": row.Title})
	}
	return contracts.Output{Data: map[string]any{"requirements": requirements}, Summary: "requirements=" + itoa(len(requirements))}, nil
}
