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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

func itemLinkSessionDefinition() contracts.Definition {
	return contracts.Definition{
		Name: "studio.item.link_session", Version: "1.0.0", Title: "Link a DSH session",
		Description: "Bind an execution task to its DSH session. An existing binding remains authoritative.",
		Domain:      "studio", Entity: "studio_item", Layer: contracts.LayerAtomic,
		Permission: WritePermission, Risk: contracts.RiskLow, Approval: contracts.ApprovalAuto,
		Tags: []string{"studio", "manual-only"}, Executor: "go:studio.item.link_session",
		Idempotency:   contracts.Idempotency{Required: true, Scope: "tenant+operation+key"},
		SuccessEvents: []string{"studio.item.session_binding_confirmed"},
		Input: contracts.Object(map[string]*contracts.Schema{
			"id": contracts.Int(1, 1<<53), "sessionId": text(1, 256),
		}, "id", "sessionId"),
		Output:   contracts.Object(map[string]*contracts.Schema{"sessionId": text(1, 256)}, "sessionId"),
		Loggable: []string{"id"},
	}
}

func (m *Module) itemLinkSession(ctx context.Context, tx pgx.Tx, in contracts.Input) (contracts.Output, error) {
	p, err := identity.Current(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	if !p.Can(WritePermission) {
		return contracts.Output{}, contracts.Problem("forbidden")
	}
	id, ok := number(in["id"])
	sessionID, valid := studioSessionID(in)
	if !ok || id < 1 || !valid {
		return contracts.Output{}, contracts.Problem("invalid")
	}

	q := studiodb.New(tx)
	linked, err := q.StudioItemLinkSession(ctx, studiodb.StudioItemLinkSessionParams{
		TenantID: p.TenantID, CreatedBy: p.Account.ID, ItemID: int64(id), SessionID: sessionID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		row, readErr := q.StudioItemGet(ctx, studiodb.StudioItemGetParams{
			TenantID: p.TenantID, CreatedBy: p.Account.ID, ID: int64(id),
		})
		if errors.Is(readErr, pgx.ErrNoRows) || (readErr == nil && !row.DshSessionID.Valid) {
			return contracts.Output{}, contracts.Problem("not_found")
		}
		if readErr != nil {
			return contracts.Output{}, readErr
		}
		return sessionBindingOutput(int64(id), row.DshSessionID.String, false), nil
	}
	var conflict *pgconn.PgError
	if errors.As(err, &conflict) && conflict.ConstraintName == "studio_items_dsh_session" {
		return contracts.Output{}, contracts.Problem("conflict")
	}
	if err != nil {
		return contracts.Output{}, err
	}
	return sessionBindingOutput(int64(id), linked.String, true), nil
}

func sessionBindingOutput(id int64, sessionID string, newBinding bool) contracts.Output {
	return contracts.Output{
		Data: map[string]any{"sessionId": sessionID},
		Events: []contracts.Event{{
			Type: "studio.item.session_binding_confirmed", AggregateType: "studio_item",
			AggregateID: strconv.FormatInt(id, 10), Payload: map[string]any{"newBinding": newBinding},
		}},
	}
}

func studioSessionID(in contracts.Input) (pgtype.Text, bool) {
	value := stringField(in, "sessionId")
	valid := value != "" && strings.TrimSpace(value) == value && !strings.ContainsRune(value, 0) && utf8.RuneCountInString(value) <= 256
	return pgtype.Text{String: value, Valid: valid}, valid
}

func sessionContextDefinition() contracts.Definition {
	label := &contracts.Schema{Type: "string"}
	id := contracts.Int(1, 1<<53)
	return contracts.Definition{
		Name: "studio.session.context", Version: "1.0.0", Title: "Read Studio session context",
		Description: "Read the owning Studio task, project development brief, and optional linked requirement for a DSH session ID.",
		Domain:      "studio", Entity: "studio_item", Entities: []string{"studio_item", "studio_requirement", "studio_project"},
		Layer: contracts.LayerAtomic, Permission: ReadPermission, ReadOnly: true, TransientResult: true,
		Risk: contracts.RiskLow, Approval: contracts.ApprovalAuto, Tags: []string{"studio", "read"},
		Executor: "go:studio.session.context",
		Input:    contracts.Object(map[string]*contracts.Schema{"sessionId": text(1, 256)}, "sessionId"),
		Output: contracts.Object(map[string]*contracts.Schema{
			"task": contracts.Object(map[string]*contracts.Schema{
				"id": id, "title": label, "stage": label, "checks": label, "reference": label,
				"catalogTarget": label, "catalogKind": label,
			}, "id", "title", "stage", "checks", "reference", "catalogTarget", "catalogKind"),
			"project": contracts.Object(map[string]*contracts.Schema{
				"id": id, "root": label, "developmentBrief": label,
			}, "id", "root", "developmentBrief"),
			"requirement": contracts.Object(map[string]*contracts.Schema{
				"id": id, "parentId": contracts.Int(0, 1<<53), "title": label, "description": label,
				"source": label, "archived": {Type: "boolean"},
				"attachments": {Type: "array", Items: contracts.Object(map[string]*contracts.Schema{
					"fileId": contracts.Int(1, 1<<40), "name": label, "mime": label, "size": contracts.Int(0, 1<<50),
				}, "fileId", "name", "mime", "size")},
			}, "id", "title", "description", "source", "archived", "attachments"),
		}, "task", "project"),
	}
}

func (m *Module) sessionContext(ctx context.Context, in contracts.Input) (contracts.Output, error) {
	p, err := identity.Current(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	if !p.Can(ReadPermission) {
		return contracts.Output{}, contracts.Problem("forbidden")
	}
	sessionID, valid := studioSessionID(in)
	if !valid {
		return contracts.Output{}, contracts.Problem("invalid")
	}
	row, err := studiodb.New(m.pool).StudioSessionContext(ctx, studiodb.StudioSessionContextParams{
		TenantID: p.TenantID, CreatedBy: p.Account.ID, DshSessionID: sessionID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, contracts.Problem("not_found")
	}
	if err != nil {
		return contracts.Output{}, err
	}
	data := map[string]any{
		"task": map[string]any{
			"id": row.TaskID, "title": row.TaskTitle, "stage": row.Stage, "checks": row.Checks,
			"reference": row.Reference, "catalogKind": row.CatalogKind, "catalogTarget": row.CatalogTarget,
		},
		"project": map[string]any{"id": row.ProjectID, "root": row.ProjectRoot, "developmentBrief": row.DevelopmentBrief},
	}
	if row.RequirementID.Valid {
		attachments, err := m.requirementAttachments(ctx, p.TenantID, p.Account.ID, row.ProjectID, row.RequirementID.Int64)
		if err != nil {
			return contracts.Output{}, err
		}
		data["requirement"] = map[string]any{
			"id": row.RequirementID.Int64, "parentId": requirementParentID(row.RequirementParentID),
			"title":       row.RequirementTitle.String,
			"description": row.RequirementDescription.String, "source": row.RequirementSource.String,
			"archived": row.RequirementArchived.Bool, "attachments": attachments,
		}
	}
	return contracts.Output{Data: data}, nil
}
