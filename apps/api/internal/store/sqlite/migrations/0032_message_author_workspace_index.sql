CREATE INDEX IF NOT EXISTS idx_messages_author_workspace
  ON messages(author_id, workspace_id);
