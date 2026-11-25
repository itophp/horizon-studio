package example

import (
	"context"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"

	"horizon/foundation/kernel/capability/contracts"
	"horizon/foundation/kernel/capability/registry"
	"horizon/foundation/system/identity"
	"horizon/internal/repository/dbgen/exampledb"
)

const (
	orderRead       = "example-orders:read"
	orderWrite      = "example-orders:write"
	orderManage     = "example-orders:manage"
	supplierContact = "com.horizon.example.supplier-contact"
	orderCreated    = "example.order.created"
	orderUpdated    = "example.order.updated"
)

func orderDefinition(action, title, description, permission string, input, output *contracts.Schema, event string) contracts.Definition {
	definition := contracts.Definition{
		Name: "example.order." + action, Version: "1.0.0", Title: title, Description: description,
		Domain: "example", Entity: "example_order", Layer: contracts.LayerAtomic, Permission: permission,
		ReadOnly: event == "", Risk: contracts.RiskLow, Approval: contracts.ApprovalAuto,
		Executor: "go:example.order." + action, Input: input, Output: output,
	}
	if event != "" {
		definition.Risk = contracts.RiskMedium
		definition.Idempotency = contracts.Idempotency{Required: true, Scope: "tenant+operation+key"}
		definition.SuccessEvents = []string{event}
	}
	return definition
}

func (m *Module) orderEntries() []registry.Entry {
	order := contracts.Object(map[string]*contracts.Schema{"id": {Type: "integer"}, "title": {Type: "string"}}, "id", "title")
	page := contracts.Object(map[string]*contracts.Schema{
		"items": {Type: "array", Items: order}, "total": {Type: "integer"},
	}, "items", "total")
	query := contracts.Object(map[string]*contracts.Schema{
		"afterId": contracts.Int(0, 1<<53-1), "limit": contracts.Int(1, 100),
		"page": contracts.Int(1, 1000000), "pageSize": contracts.Int(1, 100),
	})
	id := contracts.Int(1, 1<<53-1)
	title := contracts.Str(1, 200)
	updated := contracts.Object(map[string]*contracts.Schema{"updated": {Type: "integer"}}, "updated")
	minBatch, maxBatch := 1, 100
	return []registry.Entry{
		{Definition: orderDefinition("list", "List supplier orders", "List visible orders with a scoped total.", orderRead, query, page, ""), Handler: contracts.Handler{Execute: m.listOrders}},
		{Definition: orderDefinition("export", "Export supplier orders", "Read a page of the scoped order projection for export.", orderRead, query, page, ""), Handler: contracts.Handler{Execute: m.listOrders}},
		{Definition: orderDefinition("get", "Read supplier order", "Read one visible order.", orderRead,
			contracts.Object(map[string]*contracts.Schema{"id": id}, "id"), order, ""), Handler: contracts.Handler{Execute: m.getOrder}},
		{Definition: orderDefinition("create", "Assign supplier order", "Create an order for an active supplier contact in this tenant.", orderManage,
			contracts.Object(map[string]*contracts.Schema{"supplierId": id, "title": title}, "supplierId", "title"), order, orderCreated), Handler: contracts.Handler{ExecuteTx: m.createOrder}},
		{Definition: orderDefinition("update", "Update supplier order", "Change the title of one visible order.", orderWrite,
			contracts.Object(map[string]*contracts.Schema{"id": id, "title": title}, "id", "title"), updated, orderUpdated), Handler: contracts.Handler{ExecuteTx: m.updateOrder}},
		{Definition: orderDefinition("batchUpdate", "Update supplier orders", "Change titles only when every order in the batch is visible.", orderWrite,
			contracts.Object(map[string]*contracts.Schema{"ids": {Type: "array", Items: id, MinItems: &minBatch, MaxItems: &maxBatch}, "title": title}, "ids", "title"), updated, orderUpdated), Handler: contracts.Handler{ExecuteTx: m.updateOrder}},
	}
}

func orderScope(ctx context.Context) (exampledb.ExampleOrderCountParams, error) {
	principal, err := identity.Current(ctx)
	if err != nil {
		return exampledb.ExampleOrderCountParams{}, err
	}
	switch {
	case principal.IdentityBindingID != "" && principal.IdentityType == supplierContact && principal.IdentityEntityID > 0:
		return exampledb.ExampleOrderCountParams{TenantID: principal.TenantID, SupplierID: principal.IdentityEntityID}, nil
	case principal.IdentityBindingID == "" && principal.CanPermanent(orderManage):
		return exampledb.ExampleOrderCountParams{TenantID: principal.TenantID, AllSuppliers: true}, nil
	default:
		return exampledb.ExampleOrderCountParams{}, contracts.Problem("forbidden")
	}
}

