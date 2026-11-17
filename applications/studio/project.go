package studio

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/system/identity"
	"horizon/internal/repository/dbgen"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func text(min, max int) *contracts.Schema {
	low, high := min, max
	return &contracts.Schema{Type: "string", MinLength: &low, MaxLength: &high}
}

func number(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	}
	return 0, false
}

func floatPointer(value float64) *float64 { return &value }

func intPtr(value int) *int { return &value }

func projectOutput(row dbgen.StudioProjectCreateRow, event string) contracts.Output {
	return contracts.Output{Data: map[string]any{
		"id": row.ID, "root": row.Root, "name": projectName(row.Name, row.Root), "developmentBrief": row.DevelopmentBrief,
	}, Summary: "project=" + row.Root, Events: []contracts.Event{{
		Type: event, AggregateType: "studio_project", AggregateID: strconv.FormatInt(row.ID, 10),
	}}}
}

func projectName(name, root string) string {
	if name != "" {
		return name
	}
	return filepath.Base(root)
}

func instant(value pgtype.Timestamptz) string {
	if !value.Valid {
		return ""
	}
	return value.Time.UTC().Format(time.RFC3339Nano)
}

func projectRefOutput(row dbgen.StudioProjectGetRow) map[string]any {
	return map[string]any{"id": row.ID, "root": row.Root, "name": projectName(row.Name, row.Root),
		"developmentBrief": row.DevelopmentBrief, "createdAt": instant(row.CreatedAt),
		"aiAnalysis": row.AiAnalysis, "aiAnalyzedAt": instant(row.AiAnalyzedAt)}
}

func developmentBrief(in contracts.Input) pgtype.Text {
	value, supplied := in["developmentBrief"].(string)
	return pgtype.Text{String: strings.TrimSpace(value), Valid: supplied}
}

func projectLinkDefinition() contracts.Definition {
	return contracts.Definition{Layer: contracts.LayerAtomic,
		Name: "studio.project.link", Version: "1.0.0", Title: "Link an existing Horizon project",
		SuccessEvents: []string{"studio.project.linked"},
		Description:   "Register an existing Horizon project root with the workbench. Symlink aliases resolve to the canonical root, so one directory stays one record.",
		Domain:        "studio", Entity: "studio_project", Permission: WritePermission,
		Risk: contracts.RiskMedium, Approval: contracts.ApprovalAuto,
		Idempotency: contracts.Idempotency{Required: true, Scope: "tenant+operation+key"},
		Executor:    "go:studio.project.link", Tags: []string{"studio", "write"},
		Input: contracts.Object(map[string]*contracts.Schema{
			"path": text(1, 1024), "api": text(1, 200), "web": text(1, 200), "name": text(0, 200), "developmentBrief": text(0, 4000),
		}, "path"),
		Output: contracts.Object(map[string]*contracts.Schema{
			"id": contracts.Int(1, 1<<53), "root": {Type: "string"}, "name": {Type: "string"}, "developmentBrief": {Type: "string"},
		}, "id", "root"),
		Loggable: []string{"path"},
	}
}

