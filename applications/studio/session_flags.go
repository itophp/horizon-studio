package studio

import (
	"context"
	"errors"
	"strconv"

	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/system/identity"
	"horizon/internal/repository/dbgen/studiodb"

	"github.com/jackc/pgx/v5"
)

func itemPinDefinition() contracts.Definition {
	return contracts.Definition{Layer: contracts.LayerAtomic,
		Name: "studio.item.pin", Version: "1.0.0", Title: "Pin a Studio session",
		SuccessEvents: []string{"studio.item.pinned"},
		Description:   "Pin or unpin one execution session so it stays at the top of the session list of its managed project.",
		Domain:        "studio", Entity: "studio_item", Permission: WritePermission,
		Risk: contracts.RiskLow, Approval: contracts.ApprovalAuto,
		Idempotency: contracts.Idempotency{Required: true, Scope: "tenant+operation+key"},
		Executor:    "go:studio.item.pin", Tags: []string{"studio", "write"},
		Input: contracts.Object(map[string]*contracts.Schema{
			"project": contracts.Int(1, 1<<53), "id": contracts.Int(1, 1<<53),
			"pinned": {Type: "boolean"},
		}, "project", "id", "pinned"),
		Output: contracts.Object(map[string]*contracts.Schema{
			"id": contracts.Int(1, 1<<53), "pinned": {Type: "boolean"},
		}, "id", "pinned"),
		Loggable: []string{"project", "id", "pinned"},
	}
}

func (m *Module) itemPin(ctx context.Context, tx pgx.Tx, in contracts.Input) (contracts.Output, error) {
	p, err := identity.Current(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	if !p.Can(WritePermission) {
		return contracts.Output{}, contracts.Problem("forbidden")
	}
	project, okProject := projectID(in)
	id, okID := number(in["id"])
	pinned, okPinned := in["pinned"].(bool)
	if !okProject || !okID || id < 1 || !okPinned {
		return contracts.Output{}, contracts.Problem("invalid")
	}
	row, err := studiodb.New(tx).StudioItemSetPinned(ctx, studiodb.StudioItemSetPinnedParams{
		TenantID: p.TenantID, ID: int64(id), Project: project,
		Pinned: pinned, CreatedBy: p.Account.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, contracts.Problem("conflict")
	}
	if err != nil {
		return contracts.Output{}, err
	}
	return contracts.Output{
		Data:    map[string]any{"id": row.ID, "pinned": row.Pinned},
		Summary: "session.pin=" + strconv.FormatBool(row.Pinned),
		Events: []contracts.Event{{
			Type: "studio.item.pinned", AggregateType: "studio_item",
			AggregateID: strconv.FormatInt(row.ID, 10),
			Payload:     map[string]any{"pinned": row.Pinned},
		}},
	}, nil
}

func itemArchiveDefinition() contracts.Definition {
	return contracts.Definition{Layer: contracts.LayerAtomic,
		Name: "studio.item.archive", Version: "1.0.0", Title: "Archive a Studio session",
		SuccessEvents: []string{"studio.item.archived"},
		Description:   "Archive or restore one execution session. Archived sessions stay readable but leave the default session list; archiving clears the pin.",
		Domain:        "studio", Entity: "studio_item", Permission: WritePermission,
		Risk: contracts.RiskLow, Approval: contracts.ApprovalAuto,
		Idempotency: contracts.Idempotency{Required: true, Scope: "tenant+operation+key"},
		Executor:    "go:studio.item.archive", Tags: []string{"studio", "write"},
		Input: contracts.Object(map[string]*contracts.Schema{
			"project": contracts.Int(1, 1<<53), "id": contracts.Int(1, 1<<53),
			"archived": {Type: "boolean"},
		}, "project", "id", "archived"),
		Output: contracts.Object(map[string]*contracts.Schema{
			"id": contracts.Int(1, 1<<53), "archived": {Type: "boolean"}, "pinned": {Type: "boolean"},
		}, "id", "archived", "pinned"),
		Loggable: []string{"project", "id", "archived"},
	}
}

func (m *Module) itemArchive(ctx context.Context, tx pgx.Tx, in contracts.Input) (contracts.Output, error) {
	p, err := identity.Current(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	if !p.Can(WritePermission) {
		return contracts.Output{}, contracts.Problem("forbidden")
	}
	project, okProject := projectID(in)
	id, okID := number(in["id"])
	archived, okArchived := in["archived"].(bool)
	if !okProject || !okID || id < 1 || !okArchived {
		return contracts.Output{}, contracts.Problem("invalid")
	}
	row, err := studiodb.New(tx).StudioItemSetArchived(ctx, studiodb.StudioItemSetArchivedParams{
		TenantID: p.TenantID, ID: int64(id), Project: project,
		Archived: archived, CreatedBy: p.Account.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, contracts.Problem("conflict")
	}
	if err != nil {
		return contracts.Output{}, err
	}
	return contracts.Output{
		Data:    map[string]any{"id": row.ID, "archived": row.Archived, "pinned": row.Pinned},
		Summary: "session.archived=" + strconv.FormatBool(row.Archived),
		Events: []contracts.Event{{
			Type: "studio.item.archived", AggregateType: "studio_item",
			AggregateID: strconv.FormatInt(row.ID, 10),
			Payload:     map[string]any{"archived": row.Archived},
		}},
	}, nil
}
