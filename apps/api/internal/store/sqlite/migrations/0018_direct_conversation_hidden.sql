CREATE TABLE IF NOT EXISTS direct_conversation_hidden (
  conversation_id TEXT NOT NULL REFERENCES direct_conversations(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  hidden_at TEXT NOT NULL,
  PRIMARY KEY (conversation_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_direct_conversation_hidden_user
  ON direct_conversation_hidden(user_id, conversation_id);
