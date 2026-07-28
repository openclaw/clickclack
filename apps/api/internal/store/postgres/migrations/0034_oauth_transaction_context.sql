ALTER TABLE oauth_transactions
  ADD COLUMN purpose TEXT NOT NULL DEFAULT 'login',
  ADD COLUMN context_json TEXT NOT NULL DEFAULT '';
