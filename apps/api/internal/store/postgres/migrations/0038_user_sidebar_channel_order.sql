CREATE TABLE IF NOT EXISTS user_sidebar_channel_order (
  user_id TEXT NOT NULL,
  workspace_id TEXT NOT NULL,
  channel_ids TEXT NOT NULL DEFAULT '[]',
  updated_at TEXT NOT NULL,
  PRIMARY KEY (user_id, workspace_id),
  FOREIGN KEY (workspace_id, user_id) REFERENCES workspace_members(workspace_id, user_id) ON DELETE CASCADE
);
