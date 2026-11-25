ALTER TABLE studio_items DROP CONSTRAINT studio_items_kind_check;
ALTER TABLE studio_items ADD CONSTRAINT studio_items_kind_check CHECK (kind IN ('application','capability','workflow','plugin','execution'));
ALTER TABLE studio_items ADD COLUMN catalog_target text NOT NULL DEFAULT '' CHECK (length(catalog_target) <= 180);
ALTER TABLE studio_items ADD COLUMN subject_item_id bigint;
ALTER TABLE studio_items ADD CONSTRAINT studio_items_subject_fkey
  FOREIGN KEY (tenant_id, subject_item_id) REFERENCES studio_items(tenant_id, id) ON DELETE SET NULL (subject_item_id);
CREATE UNIQUE INDEX studio_items_catalog_target ON studio_items (tenant_id, project_id, kind, catalog_target) WHERE catalog_target <> '';
