ALTER TABLE studio_projects ADD COLUMN name text NOT NULL DEFAULT '' CHECK (length(name) <= 200);
ALTER TABLE studio_projects ADD COLUMN ai_analysis text NOT NULL DEFAULT '' CHECK (length(ai_analysis) <= 4000);
ALTER TABLE studio_projects ADD COLUMN ai_analyzed_at timestamptz;
