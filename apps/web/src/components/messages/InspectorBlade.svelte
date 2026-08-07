<script lang="ts">
  /**
   * PROJECT LOGOS — Deep-Inspection Split-Blade (Track B)
   *
   * Inline inspector panel that slides open within the message grid when
   * triggered by clicking the [CONF: 0.XX] tag. No modal, no popup.
   *
   * Contents per spec §8.5:
   *  - System telemetry (token latency, total tokens, intent vector score, model)
   *  - Memory citations (#NODE-881, #NODE-304)
   *  - Token logprobs
   *  - Raw JSON payload dump
   *  - Execution stack (intent parser → persona engine → transform steps)
   *
   * Closes on Escape or second click of confidence tag.
   * Adjacent layout shifts instantly (100-150ms linear).
   */

  import type { Message } from "../../lib/types";
  import { getClusterId, getClusterLabel } from "../../lib/semanticThreads";

  interface Props {
    message: Message;
    /** Measured render latency in ms. */
    latencyMs?: number | null;
    onClose: () => void;
  }

  let {
    message,
    latencyMs = null,
    onClose,
  }: Props = $props();

  // ── Derived inspection data ──

  let activeTab = $state<"telemetry" | "memory" | "logprobs" | "payload" | "stack" | "thread">("telemetry");

  // Extract metadata
  const meta = $derived(message.metadata_json ?? {});
  const telemetry = $derived((meta as any)?.telemetry as Record<string, unknown> | undefined);
  const transformHistory = $derived(message.transform_history_json ?? []);

  // Telemetry (from metadata_json.telemetry, patched by analyzeAndPersist)
  const tokenTotal = $derived(telemetry?.total_tokens as number | undefined);
  const tokenLatency = $derived(telemetry?.latency_ms as number | undefined);
  const modelName = $derived(telemetry?.model as string | undefined);
  const execStackMeta = $derived(telemetry?.execution_stack as string[] | undefined);
  const intentScore = $derived(message.confidence ?? null);

  // Memory citations (from telemetry.memory_citations or context_tags)
  const memoryNodes = $derived.by(() => {
    // Check telemetry memory_citations first
    const citations = telemetry?.memory_citations as string[] | undefined;
    if (citations?.length) return citations.map((t: string) => ({ id: t, label: t }));
    // Fallback: context_tags
    const tags = (meta as any)?.context_tags as string[] | undefined;
    if (tags?.length) return tags.map((t: string) => ({ id: t, label: t }));
    // Check for memory nodes from metadata
    const nodes = (meta as any)?.memory_nodes as Array<{ id: string }> | undefined;
    if (nodes?.length) return nodes.map((n: { id: string }) => ({ id: n.id, label: n.id }));
    return [];
  });

  // Logprobs
  const logprobs = $derived((meta as any)?.logprobs as Array<{ token: string; prob: number }> | undefined);

  // Execution stack
  const execStack = $derived.by(() => {
    const steps: Array<{ label: string; detail?: string }> = [];
    if (execStackMeta?.length) {
      for (const step of execStackMeta) {
        steps.push({ label: step });
      }
    } else {
      if (message.intent) {
        steps.push({ label: "intent_parser", detail: `classified as ${message.intent}` });
      } else {
        steps.push({ label: "intent_parser", detail: "not classified" });
      }
      if (message.persona) {
        steps.push({ label: "persona_engine", detail: `active: ${message.persona}` });
      }
      for (const entry of transformHistory) {
        steps.push({
          label: `transform.${entry.op}`,
          detail: entry.timestamp
            ? new Date(entry.timestamp).toISOString()
            : undefined,
        });
      }
      if (message.execution_status) {
        steps.push({ label: "execution", detail: message.execution_status });
      }
    }
    return steps;
  });

  // Semantic thread info for THREAD tab
  const clusterId = $derived(getClusterId(message.id));
  const clusterLabel = $derived(clusterId ? getClusterLabel(message.id) : null);

  // Raw payload for dump
  const rawPayload = $derived(JSON.stringify({
    id: message.id,
    intent: message.intent,
    persona: message.persona,
    confidence: message.confidence,
    thread_id: message.thread_root_id,
    semantic_thread_id: message.semantic_thread_id,
    execution_status: message.execution_status,
    body: message.body?.slice(0, 500) + (message.body && message.body.length > 500 ? "…" : ""),
    created_at: message.created_at,
    edited_at: message.edited_at,
    metadata: meta,
    transform_history: transformHistory,
  }, null, 2));

  // ── Keyboard: close on Escape ──
  function handleKeydown(e: KeyboardEvent) {
    if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      onClose();
    }
  }

  const tabs: Array<{ id: typeof activeTab; label: string }> = [
    { id: "telemetry", label: "TELEMETRY" },
    { id: "memory", label: "MEMORY" },
    { id: "logprobs", label: "LOGPROBS" },
    { id: "payload", label: "PAYLOAD" },
    { id: "stack", label: "STACK" },
    { id: "thread", label: "THREAD" },
  ];
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="inspector-blade" onkeydown={handleKeydown}>
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
        <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" d="M6 6l12 12M18 6L6 18"/>
      </svg>
    </button>
  </div>

  <div class="inspector-body">
    {#if activeTab === "telemetry"}
      <div class="inspector-section">
        <div class="inspector-grid">
          <div class="inspector-row">
            <span class="inspector-label">INTENT VECTOR</span>
            <span class="inspector-value">{intentScore !== null ? `${(intentScore * 100).toFixed(1)}%` : "--"}</span>
          </div>
          <div class="inspector-row">
            <span class="inspector-label">TOKEN LATENCY</span>
            <span class="inspector-value">{tokenLatency !== undefined ? `${Math.round(tokenLatency)}ms` : latencyMs !== null ? `${Math.round(latencyMs)}ms` : "--"}</span>
          </div>
          <div class="inspector-row">
            <span class="inspector-label">TOTAL TOKENS</span>
            <span class="inspector-value">{tokenTotal !== undefined ? String(tokenTotal) : "--"}</span>
          </div>
          <div class="inspector-row">
            <span class="inspector-label">MODEL</span>
            <span class="inspector-value mono">{modelName ?? "--"}</span>
          </div>
          <div class="inspector-row">
            <span class="inspector-label">RENDER LATENCY</span>
            <span class="inspector-value">{latencyMs !== null ? `${Math.round(latencyMs)}ms` : "--"}</span>
          </div>
        </div>
      </div>

    {:else if activeTab === "memory"}
      <div class="inspector-section">
        {#if memoryNodes.length > 0}
          <div class="inspector-memory-list">
            {#each memoryNodes as node}
              <div class="inspector-memory-node">
                <span class="node-marker">#NODE-{node.id.slice(0, 8)}</span>
                <span class="node-label">{node.label}</span>
              </div>
            {/each}
          </div>
        {:else}
          <div class="inspector-empty">#NODE---</div>
        {/if}
      </div>

    {:else if activeTab === "logprobs"}
      <div class="inspector-section">
        {#if logprobs && logprobs.length > 0}
          <div class="inspector-logprobs">
            {#each logprobs as lp}
              <div class="logprob-row">
                <span class="logprob-token">"{lp.token}"</span>
                <span class="logprob-prob">({lp.prob.toFixed(4)})</span>
              </div>
            {/each}
          </div>
        {:else}
          <div class="inspector-empty">[logprobs: n/a]</div>
        {/if}
      </div>

    {:else if activeTab === "payload"}
      <div class="inspector-section">
        <pre class="inspector-json">{rawPayload}</pre>
      </div>

    {:else if activeTab === "stack"}
      <div class="inspector-section">
        <div class="inspector-stack">
          {#each execStack as step}
            <div class="stack-frame">
              <span class="stack-label">{step.label}()</span>
              {#if step.detail}
                <span class="stack-detail">{step.detail}</span>
              {/if}
            </div>
          {/each}
        </div>
      </div>

    {:else if activeTab === "thread"}
      <div class="inspector-section">
        <div class="inspector-grid">
          <div class="inspector-row">
            <span class="inspector-label">SEMANTIC THREAD</span>
            <span class="inspector-value mono">{message.semantic_thread_id ? `th:${message.semantic_thread_id.slice(0, 8)}` : "--"}</span>
          </div>
          <div class="inspector-row">
            <span class="inspector-label">CLUSTER</span>
            <span class="inspector-value mono">{clusterLabel ?? "--"}</span>
          </div>
          <div class="inspector-row">
            <span class="inspector-label">THREAD ROOT</span>
            <span class="inspector-value mono">{message.thread_root_id ? `#${message.thread_root_id.slice(0, 12)}` : "--"}</span>
          </div>
          <div class="inspector-row">
            <span class="inspector-label">TOPIC</span>
            <span class="inspector-value mono">{message.topic_id ? `${message.topic_id.slice(0, 12)}` : "--"}</span>
          </div>
        </div>
      </div>
    {/if}
  </div>
</div>

<style>
  .inspector-blade {
    margin-top: 4px;
    border: 1px solid var(--line-strong);
    background: var(--panel);
    font-family: var(--font-mono);
    font-size: 10px;
    line-height: 1.45;
    overflow: hidden;
    animation: blade-open var(--motion-med) linear;
    /* Adjacent layout shift: 100-150ms linear, no easing */
  }

  @keyframes blade-open {
    from {
      opacity: 0;
      max-height: 0;
    }
    to {
      opacity: 1;
      max-height: 400px;
    }
  }

  /* ── Tabs ── */
  .inspector-tabs {
    display: flex;
    align-items: center;
    gap: 0;
    border-bottom: 1px solid var(--line);
    background: var(--panel-2);
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
    outline-offset: -1px;
  }

  .inspector-close {
    margin-left: auto;
    display: grid;
    place-items: center;
    width: 24px;
    height: 24px;
    border: 0;
    background: transparent;
    color: var(--muted);
    cursor: pointer;
  }

  .inspector-close:hover {
    background: var(--hover-strong);
    color: var(--text-strong);
  }

  /* ── Body ── */
  .inspector-body {
    padding: 6px 8px;
    max-height: 280px;
    overflow-y: auto;
  }

  /* ── Sections ── */
  .inspector-section {
    min-height: 40px;
  }

  .inspector-grid {
    display: grid;
    gap: 2px;
  }

  .inspector-row {
    display: grid;
    grid-template-columns: 120px 1fr;
    gap: 8px;
    padding: 2px 0;
    border-bottom: 1px solid color-mix(in srgb, var(--line) 40%, transparent);
  }

  .inspector-row:last-child {
    border-bottom: 0;
  }

  .inspector-label {
    color: var(--muted);
    font-size: 9px;
    font-weight: 600;
    letter-spacing: 0.05em;
    text-transform: uppercase;
  }

  .inspector-value {
    color: var(--text);
    font-size: 10px;
  }

  .inspector-value.mono {
    font-family: var(--font-mono);
  }

  /* ── Memory citations ── */
  .inspector-memory-list {
    display: grid;
    gap: 3px;
  }

  .inspector-memory-node {
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
    flex-shrink: 0;
  }

  .node-label {
    color: var(--muted-2);
    font-size: 10px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* ── Logprobs ── */
  .inspector-logprobs {
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
  .inspector-json {
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
    max-height: 240px;
    overflow-y: auto;
  }

  /* ── Execution stack ── */
  .inspector-stack {
    display: grid;
    gap: 1px;
  }

  .stack-frame {
    display: flex;
    align-items: baseline;
    gap: 8px;
    padding: 2px 6px;
    border-left: 2px solid var(--line);
    font-size: 10px;
  }

  .stack-label {
    color: var(--accent-thread);
    font-weight: 600;
    letter-spacing: 0.04em;
    flex-shrink: 0;
  }

  .stack-detail {
    color: var(--muted-2);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* ── Empty/placeholder ── */
  .inspector-empty {
    padding: 12px 8px;
    color: var(--muted);
    font-style: italic;
    text-align: center;
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
      animation: none;
    }
  }
</style>
