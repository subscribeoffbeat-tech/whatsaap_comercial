-- Migration to undo Multi-Tenancy

ALTER TABLE app_config DROP CONSTRAINT IF EXISTS app_config_pkey;
ALTER TABLE app_config DROP COLUMN IF EXISTS tenant_id;
ALTER TABLE app_config ADD PRIMARY KEY (key);

ALTER TABLE audit_log DROP COLUMN IF EXISTS tenant_id;
ALTER TABLE webhook_log DROP COLUMN IF EXISTS tenant_id;
ALTER TABLE automation_rules DROP COLUMN IF EXISTS tenant_id;

ALTER TABLE campaigns DROP COLUMN IF EXISTS tenant_id;

ALTER TABLE templates DROP CONSTRAINT IF EXISTS templates_tenant_name_lang_key;
ALTER TABLE templates DROP COLUMN IF EXISTS tenant_id;
ALTER TABLE templates ADD CONSTRAINT templates_name_language_key UNIQUE (name, language);

ALTER TABLE conversations DROP COLUMN IF EXISTS tenant_id;

ALTER TABLE contacts DROP CONSTRAINT IF EXISTS contacts_tenant_phone_key;
DROP INDEX IF EXISTS contacts_tenant_opted_in_idx;
ALTER TABLE contacts DROP COLUMN IF EXISTS tenant_id;
ALTER TABLE contacts ADD CONSTRAINT contacts_wa_phone_key UNIQUE (wa_phone);

ALTER TABLE tags DROP CONSTRAINT IF EXISTS tags_tenant_name_key;
ALTER TABLE tags DROP COLUMN IF EXISTS tenant_id;
ALTER TABLE tags ADD CONSTRAINT tags_name_key UNIQUE (name);

ALTER TABLE agents DROP COLUMN IF EXISTS tenant_id;

DROP TABLE IF EXISTS tenants;
