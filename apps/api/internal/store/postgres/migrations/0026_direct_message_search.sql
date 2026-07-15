CREATE INDEX idx_messages_direct_search_fts ON messages
  USING GIN (to_tsvector('simple', body))
  WHERE direct_conversation_id IS NOT NULL AND deleted_at IS NULL AND kind = 'message';

CREATE INDEX idx_messages_direct_search_scope ON messages(direct_conversation_id)
  WHERE direct_conversation_id IS NOT NULL AND deleted_at IS NULL AND kind = 'message';
