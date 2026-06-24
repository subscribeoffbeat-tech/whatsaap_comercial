ALTER TABLE campaigns
    DROP COLUMN IF EXISTS header_media_path,
    DROP COLUMN IF EXISTS header_media_id,
    DROP COLUMN IF EXISTS header_media_type;
