<script lang="ts">
  /**
   * PROJECT LOGOS — Message Metadata Header (Track B)
   *
   * Renders the monospaced metadata strip above every message body per spec §8.4:
   *   [INTENT: COMMAND] [PERSONA: OPERATOR] [CONFIDENCE: 98.4%] [THREAD: #TH-042] [LATENCY: 14ms]
   *
   * Absent fields render [--] or omit. Confidence tag is clickable → opens
   * the deep-inspection blade. Semantic thread chip renders as th:XXXXXXXX
   * with cobalt-cyan 2px accent.
   */

  interface Props {
    intent?: string | null;
    persona?: string | null;
    confidence?: number | null;
    threadAffiliation?: string | null;
    executionStatus?: "pending" | "executing" | "complete" | "failed" | null;
    /** Measured client-side render/transform latency in ms. */
    latencyMs?: number | null;
    /** Transient: cognition analysis is in flight. */
    analyzing?: boolean;
    /** Transient: a transform op is in flight. */
    transforming?: boolean;
    /** Semantic thread id for the chip affordance. */
    semanticThreadId?: string | null;
    /** Callback when semantic thread chip is clicked. */
    onSemanticThreadClick?: (threadId: string) => void;
    /** Callback when confidence tag is clicked → opens inspector blade. */
    onInspect?: () => void;
    /** Whether the inspector blade is currently open. */
    inspectorOpen?: boolean;
  }

  let {
    intent = null,
    persona = null,
    confidence = null,
    threadAffiliation = null,
    executionStatus = null,
    latencyMs = null,
    analyzing = false,
    transforming = false,
    semanticThreadId = null,
    onSemanticThreadClick,
    onInspect,
    inspectorOpen = false,
  }: Props = $props();

  const hasAny = $derived(
    intent !== null ||
      persona !== null ||
      confidence !== null ||
      threadAffiliation !== null ||
      executionStatus !== null ||
      latencyMs !== null ||
      analyzing ||
      transforming,
  );

  function fmtIntent(v: string): string {
    return v.toUpperCase();
  }

  function fmtPersona(v: string): string {
    return v.toUpperCase();
  }

  function fmtConfidence(v: number): string {
    return `${(v * 100).toFixed(1)}%`;
  }

  function fmtLatency(v: number): string {
    return `${Math.round(v)}ms`;
  }

  function fmtExec(v: string): string {
    return v.toUpperCase();
  }
</script>

