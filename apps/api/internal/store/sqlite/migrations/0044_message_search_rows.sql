-- The search triggers used to remove a message's row by message_id, which is
-- an unindexed FTS5 column, so each deleted message scanned the whole search
-- index. Deleting a channel or workspace with many messages took minutes.
-- Record each message's search rowid so the triggers update it by rowid.
CREATE TABLE message_search_rows (
  message_id TEXT PRIMARY KEY,
  search_rowid INTEGER NOT NULL
);

INSERT OR IGNORE INTO message_search_rows (message_id, search_rowid)
SELECT message_id, rowid
FROM messages_fts;

DROP TRIGGER messages_fts_ai;
DROP TRIGGER messages_fts_ad;
DROP TRIGGER messages_fts_au;

CREATE TRIGGER messages_fts_ai AFTER INSERT ON messages
WHEN new.kind = 'message' BEGIN
  INSERT INTO messages_fts(message_id, workspace_id, body)
  VALUES (new.id, new.workspace_id, new.body);
  INSERT INTO message_search_rows(message_id, search_rowid)
  VALUES (new.id, last_insert_rowid());
END;

CREATE TRIGGER messages_fts_ad AFTER DELETE ON messages BEGIN
  DELETE FROM messages_fts
  WHERE rowid = (SELECT search_rowid FROM message_search_rows WHERE message_id = old.id);
  DELETE FROM message_search_rows WHERE message_id = old.id;
END;

CREATE TRIGGER messages_fts_au AFTER UPDATE OF body, kind ON messages BEGIN
  DELETE FROM messages_fts
  WHERE rowid = (SELECT search_rowid FROM message_search_rows WHERE message_id = old.id);
  DELETE FROM message_search_rows WHERE message_id = old.id;
  INSERT INTO messages_fts(message_id, workspace_id, body)
  SELECT new.id, new.workspace_id, new.body
  WHERE new.kind = 'message';
  INSERT INTO message_search_rows(message_id, search_rowid)
  SELECT new.id, last_insert_rowid()
  WHERE new.kind = 'message';
END;
