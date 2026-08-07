<script lang="ts">
  /**
   * PROJECT LOGOS — InspectorBlade (Inspection + Telemetry Track)
   *
   * In-grid split-blade inspection panel (spec §8.5). Slides open within the
   * message grid when triggered by clicking [CONF: 0.XX] tag. No modal overlay.
   *
   * Tabs: TELEMETRY | MEMORY | LOGPROBS | PAYLOAD | STACK
   * Closes on Escape or via onClose callback.
   * Transitions at 150ms linear (matching chassis --motion-med).
   */

  import type { LogosMessage } from "./MessageFrame.svelte";

  interface Props {
    message: LogosMessage;
    open: boolean;
    onClose: () => void;
  }

  let { message, open, onClose }: Props = $props();

  // ── Tab state ──
  type TabId = "telemetry" | "memory" | "logprobs" | "payload" | "stack";
  let activeTab = $state<TabId>("telemetry");

  // ── Derived inspection data ──

  const meta = $derived((message.metadata_json ?? {}) as Record<string, unknown>);
  const telemetry = $derived((meta.telemetry as Record<string, unknown>) ?? {});

  // TELEMETRY tab fields
  const latencyMs = $derived(telemetry.latency_ms as number | undefined);
  const totalTokens = $derived(telemetry.total_tokens as number | undefined);
  const modelName = $derived(telemetry.model as string | undefined);
  const intentVectorScore = $derived(message.confidence != null ? message.confidence : undefined);

  // MEMORY tab
  const memoryCitations = $derived.by((): string[] => {
    const citations = telemetry.memory_citations as string[] | undefined;
    if (citations?.length) return citations;
    const tags = meta.context_tags as string[] | undefined;
    if (tags?.length) return tags.map((t) => `#NODE-${t.slice(0, 8)}`);
    return [];
  });

  // LOGPROBS tab
  const logprobs = $derived(meta.logprobs as Array<{ token: string; prob: number }> | undefined);

  // PAYLOAD tab: full JSON dump
  const payloadJson = $derived.by(() => {
    const obj: Record<string, unknown> = {
      id: message.id,
      intent: message.intent ?? null,
      persona: message.persona ?? null,
      confidence: message.confidence ?? null,
      thread_id: message.thread_id ?? null,
      execution_status: message.execution_status ?? null,
      body_preview:
        (message.body?.slice(0, 500) ?? "") + (message.body && message.body.length > 500 ? "…" : ""),
      created_at: message.created_at ?? null,
      metadata_json: meta,
    };
    return JSON.stringify(obj, null, 2);
  });

  // STACK tab: execution_stack from telemetry or fallback
  const execStack = $derived.by((): string[] => {
    const stack = telemetry.execution_stack as string[] | undefined;
    if (stack?.length) return stack;
    return ["intent_parser()", "persona_engine()"];
  });

  // ── Keyboard: close on Escape ──
  function handleKeydown(e: KeyboardEvent) {
    if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      onClose();
    }
  }

  const tabs: Array<{ id: TabId; label: string }> = [
    { id: "telemetry", label: "TELEMETRY" },
    { id: "memory", label: "MEMORY" },
    { id: "logprobs", label: "LOGPROBS" },
    { id: "payload", label: "PAYLOAD" },
    { id: "stack", label: "STACK" },
  ];
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  class="inspector-blade"
  class:open
  onkeydown={handleKeydown}
