-- Header media for campaigns whose template has an IMAGE/VIDEO/DOCUMENT header.
-- The owner uploads the real media when building the campaign; we store it on
-- disk (header_media_path) and upload it once to Meta for a reusable media ID
-- (header_media_id). header_media_type is image|video|document.
ALTER TABLE campaigns
    ADD COLUMN IF NOT EXISTS header_media_path TEXT,
    ADD COLUMN IF NOT EXISTS header_media_id   TEXT,
    ADD COLUMN IF NOT EXISTS header_media_type TEXT;
