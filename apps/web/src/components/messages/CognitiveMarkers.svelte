<script lang="ts">
  /**
   * COGNITIVE OS — Cognitive State Markers (T4)
   *
   * Renders message metadata markers: intent color band, persona tag,
   * confidence indicator, thread affiliation marker, execution status marker,
   * and semantic thread chip.
   *
   * Every marker renders as a graceful absent state when its field is missing.
   * Transient "analyzing" / "transforming" states shown while requests are in flight.
   */

  interface Props {
    intent?: string | null;
    persona?: string | null;
    confidence?: number | null;
    threadAffiliation?: string | null;
    executionStatus?: "pending" | "executing" | "complete" | "failed" | null;
    /** Transient: cognition analysis is in flight. */
    analyzing?: boolean;
    /** Transient: a transform op is in flight. */
    transforming?: boolean;
    /** Semantic thread id for the chip affordance. */
    semanticThreadId?: string | null;
    /** Callback when semantic thread chip is clicked. */
    onSemanticThreadClick?: (threadId: string) => void;
  }

  let {
    intent = null,
    persona = null,
    confidence = null,
    threadAffiliation = null,
    executionStatus = null,
    analyzing = false,
    transforming = false,
    semanticThreadId = null,
    onSemanticThreadClick,
  }: Props = $props();

  // Intent → color mapping. Absent = no band.
  const intentColors: Record<string, string> = {
    ask: "var(--intent-ask)",
    command: "var(--intent-command)",
    reflect: "var(--intent-reflect)",
    draft: "var(--intent-draft)",
    clarify: "var(--intent-clarify)",
    explore: "var(--intent-explore)",
  };

  const intentColor = $derived(intent ? (intentColors[intent] ?? "var(--intent-default)") : null);
  const hasAnyMarker = $derived(
    intentColor !== null ||
      persona !== null ||
      confidence !== null ||
      threadAffiliation !== null ||
      executionStatus !== null ||
      analyzing ||
      transforming ||
      semanticThreadId !== null,
  );

  function fmtConfidence(value: number): string {
    return `${Math.round(value * 100)}%`;
  }

  function confidenceWidth(value: number): string {
    return `${Math.round(value * 100)}%`;
  }
</script>

