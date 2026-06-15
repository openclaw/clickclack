// Channel-level runtime state for the topbar status bar.
//
// ClawCanvas shows a persistent bar of pills describing how the *current
// channel/session* will run the next turn: provider, model, thinking mode,
// speed (fast mode), runtime label, and live context-window pressure. This is
// distinct from per-message RuntimeChrome (which is a receipt for a turn that
// already happened). clickclack has no native concept of this, so clickglass
// carries it out of band and computes the display state here.
//
// Mirrors the shape of clawcanvas-mvp computeChannelRuntimeState so the two
// surfaces stay conceptually aligned.

// Inlined here (clickclack has no shared ThinkingMode type). Mirrors the union
// the runtime accepts for per-session reasoning overrides.
export type ThinkingMode = "off" | "minimal" | "low" | "medium" | "high" | "xhigh" | "adaptive";

// One composition slot of the context window: a named region (system prompt,
// tool defs, HyperMem recall, conversation, ...) with its current token cost
// and an optional budget ceiling. Populated upstream by HyperMem instrumentation
// (see the ClawCanvas channel `contextBreakdown` field); absent until the
// runtime reports it, in which case the inspector shows an honest "not reported"
// note rather than a fabricated split.
export type ContextSlot = {
  id?: string;
  label: string;
  tokens: number;
  budget?: number | null;
};

export type ChannelRuntime = {
  // Agent defaults (what the channel runs as with no session override).
  default_provider?: string;
  default_model?: string;
  default_thinking?: ThinkingMode;
  // Session overrides (set via the Controls bar). When present, the effective
  // value differs from the agent default and the pill shows an override state.
  provider?: string;
  model?: string;
  thinking?: ThinkingMode;
  // Speed: fast/priority processing.
  fast?: boolean;
  // Free-form runtime label, e.g. "OpenClaw Default", "Codex".
  runtime_label?: string;
  runtime_source?: string;
  // Session runtime-engine override (auto|pi|codex). When set, the operator has
  // pinned the execution runtime; null/absent means "auto" (the platform picks
  // by model). Plumbed to ClawCanvas via setLiveChannelRuntime.
  runtime_override?: string;
  // Responding agent identity for transient UI such as inline preamble bubbles.
  responder_id?: string;
  // Live context-window accounting for pressure chip.
  context_used?: number;
  context_limit?: number;
  context_fresh?: boolean;
  // HyperMem cache + composition diagnostics. cache_hit_pct is the composition
  // cache hit rate (0..1) for the session serving this channel; context_breakdown
  // is the per-slot token split of what's filling the window. Both are optional
  // and only render when the runtime actually reports them (no fabrication).
  cache_hit_pct?: number | null;
  context_breakdown?: ContextSlot[];
  // Whether an agent run is currently active (drives Stop button enable).
  running?: boolean;
  // Wall-clock anchors for the live run timer. run_started_at is stamped on the
  // rising edge of `running` (the moment the operator's message is sent) and
  // run_ended_at on the falling edge (final block delivered). The composer's
  // Stop button shows the elapsed time ticking between them. Epoch ms.
  run_started_at?: number;
  run_ended_at?: number;
  // Preamble / tool-progress streaming toggle for this channel.
  preamble_enabled?: boolean;
  observed_at?: string;
};

export type ContextTier = "normal" | "elevated" | "high" | "unknown";

export type RuntimeField = {
  effective: string;
  agent: string;
  overridden: boolean;
};

export type ResolvedChannelRuntime = {
  provider: RuntimeField;
  model: RuntimeField;
  thinking: RuntimeField;
  fast: boolean;
  // Runtime engine (OpenClaw vs Codex) as a current-vs-default field so the
  // pill can show drift when a session override flips the engine (e.g. an
  // Anthropic default channel overridden onto a Codex/OpenAI model).
  runtime: RuntimeField;
  runtimeLabel: string;
  runtimeSource: string;
  // Explicit runtime-engine override (auto|pi|codex) when the operator pinned
  // one; null means auto (derive from the model). Drives the picker Runtime row.
  runtimeOverride: string | null;
  context: {
    present: boolean;
    used: number | null;
    limit: number | null;
    pct: number | null;
    tier: ContextTier;
    fresh: boolean;
    // Composition cache hit rate (0..1) and per-slot token breakdown, when the
    // runtime reports them. cacheHitPct is null and breakdown is empty until
    // HyperMem instrumentation lands them in the channel payload.
    cacheHitPct: number | null;
    breakdown: ContextSlot[];
  };
  running: boolean;
  preambleEnabled: boolean;
  observedAt: string;
};

