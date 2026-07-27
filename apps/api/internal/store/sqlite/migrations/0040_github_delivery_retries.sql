CREATE TABLE github_deliveries_retry (
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  delivery_id TEXT NOT NULL,
  event_type TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('processing', 'failed', 'complete')),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  completed_at TEXT,
  failed_at TEXT,
  PRIMARY KEY (project_id, delivery_id)
);

INSERT INTO github_deliveries_retry (
  project_id, delivery_id, event_type, status, created_at, updated_at, completed_at
)
SELECT
  project_id, delivery_id, event_type, status, created_at,
  COALESCE(completed_at, created_at), completed_at
FROM github_deliveries;

DROP TABLE github_deliveries;
ALTER TABLE github_deliveries_retry RENAME TO github_deliveries;
