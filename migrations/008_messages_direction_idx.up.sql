CREATE INDEX IF NOT EXISTS messages_direction_created_at_idx
    ON messages (direction, created_at);
