-- Add media_path to messages for locally-downloaded inbound media.
-- Meta media URLs expire after ~5 minutes; we download immediately on receipt.
ALTER TABLE messages ADD COLUMN IF NOT EXISTS media_path TEXT;
