DROP INDEX IF EXISTS agents_password_reset_token_idx;
ALTER TABLE agents
    DROP COLUMN IF EXISTS password_reset_token,
    DROP COLUMN IF EXISTS password_reset_expires_at;
