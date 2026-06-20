-- Per-agent monthly send/spend limits.
CREATE TABLE IF NOT EXISTS agent_limits (
    agent_id                UUID        PRIMARY KEY REFERENCES agents(id) ON DELETE CASCADE,
    monthly_msg_cap         INT         NOT NULL DEFAULT 0,
    monthly_spend_cap_paise BIGINT      NOT NULL DEFAULT 0,
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
