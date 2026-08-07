ALTER TABLE messages ADD COLUMN intent TEXT NOT NULL DEFAULT '';
ALTER TABLE messages ADD COLUMN persona TEXT NOT NULL DEFAULT '';
ALTER TABLE messages ADD COLUMN confidence REAL;
ALTER TABLE messages ADD COLUMN context_json TEXT;
ALTER TABLE messages ADD COLUMN metadata_json TEXT;
ALTER TABLE messages ADD COLUMN transform_history_json TEXT;
