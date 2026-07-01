-- Durable agent activity messages (postgres mirror of sqlite 0017).
--
-- Adds a message kind discriminator and an optional turn correlation id. See
-- the sqlite migration for the full rationale. Activity rows
-- ('agent_commentary', 'agent_tool') are excluded from the full-text search
-- index and from unread/notification paths; 'message' is the default.

ALTER TABLE messages ADD COLUMN kind TEXT NOT NULL DEFAULT 'message';
ALTER TABLE messages ADD COLUMN turn_id TEXT;

CREATE INDEX IF NOT EXISTS idx_messages_turn ON messages(turn_id);

-- Rebuild the search GIN index so activity rows are never indexed. The search
-- query also filters on kind = 'message', but keeping the index partial avoids
-- wasting space on rows that can never match.
DROP INDEX IF EXISTS idx_messages_search_fts;
CREATE INDEX idx_messages_search_fts ON messages
  USING GIN (to_tsvector('simple', body))
  WHERE direct_conversation_id IS NULL AND channel_id IS NOT NULL AND deleted_at IS NULL AND kind = 'message';
