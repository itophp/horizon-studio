-- +goose Up
ALTER TABLE studio_items ADD COLUMN pinned boolean NOT NULL DEFAULT false;
ALTER TABLE studio_items ADD COLUMN archived boolean NOT NULL DEFAULT false;
ALTER TABLE studio_items ADD CONSTRAINT studio_items_session_flags
  CHECK ((NOT pinned AND NOT archived) OR kind = 'execution');

-- +goose Down
ALTER TABLE studio_items DROP CONSTRAINT studio_items_session_flags;
ALTER TABLE studio_items DROP COLUMN archived;
ALTER TABLE studio_items DROP COLUMN pinned;
