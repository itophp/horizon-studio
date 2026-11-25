ALTER TABLE studio_projects ADD COLUMN development_brief text NOT NULL DEFAULT '' CHECK (length(development_brief) <= 4000);
ALTER TABLE studio_projects ALTER COLUMN api_origin SET DEFAULT '';
ALTER TABLE studio_projects ALTER COLUMN web_origin SET DEFAULT '';
