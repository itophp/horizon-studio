package studio

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/system/identity"
	"horizon/internal/repository/dbgen"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// Deterministic stage machine, mirrored by the studio_item entity declaration
// and the database CHECK constraint.
var itemStages = map[string][]string{
	"planned":    {"developing"},
	"developing": {"verifying"},
	"verifying":  {"developing", "accepted"},
	"accepted":   {},
}

func itemCreateDefinition() contracts.Definition {
	return contracts.Definition{Layer: contracts.LayerAtomic,
		Name: "studio.item.create", Version: "1.0.0", Title: "Register a work item",
		SuccessEvents: []string{"studio.item.created"},
		Description:   "Register one application, capability, workflow or execution item for a managed project, optionally tied to a collected requirement.",
		Domain:        "studio", Entity: "studio_item", Permission: WritePermission,
		Risk: contracts.RiskLow, Approval: contracts.ApprovalAuto,
		Idempotency: contracts.Idempotency{Required: true, Scope: "tenant+operation+key"},
		Executor:    "go:studio.item.create", Tags: []string{"studio", "write"},
		Input: contracts.Object(map[string]*contracts.Schema{
			"project": contracts.Int(1, 1<<53), "kind": {Type: "string", Enum: []string{"application", "capability", "workflow", "plugin", "execution"}},
			"title": text(1, 200), "requirementId": contracts.Int(1, 1<<53),
			"checks":        {Type: "string", MaxLength: intPtr(12000)},
			"catalogTarget": text(1, 180), "subjectItemId": contracts.Int(1, 1<<53),
		}, "project", "kind", "title"),
		Output: contracts.Object(map[string]*contracts.Schema{
			"id": contracts.Int(1, 1<<53), "stage": {Type: "string"},
		}, "id", "stage"),
		Loggable: []string{"project", "kind", "title", "requirementId"},
	}
}

