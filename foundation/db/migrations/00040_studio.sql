-- +goose Up
-- HorizonStudio manages external Horizon projects from this workbench. One row
-- per canonical project directory per tenant: linking through a symlink or an
-- alias resolves to the same canonical root, so records stay attached to the
-- real project.
CREATE TABLE studio_projects (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 tenant_id text NOT NULL,
 root text NOT NULL,
 api_origin text NOT NULL,
 web_origin text NOT NULL,
 created_by bigint NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE (tenant_id, root),
 UNIQUE (tenant_id, id)
);
CREATE INDEX studio_projects_owner ON studio_projects (tenant_id, id DESC);

-- Requirements collected for one managed project. Archiving keeps history
-- readable without deleting collected context.
CREATE TABLE studio_requirements (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 tenant_id text NOT NULL,
 project_id bigint NOT NULL,
 title text NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
 description text NOT NULL DEFAULT '',
 source text NOT NULL DEFAULT '',
 archived boolean NOT NULL DEFAULT false,
 created_by bigint NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE (tenant_id, id),
 CONSTRAINT studio_requirements_project_fkey FOREIGN KEY (tenant_id, project_id)
   REFERENCES studio_projects(tenant_id, id) ON DELETE CASCADE
);
CREATE INDEX studio_requirements_project ON studio_requirements (tenant_id, project_id, id DESC);

-- Work items pinned to fixed definition versions. The stage machine is
-- enforced by the database; acceptance requires recorded evidence.
CREATE TABLE studio_items (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 tenant_id text NOT NULL,
 project_id bigint NOT NULL,
 requirement_id bigint,
 kind text NOT NULL CHECK (kind IN ('application','capability','execution')),
 title text NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
 stage text NOT NULL DEFAULT 'planned' CHECK (stage IN ('planned','developing','verifying','accepted')),
 reference text NOT NULL DEFAULT '',
 reference_kind text NOT NULL DEFAULT '',
 checks text NOT NULL DEFAULT '',
 evidence jsonb NOT NULL DEFAULT '{}'::jsonb,
 created_by bigint NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE (tenant_id, id),
 CONSTRAINT studio_items_project_fkey FOREIGN KEY (tenant_id, project_id)
   REFERENCES studio_projects(tenant_id, id) ON DELETE CASCADE,
 CONSTRAINT studio_items_requirement_fkey FOREIGN KEY (tenant_id, requirement_id)
   REFERENCES studio_requirements(tenant_id, id) ON DELETE SET NULL
);
CREATE INDEX studio_items_project ON studio_items (tenant_id, project_id, id DESC);

-- Gateway invocation records: one row per idempotency key so a retried call
-- resolves to the same record instead of forking history.
CREATE TABLE studio_runs (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 tenant_id text NOT NULL,
 project_id bigint NOT NULL,
 mode text NOT NULL,
 capability text NOT NULL,
 version text NOT NULL,
 request_key text NOT NULL,
 status text NOT NULL DEFAULT '',
 invocation_id text NOT NULL DEFAULT '',
 approval_id bigint NOT NULL DEFAULT 0,
 run_id bigint NOT NULL DEFAULT 0,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE (tenant_id, project_id, request_key),
 CONSTRAINT studio_runs_project_fkey FOREIGN KEY (tenant_id, project_id)
   REFERENCES studio_projects(tenant_id, id) ON DELETE CASCADE
);
CREATE INDEX studio_runs_project ON studio_runs (tenant_id, project_id, id DESC);

-- +goose Down
DROP TABLE IF EXISTS studio_runs;
DROP TABLE IF EXISTS studio_items;
DROP TABLE IF EXISTS studio_requirements;
DROP TABLE IF EXISTS studio_projects;
