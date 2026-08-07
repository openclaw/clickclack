<script lang="ts">
  /**
   * COGNITIVE OS — Inline Cognitive Result Strip (T4)
   *
   * Renders transform results or memory query results inline below a message.
   * Non-modal, monochrome, flat. Renders nothing when resultKind is null.
   */

  interface TransformStripData {
    kind: "transform";
    op: string;
    original: string;
    transformed: string;
  }

  interface MemoryStripData {
    kind: "memory";
    nodes: Array<{ content: string; score: number }>;
  }

  type StripData = TransformStripData | MemoryStripData;

  interface Props {
    data: StripData | null;
    /** Whether a request is currently in flight. */
    loading?: boolean;
    onApply?: () => void;
    onDismiss: () => void;
    /** Whether the apply action is pending. */
    applying?: boolean;
  }

  let {
    data = null,
    loading = false,
    onApply,
    onDismiss,
    applying = false,
  }: Props = $props();

  let showOriginal = $state(false);
  let expandedNodeIndex = $state<number | null>(null);

  function resetToggle() {
    showOriginal = false;
    expandedNodeIndex = null;
  }

  // Reset when data changes
  $effect(() => {
    if (data) resetToggle();
  });
</script>

{#if data}
  <div class="cog-result-strip" aria-live="polite">
    {#if data.kind === "transform"}
      <div class="cog-result-toolbar">
        <div class="cog-result-tabs">
          <button
            type="button"
            class="cog-result-tab"
            class:active={!showOriginal}
            onclick={() => (showOriginal = false)}
          >
            {data.op}
          </button>
          <button
            type="button"
            class="cog-result-tab"
            class:active={showOriginal}
            onclick={() => (showOriginal = true)}
          >
            original
          </button>
        </div>
        <div class="cog-result-actions">
          {#if onApply}
            <button
              type="button"
              class="cog-result-apply"
              disabled={applying}
              onclick={onApply}
              title="Apply transform"
            >{applying ? "applying…" : "apply"}</button>
          {/if}
          <button
            type="button"
            class="cog-result-dismiss"
            onclick={onDismiss}
            aria-label="Dismiss result"
            title="Dismiss"
          >
            <svg viewBox="0 0 24 24" width="12" height="12" aria-hidden="true">
              <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" d="M6 6l12 12M18 6L6 18"/>
            </svg>
          </button>
        </div>
      </div>
      <div class="cog-result-body">
        <span class="cog-result-text">{showOriginal ? data.original : data.transformed}</span>
      </div>
    {:else if data.kind === "memory"}
      <div class="cog-result-toolbar">
        <div class="cog-result-label">Memory matches ({data.nodes.length})</div>
        <button
          type="button"
          class="cog-result-dismiss"
          onclick={onDismiss}
          aria-label="Dismiss result"
          title="Dismiss"
        >
          <svg viewBox="0 0 24 24" width="12" height="12" aria-hidden="true">
            <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" d="M6 6l12 12M18 6L6 18"/>
          </svg>
        </button>
      </div>
      <div class="cog-result-body">
        {#each data.nodes as node, i}
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div
            class="cog-memory-node"
            class:expanded={expandedNodeIndex === i}
            onclick={() => (expandedNodeIndex = expandedNodeIndex === i ? null : i)}
            onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') expandedNodeIndex = expandedNodeIndex === i ? null : i; }}
            role="button"
            tabindex="0"
            aria-expanded={expandedNodeIndex === i}
          >
            <span class="cog-memory-score">{Math.round(node.score * 100)}%</span>
            <span class="cog-memory-content">{node.content}</span>
          </div>
        {/each}
      </div>
    {/if}

    {#if loading}
      <div class="cog-result-loading" aria-label="Loading">
        <span class="cog-result-loading-bar"></span>
      </div>
    {/if}
  </div>
{/if}

<style>
  .cog-result-strip {
    margin-top: 6px;
    border: 1px solid var(--line);
    border-left: 0;
    border-right: 0;
    background: color-mix(in srgb, var(--panel) 70%, var(--bg));
    font-family: var(--font-mono);
    font-size: 11px;
    line-height: 1.5;
    position: relative;
    overflow: hidden;
  }

  .cog-result-toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 2px 4px;
    border-bottom: 1px solid var(--line);
    min-height: 22px;
  }

  .cog-result-tabs {
    display: flex;
    gap: 0;
  }

  .cog-result-tab {
    padding: 1px 8px;
    border: 0;
    background: transparent;
    color: var(--muted-2);
    font-family: var(--font-mono);
    font-size: 9.5px;
    font-weight: 600;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    cursor: pointer;
    line-height: 1.4;
  }

  .cog-result-tab.active {
    color: var(--text-strong);
  }

  .cog-result-tab:hover {
    color: var(--text);
  }

  .cog-result-label {
    color: var(--muted);
    font-size: 9.5px;
    font-weight: 600;
    letter-spacing: 0.04em;
    text-transform: uppercase;
  }

  .cog-result-actions {
    display: flex;
    align-items: center;
    gap: 2px;
  }

  .cog-result-apply {
    padding: 1px 8px;
    border: 1px solid var(--line-strong);
    background: transparent;
    color: var(--text);
    font-family: var(--font-mono);
    font-size: 9px;
    font-weight: 600;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    cursor: pointer;
  }

  .cog-result-apply:hover:not(:disabled) {
    background: var(--hover-strong);
    color: var(--text-strong);
  }

  .cog-result-apply:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .cog-result-dismiss {
    display: grid;
    place-items: center;
    width: 22px;
    height: 22px;
    border: 0;
    background: transparent;
    color: var(--muted);
    cursor: pointer;
  }

  .cog-result-dismiss:hover {
    background: var(--hover-strong);
    color: var(--text-strong);
  }

  .cog-result-body {
    padding: 6px 8px;
    max-height: 120px;
    overflow-y: auto;
  }

  .cog-result-text {
    color: var(--text);
    white-space: pre-wrap;
    word-break: break-word;
  }

  /* Memory nodes */
  .cog-memory-node {
    display: flex;
    align-items: flex-start;
    gap: 6px;
    padding: 2px 0;
    cursor: pointer;
    border-bottom: 1px solid color-mix(in srgb, var(--line) 40%, transparent);
  }

  .cog-memory-node:last-child {
    border-bottom: 0;
  }

  .cog-memory-score {
    flex-shrink: 0;
    min-width: 30px;
    color: var(--muted-2);
    font-size: 9.5px;
    font-weight: 600;
  }

  .cog-memory-content {
    color: var(--text);
    font-size: 10.5px;
    line-height: 1.4;
    overflow: hidden;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
  }

  .cog-memory-node.expanded .cog-memory-content {
    display: block;
    -webkit-line-clamp: unset;
  }

  /* Loading indicator */
  .cog-result-loading {
    position: absolute;
    bottom: 0;
    left: 0;
    right: 0;
    height: 2px;
    background: transparent;
  }

  .cog-result-loading-bar {
    display: block;
    height: 100%;
    width: 30%;
    background: var(--text);
    animation: cog-loading-slide 1.2s ease-in-out infinite;
  }

  @keyframes cog-loading-slide {
    0% { transform: translateX(-100%); }
    100% { transform: translateX(400%); }
  }

  @media (prefers-reduced-motion: reduce) {
    .cog-result-loading-bar {
      animation: none;
      width: 100%;
    }
  }
</style>
