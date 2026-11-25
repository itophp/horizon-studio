-- name: ExampleOrderCreate :one
INSERT INTO example_orders (tenant_id, supplier_id, title)
SELECT supplier.tenant_id, supplier.id, sqlc.arg(title) FROM example_records supplier
WHERE supplier.tenant_id = sqlc.arg(tenant_id) AND supplier.id = sqlc.arg(supplier_id) AND supplier.status <> 'archived'
RETURNING id;

-- name: ExampleOrderList :many
SELECT visible.id, visible.title FROM example_accessible_orders AS visible
WHERE visible.tenant_id = sqlc.arg(tenant_id)
AND (sqlc.arg(all_suppliers)::boolean OR visible.supplier_id = sqlc.arg(supplier_id))
AND visible.id > sqlc.arg(after_id) ORDER BY visible.id LIMIT sqlc.arg(max_rows) OFFSET sqlc.arg(skip_rows);

-- name: ExampleOrderCount :one
SELECT count(*) FROM example_accessible_orders AS visible
WHERE visible.tenant_id = sqlc.arg(tenant_id)
AND (sqlc.arg(all_suppliers)::boolean OR visible.supplier_id = sqlc.arg(supplier_id));

-- name: ExampleOrderGet :one
SELECT visible.id, visible.title FROM example_accessible_orders AS visible
WHERE visible.tenant_id = sqlc.arg(tenant_id) AND visible.id = sqlc.arg(id)
AND (sqlc.arg(all_suppliers)::boolean OR visible.supplier_id = sqlc.arg(supplier_id));

-- name: ExampleOrderUpdate :execrows
UPDATE example_orders orders SET title = sqlc.arg(title)
WHERE orders.tenant_id = sqlc.arg(tenant_id) AND orders.id IN (
    SELECT visible.id FROM example_accessible_orders visible
    WHERE visible.tenant_id = sqlc.arg(tenant_id) AND visible.id = ANY(sqlc.arg(ids)::bigint[])
    AND (sqlc.arg(all_suppliers)::boolean OR visible.supplier_id = sqlc.arg(supplier_id))
);
