-- Per-channel agent runtime snapshot: the facts the composer model picker and
-- context meter render. Written by the agent-bridge (bot token, agent_activity:write)
-- reading the gateway directly; read by the web client. Independent of ClawCanvas.
CREATE TABLE IF NOT EXISTS channel_runtime (
  channel_id        TEXT PRIMARY KEY,
  default_model     TEXT NOT NULL DEFAULT '',
  default_thinking  TEXT NOT NULL DEFAULT '',
  model             TEXT NOT NULL DEFAULT '',
  thinking          TEXT NOT NULL DEFAULT '',
  override_model    TEXT NOT NULL DEFAULT '',
  override_thinking TEXT NOT NULL DEFAULT '',
  context_used      INTEGER NOT NULL DEFAULT 0,
  context_limit     INTEGER NOT NULL DEFAULT 0,
  cache_hit_pct     REAL,
  context_breakdown TEXT,
  updated_at        TEXT NOT NULL DEFAULT ''
);