>
  <!-- Tab bar -->
  <div class="inspector-tabs">
    {#each tabs as tab}
      <button
        type="button"
        class="inspector-tab"
        class:active={activeTab === tab.id}
        onclick={() => (activeTab = tab.id)}
      >
        {tab.label}
      </button>
    {/each}
    <button
      type="button"
      class="inspector-close"
      onclick={onClose}
      aria-label="Close inspector"
      title="Close inspector (Esc)"
    >
      <svg viewBox="0 0 24 24" width="11" height="11" aria-hidden="true">
        <path
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          d="M6 6l12 12M18 6L6 18"
        />
      </svg>
    </button>
  </div>

  <!-- Panel body -->
  <div class="inspector-body">
    {#if activeTab === "telemetry"}
      <div class="inspector-section">
        <div class="inspect-grid">
          <div class="inspect-row">
            <span class="inspect-label">LATENCY (MS)</span>
            <span class="inspect-value">{latencyMs !== undefined ? `${Math.round(latencyMs)} ms` : "n/a"}</span>
          </div>
          <div class="inspect-row">
            <span class="inspect-label">TOTAL TOKENS</span>
            <span class="inspect-value">{totalTokens !== undefined ? String(totalTokens) : "n/a"}</span>
          </div>
          <div class="inspect-row">
            <span class="inspect-label">MODEL</span>
            <span class="inspect-value mono">{modelName ?? "n/a"}</span>
          </div>
          <div class="inspect-row">
            <span class="inspect-label">INTENT VECTOR</span>
            <span class="inspect-value">{intentVectorScore !== undefined ? (intentVectorScore * 100).toFixed(1) + "%" : "n/a"}</span>
          </div>
        </div>
      </div>

    {:else if activeTab === "memory"}
      <div class="inspector-section">
        {#if memoryCitations.length > 0}
          <div class="inspect-memory-list">
            {#each memoryCitations as node}
              <div class="inspect-memory-node">
                <span class="node-marker">{node.startsWith("#") ? node : `#NODE-${node.slice(0, 8)}`}</span>
              </div>
            {/each}
          </div>
        {:else}
          <div class="inspect-empty">#NODE---</div>
        {/if}
      </div>

    {:else if activeTab === "logprobs"}
      <div class="inspector-section">
        {#if logprobs?.length}
          <div class="inspect-logprobs">
            {#each logprobs as lp}
              <div class="logprob-row">
                <span class="logprob-token">"{lp.token}"</span>
                <span class="logprob-prob">({lp.prob.toFixed(4)})</span>
              </div>
            {/each}
          </div>
        {:else}
          <div class="inspect-empty">[logprobs: n/a]</div>
        {/if}
      </div>

    {:else if activeTab === "payload"}
      <div class="inspector-section">
        <pre class="inspect-json">{payloadJson}</pre>
      </div>

    {:else if activeTab === "stack"}
      <div class="inspector-section">
        <div class="inspect-stack">
          {#each execStack as step}
            <div class="stack-frame">
              <span class="stack-label">{step}</span>
            </div>
          {/each}
        </div>
      </div>
    {/if}
  </div>
</div>

<style>
  .inspector-blade {
    display: grid;
    grid-template-rows: auto minmax(0, 1fr);
    border: 1px solid var(--line-strong);
    border-top: none;
    background: var(--panel);
    font-family: var(--font-mono);
    font-size: 10px;
    line-height: 1.45;
    max-height: 0;
    overflow: hidden;
    opacity: 0;
    transition:
      max-height var(--motion-med),
      opacity var(--motion-med);
  }

  .inspector-blade.open {
    max-height: 400px;
    opacity: 1;
  }

  /* ── Tabs ── */
  .inspector-tabs {
    display: flex;
    align-items: center;
    gap: 0;
    border-bottom: 1px solid var(--line);
    background: var(--panel-3);
  }

  .inspector-tab {
    padding: 4px 10px;
    border: 0;
    border-right: 1px solid var(--line);
    background: transparent;
    color: var(--muted-2);
    font-family: var(--font-mono);
    font-size: 9px;
    font-weight: 600;
    letter-spacing: 0.05em;
    cursor: pointer;
    line-height: 1.4;
    text-transform: uppercase;
  }

  .inspector-tab:hover {
    background: var(--hover-strong);
    color: var(--text);
  }

  .inspector-tab.active {
    background: var(--panel);
    color: var(--text-strong);
    border-bottom: 1px solid var(--panel);
    margin-bottom: -1px;
  }

  .inspector-tab:focus-visible {
    outline: 1px solid var(--text-strong);
    outline-offset: -2px;
  }

  .inspector-close {
    margin-left: auto;
    display: grid;
    place-items: center;
    width: 28px;
    height: 28px;
    border: 0;
    background: transparent;
    color: var(--muted);
    cursor: pointer;
    flex-shrink: 0;
  }

  .inspector-close:hover {
    background: var(--hover-strong);
    color: var(--text-strong);
  }

  /* ── Body ── */
  .inspector-body {
    padding: 6px 8px;
    max-height: 300px;
    overflow-y: auto;
  }

  /* ── Sections ── */
  .inspector-section {
    min-height: 40px;
  }

  .inspect-grid {
    display: grid;
    gap: 1px;
  }

  .inspect-row {
    display: grid;
    grid-template-columns: 130px 1fr;
    gap: 8px;
    padding: 3px 4px;
    border-bottom: 1px solid color-mix(in srgb, var(--line) 60%, transparent);
  }

  .inspect-row:last-child {
    border-bottom: 0;
  }

  .inspect-label {
    color: var(--muted);
    font-size: 9px;
    font-weight: 600;
    letter-spacing: 0.05em;
    text-transform: uppercase;
  }

  .inspect-value {
    color: var(--text);
    font-size: 10px;
  }

  .inspect-value.mono {
    font-family: var(--font-mono);
  }

  /* ── Memory citations ── */
  .inspect-memory-list {
    display: grid;
    gap: 3px;
  }

  .inspect-memory-node {
    display: flex;
    align-items: baseline;
    gap: 8px;
    padding: 3px 6px;
    border: 1px solid var(--line);
    background: var(--panel-2);
  }

  .node-marker {
    color: var(--accent-thread);
    font-weight: 700;
    font-size: 9px;
    letter-spacing: 0.04em;
  }

  /* ── Logprobs ── */
  .inspect-logprobs {
    display: grid;
    gap: 1px;
  }

  .logprob-row {
    display: flex;
    align-items: baseline;
    gap: 6px;
    padding: 1px 4px;
  }

  .logprob-token {
    color: var(--text);
    font-weight: 600;
  }

  .logprob-prob {
    color: var(--muted-2);
    font-size: 9px;
  }

  /* ── Raw JSON ── */
  .inspect-json {
    margin: 0;
    padding: 6px 8px;
    border: 1px solid var(--line);
    background: var(--panel-2);
    color: var(--text);
    font-family: var(--font-mono);
    font-size: 9.5px;
    line-height: 1.5;
    white-space: pre-wrap;
    word-break: break-all;
    max-height: 260px;
    overflow-y: auto;
  }

  /* ── Execution stack ── */
  .inspect-stack {
    display: grid;
    gap: 1px;
  }

  .stack-frame {
    display: flex;
    align-items: baseline;
    gap: 8px;
    padding: 3px 6px;
    border-left: 2px solid var(--line);
    font-size: 10px;
  }

  .stack-label {
    color: var(--accent-thread);
    font-weight: 600;
    letter-spacing: 0.04em;
    font-family: var(--font-mono);
  }

  /* ── Empty/placeholder ── */
  .inspect-empty {
    padding: 14px 8px;
    color: var(--muted);
    font-style: italic;
    text-align: center;
    font-family: var(--font-mono);
  }

  /* ── Scrollbar ── */
  .inspector-body::-webkit-scrollbar {
    width: 3px;
  }

  .inspector-body::-webkit-scrollbar-thumb {
    background: var(--scrollbar);
  }

  @media (prefers-reduced-motion: reduce) {
    .inspector-blade {
      transition: none;
    }
  }
</style>
