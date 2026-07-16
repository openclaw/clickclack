CREATE TABLE bot_setup_codes (
  id TEXT PRIMARY KEY,
  code_hash TEXT NOT NULL UNIQUE,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  bot_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_name TEXT NOT NULL,
  scopes_json TEXT NOT NULL,
  created_by TEXT REFERENCES users(id) ON DELETE SET NULL,
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  claimed_at TEXT,
  claimed_token_id TEXT REFERENCES bot_tokens(id) ON DELETE SET NULL
);

CREATE INDEX idx_bot_setup_codes_workspace_bot ON bot_setup_codes(workspace_id, bot_user_id);
