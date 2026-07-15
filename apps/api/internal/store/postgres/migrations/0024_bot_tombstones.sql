CREATE TABLE bot_tombstones (
  bot_user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  former_handle TEXT NOT NULL,
  deleted_at TEXT NOT NULL,
  deleted_by TEXT REFERENCES users(id) ON DELETE SET NULL
);
