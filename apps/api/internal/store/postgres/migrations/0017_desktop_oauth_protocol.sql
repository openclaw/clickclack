ALTER TABLE oauth_transactions
ADD COLUMN desktop_protocol BIGINT NOT NULL DEFAULT 0;
