-- The Studio application ships as a built-in plugin and is enabled by default
-- for every tenant, mirroring the application runtime template seed pattern.
INSERT INTO runtime_installation_templates(plugin_id, version, application_id, enabled)
VALUES ('com.horizon.studio', '1.0.0', 'studio', true)
ON CONFLICT (plugin_id) DO UPDATE SET version = EXCLUDED.version, application_id = EXCLUDED.application_id, enabled = EXCLUDED.enabled;

INSERT INTO runtime_installations(tenant_id, plugin_id, version, status)
SELECT DISTINCT a.tenant_id, t.plugin_id, t.version, CASE WHEN t.enabled THEN 'enabled' ELSE 'installed' END
FROM iam_accounts a CROSS JOIN runtime_installation_templates t
ON CONFLICT (tenant_id, plugin_id) DO NOTHING;

INSERT INTO runtime_application_states(tenant_id, application_id, plugin_id)
SELECT i.tenant_id, t.application_id, i.plugin_id FROM runtime_installations i
JOIN runtime_installation_templates t ON t.plugin_id = i.plugin_id
ON CONFLICT (tenant_id, application_id) DO NOTHING;
