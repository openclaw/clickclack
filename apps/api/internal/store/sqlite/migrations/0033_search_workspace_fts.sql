DROP TRIGGER IF EXISTS messages_fts_ai;
DROP TRIGGER IF EXISTS messages_fts_ad;
DROP TRIGGER IF EXISTS messages_fts_au;

DROP TABLE messages_fts;

CREATE VIRTUAL TABLE messages_fts USING fts5(
  message_id UNINDEXED,
  workspace_id,
  body,
  tokenize = 'porter unicode61'
);

INSERT INTO messages_fts(message_id, workspace_id, body)
SELECT id, workspace_id, body
FROM messages
WHERE kind = 'message';

CREATE TRIGGER messages_fts_ai AFTER INSERT ON messages
WHEN new.kind = 'message' BEGIN
  INSERT INTO messages_fts(message_id, workspace_id, body)
  VALUES (new.id, new.workspace_id, new.body);
END;

CREATE TRIGGER messages_fts_ad AFTER DELETE ON messages BEGIN
  DELETE FROM messages_fts WHERE message_id = old.id;
END;

CREATE TRIGGER messages_fts_au AFTER UPDATE OF body, kind ON messages BEGIN
  DELETE FROM messages_fts WHERE message_id = old.id;
  INSERT INTO messages_fts(message_id, workspace_id, body)
  SELECT new.id, new.workspace_id, new.body
  WHERE new.kind = 'message';
END;
