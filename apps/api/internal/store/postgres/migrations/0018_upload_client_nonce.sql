ALTER TABLE uploads ADD COLUMN client_nonce TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_uploads_owner_client_nonce
  ON uploads(owner_id, client_nonce)
  WHERE client_nonce <> '';
