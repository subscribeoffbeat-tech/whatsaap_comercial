-- Saved segments for targeting campaigns
CREATE TABLE saved_segments (
    id          BIGSERIAL   PRIMARY KEY,
    name        TEXT        NOT NULL UNIQUE,
    -- {"tags":[1,2],"tag_op":"all"|"any","opted_in":true,"clicked_campaign_id":"uuid"}
    filter      JSONB       NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