{#if hasAny}
  <div class="msg-meta-header" aria-label="Message metadata">
    {#if intent !== null}
      <span class="meta-tag meta-intent">
        <span class="meta-bracket">[</span>INTENT<span class="meta-colon">:</span> {fmtIntent(intent)}<span class="meta-bracket">]</span>
      </span>
    {:else}
      <span class="meta-tag meta-absent">[INTENT: --]</span>
    {/if}

    {#if persona !== null}
      <span class="meta-tag meta-persona">
        <span class="meta-bracket">[</span>PERSONA<span class="meta-colon">:</span> {fmtPersona(persona)}<span class="meta-bracket">]</span>
      </span>
    {/if}

    {#if confidence !== null}
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <span
        class="meta-tag meta-confidence"
        class:meta-clickable={onInspect !== undefined}
        class:meta-active={inspectorOpen}
        role={onInspect ? "button" : undefined}
        tabindex={onInspect ? 0 : undefined}
        title={onInspect ? "Inspect message telemetry" : `Confidence: ${fmtConfidence(confidence)}`}
        aria-label={onInspect ? "Open message inspector" : undefined}
        onclick={() => onInspect?.()}
        onkeydown={(e: KeyboardEvent) => {
          if ((e.key === "Enter" || e.key === " ") && onInspect) {
            e.preventDefault();
            onInspect();
          }
        }}
      >
        <span class="meta-bracket">[</span>CONF<span class="meta-colon">:</span> {fmtConfidence(confidence)}<span class="meta-bracket">]</span>
      </span>
    {:else}
      <span class="meta-tag meta-absent">[CONF: --]</span>
    {/if}

    {#if threadAffiliation !== null}
      <span class="meta-tag meta-thread">
        <span class="meta-bracket">[</span>THREAD<span class="meta-colon">:</span> {threadAffiliation}<span class="meta-bracket">]</span>
      </span>
    {/if}

    {#if executionStatus !== null}
      <span
        class="meta-tag meta-exec"
        class:exec-pending={executionStatus === "pending"}
        class:exec-executing={executionStatus === "executing"}
        class:exec-complete={executionStatus === "complete"}
        class:exec-failed={executionStatus === "failed"}
      >
        <span class="meta-bracket">[</span>EXEC<span class="meta-colon">:</span> {fmtExec(executionStatus)}<span class="meta-bracket">]</span>
      </span>
    {/if}

    {#if latencyMs !== null}
      <span class="meta-tag meta-latency">
        <span class="meta-bracket">[</span>LATENCY<span class="meta-colon">:</span> {fmtLatency(latencyMs)}<span class="meta-bracket">]</span>
      </span>
    {/if}

    {#if analyzing}
      <span class="meta-tag meta-transient analyzing" title="Analyzing…" aria-label="Analyzing message">
        <span class="meta-bracket">[</span>ANALYZING<span class="meta-bracket">]</span>
      </span>
    {/if}

    {#if transforming}
      <span class="meta-tag meta-transient transforming" title="Transforming…" aria-label="Transforming message">
        <span class="meta-bracket">[</span>TRANSFORMING<span class="meta-bracket">]</span>
      </span>
    {/if}
  </div>
{/if}

{#if semanticThreadId !== null}
  <button
    type="button"
    class="semantic-thread-chip"
    title="Semantic thread: {semanticThreadId}"
    aria-label="Semantic thread: {semanticThreadId}"
    onclick={() => onSemanticThreadClick?.(semanticThreadId!)}
  >
    th:{semanticThreadId.slice(0, 8)}
  </button>
{/if}

<style>
  /* ── Metadata header strip ── */
  .msg-meta-header {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 0 8px;
    margin-bottom: 6px;
    font-family: var(--font-mono);
    font-size: 9.5px;
    font-weight: 500;
    line-height: 1.45;
    letter-spacing: 0.03em;
    color: var(--muted-2);
    user-select: none;
  }

  .meta-tag {
    display: inline;
    white-space: nowrap;
  }

  .meta-bracket,
  .meta-colon {
    color: var(--muted);
  }

  .meta-intent { color: var(--muted-2); }
  .meta-persona { color: var(--muted-2); }
  .meta-thread { color: var(--muted-2); }
  .meta-latency { color: var(--muted-2); }
  .meta-absent { color: var(--muted); opacity: 0.5; font-style: italic; }

  /* ── Confidence tag: clickable → inspector blade ── */
  .meta-confidence { color: var(--text); }
  .meta-clickable {
    cursor: pointer;
    color: var(--accent-thread); /* cobalt cyan */
    font-weight: 600;
  }

  .meta-clickable:hover {
    color: var(--text-strong);
    text-decoration: underline;
    text-underline-offset: 3px;
  }

  .meta-clickable:focus-visible {
    outline: 1px solid var(--text-strong);
    outline-offset: 2px;
  }

  .meta-active {
    color: var(--text-strong);
    text-decoration: underline;
    text-underline-offset: 3px;
  }

  /* ── Execution status colors ── */
  .exec-pending { color: var(--muted-2); }
  .exec-executing {
    color: var(--accent-thread);
    animation: meta-exec-pulse 1.2s steps(2, jump-none) infinite;
  }
  .exec-complete { color: var(--accent-verified); }
  .exec-failed { color: var(--status-danger); }

  @keyframes meta-exec-pulse {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.4; }
  }

  /* ── Transient states ── */
  .meta-transient {
    color: var(--muted-2);
    font-style: italic;
  }
  .meta-transient.analyzing {
    animation: meta-exec-pulse 1.2s steps(2, jump-none) infinite;
  }
  .meta-transient.transforming {
    animation: meta-exec-pulse 0.8s steps(2, jump-none) infinite;
  }

  @media (prefers-reduced-motion: reduce) {
    .exec-executing,
    .meta-transient.analyzing,
    .meta-transient.transforming {
      animation: none;
    }
  }

  /* ── Semantic thread chip (th:XXXXXXXX) ── */
  .semantic-thread-chip {
    display: inline-flex;
    align-items: center;
    padding: 1px 6px;
    margin-top: 2px;
    border: 0;
    border-left: 2px solid var(--accent-thread);
    background: var(--panel-2);
    color: var(--accent-thread);
    font-family: var(--font-mono);
    font-size: 9px;
    font-weight: 600;
    letter-spacing: 0.04em;
    cursor: pointer;
    line-height: 1.4;
  }

  .semantic-thread-chip:hover {
    background: var(--hover-strong);
    color: var(--text-strong);
    border-left-color: var(--text-strong);
  }

  .semantic-thread-chip:focus-visible {
    outline: 1px solid var(--text-strong);
    outline-offset: 1px;
  }
</style>
