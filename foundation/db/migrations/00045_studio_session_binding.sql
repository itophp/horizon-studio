-- +goose Up
ALTER TABLE studio_items ADD COLUMN dsh_session_id text
  CHECK (dsh_session_id IS NULL OR (kind = 'execution' AND length(dsh_session_id) BETWEEN 1 AND 256));
CREATE UNIQUE INDEX studio_items_dsh_session ON studio_items (tenant_id, dsh_session_id)
  WHERE dsh_session_id IS NOT NULL;

-- +goose Down
DROP INDEX studio_items_dsh_session;
ALTER TABLE studio_items DROP COLUMN dsh_session_id;
