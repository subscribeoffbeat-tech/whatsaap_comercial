-- Phase 1 initial schema for WhatsApp Tool

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- ─── Agents (system users: admin / manager / agent) ────────────────────────
CREATE TABLE agents (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name          TEXT        NOT NULL,
    email         TEXT        NOT NULL UNIQUE,
    password_hash TEXT        NOT NULL,
    role          TEXT        NOT NULL CHECK (role IN ('admin', 'manager', 'agent')),
    available     BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ─── Tags ──────────────────────────────────────────────────────────────────
CREATE TABLE tags (
    id         BIGSERIAL   PRIMARY KEY,
    name       TEXT        NOT NULL UNIQUE,
    color      TEXT        NOT NULL DEFAULT '#6B7280',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ─── Contacts ──────────────────────────────────────────────────────────────
CREATE TABLE contacts (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    -- E.164 format, e.g. +919876543210; +1 numbers are excluded from marketing
    wa_phone       TEXT        NOT NULL UNIQUE,
    name           TEXT        NOT NULL DEFAULT '',
    email          TEXT,
    custom_fields  JSONB       NOT NULL DEFAULT '{}',
    opted_in       BOOLEAN     NOT NULL DEFAULT FALSE,
    opt_in_source  TEXT,        -- 'csv_import' | 'api' | 'manual' | 'inbound_message'
    opt_in_at      TIMESTAMPTZ,
    opt_out_at     TIMESTAMPTZ,
    is_blocked     BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX contacts_opted_in_idx ON contacts (opted_in) WHERE opted_in = TRUE;
CREATE INDEX contacts_wa_phone_idx ON contacts (wa_phone);

-- ─── Contact ↔ Tag (M:M) ───────────────────────────────────────────────────
CREATE TABLE contact_tags (
    contact_id UUID        NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
    tag_id     BIGINT      NOT NULL REFERENCES tags(id)     ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (contact_id, tag_id)
);

-- ─── Contact Notes ─────────────────────────────────────────────────────────
CREATE TABLE contact_notes (
    id         BIGSERIAL   PRIMARY KEY,
    contact_id UUID        NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
    agent_id   UUID        REFERENCES agents(id) ON DELETE SET NULL,
    body       TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ─── Conversations (one per contact) ───────────────────────────────────────
CREATE TABLE conversations (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    contact_id      UUID        NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
    assigned_to     UUID        REFERENCES agents(id) ON DELETE SET NULL,
    status          TEXT        NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'closed', 'pending')),
    -- Tracks the 24-hour free-form reply window: reset on every inbound message
    last_inbound_at TIMESTAMPTZ,
    last_message_at TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (contact_id)  -- one conversation per contact; reopen instead of creating new
);

CREATE INDEX conversations_assigned_to_idx ON conversations (assigned_to) WHERE assigned_to IS NOT NULL;
CREATE INDEX conversations_status_idx      ON conversations (status);

-- ─── Messages ──────────────────────────────────────────────────────────────
CREATE TABLE messages (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id UUID        NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    direction       TEXT        NOT NULL CHECK (direction IN ('inbound', 'outbound')),
    -- text | image | video | audio | document | template | sticker | location
    message_type    TEXT        NOT NULL DEFAULT 'text',
    -- Structured content: {body: "..."} for text, {url: "...", caption: "..."} for media, etc.
    content         JSONB       NOT NULL DEFAULT '{}',
    -- Meta's wamid; NULL until confirmed by send API / arrives inbound
    wa_message_id   TEXT        UNIQUE,
    status          TEXT        NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending', 'sent', 'delivered', 'read', 'failed')),
    -- Cost tracking (stamped on every outbound)
    category        TEXT        CHECK (category IN ('marketing', 'utility', 'authentication', 'service')),
    cost_inr        NUMERIC(10, 4),
    -- Error info from Meta
    error_code      TEXT,
    error_message   TEXT,
    -- Foreign keys to campaigns/templates added after those tables are created
    campaign_id     UUID,
    template_id     UUID,
    sent_by         UUID        REFERENCES agents(id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX messages_conversation_id_idx ON messages (conversation_id);
CREATE INDEX messages_created_at_idx      ON messages (created_at);
CREATE INDEX messages_wa_message_id_idx   ON messages (wa_message_id) WHERE wa_message_id IS NOT NULL;

-- ─── WhatsApp Templates ────────────────────────────────────────────────────
CREATE TABLE templates (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name             TEXT        NOT NULL,
    language         TEXT        NOT NULL DEFAULT 'en',
    category         TEXT        NOT NULL CHECK (category IN ('marketing', 'utility', 'authentication')),
    status           TEXT        NOT NULL DEFAULT 'pending'
                     CHECK (status IN ('pending', 'approved', 'rejected', 'paused')),
    -- Meta components array: [{type: "HEADER", ...}, {type: "BODY", text: "..."}, ...]
    components       JSONB       NOT NULL DEFAULT '[]',
    wa_template_id   TEXT,        -- Meta's internal ID, populated after submission
    rejection_reason TEXT,
    submitted_at     TIMESTAMPTZ,
    approved_at      TIMESTAMPTZ,
    created_by       UUID        REFERENCES agents(id) ON DELETE SET NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (name, language)
);

-- ─── Campaigns ─────────────────────────────────────────────────────────────
CREATE TABLE campaigns (
    id                 UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name               TEXT        NOT NULL,
    template_id        UUID        NOT NULL REFERENCES templates(id),
    -- Maps template variable name → contact field key, e.g. {"1": "name", "2": "custom_fields.order_id"}
    template_variables JSONB       NOT NULL DEFAULT '{}',
    -- Arrays of tag IDs
    segment_tags       JSONB       NOT NULL DEFAULT '[]',
    exclude_tags       JSONB       NOT NULL DEFAULT '[]',
    status             TEXT        NOT NULL DEFAULT 'draft'
                       CHECK (status IN ('draft', 'scheduled', 'running', 'completed', 'paused', 'cancelled')),
    scheduled_at       TIMESTAMPTZ,
    started_at         TIMESTAMPTZ,
    completed_at       TIMESTAMPTZ,
    -- Denormalised stats for fast dashboard reads
    total_recipients   INT         NOT NULL DEFAULT 0,
    sent_count         INT         NOT NULL DEFAULT 0,
    delivered_count    INT         NOT NULL DEFAULT 0,
    read_count         INT         NOT NULL DEFAULT 0,
    failed_count       INT         NOT NULL DEFAULT 0,
    skipped_count      INT         NOT NULL DEFAULT 0,
    cost_total_inr     NUMERIC(12, 4) NOT NULL DEFAULT 0,
    created_by         UUID        REFERENCES agents(id) ON DELETE SET NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ─── Campaign Recipients ────────────────────────────────────────────────────
CREATE TABLE campaign_recipients (
    id          BIGSERIAL   PRIMARY KEY,
    campaign_id UUID        NOT NULL REFERENCES campaigns(id)  ON DELETE CASCADE,
    contact_id  UUID        NOT NULL REFERENCES contacts(id)   ON DELETE CASCADE,
    message_id  UUID        REFERENCES messages(id)            ON DELETE SET NULL,
    status      TEXT        NOT NULL DEFAULT 'pending'
                CHECK (status IN ('pending', 'sent', 'delivered', 'read', 'failed', 'skipped')),
    -- 'opted_out' | 'freq_cap' | 'us_number' | 'invalid_number' | 'daily_cap'
    skip_reason TEXT,
    sent_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (campaign_id, contact_id)
);

CREATE INDEX campaign_recipients_campaign_id_idx ON campaign_recipients (campaign_id);
CREATE INDEX campaign_recipients_status_idx      ON campaign_recipients (campaign_id, status);

-- ─── Deferred FK constraints on messages ───────────────────────────────────
ALTER TABLE messages
    ADD CONSTRAINT messages_campaign_fk
    FOREIGN KEY (campaign_id) REFERENCES campaigns(id) ON DELETE SET NULL;

ALTER TABLE messages
    ADD CONSTRAINT messages_template_fk
    FOREIGN KEY (template_id) REFERENCES templates(id) ON DELETE SET NULL;

-- ─── Automation Rules ──────────────────────────────────────────────────────
CREATE TABLE automation_rules (
    id            BIGSERIAL PRIMARY KEY,
    name          TEXT      NOT NULL,
    trigger_type  TEXT      NOT NULL CHECK (trigger_type IN ('keyword', 'welcome', 'away', 'stop')),
    keyword       TEXT,      -- only for trigger_type = 'keyword'
    keyword_match TEXT      NOT NULL DEFAULT 'exact' CHECK (keyword_match IN ('exact', 'contains')),
    template_id   UUID      REFERENCES templates(id) ON DELETE SET NULL,
    response_text TEXT,      -- plain-text reply; mutually exclusive with template_id
    active        BOOLEAN   NOT NULL DEFAULT TRUE,
    priority      INT       NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ─── Click Tracking (CTA link wrapping) ────────────────────────────────────
CREATE TABLE click_tracking (
    id               BIGSERIAL   PRIMARY KEY,
    short_code       TEXT        NOT NULL UNIQUE,
    original_url     TEXT        NOT NULL,
    campaign_id      UUID        REFERENCES campaigns(id)  ON DELETE CASCADE,
    contact_id       UUID        REFERENCES contacts(id)   ON DELETE CASCADE,
    message_id       UUID        REFERENCES messages(id)   ON DELETE CASCADE,
    click_count      INT         NOT NULL DEFAULT 0,
    first_clicked_at TIMESTAMPTZ,
    last_clicked_at  TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ─── Tag → Agent Routing ────────────────────────────────────────────────────
CREATE TABLE tag_routing (
    tag_id   BIGINT NOT NULL REFERENCES tags(id)   ON DELETE CASCADE,
    agent_id UUID   NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    PRIMARY KEY (tag_id, agent_id)
);

-- ─── Webhook Log (raw HTTP payloads from Meta) ─────────────────────────────
CREATE TABLE webhook_log (
    id               BIGSERIAL   PRIMARY KEY,
    received_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    payload          JSONB       NOT NULL,
    processed        BOOLEAN     NOT NULL DEFAULT FALSE,
    processing_error TEXT
);

-- ─── Webhook Message ID Dedup ──────────────────────────────────────────────
-- One row per wa_message_id seen; prevents double-processing retried webhooks.
CREATE TABLE webhook_message_ids (
    wa_message_id  TEXT      PRIMARY KEY,
    webhook_log_id BIGINT    NOT NULL REFERENCES webhook_log(id),
    seen_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ─── Audit Log ─────────────────────────────────────────────────────────────
CREATE TABLE audit_log (
    id          BIGSERIAL   PRIMARY KEY,
    actor_id    UUID        REFERENCES agents(id) ON DELETE SET NULL,
    action      TEXT        NOT NULL,   -- 'send_campaign' | 'opt_out_contact' | 'update_setting' …
    entity_type TEXT,                   -- 'contact' | 'campaign' | 'template' …
    entity_id   TEXT,
    details     JSONB       NOT NULL DEFAULT '{}',
    ip_address  TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX audit_log_actor_idx    ON audit_log (actor_id);
CREATE INDEX audit_log_created_idx  ON audit_log (created_at);
CREATE INDEX audit_log_entity_idx   ON audit_log (entity_type, entity_id);

-- ─── App Config (key-value store for runtime settings) ─────────────────────
CREATE TABLE app_config (
    key         TEXT        PRIMARY KEY,
    value       JSONB       NOT NULL DEFAULT 'null',
    description TEXT,
    updated_by  UUID        REFERENCES agents(id) ON DELETE SET NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Seed default values (costs come from CLAUDE.md; stored here so they can drift)
INSERT INTO app_config (key, value, description) VALUES
    ('meta_tier',              '1000',        'Current Meta messaging tier (messages/day)'),
    ('daily_message_cap',      '900',         'Stop queuing at 90% of meta_tier'),
    ('rate_limit_per_sec',     '80',          'Token-bucket rate: outbound messages per second'),
    ('marketing_cost_inr',     '0.8631',      'Cost per marketing message (INR, excl. GST)'),
    ('utility_cost_inr',       '0.115',       'Cost per utility message (INR, excl. GST)'),
    ('auth_cost_inr',          '0.115',       'Cost per authentication message (INR, excl. GST)'),
    ('gst_rate',               '0.18',        'GST rate applied to all Meta charges'),
    ('quiet_hours_start_ist',  '"21:00"',     'Quiet hours start time in IST (business-initiated sends blocked)'),
    ('quiet_hours_end_ist',    '"09:00"',     'Quiet hours end time in IST'),
    ('freq_cap_hours',         '24',          'Min hours between marketing messages to the same contact'),
    ('data_retention_months',  '24',          'Months to retain conversation and message data');
