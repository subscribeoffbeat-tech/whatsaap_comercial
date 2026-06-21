ALTER TABLE agents
    ADD COLUMN IF NOT EXISTS password_reset_token TEXT,
    ADD COLUMN IF NOT EXISTS password_reset_expires_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS agents_password_reset_token_idx
    ON agents (password_reset_token)
    WHERE password_reset_token IS NOT NULL;
