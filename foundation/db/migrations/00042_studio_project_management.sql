-- +goose Up
ALTER TABLE studio_projects ADD COLUMN name text NOT NULL DEFAULT '' CHECK (length(name) <= 200);
ALTER TABLE studio_projects ADD COLUMN ai_analysis text NOT NULL DEFAULT '' CHECK (length(ai_analysis) <= 4000);
ALTER TABLE studio_projects ADD COLUMN ai_analyzed_at timestamptz;

-- +goose Down
-- Remove the catalog contributions created by Studio before rolling back its installation.
DELETE FROM runtime_application_changes AS change
USING runtime_application_heads AS head
WHERE head.plugin_id = 'com.horizon.studio'
  AND change.tenant_id = head.tenant_id AND change.application_id = head.application_id;

ALTER TABLE runtime_application_versions DISABLE TRIGGER runtime_version_immutable;
DELETE FROM runtime_application_versions AS version
USING runtime_application_heads AS head
WHERE head.plugin_id = 'com.horizon.studio'
  AND version.tenant_id = head.tenant_id AND version.application_id = head.application_id;
ALTER TABLE runtime_application_versions ENABLE TRIGGER runtime_version_immutable;

DELETE FROM runtime_application_heads WHERE plugin_id = 'com.horizon.studio';
DELETE FROM runtime_run_dependencies WHERE plugin_id = 'com.horizon.studio';
DELETE FROM runtime_invocation_dependencies WHERE plugin_id = 'com.horizon.studio';
DELETE FROM runtime_installation_history WHERE plugin_id = 'com.horizon.studio';

ALTER TABLE studio_projects DROP COLUMN ai_analyzed_at;
ALTER TABLE studio_projects DROP COLUMN ai_analysis;
ALTER TABLE studio_projects DROP COLUMN name;
