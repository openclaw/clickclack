-- Durable agent activity messages.
--
-- Adds a message kind discriminator and an optional turn correlation id so a
-- bot bridge can persist agent commentary/tool rows in the normal message
-- stream. Activity rows ('agent_commentary', 'agent_tool') flow through the
-- ordinary message.created fan-out and inherit channel_seq for scrollback, but
-- they are excluded from full-text search and from unread/notification paths.
-- 'message' is the existing, default behaviour.

ALTER TABLE messages ADD COLUMN kind TEXT NOT NULL DEFAULT 'message';
ALTER TABLE messages ADD COLUMN turn_id TEXT;

CREATE INDEX IF NOT EXISTS idx_messages_turn ON messages(turn_id);

-- Recreate the FTS insert/update triggers so only kind='message' rows are
-- indexed. Activity rows are never user-authored prose and must not surface in
-- search. The delete trigger is unchanged (deleting a never-indexed row is a
-- no-op), but we keep all three definitions co-located for clarity.
DROP TRIGGER IF EXISTS messages_fts_ai;
DROP TRIGGER IF EXISTS messages_fts_au;

CREATE TRIGGER messages_fts_ai AFTER INSERT ON messages
WHEN new.kind = 'message' BEGIN
  INSERT INTO messages_fts(message_id, workspace_id, body) VALUES (new.id, new.workspace_id, new.body);
END;

CREATE TRIGGER messages_fts_au AFTER UPDATE OF body ON messages
WHEN new.kind = 'message' BEGIN
  DELETE FROM messages_fts WHERE message_id = old.id;
  INSERT INTO messages_fts(message_id, workspace_id, body) VALUES (new.id, new.workspace_id, new.body);
END;