func (m *Module) projectLink(ctx context.Context, tx pgx.Tx, in contracts.Input) (contracts.Output, error) {
	p, err := identity.Current(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	if !p.Can(WritePermission) {
		return contracts.Output{}, contracts.Problem("forbidden")
	}
	path, _ := in["path"].(string)
	root, err := projectRoot(path)
	if err != nil {
		return contracts.Output{}, err
	}
	row, err := dbgen.New(tx).StudioProjectCreate(ctx, dbgen.StudioProjectCreateParams{
		TenantID: p.TenantID, Root: root, CreatedBy: p.Account.ID,
		Name: strings.TrimSpace(stringField(in, "name")), DevelopmentBrief: developmentBrief(in),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, contracts.Problem("conflict")
	}
	if err != nil {
		return contracts.Output{}, err
	}
	return projectOutput(row, "studio.project.linked"), nil
}

func projectCreateDefinition() contracts.Definition {
	return contracts.Definition{Layer: contracts.LayerAtomic,
		Name: "studio.project.create", Version: "1.0.0", Title: "Create a Horizon project",
		SuccessEvents: []string{"studio.project.created"},
		Description:   "Clone a fresh Horizon project through horizon-cli init and register it with the workbench. The optional Git source may be a repository URL or a local path.",
		Domain:        "studio", Entity: "studio_project", Permission: WritePermission,
		Risk: contracts.RiskMedium, Approval: contracts.ApprovalAuto,
		Idempotency: contracts.Idempotency{Required: true, Scope: "tenant+operation+key"},
		Executor:    "go:studio.project.create", Tags: []string{"studio", "write"},
		Input: contracts.Object(map[string]*contracts.Schema{
			"path": text(1, 1024), "api": text(1, 200), "web": text(1, 200), "name": text(0, 200), "developmentBrief": text(0, 4000),
			"repository": {Type: "string", MaxLength: intPtr(500)},
		}, "path"),
		Output: contracts.Object(map[string]*contracts.Schema{
			"id": contracts.Int(1, 1<<53), "root": {Type: "string"}, "name": {Type: "string"}, "developmentBrief": {Type: "string"},
		}, "id", "root"),
		Loggable: []string{"path", "repository"},
	}
}

func (m *Module) projectCreate(ctx context.Context, in contracts.Input) (contracts.Output, error) {
	p, err := identity.Current(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	if !p.Can(WritePermission) {
		return contracts.Output{}, contracts.Problem("forbidden")
	}
	root, err := initializeProject(ctx, stringField(in, "path"), stringField(in, "repository"))
	if err != nil {
		return contracts.Output{}, err
	}
	row, err := dbgen.New(m.pool).StudioProjectCreate(ctx, dbgen.StudioProjectCreateParams{
		TenantID: p.TenantID, Root: root, CreatedBy: p.Account.ID,
		Name: strings.TrimSpace(stringField(in, "name")), DevelopmentBrief: developmentBrief(in),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, contracts.Problem("conflict")
	}
	if err != nil {
		return contracts.Output{}, err
	}
	return projectOutput(row, "studio.project.created"), nil
}

func projectListDefinition() contracts.Definition {
	return contracts.Definition{Layer: contracts.LayerAtomic,
		Name: "studio.project.list", Version: "1.0.0", Title: "List managed projects",
		Description: "List the Horizon projects linked by the current account, newest first.",
		Domain:      "studio", Entity: "studio_project", Permission: ReadPermission,
		Risk: contracts.RiskLow, Approval: contracts.ApprovalAuto, ReadOnly: true,
		Executor: "go:studio.project.list", Tags: []string{"studio", "read"},
		Input: contracts.Object(map[string]*contracts.Schema{}),
		Output: contracts.Object(map[string]*contracts.Schema{
			"projects": {Type: "array", Items: contracts.Object(map[string]*contracts.Schema{
				"id": contracts.Int(1, 1<<53), "root": {Type: "string"}, "name": {Type: "string"}, "developmentBrief": {Type: "string"},
				"requirementsTotal": {Type: "integer"}, "requirementsPending": {Type: "integer"},
				"createdAt": {Type: "string"}, "aiAnalysis": {Type: "string"}, "aiAnalyzedAt": {Type: "string"},
			})},
		}, "projects"),
	}
}

func (m *Module) projectList(ctx context.Context, in contracts.Input) (contracts.Output, error) {
	p, err := identity.Current(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	if !p.Can(ReadPermission) {
		return contracts.Output{}, contracts.Problem("forbidden")
	}
	rows, err := dbgen.New(m.pool).StudioProjectList(ctx, dbgen.StudioProjectListParams{TenantID: p.TenantID, CreatedBy: p.Account.ID})
	if err != nil {
		return contracts.Output{}, err
	}
	projects := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		projects = append(projects, map[string]any{"id": row.ID, "root": row.Root, "name": projectName(row.Name, row.Root),
			"developmentBrief": row.DevelopmentBrief, "createdAt": instant(row.CreatedAt),
			"requirementsTotal": row.RequirementsTotal, "requirementsPending": row.RequirementsPending,
			"aiAnalysis": row.AiAnalysis, "aiAnalyzedAt": instant(row.AiAnalyzedAt)})
	}
	return contracts.Output{Data: map[string]any{"projects": projects}, Summary: "projects=" + itoa(len(rows))}, nil
}

func projectGetDefinition() contracts.Definition {
	return contracts.Definition{Layer: contracts.LayerAtomic,
		Name: "studio.project.get", Version: "1.0.0", Title: "Inspect a managed project",
		Description: "Read one Horizon project linked by the current account.",
		Domain:      "studio", Entity: "studio_project", Permission: ReadPermission,
		Risk: contracts.RiskLow, Approval: contracts.ApprovalAuto, ReadOnly: true,
		Executor: "go:studio.project.get", Tags: []string{"studio", "read"},
		Input: contracts.Object(map[string]*contracts.Schema{
			"id": contracts.Int(1, 1<<53),
		}, "id"),
		Output: contracts.Object(map[string]*contracts.Schema{
			"id": contracts.Int(1, 1<<53), "root": {Type: "string"}, "name": {Type: "string"}, "developmentBrief": {Type: "string"},
			"createdAt": {Type: "string"}, "aiAnalysis": {Type: "string"}, "aiAnalyzedAt": {Type: "string"},
		}, "id", "root"),
	}
}

func (m *Module) projectGet(ctx context.Context, in contracts.Input) (contracts.Output, error) {
	p, err := identity.Current(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	if !p.Can(ReadPermission) {
		return contracts.Output{}, contracts.Problem("forbidden")
	}
	id, ok := number(in["id"])
	if !ok || id < 1 {
		return contracts.Output{}, contracts.Problem("invalid")
	}
	row, err := dbgen.New(m.pool).StudioProjectGet(ctx, dbgen.StudioProjectGetParams{TenantID: p.TenantID, ID: int64(id), CreatedBy: p.Account.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, contracts.Problem("not_found")
	}
	if err != nil {
		return contracts.Output{}, err
	}
	return contracts.Output{Data: projectRefOutput(row)}, nil
}

// ownedProject loads the project row for a scoped operation, guaranteeing the
// caller owns it before any side effect runs.
func (m *Module) ownedProject(ctx context.Context, in contracts.Input) (*identity.Principal, dbgen.StudioProjectGetRow, error) {
	var zero dbgen.StudioProjectGetRow
	p, err := identity.Current(ctx)
	if err != nil {
		return p, zero, err
	}
	id, ok := number(in["project"])
	if !ok || id < 1 {
		return p, zero, contracts.Problem("invalid")
	}
	row, err := dbgen.New(m.pool).StudioProjectGet(ctx, dbgen.StudioProjectGetParams{TenantID: p.TenantID, ID: int64(id), CreatedBy: p.Account.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return p, zero, contracts.Problem("not_found")
	}
	return p, row, err
}

func ref(row dbgen.StudioProjectGetRow) projectRef {
	return projectRef{ID: row.ID, Root: row.Root}
}

func stringField(in contracts.Input, key string) string {
	value, _ := in[key].(string)
	return value
}

func itoa(value int) string {
	data, _ := json.Marshal(value)
	return string(data)
}

// projectRootOf loads the canonical root of an owned project for CLI side effects.
func (m *Module) projectRootOf(ctx context.Context, p *identity.Principal, projectID int64) (string, error) {
	row, err := dbgen.New(m.pool).StudioProjectGet(ctx, dbgen.StudioProjectGetParams{TenantID: p.TenantID, ID: projectID, CreatedBy: p.Account.ID})
	if err != nil {
		return "", contracts.Problem("not_found")
	}
	return row.Root, nil
}
