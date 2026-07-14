ALTER TABLE bot_tokens ADD COLUMN setup_nonce TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX idx_bot_tokens_setup_nonce
  ON bot_tokens(created_by, setup_nonce)
  WHERE setup_nonce <> '';

ALTER TABLE app_installations ADD COLUMN setup_nonce TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX idx_app_installations_setup_nonce
  ON app_installations(created_by, setup_nonce)
  WHERE setup_nonce <> '';