{#if hasAnyMarker}
  <div class="cog-markers" aria-label="Cognitive state markers">
    {#if intentColor !== null}
      <span
        class="cog-marker cog-intent-band"
        style="--intent-color: {intentColor}"
        title="Intent: {intent ?? 'unknown'}"
        aria-label="Intent: {intent}"
      ></span>
    {/if}
    {#if persona !== null}
      <span class="cog-marker cog-persona-tag" title="Persona: {persona}" aria-label="Persona: {persona}">
        {persona}
      </span>
    {/if}
    {#if confidence !== null}
      <span class="cog-marker cog-confidence" title="Confidence: {fmtConfidence(confidence)}" aria-label="Confidence: {fmtConfidence(confidence)}">
        <span class="cog-confidence-bar" style="width: {confidenceWidth(confidence)}"></span>
        <span class="cog-confidence-label">{fmtConfidence(confidence)}</span>
      </span>
    {/if}
    {#if threadAffiliation !== null}
      <span class="cog-marker cog-thread-affil" title="Thread: {threadAffiliation}" aria-label="Thread: {threadAffiliation}">
        <svg viewBox="0 0 24 24" width="10" height="10" aria-hidden="true">
          <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M21 12a8 8 0 0 1-11.6 7.16L3 21l1.84-6.4A8 8 0 1 1 21 12Z"/>
        </svg>
        <span>{threadAffiliation}</span>
      </span>
    {/if}
    {#if executionStatus !== null}
      <span
        class="cog-marker cog-exec-status"
        class:exec-pending={executionStatus === "pending"}
        class:exec-executing={executionStatus === "executing"}
        class:exec-complete={executionStatus === "complete"}
        class:exec-failed={executionStatus === "failed"}
        title="Execution: {executionStatus}"
        aria-label="Execution status: {executionStatus}"
      >
        <span class="cog-exec-dot" aria-hidden="true"></span>
        <span>{executionStatus}</span>
      </span>
    {/if}
    <!-- Semantic thread chip (T4) -->
    {#if semanticThreadId !== null}
      <button
        type="button"
        class="cog-marker cog-semantic-chip"
        title="Semantic thread: {semanticThreadId}"
        aria-label="Semantic thread: {semanticThreadId}"
        onclick={() => onSemanticThreadClick?.(semanticThreadId!)}
      >
        <svg viewBox="0 0 24 24" width="10" height="10" aria-hidden="true">
          <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M17 3a2.83 2.83 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5Z"/>
        </svg>
        <span>th:{semanticThreadId.slice(0, 8)}</span>
      </button>
    {/if}
    <!-- Transient states (shown when inflight, even if metadata absent) -->
    {#if analyzing}
      <span class="cog-marker cog-transient" title="Analyzing…" aria-label="Analyzing message">
        <span class="cog-transient-dot analyzing"></span>
        analyzing
      </span>
    {/if}
    {#if transforming}
      <span class="cog-marker cog-transient" title="Transforming…" aria-label="Transforming message">
        <span class="cog-transient-dot transforming"></span>
        transforming
      </span>
    {/if}
  </div>
{/if}

<style>
  .cog-markers {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
    margin-bottom: 4px;
    min-height: 0;
  }

  .cog-marker {
    display: inline-flex;
    align-items: center;
    font-family: var(--font-mono);
    font-size: 9.5px;
    font-weight: 600;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    line-height: 1;
    white-space: nowrap;
  }

  /* Intent color band: high-contrast left edge bar */
  .cog-intent-band {
    width: 3px;
    height: 14px;
    background: var(--intent-color);
    flex-shrink: 0;
    border-radius: 0;
  }

  /* Persona tag */
  .cog-persona-tag {
    padding: 2px 6px;
    border: 1px solid var(--line);
    color: var(--muted);
    background: var(--panel-2);
    border-radius: 0;
  }

  /* Confidence indicator: density bar + numeric */
  .cog-confidence {
    gap: 5px;
    min-width: 50px;
  }

  .cog-confidence-bar {
    height: 4px;
    background: var(--text-strong);
    border-radius: 0;
    transition: width 120ms ease;
  }

  .cog-confidence-label {
    color: var(--muted);
    font-size: 9px;
  }

  /* Thread affiliation marker */
  .cog-thread-affil {
    gap: 3px;
    color: var(--muted);
  }

  .cog-thread-affil svg {
    flex-shrink: 0;
  }

  /* Execution status marker */
  .cog-exec-status {
    gap: 4px;
    color: var(--muted);
  }

  .cog-exec-dot {
    width: 5px;
    height: 5px;
    background: var(--muted);
    border-radius: 0;
  }

  .exec-pending .cog-exec-dot {
    background: var(--status-pending);
  }

  .exec-executing .cog-exec-dot {
    background: var(--status-executing);
    animation: cog-exec-pulse 1.2s ease-in-out infinite;
  }

  .exec-complete .cog-exec-dot {
    background: var(--status-success);
  }

  .exec-failed .cog-exec-dot {
    background: var(--status-danger);
  }

  /* Semantic thread chip */
  .cog-semantic-chip {
    padding: 2px 6px;
    border: 1px solid var(--line);
    background: transparent;
    color: var(--muted-2);
    cursor: pointer;
    gap: 4px;
    font-family: var(--font-mono);
    font-size: 9px;
    font-weight: 600;
    letter-spacing: 0.04em;
    text-transform: uppercase;
  }

  .cog-semantic-chip:hover {
    background: var(--hover-strong);
    color: var(--text);
  }

  .cog-semantic-chip:focus-visible {
    outline: 1px solid var(--text-strong);
    outline-offset: -1px;
  }

  /* Transient analyzing/transforming */
  .cog-transient {
    gap: 4px;
    color: var(--muted-2);
    font-style: italic;
  }

  .cog-transient-dot {
    width: 5px;
    height: 5px;
    background: var(--muted-2);
    border-radius: 0;
  }

  .cog-transient-dot.analyzing {
    animation: cog-exec-pulse 1.2s ease-in-out infinite;
    background: var(--status-executing);
  }

  .cog-transient-dot.transforming {
    animation: cog-exec-pulse 0.9s ease-in-out infinite;
    background: var(--status-executing);
  }

  @keyframes cog-exec-pulse {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.3; }
  }

  @media (prefers-reduced-motion: reduce) {
    .exec-executing .cog-exec-dot,
    .cog-transient-dot.analyzing,
    .cog-transient-dot.transforming {
      animation: none;
    }
  }
</style>
