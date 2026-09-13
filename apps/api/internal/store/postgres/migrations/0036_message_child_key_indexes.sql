-- Deleting a channel or workspace cascades through messages. For every deleted
-- message, PostgreSQL looks up the replies and quotes that reference it;
-- without an index on each child key, every lookup scans the messages table.
CREATE INDEX IF NOT EXISTS idx_messages_parent_message
  ON messages(parent_message_id)
  WHERE parent_message_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_messages_quoted_message
  ON messages(quoted_message_id)
  WHERE quoted_message_id IS NOT NULL;