function clean(value?: string | null): string | null {
  if (value == null) return null;
  const v = String(value).trim();
  if (!v || v === "None" || v === "null" || v === "undefined") return null;
  return v;
}

/** Drop the provider/ prefix for a compact pill label. */
export function shortModel(model?: string): string {
  const raw = clean(model);
  if (!raw) return "";
  const slash = raw.lastIndexOf("/");
  return slash >= 0 ? raw.slice(slash + 1) : raw;
}

/** Derive a provider label from an explicit field or the model prefix. */
export function shortProvider(provider?: string, model?: string): string {
  const explicit = clean(provider);
  if (explicit) return explicit;
  const raw = clean(model);
  if (raw && raw.includes("/")) return raw.split("/")[0];
  return "";
}

export function inferDefaultThinking(model?: string): ThinkingMode {
  const lower = String(model || "").toLowerCase();
  if (lower.includes("claude-")) return "adaptive";
  if (lower.includes("gpt-5") || lower.includes("codex")) return "off";
  return "off";
}

// The OpenClaw label string the platform uses for its native runtime. The
// upstream ClawCanvas payload spells it this way in runtimeLabel; mirror it so
// a derived default-runtime label sits visually next to the effective one.
export const HYPERION_RUNTIME_LABEL = "Openclaw";

/**
 * Derive the runtime engine label (Codex vs OpenClaw) from a provider/model
 * pair. OpenAI / codex models run on the Codex runtime; everything else runs
 * on OpenClaw's native runtime. `nonCodexLabel` lets the caller reuse the
 * exact effective label string (e.g. when the upstream already resolved it) so
 * the current/default pair reads consistently.
 */
export function runtimeForModel(provider?: string, model?: string, nonCodexLabel?: string): string {
  const p = String(provider || "").toLowerCase();
  const m = String(model || "").toLowerCase();
  const isCodex =
    p.startsWith("openai") || p === "openai-codex" || m.includes("codex") || m.includes("gpt-");
  if (isCodex) return "Codex";
  return clean(nonCodexLabel) || HYPERION_RUNTIME_LABEL;
}

