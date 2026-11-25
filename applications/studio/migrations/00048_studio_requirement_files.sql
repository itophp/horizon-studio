-- Binding between one requirement and one uploaded file. Removing a binding
-- never deletes the file; storage lifecycle stays with the shared file service.
CREATE TABLE studio_requirement_files (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 tenant_id text NOT NULL,
 requirement_id bigint NOT NULL,
 file_id bigint NOT NULL,
 created_by bigint NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE (tenant_id, requirement_id, file_id),
 CONSTRAINT studio_requirement_files_requirement_fkey FOREIGN KEY (tenant_id, requirement_id)
   REFERENCES studio_requirements(tenant_id, id) ON DELETE CASCADE,
 CONSTRAINT studio_requirement_files_file_fkey FOREIGN KEY (tenant_id, file_id)
   REFERENCES gov_files(tenant_id, id)
);
CREATE INDEX studio_requirement_files_file ON studio_requirement_files (tenant_id, file_id);
