ALTER TABLE templates DROP CONSTRAINT IF EXISTS templates_status_check;
ALTER TABLE templates ADD CONSTRAINT templates_status_check
  CHECK (status IN ('draft', 'pending', 'approved', 'rejected', 'paused'));
ALTER TABLE templates ALTER COLUMN status SET DEFAULT 'draft';
