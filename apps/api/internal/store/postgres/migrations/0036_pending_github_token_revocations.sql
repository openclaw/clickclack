CREATE TABLE pending_github_token_revocations (
  id TEXT PRIMARY KEY,
  encrypted_token TEXT NOT NULL,
  revoke_after_unix BIGINT NOT NULL,
  created_at_unix BIGINT NOT NULL
);

CREATE INDEX idx_pending_github_token_revocations_revoke_after
  ON pending_github_token_revocations(revoke_after_unix, id);