export function resolveChannelRuntime(rt?: ChannelRuntime): ResolvedChannelRuntime {
  const r = rt || {};

  const agentProvider = shortProvider(r.default_provider, r.default_model);
  const agentModel = shortModel(r.default_model);
  const agentThinking =
    (clean(r.default_thinking) as ThinkingMode) || inferDefaultThinking(r.default_model);

  const sessionProviderRaw =
    clean(r.provider) || (clean(r.model)?.includes("/") ? clean(r.model)!.split("/")[0] : null);
  const sessionModelRaw = clean(r.model);
  const sessionThinkingRaw = clean(r.thinking) as ThinkingMode | null;

  const effectiveProvider = shortProvider(r.provider, r.model) || agentProvider;
  const effectiveModel = shortModel(r.model) || agentModel;
  const effectiveThinking = (sessionThinkingRaw as ThinkingMode) || agentThinking;

  const providerOverridden = Boolean(sessionProviderRaw) && effectiveProvider !== agentProvider;
  const modelOverridden = Boolean(sessionModelRaw) && effectiveModel !== agentModel;
  const thinkingOverridden = Boolean(sessionThinkingRaw) && effectiveThinking !== agentThinking;

  // Runtime engine: effective comes from the upstream-resolved runtimeLabel
  // when present (authoritative), else derive it from the effective model.
  // The agent default is derived from the channel's default provider/model.
  // A non-codex effective label is reused as the non-codex base so OpenClaw
  // reads identically on both sides of the current/default pair.
  const effectiveRuntimeLabelRaw = clean(r.runtime_label);
  const effectiveRuntime =
    effectiveRuntimeLabelRaw || runtimeForModel(effectiveProvider, effectiveModel);
  const nonCodexBase = effectiveRuntime !== "Codex" ? effectiveRuntime : HYPERION_RUNTIME_LABEL;
  const agentRuntime = runtimeForModel(agentProvider, agentModel, nonCodexBase);
  const runtimeOverridden =
    (modelOverridden || providerOverridden) && effectiveRuntime !== agentRuntime;
  // Explicit operator runtime pin (auto|pi|codex), independent of the derived
  // engine label above. Null until the operator picks one in the Runtime row.
  const runtimeOverride = clean(r.runtime_override);

  const limit = Number(r.context_limit);
  const used = Number(r.context_used);
  const hasLimit = Number.isFinite(limit) && limit > 0;
  const hasUsed = Number.isFinite(used) && used >= 0;
  const pct = hasLimit && hasUsed ? Math.max(0, Math.min(1, used / limit)) : null;
  const tier: ContextTier =
    pct == null ? "unknown" : pct >= 0.85 ? "high" : pct >= 0.65 ? "elevated" : "normal";

  const cacheRaw = Number(r.cache_hit_pct);
  const cacheHitPct =
    r.cache_hit_pct != null && Number.isFinite(cacheRaw)
      ? Math.max(0, Math.min(1, cacheRaw))
      : null;
  const breakdown: ContextSlot[] = Array.isArray(r.context_breakdown)
    ? r.context_breakdown
        .filter(
          (s): s is ContextSlot =>
            !!s &&
            typeof s.tokens === "number" &&
            Number.isFinite(s.tokens) &&
            s.tokens >= 0 &&
            !!s.label,
        )
        .map((s) => ({
          id: s.id,
          label: String(s.label),
          tokens: Number(s.tokens),
          budget:
            s.budget != null && Number.isFinite(Number(s.budget)) && Number(s.budget) > 0
              ? Number(s.budget)
              : null,
        }))
    : [];

  return {
    provider: {
      effective: effectiveProvider,
      agent: agentProvider,
      overridden: providerOverridden,
    },
    model: { effective: effectiveModel, agent: agentModel, overridden: modelOverridden },
    thinking: {
      effective: effectiveThinking,
      agent: agentThinking,
      overridden: thinkingOverridden,
    },
    fast: Boolean(r.fast),
    runtime: { effective: effectiveRuntime, agent: agentRuntime, overridden: runtimeOverridden },
    runtimeLabel: effectiveRuntimeLabelRaw || effectiveRuntime,
    runtimeSource: clean(r.runtime_source) || "",
    runtimeOverride,
    context: {
      present: hasLimit && hasUsed,
      used: hasUsed ? used : null,
      limit: hasLimit ? limit : null,
      pct,
      tier,
      fresh: r.context_fresh !== false,
      cacheHitPct,
      breakdown,
    },
    running: Boolean(r.running),
    preambleEnabled: Boolean(r.preamble_enabled),
    observedAt: clean(r.observed_at) || "",
  };
}

