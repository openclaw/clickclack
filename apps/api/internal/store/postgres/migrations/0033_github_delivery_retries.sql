ALTER TABLE github_deliveries
  ADD COLUMN updated_at TEXT,
  ADD COLUMN failed_at TEXT;

UPDATE github_deliveries
SET updated_at = COALESCE(completed_at, created_at);

ALTER TABLE github_deliveries
  ALTER COLUMN updated_at SET NOT NULL,
  DROP CONSTRAINT github_deliveries_status_check,
  ADD CONSTRAINT github_deliveries_status_check
    CHECK (status IN ('processing', 'failed', 'complete'));
