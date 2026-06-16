<script lang="ts">
  // Composer-anchored context meter. Lives in the center of the composer's
  // utility bar (between the markdown glyphs and the model picker) and uses the
  // same text-as-button pill language as ComposerModelPicker so the two read as
  // one row. The button shows live context-window pressure (used / limit · pct);
  // clicking it opens a compact "Context Window" inspector upward, anchored with
  // a small downward caret, mirroring the model picker dropdown surface.
  //
  // Self-contained like ComposerModelPicker: reads the channel runtime store so
  // it stays reactive on channel switch. Only renders when context data is
  // present (live/structured channels). All figures are real runtime values —
  // no fabricated cache % or per-category breakdown.
  import {
    formatContextTokens,
    formatContextPct,
    formatCacheHitPct,
    ratioGradientColor,
    resolveChannelRuntime,
  } from "../../lib/chat/channel-runtime";
  import { channelRuntime } from "../../lib/chat/channel-runtime-store.svelte";

  let resolved = $derived(resolveChannelRuntime(channelRuntime.runtime));
  let ctx = $derived(resolved.context);
  let open = $state(false);

  // The popup is positioned `fixed` (computed from the trigger pill's screen
  // rect) so it escapes the composer card's `overflow: hidden` clip instead of
  // being trapped inside the composer window. We center it on the pill and
  // clamp it to the viewport, then point the caret at the real pill center.
  let pillEl: HTMLButtonElement | null = $state(null);
  let popEl: HTMLDivElement | null = $state(null);
  let popLeft = $state(0);
  let popBottom = $state(0);
  let caretX = $state(130);

  function positionPop() {
    if (!pillEl) return;
    const r = pillEl.getBoundingClientRect();
    const margin = 12;
    const popW = popEl?.offsetWidth ?? 260;
    const pillCenter = r.left + r.width / 2;
    let left = pillCenter - popW / 2;
    left = Math.max(margin, Math.min(left, window.innerWidth - popW - margin));
    popLeft = left;
    popBottom = Math.round(window.innerHeight - r.top + 9);
    caretX = Math.max(14, Math.min(pillCenter - left, popW - 14));
  }

  $effect(() => {
    if (!open) return;
    // Measure once now and again after the popup paints (real width), then keep
    // it anchored on scroll/resize while open.
    positionPop();
    requestAnimationFrame(positionPop);
    const onMove = () => positionPop();
    window.addEventListener("scroll", onMove, true);
    window.addEventListener("resize", onMove);
    return () => {
      window.removeEventListener("scroll", onMove, true);
      window.removeEventListener("resize", onMove);
    };
  });

  // Fill tier per the composer spec: green < 50%, amber 50–80%, red ≥ 80%.
  // (Independent of the topbar pill's normal/elevated/high thresholds.)
  let fillTier = $derived.by(() => {
    const p = ctx.pct;
    if (p == null) return "normal";
    if (p >= 0.8) return "high";
    if (p >= 0.5) return "elevated";
    return "normal";
  });

  let usedTokens = $derived(formatContextTokens(ctx.used));
  let limitTokens = $derived(formatContextTokens(ctx.limit));
  let pctLabel = $derived(formatContextPct(ctx.pct));
  let usedPct = $derived(ctx.pct == null ? 0 : Math.round(ctx.pct * 100));
  let freePct = $derived(ctx.pct == null ? 0 : Math.max(0, 100 - Math.round(ctx.pct * 100)));

  // Cache hit rate: a quick "system is composing efficiently" signal. The
  // percentage number is tinted on a continuous red→green gradient (green = full
  // cache reuse, red = cold). Only shown when the runtime reports it.
  let hasCacheHit = $derived(ctx.cacheHitPct != null);
  let cacheHitLabel = $derived(formatCacheHitPct(ctx.cacheHitPct));
  let cacheColor = $derived(ratioGradientColor(ctx.cacheHitPct));

  // Per-slot composition of the window (system / tools / memory / messages),
  // when the runtime's context instrumentation reports it. Empty -> honest fallback note.
  let breakdown = $derived(ctx.breakdown);
  let hasBreakdown = $derived(breakdown.length > 0);
  // Denominator for per-slot share: the sum of the reported slots, so each row
  // reads as its share of the composed context (sums to ~100%) rather than a
  // tiny fraction of the full window ceiling.
  let slotTotal = $derived(breakdown.reduce((sum, s) => sum + s.tokens, 0));
</script>

<svelte:window onkeydown={(e) => { if (open && e.key === "Escape") open = false; }} />

