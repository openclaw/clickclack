CREATE TABLE oauth_transactions (
  id TEXT PRIMARY KEY,
  state_hash TEXT NOT NULL UNIQUE,
  browser_binding_hash TEXT NOT NULL,
  mode TEXT NOT NULL CHECK (mode IN ('browser', 'desktop')),
  pkce_verifier TEXT NOT NULL,
  desktop_challenge TEXT NOT NULL DEFAULT '',
  created_at_unix BIGINT NOT NULL,
  expires_at_unix BIGINT NOT NULL
);

CREATE INDEX idx_oauth_transactions_binding_expiry
  ON oauth_transactions(browser_binding_hash, expires_at_unix);

CREATE INDEX idx_oauth_transactions_expiry
  ON oauth_transactions(expires_at_unix);

CREATE TABLE desktop_oauth_grants (
  id TEXT PRIMARY KEY,
  grant_hash TEXT NOT NULL UNIQUE,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  desktop_challenge TEXT NOT NULL,
  created_at_unix BIGINT NOT NULL,
  expires_at_unix BIGINT NOT NULL
);

CREATE INDEX idx_desktop_oauth_grants_expiry
  ON desktop_oauth_grants(expires_at_unix);
