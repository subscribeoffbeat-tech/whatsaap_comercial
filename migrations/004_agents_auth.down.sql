ALTER TABLE agents
    DROP COLUMN IF EXISTS active,
    DROP COLUMN IF EXISTS invite_token,
    DROP COLUMN IF EXISTS invite_expires_at,
    DROP COLUMN IF EXISTS last_active_at;

DELETE FROM app_config WHERE key IN (
    'onboarding_complete',
    'wa_display_name',
    'business_verification_status'
);