<span class="ctxm-anchor">
  <button
    type="button"
    class="ctxm-pill"
    class:is-open={open}
    bind:this={pillEl}
    onclick={() => (open = !open)}
    aria-haspopup="dialog"
    aria-expanded={open}
    title={`Context window: ${usedTokens} / ${limitTokens} (${pctLabel}% used)${hasCacheHit ? ` — Cache Hit ${cacheHitLabel}%` : ""}${ctx.fresh ? "" : " — last known value"} — click for details`}
  >
    <span class="ctxm-fill" data-tier={fillTier} style={`width:${usedPct}%`} aria-hidden="true"></span>
    <span class="ctxm-key">Context</span>
    <span class="ctxm-val">{usedTokens}<span class="ctxm-slash">/</span>{limitTokens}</span>
    <span class="ctxm-pct" data-tier={fillTier}>({pctLabel}%)</span>
    {#if hasCacheHit}
      <span class="ctxm-dash" aria-hidden="true">-</span>
      <span class="ctxm-cache-key">Cache Hit</span>
      <span
        class="ctxm-cache-num"
        style={`color:${cacheColor}`}
        title={`Cache hit rate: ${cacheHitLabel}% of the context served from cache`}
      >{cacheHitLabel}%</span>
    {/if}
    {#if !ctx.fresh}<span class="ctxm-stale" aria-hidden="true" title="last known value">~</span>{/if}
    <span class="ctxm-caret" aria-hidden="true">▾</span>
  </button>

  {#if open}
    <button class="ctxm-scrim" aria-label="Close context details" onclick={() => (open = false)}></button>
    <div
      class="ctxm-pop"
      role="dialog"
      aria-label="Context window details"
      bind:this={popEl}
      style={`left:${popLeft}px; bottom:${popBottom}px; --caret-x:${caretX}px`}
    >
      <div class="ctxm-title">Context Window</div>

      <div class="ctxm-total">
        <strong>{usedTokens} / {limitTokens} tokens</strong>
        <span class="ctxm-total-pct" data-tier={fillTier}>{pctLabel}%</span>
      </div>

      <div class="ctxm-bar" aria-hidden="true">
        <span class="ctxm-bar-fill" data-tier={fillTier} style={`width:${usedPct}%`}></span>
      </div>

      {#if hasCacheHit}
        <div class="ctxm-cacheline">
          <span class="ctxm-cacheline-label">Cache Hit</span>
          <span class="ctxm-cacheline-val" style={`color:${cacheColor}`}>
            <span class="ctxm-cache-glyph" aria-hidden="true">⚡</span>{cacheHitLabel}%
          </span>
        </div>
      {/if}

      {#if hasBreakdown}
        <div class="ctxm-sec">Composition</div>
        {#each breakdown as slot (slot.id ?? slot.label)}
          {@const sharePct = slotTotal ? Math.round((slot.tokens / slotTotal) * 100) : 0}
          {@const overBudget = slot.budget != null && slot.tokens > slot.budget}
          <div class="ctxm-row">
            <span class="ctxm-row-label">{slot.label}</span>
            <span class="ctxm-row-val" class:over={overBudget}>
              {formatContextTokens(slot.tokens)}{slot.budget
                ? ` / ${formatContextTokens(slot.budget)}`
                : ""} · {sharePct}%
            </span>
          </div>
          <div class="ctxm-slotbar" aria-hidden="true">
            <span
              class="ctxm-slotbar-fill"
              data-over={overBudget}
              style={`width:${Math.min(100, sharePct)}%`}
            ></span>
          </div>
        {/each}
      {:else}
        <div class="ctxm-sec">Usage</div>
        <div class="ctxm-row">
          <span class="ctxm-row-label">Conversation</span>
          <span class="ctxm-row-val">{usedTokens} · {usedPct}%</span>
        </div>
        <div class="ctxm-row">
          <span class="ctxm-row-label">Free</span>
          <span class="ctxm-row-val muted">{formatContextTokens(ctx.limit != null && ctx.used != null ? ctx.limit - ctx.used : null)} · {freePct}%</span>
        </div>
      {/if}

      {#if !ctx.fresh}
        <div class="ctxm-note">Last known value; not yet refreshed for the current turn.</div>
      {/if}
      {#if !hasBreakdown}
        <div class="ctxm-note muted">
          Per-slot composition (system, tools, memory, messages) isn't reported by the runtime yet.
        </div>
      {/if}
    </div>
  {/if}
</span>

<style>
  .ctxm-anchor {
    position: relative;
    display: inline-flex;
    flex-shrink: 1;
    min-width: 0;
  }

  /* Text-as-button, unified with ComposerModelPicker's .cmp-pill: transparent
     at rest, embedded in the glyph row, subtle hover/open background. */
  .ctxm-pill {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    max-width: 100%;
    padding: 4px 10px;
    /* Squared to match the composer card's shape language (flat sides, sharp
       rounded corners) rather than a fully-rounded capsule. */
    border-radius: var(--radius);
    border: 1px solid transparent;
    background: transparent;
    color: var(--muted);
    font-size: 12.5px;
    font-weight: 600;
    line-height: 1;
    white-space: nowrap;
    cursor: pointer;
    font-family: inherit;
    opacity: 0.82;
    transition: border-color 120ms ease, background 120ms ease, opacity 120ms ease;
  }

  .ctxm-pill {
    position: relative;
    overflow: hidden;
  }

  .ctxm-pill:hover,
  .ctxm-pill.is-open {
    background: var(--hover-strong);
    border-color: var(--line);
    opacity: 1;
  }

  /* Left-anchored proportional fill: the pill itself reads as a gauge whose
     width matches the used-context percentage, tinted by the same green/amber/
     red tier as the percent label. Kept low-opacity so the text stays legible,
     and sits behind the content (z-index). */
  .ctxm-fill {
    position: absolute;
    left: 0;
    top: 0;
    bottom: 0;
    width: 0;
    z-index: 0;
    pointer-events: none;
    border-radius: inherit;
    opacity: 0.16;
    transition: width 240ms ease, background 160ms ease;
  }

  .ctxm-fill[data-tier="normal"] {
    background: var(--success, #22c55e);
  }
  .ctxm-fill[data-tier="elevated"] {
    background: var(--warn);
  }
  .ctxm-fill[data-tier="high"] {
    background: var(--danger);
  }

  /* Keep all textual segments above the fill. */
  .ctxm-pill > span:not(.ctxm-fill) {
    position: relative;
    z-index: 1;
  }

  .ctxm-key {
    color: var(--muted);
    font-weight: 600;
  }

  .ctxm-val {
    color: var(--text);
    font-weight: 700;
    font-family: "Geist Mono Variable", ui-monospace, SFMono-Regular, monospace;
  }

  .ctxm-slash {
    color: var(--muted-2);
    margin: 0 4px;
  }

  .ctxm-pct {
    font-weight: 700;
    font-family: "Geist Mono Variable", ui-monospace, SFMono-Regular, monospace;
    color: var(--muted);
  }

  .ctxm-pct[data-tier="normal"] {
    color: var(--success, #22c55e);
  }
  .ctxm-pct[data-tier="elevated"] {
    color: var(--warn);
  }
  .ctxm-pct[data-tier="high"] {
    color: var(--danger);
  }

  /* " - " separator between the context reading and the cache-hit segment. */
  .ctxm-dash {
    color: var(--muted-2);
    margin: 0 2px;
  }

  /* Cache-hit segment: a "Cache Hit NN%" reading whose number is tinted on a
     continuous red→green gradient (green = full reuse). The label stays muted
     so the colored number carries the signal. */
  .ctxm-cache-key {
    color: var(--muted);
    font-weight: 600;
  }

  .ctxm-cache-num {
    font-weight: 700;
    font-family: "Geist Mono Variable", ui-monospace, SFMono-Regular, monospace;
  }

  .ctxm-cache-glyph {
    font-size: 11px;
    line-height: 1;
    opacity: 0.9;
  }

  .ctxm-stale {
    color: var(--muted-2);
    font-size: 13px;
  }

  .ctxm-caret {
    color: var(--muted);
    font-size: 9.5px;
  }

  /* ---- Inspector popup ---- */
  .ctxm-scrim {
    position: fixed;
    inset: 0;
    z-index: 40;
    background: transparent;
    border: 0;
    cursor: default;
  }

  .ctxm-pop {
    /* Fixed so it escapes the composer card's overflow clip; left/bottom are set
       inline from the trigger pill's screen rect. */
    position: fixed;
    z-index: 41;
    width: 260px;
    max-width: min(260px, calc(100vw - 24px));
    padding: 11px 12px 12px;
    border-radius: 12px;
    border: 1px solid var(--line-strong);
    background: var(--ic-panel-solid, #1d1e22);
    box-shadow: 0 -18px 50px -18px rgba(0, 0, 0, 0.6);
    animation: ctxm-pop-in 140ms cubic-bezier(0.2, 0.8, 0.2, 1);
  }

  /* Downward caret pointed at the real pill center (set via --caret-x). */
  .ctxm-pop::after {
    content: "";
    position: absolute;
    top: 100%;
    left: var(--caret-x, 50%);
    transform: translateX(-50%);
    width: 0;
    height: 0;
    border-left: 7px solid transparent;
    border-right: 7px solid transparent;
    border-top: 7px solid var(--ic-panel-solid, #1d1e22);
    filter: drop-shadow(0 1px 0 var(--line-strong));
  }

  @keyframes ctxm-pop-in {
    from {
      opacity: 0;
      transform: translateY(6px) scale(0.98);
    }
    to {
      opacity: 1;
      transform: translateY(0) scale(1);
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .ctxm-pop {
      animation: none;
    }
  }

  .ctxm-title {
    font-size: 11px;
    font-weight: 700;
    color: var(--text-strong);
    margin-bottom: 9px;
  }

  .ctxm-total {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 8px;
    margin-bottom: 7px;
  }

  .ctxm-total strong {
    color: var(--text);
    font-size: 13px;
    font-family: "Geist Mono Variable", ui-monospace, SFMono-Regular, monospace;
  }

  .ctxm-total-pct {
    font-size: 12px;
    font-weight: 700;
    font-family: "Geist Mono Variable", ui-monospace, SFMono-Regular, monospace;
  }
  .ctxm-total-pct[data-tier="normal"] {
    color: var(--success, #22c55e);
  }
  .ctxm-total-pct[data-tier="elevated"] {
    color: var(--warn);
  }
  .ctxm-total-pct[data-tier="high"] {
    color: var(--danger);
  }

  .ctxm-bar {
    height: 6px;
    border-radius: 999px;
    background: var(--panel-3, rgba(255, 255, 255, 0.06));
    overflow: hidden;
    margin-bottom: 12px;
  }

  .ctxm-bar-fill {
    display: block;
    height: 100%;
    border-radius: 999px;
    transition: width 200ms ease;
  }
  .ctxm-bar-fill[data-tier="normal"] {
    background: var(--success, #22c55e);
  }
  .ctxm-bar-fill[data-tier="elevated"] {
    background: var(--warn);
  }
  .ctxm-bar-fill[data-tier="high"] {
    background: var(--danger);
  }

  /* Cache-hit readout inside the inspector, mirroring the pill segment. */
  .ctxm-cacheline {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    margin: -2px 0 12px;
    padding: 6px 8px;
    border-radius: 8px;
    background: var(--panel-3, rgba(255, 255, 255, 0.04));
  }

  .ctxm-cacheline-label {
    font-size: 11.5px;
    color: var(--muted);
    font-weight: 600;
  }

  .ctxm-cacheline-val {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    font-size: 12px;
    font-weight: 700;
    font-family: "Geist Mono Variable", ui-monospace, SFMono-Regular, monospace;
    color: var(--muted);
  }

  /* Per-slot composition bar: thin accent track under each composition row. */
  .ctxm-slotbar {
    height: 4px;
    border-radius: 999px;
    background: var(--panel-3, rgba(255, 255, 255, 0.06));
    overflow: hidden;
    margin: 1px 0 7px;
  }

  .ctxm-slotbar-fill {
    display: block;
    height: 100%;
    border-radius: 999px;
    background: var(--accent, #6366f1);
    transition: width 200ms ease;
  }

  .ctxm-slotbar-fill[data-over="true"] {
    background: var(--danger);
  }

  .ctxm-sec {
    font-size: 10px;
    font-weight: 600;
    color: var(--muted-2);
    margin-bottom: 5px;
  }

  .ctxm-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 3px 0;
    font-size: 12px;
  }

  .ctxm-row-label {
    color: var(--text);
  }

  .ctxm-row-val {
    color: var(--text);
    font-family: "Geist Mono Variable", ui-monospace, SFMono-Regular, monospace;
    font-size: 11px;
  }

  .ctxm-row-val.muted {
    color: var(--muted);
  }

  /* Slot value tints red when its tokens exceed the reported budget. */
  .ctxm-row-val.over {
    color: var(--danger);
  }

  .ctxm-note {
    margin-top: 9px;
    font-size: 10.5px;
    line-height: 1.35;
    color: var(--muted);
  }

  .ctxm-note.muted {
    color: var(--muted-2);
  }

  /* Phone: the verbose pill (Context 131k/1M (13%) - Cache Hit 78%) is far too
     wide to share the runtime row. Collapse the trigger to the load-bearing
     signal: the fill bar, the percentage, and the caret. The full breakdown is
     one tap away in the inspector popover. */
  @media (max-width: 520px) {
    .ctxm-val,
    .ctxm-dash,
    .ctxm-cache-key,
    .ctxm-cache-num,
    .ctxm-stale {
      display: none;
    }

    .ctxm-pill {
      gap: 4px;
      padding: 4px 8px;
    }
  }
</style>
