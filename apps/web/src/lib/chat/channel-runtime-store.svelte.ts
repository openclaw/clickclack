// Reactive store for channel-level agent runtime state (model + thinking +
// context-window pressure) that the composer model picker and context meter
// render. This is clickclack's own data plane: it reads from the native
// /api/channels/{id}/runtime endpoint (fed by the agent-bridge reading the
// gateway directly) with no dependency on ClawCanvas.
//
// Unlike clickglass, clickclack already owns its live-activity / preamble feed
// elsewhere, so this store is intentionally narrow: just the runtime snapshot
// and the per-session override controls.

import type { ChannelRuntime, ThinkingMode } from "./channel-runtime";

// Shape returned by GET /api/channels/{id}/runtime (Go ChannelRuntime). The
// backend splits the bridge-owned effective snapshot (default_model/model) from
// the picker-owned desired override (override_model/override_thinking) so a
// bridge write never clobbers a pending override. The frontend resolver treats
// `model`/`thinking` as the *session override* (drift from default), so we map
// the backend override_* fields onto those.
type RuntimeApiRecord = {
  channel_id?: string;
  default_model?: string;
  default_thinking?: string;
  model?: string;
  thinking?: string;
  override_model?: string;
  override_thinking?: string;
  context_used?: number;
  context_limit?: number;
  cache_hit_pct?: number | null;
  context_breakdown?: unknown;
  updated_at?: string;
};

class ChannelRuntimeStore {
  runtime = $state<ChannelRuntime>({});

  set(rt: ChannelRuntime): void {
    this.runtime = { ...this.runtime, ...rt };
  }

  reset(rt: ChannelRuntime): void {
    this.runtime = { ...rt };
  }

  setModel(model: string | undefined): void {
    this.runtime = { ...this.runtime, model };
  }

  setProvider(provider: string | undefined): void {
    this.runtime = { ...this.runtime, provider };
  }

  setThinking(thinking: ThinkingMode | undefined): void {
    this.runtime = { ...this.runtime, thinking };
  }

  setFast(fast: boolean): void {
    this.runtime = { ...this.runtime, fast };
  }

  setRuntime(runtime: string | undefined): void {
    this.runtime = { ...this.runtime, runtime_override: runtime };
  }

  clearOverrides(): void {
    const { model, provider, thinking, fast, runtime_override, ...rest } = this.runtime;
    this.runtime = { ...rest };
  }
}

export const channelRuntime = new ChannelRuntimeStore();

/** Map the backend runtime record onto the frontend ChannelRuntime shape. */
function mapApiRecord(rec: RuntimeApiRecord): ChannelRuntime {
  let breakdown: ChannelRuntime["context_breakdown"];
  const raw = rec.context_breakdown;
  if (Array.isArray(raw)) {
    breakdown = raw as ChannelRuntime["context_breakdown"];
  } else if (typeof raw === "string" && raw.trim()) {
    try {
      const parsed = JSON.parse(raw);
      if (Array.isArray(parsed)) breakdown = parsed;
    } catch {
      breakdown = undefined;
    }
  }
  return {
    default_model: rec.default_model || undefined,
    default_thinking: (rec.default_thinking as ThinkingMode) || undefined,
    // Backend override_* is the picker's desired next-turn override; the
    // resolver reads `model`/`thinking` as the session override (drift).
    model: rec.override_model || undefined,
    thinking: (rec.override_thinking as ThinkingMode) || undefined,
    context_used: typeof rec.context_used === "number" ? rec.context_used : undefined,
    context_limit: typeof rec.context_limit === "number" ? rec.context_limit : undefined,
    context_fresh: true,
    cache_hit_pct: rec.cache_hit_pct ?? undefined,
    context_breakdown: breakdown,
    observed_at: rec.updated_at || undefined,
  };
}

/**
 * Load the runtime snapshot for a channel from clickclack's own endpoint and
 * merge it into the store. A row that carries no model/context (the bridge
 * hasn't stamped one yet) leaves the demo seed in place so the chrome stays
 * populated. Returns true when a real (non-empty) record was applied.
 */
export async function loadChannelRuntime(channelID: string): Promise<boolean> {
  const id = String(channelID || "").trim();
  if (!id) return false;
  try {
    const res = await fetch(`/api/channels/${encodeURIComponent(id)}/runtime`, {
      headers: { Accept: "application/json" },
    });
    if (!res.ok) return false;
    const data = (await res.json()) as { runtime?: RuntimeApiRecord };
    const rec = data.runtime;
    if (!rec) return false;
    const hasReal =
      Boolean(rec.default_model || rec.model) ||
      (typeof rec.context_limit === "number" && rec.context_limit > 0);
    if (!hasReal) return false;
    channelRuntime.set(mapApiRecord(rec));
    return true;
  } catch {
    return false;
  }
}

/**
 * Persist the picker's desired override to clickclack's runtime endpoint so the
 * bridge can apply it to the next turn. Display state is updated optimistically
 * by the store setters; this is the durable write. Empty strings clear an
 * override (revert to the agent default).
 */
export async function persistRuntimeOverride(
  channelID: string,
  override: { model?: string; thinking?: string },
): Promise<boolean> {
  const id = String(channelID || "").trim();
  if (!id) return false;
  try {
    const res = await fetch(`/api/channels/${encodeURIComponent(id)}/runtime`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json", Accept: "application/json" },
      body: JSON.stringify({ model: override.model ?? "", thinking: override.thinking ?? "" }),
    });
    return res.ok;
  } catch {
    return false;
  }
}

// ---------- demo seeding ----------
//
// clickclack's bridge stamp (increment 2) isn't wired yet, so absent a stamped
// row the runtime would be empty and the picker/meter would render "default" /
// "?". Seed a representative snapshot so the chrome is live and demoable. When
// the bridge lands, loadChannelRuntime() overwrites this with real data; the
// components and field shapes stay identical.

export function seedDemoChannelRuntime(): void {
  channelRuntime.reset({
    default_provider: "anthropic",
    default_model: "anthropic/claude-opus-4-8",
    default_thinking: "adaptive",
    runtime_label: "Openclaw",
    runtime_source: "seed",
    context_used: 131_000,
    context_limit: 1_000_000,
    context_fresh: true,
    cache_hit_pct: 0.78,
    // No seeded context_breakdown: per-slot composition is only shown when the
    // runtime actually stamps one (increment 2 bridge). Until then the
    // inspector renders its honest "not reported yet" fallback rather than
    // fabricated slots.
    preamble_enabled: true,
    observed_at: new Date().toISOString(),
  });
}
