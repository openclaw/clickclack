CREATE TABLE bot_setup_requests (
  created_by TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  setup_nonce TEXT NOT NULL,
  bot_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  owner_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
  display_name TEXT NOT NULL,
  handle TEXT NOT NULL,
  avatar_url TEXT NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY (created_by, setup_nonce)
);

CREATE INDEX idx_bot_setup_requests_bot ON bot_setup_requests(bot_user_id);