func (m *Module) listOrders(ctx context.Context, input contracts.Input) (contracts.Output, error) {
	scope, err := orderScope(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	request := struct {
		AfterID  int64 `json:"afterId"`
		Limit    int32 `json:"limit"`
		Page     int32 `json:"page"`
		PageSize int32 `json:"pageSize"`
	}{Page: 1, PageSize: 30}
	if err := contracts.Bind(input, &request); err != nil {
		return contracts.Output{}, contracts.Problem("invalid")
	}
	// Published Runtime pages may still send an explicit limit alongside pageSize.
	if request.Limit == 0 {
		request.Limit = request.PageSize
	}
	tx, err := m.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return contracts.Output{}, err
	}
	defer tx.Rollback(ctx)
	queries := exampledb.New(tx)
	rows, err := queries.ExampleOrderList(ctx, exampledb.ExampleOrderListParams{
		TenantID: scope.TenantID, AllSuppliers: scope.AllSuppliers, SupplierID: scope.SupplierID, AfterID: request.AfterID, MaxRows: request.Limit, SkipRows: (request.Page - 1) * request.Limit,
	})
	if err != nil {
		return contracts.Output{}, err
	}
	total, err := queries.ExampleOrderCount(ctx, scope)
	if err != nil {
		return contracts.Output{}, err
	}
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, orderProjection(row.ID, row.Title))
	}
	return contracts.Output{Data: map[string]any{"items": items, "total": total}}, tx.Commit(ctx)
}

func (m *Module) getOrder(ctx context.Context, input contracts.Input) (contracts.Output, error) {
	scope, err := orderScope(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	id, _ := number(input["id"])
	row, err := exampledb.New(m.pool).ExampleOrderGet(ctx, exampledb.ExampleOrderGetParams{
		TenantID: scope.TenantID, AllSuppliers: scope.AllSuppliers, SupplierID: scope.SupplierID, ID: int64(id),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, contracts.Problem("not_found")
	}
	return contracts.Output{Data: orderProjection(row.ID, row.Title)}, err
}

func (m *Module) createOrder(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
	scope, err := orderScope(ctx)
	if err != nil || !scope.AllSuppliers {
		return contracts.Output{}, contracts.Problem("forbidden")
	}
	supplier, _ := number(input["supplierId"])
	title, _ := input["title"].(string)
	id, err := exampledb.New(tx).ExampleOrderCreate(ctx, exampledb.ExampleOrderCreateParams{
		TenantID: scope.TenantID, SupplierID: int64(supplier), Title: title,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Output{}, contracts.Problem("not_found")
	}
	if err != nil {
		return contracts.Output{}, err
	}
	return contracts.Output{Data: orderProjection(id, title), Events: []contracts.Event{orderEvent(orderCreated, id)}}, nil
}

func (m *Module) updateOrder(ctx context.Context, tx pgx.Tx, input contracts.Input) (contracts.Output, error) {
	scope, err := orderScope(ctx)
	if err != nil {
		return contracts.Output{}, err
	}
	var request struct {
		ID    int64   `json:"id"`
		IDs   []int64 `json:"ids"`
		Title string  `json:"title"`
	}
	if err := contracts.Bind(input, &request); err != nil {
		return contracts.Output{}, contracts.Problem("invalid")
	}
	ids := request.IDs
	if request.ID > 0 {
		ids = []int64{request.ID}
	}
	updated, err := exampledb.New(tx).ExampleOrderUpdate(ctx, exampledb.ExampleOrderUpdateParams{
		Title: request.Title, TenantID: scope.TenantID, SupplierID: scope.SupplierID, AllSuppliers: scope.AllSuppliers, Ids: ids,
	})
	if err != nil {
		return contracts.Output{}, err
	}
	if updated != int64(len(ids)) {
		return contracts.Output{}, contracts.Problem("not_found")
	}
	events := make([]contracts.Event, 0, len(ids))
	for _, id := range ids {
		events = append(events, orderEvent(orderUpdated, id))
	}
	return contracts.Output{Data: map[string]any{"updated": updated}, Events: events}, nil
}

func orderProjection(id int64, title string) map[string]any {
	return map[string]any{"id": id, "title": title}
}

func orderEvent(kind string, id int64) contracts.Event {
	return contracts.Event{Type: kind, AggregateType: "example_order", AggregateID: strconv.FormatInt(id, 10)}
}