func (m *Module) itemCreate(ctx context.Context, tx pgx.Tx, in contracts.Input) (contracts.Output, error) {
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
	kind, _ := in["kind"].(string)
	title := strings.TrimSpace(stringField(in, "title"))
	if title == "" || utf8.RuneCountInString(title) > 200 {
		return contracts.Output{}, contracts.Problem("invalid")
	}
	var requirementID pgtype.Int8
	if raw, ok := number(in["requirementId"]); ok && raw >= 1 {
		requirementID = pgtype.Int8{Int64: int64(raw), Valid: true}
	}
	target := stringField(in, "catalogTarget")
	if (target != "" && (kind != "capability" && kind != "workflow" && kind != "plugin" || !fixedReference(target) || strings.Count(target, "@") != 1)) ||
		((kind == "workflow" || kind == "plugin") && target == "") {
		return contracts.Output{}, contracts.Problem("invalid")
	}
	var subject pgtype.Int8
	if raw, ok := number(in["subjectItemId"]); ok && raw >= 1 {
		if kind != "execution" {
			return contracts.Output{}, contracts.Problem("invalid")
		}
		subject = pgtype.Int8{Int64: int64(raw), Valid: true}
	}
	row, err := dbgen.New(tx).StudioItemCreate(ctx, dbgen.StudioItemCreateParams{
		TenantID: p.TenantID, ID: projectID, RequirementID: requirementID, Kind: kind,
		Title: title, Checks: stringField(in, "checks"), CreatedBy: p.Account.ID, CatalogTarget: target, SubjectItemID: subject,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, contracts.Problem("not_found")
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.ConstraintName == "studio_items_catalog_target" {
		return contracts.Output{}, contracts.Problem("conflict")
	}
	if err != nil {
		return contracts.Output{}, err
	}
	return contracts.Output{Data: map[string]any{"id": row.ID, "stage": row.Stage}, Summary: "item=" + itoa(int(row.ID)), Events: []contracts.Event{{Type: "studio.item.created", AggregateType: "studio_item", AggregateID: strconv.FormatInt(row.ID, 10), Payload: map[string]any{}}}}, nil
}

func itemBindDefinition() contracts.Definition {
	return contracts.Definition{Layer: contracts.LayerAtomic,
		Name: "studio.item.bind", Version: "1.0.0", Title: "Pin a fixed definition version",
		SuccessEvents: []string{"studio.item.bound"},
		Description:   "Pin one fixed name@version definition onto a work item that has not been accepted yet. The project CLI must return the selected definition for the pin to stick.",
		Domain:        "studio", Entity: "studio_item", Permission: WritePermission,
		Risk: contracts.RiskLow, Approval: contracts.ApprovalAuto,
		Idempotency: contracts.Idempotency{Required: true, Scope: "tenant+operation+key"},
		Executor:    "go:studio.item.bind", Tags: []string{"studio", "write"},
		Input: contracts.Object(map[string]*contracts.Schema{
			"project": contracts.Int(1, 1<<53), "id": contracts.Int(1, 1<<53),
			"reference": text(1, 180), "kind": {Type: "string", Enum: []string{"application", "capability", "workflow", "graph"}},
		}, "project", "id", "reference"),
		Output: contracts.Object(map[string]*contracts.Schema{
			"id": contracts.Int(1, 1<<53), "reference": {Type: "string"},
		}, "id", "reference"),
		Loggable: []string{"project", "id", "reference"},
	}
}

func (m *Module) itemBind(ctx context.Context, tx pgx.Tx, in contracts.Input) (contracts.Output, error) {
	p, err := identity.Current(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	if !p.Can(WritePermission) {
		return contracts.Output{}, contracts.Problem("forbidden")
	}
	id, ok := number(in["id"])
	reference, _ := in["reference"].(string)
	kind, _ := in["kind"].(string)
	if !ok || id < 1 || !fixedReference(reference) {
		return contracts.Output{}, contracts.Problem("invalid")
	}
	current, err := dbgen.New(tx).StudioItemGet(ctx, dbgen.StudioItemGetParams{TenantID: p.TenantID, ID: int64(id), CreatedBy: p.Account.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, contracts.Problem("not_found")
	}
	if err != nil {
		return contracts.Output{}, err
	}
	if current.Stage == "accepted" {
		return contracts.Output{}, contracts.Problem("已验收事项已固定版本")
	}
	if kind == "" {
		kind = current.Kind
	}
	root, err := m.projectRootOf(ctx, p, current.ProjectID)
	if err != nil {
		return contracts.Output{}, err
	}
	definition, err := inspect(ctx, projectRef{ID: current.ProjectID, Root: root}, kind, reference)
	if err != nil {
		return contracts.Output{}, err
	}
	var schema struct {
		Entry *struct {
			ID      string `json:"id"`
			Version string `json:"version"`
		} `json:"entry"`
	}
	_ = json.Unmarshal(definition, &schema)
	var entry struct{ ID, Version string }
	if schema.Entry != nil {
		entry.ID, entry.Version = schema.Entry.ID, schema.Entry.Version
	}
	name, version, _ := strings.Cut(reference, "@")
	if entry.ID != name || entry.Version != version {
		return contracts.Output{}, contracts.Problem("CLI 未返回所选版本的定义")
	}
	row, err := dbgen.New(tx).StudioItemBind(ctx, dbgen.StudioItemBindParams{
		TenantID: p.TenantID, ID: int64(id), Reference: reference, ReferenceKind: kind, CreatedBy: p.Account.ID,
	})
	if err != nil {
		return contracts.Output{}, err
	}
	return contracts.Output{Data: map[string]any{"id": row.ID, "reference": row.Reference}, Summary: "item.bind=" + reference, Events: []contracts.Event{{Type: "studio.item.bound", AggregateType: "studio_item", AggregateID: strconv.FormatInt(row.ID, 10), Payload: map[string]any{}}}}, nil
}

func itemTransitionDefinition() contracts.Definition {
	return contracts.Definition{Layer: contracts.LayerAtomic,
		Name: "studio.item.transition", Version: "1.0.0", Title: "Transition a work item stage",
		SuccessEvents: []string{"studio.item.stage.changed"},
		Description:   "Move a work item through planned → developing → verifying → accepted. Acceptance requires recorded evidence and pins the project source revision.",
		Domain:        "studio", Entity: "studio_item", Permission: WritePermission,
		Risk: contracts.RiskMedium, Approval: contracts.ApprovalAuto,
		Idempotency: contracts.Idempotency{Required: true, Scope: "tenant+operation+key"},
		Executor:    "go:studio.item.transition", Tags: []string{"studio", "write"},
		Input: contracts.Object(map[string]*contracts.Schema{
			"project": contracts.Int(1, 1<<53), "id": contracts.Int(1, 1<<53),
			"stage": {Type: "string", Enum: []string{"planned", "developing", "verifying", "accepted"}},
			"evidence": {Type: "object", Properties: map[string]*contracts.Schema{
				"notes": text(1, 4000),
			}},
		}, "project", "id", "stage"),
		Output: contracts.Object(map[string]*contracts.Schema{
			"id": contracts.Int(1, 1<<53), "stage": {Type: "string"},
		}, "id", "stage"),
		Loggable: []string{"project", "id", "stage"},
	}
}

func (m *Module) itemTransition(ctx context.Context, tx pgx.Tx, in contracts.Input) (contracts.Output, error) {
	p, err := identity.Current(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	if !p.Can(WritePermission) {
		return contracts.Output{}, contracts.Problem("forbidden")
	}
	id, ok := number(in["id"])
	stage, _ := in["stage"].(string)
	if !ok || id < 1 {
		return contracts.Output{}, contracts.Problem("invalid")
	}
	current, err := dbgen.New(tx).StudioItemGet(ctx, dbgen.StudioItemGetParams{TenantID: p.TenantID, ID: int64(id), CreatedBy: p.Account.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, contracts.Problem("not_found")
	}
	if err != nil {
		return contracts.Output{}, err
	}
	allowed := false
	for _, candidate := range itemStages[current.Stage] {
		if candidate == stage {
			allowed = true
		}
	}
	if current.Stage == stage || !allowed {
		return contracts.Output{}, contracts.Problem("无法从「" + current.Stage + "」转移到「" + stage + "」，请检查事项状态")
	}
	evidence := []byte(current.Evidence)
	if stage == "accepted" {
		notes, _ := in["evidence"].(map[string]any)["notes"].(string)
		if notes == "" {
			return contracts.Output{}, contracts.Problem("验收必须记录验证证据")
		}
		root, err := m.projectRootOf(ctx, p, current.ProjectID)
		if err != nil {
			return contracts.Output{}, err
		}
		revision, modified, err := sourceRevision(ctx, root)
		if err != nil {
			return contracts.Output{}, err
		}
		evidence, _ = json.Marshal(map[string]any{"revision": revision, "modified": modified, "notes": notes})
	}
	row, err := dbgen.New(tx).StudioItemTransition(ctx, dbgen.StudioItemTransitionParams{
		TenantID: p.TenantID, ID: int64(id), Stage: stage, CreatedBy: p.Account.ID, Evidence: evidence,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, contracts.Problem("conflict")
	}
	if err != nil {
		return contracts.Output{}, err
	}
	return contracts.Output{Data: map[string]any{"id": row.ID, "stage": row.Stage}, Summary: "item=" + itoa(int(row.ID)) + " stage=" + row.Stage, Events: []contracts.Event{{Type: "studio.item.stage.changed", AggregateType: "studio_item", AggregateID: strconv.FormatInt(row.ID, 10), Payload: map[string]any{}}}}, nil
}

func itemListDefinition() contracts.Definition {
	return contracts.Definition{Layer: contracts.LayerAtomic,
		Name: "studio.item.list", Version: "1.0.0", Title: "List work items",
		Description: "List the work items registered for one managed project, newest first.",
		Domain:      "studio", Entity: "studio_item", Permission: ReadPermission,
		Risk: contracts.RiskLow, Approval: contracts.ApprovalAuto, ReadOnly: true,
		Executor: "go:studio.item.list", Tags: []string{"studio", "read"},
		Input: contracts.Object(map[string]*contracts.Schema{"project": contracts.Int(1, 1<<53)}, "project"),
		Output: contracts.Object(map[string]*contracts.Schema{
			"items": {Type: "array", Items: contracts.Object(map[string]*contracts.Schema{
				"id": contracts.Int(1, 1<<53), "kind": {Type: "string"}, "title": {Type: "string"}, "stage": {Type: "string"},
			})},
		}, "items"),
	}
}

func (m *Module) itemList(ctx context.Context, in contracts.Input) (contracts.Output, error) {
	p, project, err := m.ownedProject(ctx, in)
	if err != nil {
		return contracts.Output{}, err
	}
	if !p.Can(ReadPermission) {
		return contracts.Output{}, contracts.Problem("forbidden")
	}
	rows, err := dbgen.New(m.pool).StudioItemList(ctx, dbgen.StudioItemListParams{
		TenantID: p.TenantID, ProjectID: project.ID, CreatedBy: p.Account.ID,
	})
	if err != nil {
		return contracts.Output{}, err
	}
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		var evidence map[string]any
		_ = json.Unmarshal(row.Evidence, &evidence)
		items = append(items, map[string]any{
			"id": row.ID, "projectId": row.ProjectID, "kind": row.Kind, "title": row.Title,
			"stage": row.Stage, "reference": row.Reference, "referenceKind": row.ReferenceKind,
			"checks": row.Checks, "evidence": evidence, "catalogTarget": row.CatalogTarget,
			"requirementId": row.RequirementID, "subjectItemId": row.SubjectItemID, "dshSessionId": row.DshSessionID,
			"pinned": row.Pinned, "archived": row.Archived,
		})
	}
	return contracts.Output{Data: map[string]any{"items": items}, Summary: "items=" + itoa(len(rows))}, nil
}
