-- An automation job keeps how its client takes results: the client's
-- capabilities and the delivery address of the run that created the job.
ALTER TABLE automation_jobs ADD COLUMN client_capabilities_json TEXT NOT NULL DEFAULT '';
ALTER TABLE automation_jobs ADD COLUMN delivery_address_json TEXT NOT NULL DEFAULT '';
UPDATE automation_jobs
SET client_capabilities_json = '{"supports_voice_delivery":true,"supports_document_delivery":true,"receives_deliveries":true}'
WHERE client = 'telegram';

-- No code reads the plugin MCP candidates the skills module once noted.
DROP TABLE skill_plugin_mcp_candidates;
