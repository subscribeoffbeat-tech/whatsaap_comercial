-- Drop all tables in reverse dependency order

DROP TABLE IF EXISTS app_config;
DROP TABLE IF EXISTS audit_log;
DROP TABLE IF EXISTS webhook_message_ids;
DROP TABLE IF EXISTS webhook_log;
DROP TABLE IF EXISTS tag_routing;
DROP TABLE IF EXISTS click_tracking;
DROP TABLE IF EXISTS automation_rules;

ALTER TABLE messages DROP CONSTRAINT IF EXISTS messages_campaign_fk;
ALTER TABLE messages DROP CONSTRAINT IF EXISTS messages_template_fk;

DROP TABLE IF EXISTS campaign_recipients;
DROP TABLE IF EXISTS campaigns;
DROP TABLE IF EXISTS templates;
DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS conversations;
DROP TABLE IF EXISTS contact_notes;
DROP TABLE IF EXISTS contact_tags;
DROP TABLE IF EXISTS contacts;
DROP TABLE IF EXISTS tags;
DROP TABLE IF EXISTS agents;

DROP EXTENSION IF EXISTS "pgcrypto";
