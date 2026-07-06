-- Migration to enable Multi-Tenancy

CREATE TABLE tenants (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT        NOT NULL,
    slug       TEXT        NOT NULL UNIQUE,
    status     TEXT        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended', 'trial')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Seed default system tenant to migrate existing data cleanly
INSERT INTO tenants (id, name, slug, status)
VALUES ('00000000-0000-0000-0000-000000000000', 'Default Tenant', 'default', 'active')
ON CONFLICT DO NOTHING;

-- Scoping agents
ALTER TABLE agents ADD COLUMN tenant_id UUID REFERENCES tenants(id) ON DELETE CASCADE;
UPDATE agents SET tenant_id = '00000000-0000-0000-0000-000000000000';

-- Scoping tags
ALTER TABLE tags ADD COLUMN tenant_id UUID REFERENCES tenants(id) ON DELETE CASCADE;
UPDATE tags SET tenant_id = '00000000-0000-0000-0000-000000000000';
ALTER TABLE tags ALTER COLUMN tenant_id SET NOT NULL;
ALTER TABLE tags DROP CONSTRAINT IF EXISTS tags_name_key;
ALTER TABLE tags ADD CONSTRAINT tags_tenant_name_key UNIQUE (tenant_id, name);

-- Scoping contacts
ALTER TABLE contacts ADD COLUMN tenant_id UUID REFERENCES tenants(id) ON DELETE CASCADE;
UPDATE contacts SET tenant_id = '00000000-0000-0000-0000-000000000000';
ALTER TABLE contacts ALTER COLUMN tenant_id SET NOT NULL;
ALTER TABLE contacts DROP CONSTRAINT IF EXISTS contacts_wa_phone_key;
ALTER TABLE contacts ADD CONSTRAINT contacts_tenant_phone_key UNIQUE (tenant_id, wa_phone);
CREATE INDEX IF NOT EXISTS contacts_tenant_opted_in_idx ON contacts (tenant_id, opted_in) WHERE opted_in = TRUE;

-- Scoping conversations
ALTER TABLE conversations ADD COLUMN tenant_id UUID REFERENCES tenants(id) ON DELETE CASCADE;
UPDATE conversations SET tenant_id = '00000000-0000-0000-0000-000000000000';
ALTER TABLE conversations ALTER COLUMN tenant_id SET NOT NULL;

-- Scoping templates
ALTER TABLE templates ADD COLUMN tenant_id UUID REFERENCES tenants(id) ON DELETE CASCADE;
UPDATE templates SET tenant_id = '00000000-0000-0000-0000-000000000000';
ALTER TABLE templates ALTER COLUMN tenant_id SET NOT NULL;
ALTER TABLE templates DROP CONSTRAINT IF EXISTS templates_name_language_key;
ALTER TABLE templates ADD CONSTRAINT templates_tenant_name_lang_key UNIQUE (tenant_id, name, language);

-- Scoping campaigns
ALTER TABLE campaigns ADD COLUMN tenant_id UUID REFERENCES tenants(id) ON DELETE CASCADE;
UPDATE campaigns SET tenant_id = '00000000-0000-0000-0000-000000000000';
ALTER TABLE campaigns ALTER COLUMN tenant_id SET NOT NULL;

-- Scoping automation_rules
ALTER TABLE automation_rules ADD COLUMN tenant_id UUID REFERENCES tenants(id) ON DELETE CASCADE;
UPDATE automation_rules SET tenant_id = '00000000-0000-0000-0000-000000000000';
ALTER TABLE automation_rules ALTER COLUMN tenant_id SET NOT NULL;

-- Scoping webhook_log
ALTER TABLE webhook_log ADD COLUMN tenant_id UUID REFERENCES tenants(id) ON DELETE CASCADE;
UPDATE webhook_log SET tenant_id = '00000000-0000-0000-0000-000000000000';
ALTER TABLE webhook_log ALTER COLUMN tenant_id SET NOT NULL;

-- Scoping audit_log
ALTER TABLE audit_log ADD COLUMN tenant_id UUID REFERENCES tenants(id) ON DELETE CASCADE;
UPDATE audit_log SET tenant_id = '00000000-0000-0000-0000-000000000000';
ALTER TABLE audit_log ALTER COLUMN tenant_id SET NOT NULL;

-- Scoping app_config
ALTER TABLE app_config ADD COLUMN tenant_id UUID REFERENCES tenants(id) ON DELETE CASCADE;
UPDATE app_config SET tenant_id = '00000000-0000-0000-0000-000000000000';
ALTER TABLE app_config ALTER COLUMN tenant_id SET NOT NULL;
ALTER TABLE app_config DROP CONSTRAINT IF EXISTS app_config_pkey;
ALTER TABLE app_config ADD PRIMARY KEY (tenant_id, key);
