-- Two-level requirement tree: a child references one top-level requirement of
-- the same tenant. The application enforces "parent must be top-level" and
-- "a requirement with children cannot move under another requirement"; the
-- composite foreign key keeps parent and child in one tenant.
ALTER TABLE studio_requirements ADD COLUMN parent_id bigint;
ALTER TABLE studio_requirements ADD CONSTRAINT studio_requirements_parent_fkey
  FOREIGN KEY (tenant_id, parent_id) REFERENCES studio_requirements(tenant_id, id);
ALTER TABLE studio_requirements ADD CONSTRAINT studio_requirements_parent_self
  CHECK (parent_id IS NULL OR parent_id <> id);
CREATE INDEX studio_requirements_parent ON studio_requirements (tenant_id, parent_id);
