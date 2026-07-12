CREATE TABLE IF NOT EXISTS pending_upload_cleanups (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  storage_path TEXT NOT NULL UNIQUE,
  attempts INTEGER NOT NULL DEFAULT 0,
  last_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_pending_upload_cleanups_updated
  ON pending_upload_cleanups(updated_at, id);
