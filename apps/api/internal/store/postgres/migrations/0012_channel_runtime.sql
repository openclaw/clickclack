-- Per-channel agent runtime snapshot (postgres). Mirror of the sqlite
-- 0018_channel_runtime migration. Written by the agent-bridge from the gateway;
-- read by the web client. Independent of ClawCanvas.
CREATE TABLE IF NOT EXISTS channel_runtime (
  channel_id        TEXT PRIMARY KEY,
  default_model     TEXT NOT NULL DEFAULT '',
  default_thinking  TEXT NOT NULL DEFAULT '',
  model             TEXT NOT NULL DEFAULT '',
  thinking          TEXT NOT NULL DEFAULT '',
  override_model    TEXT NOT NULL DEFAULT '',
  override_thinking TEXT NOT NULL DEFAULT '',
  context_used      BIGINT NOT NULL DEFAULT 0,
  context_limit     BIGINT NOT NULL DEFAULT 0,
  cache_hit_pct     DOUBLE PRECISION,
  context_breakdown TEXT,
  updated_at        TEXT NOT NULL DEFAULT ''
);
