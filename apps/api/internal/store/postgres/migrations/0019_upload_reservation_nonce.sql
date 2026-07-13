ALTER TABLE upload_quota_reservations ADD COLUMN client_nonce TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_upload_quota_reservations_owner_client_nonce
  ON upload_quota_reservations(owner_id, client_nonce)
  WHERE client_nonce <> '';
