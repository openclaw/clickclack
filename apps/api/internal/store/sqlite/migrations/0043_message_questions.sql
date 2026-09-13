-- Structured questions that bots attach to their messages. The question itself
-- is immutable; the first valid answer and the bot's recorded outcome live on
-- the same row so each transition is one conditional update.
CREATE TABLE IF NOT EXISTS message_questions (
  message_id TEXT PRIMARY KEY REFERENCES messages(id) ON DELETE CASCADE,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  bot_user_id TEXT NOT NULL REFERENCES users(id),
  external_id TEXT NOT NULL DEFAULT '',
  spec_json TEXT NOT NULL,
  responder_user_ids TEXT NOT NULL DEFAULT '[]',
  allow_skip INTEGER NOT NULL DEFAULT 1 CHECK (allow_skip IN (0, 1)),
  expires_at TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'open'
    CHECK (status IN ('open', 'submitted', 'answered', 'cancelled', 'expired', 'failed')),
  response_json TEXT NOT NULL DEFAULT '',
  response_source TEXT NOT NULL DEFAULT '',
  responded_by TEXT REFERENCES users(id) ON DELETE SET NULL,
  responded_at TEXT,
  response_nonce TEXT NOT NULL DEFAULT '',
  note TEXT NOT NULL DEFAULT '',
  resolved_at TEXT,
  version INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_message_questions_bot_status
  ON message_questions(workspace_id, bot_user_id, status, message_id);
