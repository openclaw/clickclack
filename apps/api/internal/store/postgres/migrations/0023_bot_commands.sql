CREATE TABLE bot_commands (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  bot_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  command TEXT NOT NULL,
  description TEXT NOT NULL,
  args_hint TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(workspace_id, bot_user_id, command)
);

CREATE INDEX idx_bot_commands_workspace ON bot_commands(workspace_id);
