CREATE TABLE IF NOT EXISTS user_notification_settings (
  user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  pushover_enabled INTEGER NOT NULL DEFAULT 0,
  pushover_user_key TEXT NOT NULL DEFAULT ''
);