/** Compact k/M formatting for context token counts. 84913 -> "83k". */
export function formatContextTokens(n: number | null): string {
  if (n == null || !Number.isFinite(n)) return "?";
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1).replace(/\.0$/, "")}M`;
  if (n >= 1000) return `${Math.round(n / 1000)}k`;
  return String(n);
}

export function formatContextPct(pct: number | null): string {
  if (pct == null) return "?";
  const raw = pct * 100;
  return raw >= 10 ? String(Math.round(raw)) : raw.toFixed(1).replace(/\.0$/, "");
}

/** Cache hit rate (0..1) as a whole-percent label. 0.78 -> "78". */
export function formatCacheHitPct(pct: number | null): string {
  if (pct == null || !Number.isFinite(pct)) return "?";
  return String(Math.round(Math.max(0, Math.min(1, pct)) * 100));
}

/**
 * Red→green gradient color for a 0..1 ratio (0 = red, 1 = green) with a
 * continuous sweep between. Used to tint the cache-hit percentage so a healthy,
 * fully-cached turn reads green and a cold cache reads red. Returns an hsl()
 * string (or a muted fallback when the ratio is unknown).
 */
export function ratioGradientColor(pct: number | null): string {
  if (pct == null || !Number.isFinite(pct)) return "var(--muted)";
  const clamped = Math.max(0, Math.min(1, pct));
  const hue = Math.round(clamped * 130); // 0=red → 130=green
  return `hsl(${hue} 70% 52%)`;
}

// Thinking modes the runtime accepts. Mirrors the union of per-channel
// thinkingOptions the live ClawCanvas payload reports (off|minimal|low|medium|
// high|xhigh) plus "adaptive", which Anthropic channels use as their default.
export const THINKING_MODES: ThinkingMode[] = [
  "off",
  "minimal",
  "low",
  "medium",
  "high",
  "xhigh",
  "adaptive",
];

export type RuntimeChoice = { value: string; label: string };

// Runtime-engine options offered in the model picker's Runtime row. "auto" lets
// the platform pick by model; "pi" pins OpenClaw's native runtime; "codex" pins
// the Codex runtime. Values match the ClawCanvas /runtime override contract
// (auto|pi|codex) forwarded by setLiveChannelRuntime.
export const RUNTIME_CHOICES: RuntimeChoice[] = [
  { value: "auto", label: "Auto" },
  { value: "pi", label: HYPERION_RUNTIME_LABEL },
  { value: "codex", label: "Codex" },
];

/** Friendly label for a runtime override value; falls back to the raw value. */
export function runtimeChoiceLabel(value?: string | null): string {
  const v = clean(value) || "auto";
  return RUNTIME_CHOICES.find((c) => c.value === v)?.label ?? v;
}

export type ModelChoice = { value: string; label: string };
export type ProviderChoice = { value: string; label: string };

// Providers offered in the Controls bar, each with its own model set. Model
// lists are provider-scoped: selecting a provider filters the model dropdown
// to that provider's models, because every provider ships a different lineup.
//
// SOURCE OF TRUTH: this mirrors `models.providers` + `auth.profiles` in the
// OpenClaw host config (~/.openclaw/openclaw.json). It is a manually
// synced mirror because neither ClawCanvas nor ClickClack currently exposes a
// model-catalog endpoint to proxy (every /api/clawcanvas/models probe returns
// the SPA shell). Only providers with a working auth path and selectable chat
// models are listed; embedding-only models and unauthed providers are omitted.
// When a live catalog endpoint lands, replace this constant with that feed.
export const PROVIDER_MODELS: Record<string, { label: string; models: ModelChoice[] }> = {
  anthropic: {
    label: "Anthropic",
    models: [
      { value: "anthropic/claude-fable-5", label: "Claude Fable 5" },
      { value: "anthropic/claude-opus-4-8", label: "Claude Opus 4.8" },
      { value: "anthropic/claude-opus-4-7", label: "Claude Opus 4.7" },
      { value: "anthropic/claude-opus-4-6", label: "Claude Opus 4.6" },
      { value: "anthropic/claude-sonnet-4-6", label: "Claude Sonnet 4.6" },
    ],
  },
  openai: {
    label: "OpenAI",
    models: [
      { value: "openai/gpt-5.5", label: "GPT-5.5" },
      { value: "openai/gpt-5.4", label: "GPT-5.4" },
      { value: "openai/gpt-5.4-mini", label: "GPT-5.4 Mini" },
    ],
  },
  pioneer: {
    label: "Pioneer",
    models: [
      { value: "pioneer/gpt-5.5", label: "GPT-5.5" },
      { value: "pioneer/claude-fable-5", label: "Claude Fable 5" },
      { value: "pioneer/claude-opus-4-8", label: "Claude Opus 4.8" },
      { value: "pioneer/claude-opus-4-7", label: "Claude Opus 4.7" },
      { value: "pioneer/claude-opus-4-6", label: "Claude Opus 4.6" },
      { value: "pioneer/claude-sonnet-4-6", label: "Claude Sonnet 4.6" },
      { value: "pioneer/gemini-3.5-flash", label: "Gemini 3.5 Flash" },
      { value: "pioneer/deepseek-ai/DeepSeek-V3.1", label: "DeepSeek V3.1" },
      { value: "pioneer/moonshotai/Kimi-K2.6", label: "Kimi K2.6" },
      { value: "pioneer/Qwen/Qwen3-8B", label: "Qwen3 8B" },
      { value: "pioneer/meta-llama/Llama-3.3-70B-Instruct", label: "Llama 3.3 70B" },
      { value: "pioneer/openai/gpt-oss-120b", label: "GPT-OSS 120B" },
      { value: "pioneer/openai/gpt-oss-20b", label: "GPT-OSS 20B" },
    ],
  },
  "opencode-go": {
    label: "OpenCode Go",
    models: [
      { value: "opencode-go/glm-5.1", label: "GLM 5.1" },
      { value: "opencode-go/glm-5", label: "GLM 5" },
      { value: "opencode-go/kimi-k2.6", label: "Kimi K2.6" },
      { value: "opencode-go/minimax-m2.7", label: "MiniMax M2.7" },
      { value: "opencode-go/minimax-m2.5", label: "MiniMax M2.5" },
      { value: "opencode-go/deepseek-v4-pro", label: "DeepSeek V4 Pro" },
      { value: "opencode-go/deepseek-v4-flash", label: "DeepSeek V4 Flash" },
      { value: "opencode-go/mimo-v2.5-pro", label: "MiMo V2.5 Pro" },
      { value: "opencode-go/qwen3.6-plus", label: "Qwen3.6 Plus" },
    ],
  },
  "github-copilot": {
    label: "GitHub Copilot",
    models: [{ value: "github-copilot/gemini-3.1-pro-preview", label: "Gemini 3.1 Pro (preview)" }],
  },
  ollama: {
    label: "Ollama (local)",
    models: [
      { value: "ollama/kimi-k2.6:cloud", label: "Kimi K2.6 (cloud)" },
      { value: "ollama/minimax-m2.7:cloud", label: "MiniMax M2.7 (cloud)" },
      { value: "ollama/glm-5.1:cloud", label: "GLM 5.1 (cloud)" },
    ],
  },
};

export const PROVIDER_CHOICES: ProviderChoice[] = Object.entries(PROVIDER_MODELS).map(
  ([value, { label }]) => ({ value, label }),
);

/** Models for a given provider; empty list when the provider is unknown. */
export function modelsForProvider(provider?: string): ModelChoice[] {
  const key = String(provider || "").toLowerCase();
  return PROVIDER_MODELS[key]?.models ?? [];
}

/** Provider key that owns a given model value (by the provider/ prefix). */
export function providerForModel(model?: string): string {
  const raw = clean(model);
  if (raw && raw.includes("/")) return raw.split("/")[0].toLowerCase();
  return "";
}

// Flat list retained for any caller that wants every model regardless of provider.
export const MODEL_CHOICES: ModelChoice[] = Object.values(PROVIDER_MODELS).flatMap((p) => p.models);

/** Pretty provider label from the picker catalog ("pioneer" -> "Pioneer"). */
export function prettyProviderLabel(provider?: string): string {
  const key = String(provider || "").toLowerCase();
  return PROVIDER_MODELS[key]?.label || (clean(provider) ?? "");
}

/**
 * Pretty model label for a provider+model pair, matching the picker menu
 * (e.g. ("anthropic","claude-opus-4-8") -> "Claude Opus 4.8"). Resolves the
 * model slug within the given provider first (so Pioneer's Claude rows map to
 * Pioneer labels), then falls back to any provider's matching slug, then to the
 * short model name so an unknown model still reads cleanly.
 */
export function prettyModelLabel(provider?: string, model?: string): string {
  const short = shortModel(model);
  if (!short) return "";
  const key = String(provider || "").toLowerCase();
  const inProvider = PROVIDER_MODELS[key]?.models.find(
    (m) => shortModel(m.value) === short || m.value === model,
  );
  if (inProvider) return inProvider.label;
  const anywhere = MODEL_CHOICES.find((m) => shortModel(m.value) === short);
  return anywhere?.label || short;
}
