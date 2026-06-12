-- Add auth/invite columns to agents
ALTER TABLE agents
    ADD COLUMN IF NOT EXISTS active           BOOLEAN     NOT NULL DEFAULT TRUE,
    ADD COLUMN IF NOT EXISTS invite_token     TEXT        UNIQUE,
    ADD COLUMN IF NOT EXISTS invite_expires_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS last_active_at   TIMESTAMPTZ;

-- Onboarding and WhatsApp connection config
INSERT INTO app_config (key, value, description) VALUES
    ('onboarding_complete',          'false',           'Set to true after first-run wizard is completed'),
    ('wa_display_name',              'null',            'WhatsApp Business display name'),
    ('business_verification_status', '"not_verified"',  'Meta business verification status: not_verified|pending|verified')
ON CONFLICT (key) DO NOTHING;
