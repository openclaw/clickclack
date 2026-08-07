<script lang="ts">
  /**
   * PROJECT LOGOS — Semantic Thread & Memory Graph Pane
   *
   * Two tabs: THREADS | MEMORY.
   *
   * THREADS — "CLUSTER WORKSPACE" button + cluster list (CL-XX, 2px --accent-thread border)
   *           + cross-thread retrieval input → memory query → #NODE-XX (score) preview.
   * MEMORY  — anchor list from listMemoryAnchors, #NODE-XX (score) rows.
   *
   * Props:
   *   messages      — { id: string; content: string }[]
   *   onClose       — () => void
   *   onFocusMessage — (messageId: string) => void
   */

  import {
    runClustering,
    getClusters,
    getClusterMessageCount,
    getClusterMessageIds,
    isRunning as isClustering,
    clear as clearClusters,
  } from "../semanticThreads";
  import {
    memoryQuery,
    listMemoryAnchors,
    type MemoryNode,
  } from "../cognition";

  // ── Props ──

  interface Props {
    messages: Array<{ id: string; content: string }>;
    onClose: () => void;
    onFocusMessage: (messageId: string) => void;
  }

  let { messages, onClose, onFocusMessage }: Props = $props();

  // ── Tab state ──

  type Tab = "threads" | "memory";
  let activeTab = $state<Tab>("threads");

  // ── THREADS tab state ──

  let clustering = $state(false);
  let queryText = $state("");
  let searchResults = $state<MemoryNode[]>([]);
  let searching = $state(false);
  let searchError = $state("");

  // ── MEMORY tab state ──

  let anchors = $state<MemoryNode[]>([]);
  let anchorsLoading = $state(false);
  let anchorsError = $state("");

  // ── Derived ──

  let clusters = $derived(getClusters());
  let hasClusters = $derived(clusters.length > 0);
  let hasSearchResults = $derived(searchResults.length > 0);

  // ── Scroll to first message in a cluster ──

  function focusCluster(clusterId: string): void {
    const ids = getClusterMessageIds(clusterId);
    const firstId = ids[0];
    if (firstId) {
      onFocusMessage(firstId);
    }
  }

  // ── Run clustering ──

  async function handleCluster(): Promise<void> {
    if (clustering) return;
    clustering = true;
    try {
      clearClusters();
      const items = messages
        .filter((m) => m.content && m.content.trim().length > 0)
        .map((m) => ({ id: m.id, content: m.content }));
      await runClustering(items);
    } finally {
      clustering = false;
    }
  }

  // ── Cross-thread retrieval ──

  async function handleSearch(): Promise<void> {
    const q = queryText.trim();
    if (!q || searching) return;
    searching = true;
    searchError = "";
    searchResults = [];
    try {
      const result = await memoryQuery(q);
      if (result?.nodes) {
        searchResults = result.nodes;
      }
    } catch (err) {
      searchError = "Search failed";
    } finally {
      searching = false;
    }
  }

  // ── Scroll to a node by content match ──

  function focusNode(node: MemoryNode): void {
    const matched = messages.find((m) => m.content === node.content);
    if (matched) {
      onFocusMessage(matched.id);
    }
  }

  // ── Load memory anchors ──

  async function loadAnchors(): Promise<void> {
    anchorsLoading = true;
    anchorsError = "";
    try {
      const result = await listMemoryAnchors(20);
      if (result?.nodes) {
        anchors = result.nodes;
      } else {
        anchors = [];
      }
    } catch {
      anchorsError = "Could not load anchors";
    } finally {
      anchorsLoading = false;
    }
  }

  // Auto-load when switching to memory tab
  $effect(() => {
    if (activeTab === "memory" && anchors.length === 0 && !anchorsLoading) {
      void loadAnchors();
    }
  });

  // ── Keyboard ──

  function handleKeydown(e: KeyboardEvent): void {
    if (e.key === "Escape") {
      e.preventDefault();
      onClose();
    }
  }
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<aside
  class="logos-semantic-pane"
  onkeydown={handleKeydown}
  aria-label="Semantic thread pane"
  role="complementary"
>
  <!-- Tab strip -->
  <div class="logos-pane-tabs">
    <button
      type="button"
      class="logos-pane-tab"
      class:active={activeTab === "threads"}
      onclick={() => (activeTab = "threads")}
    >THREADS</button>
    <button
      type="button"
      class="logos-pane-tab"
      class:active={activeTab === "memory"}
      onclick={() => (activeTab = "memory")}
    >MEMORY</button>
    <button
      type="button"
      class="logos-pane-close"
      onclick={onClose}
      aria-label="Close semantic pane"
      title="Close (Esc)"
    >
      <svg viewBox="0 0 24 24" width="11" height="11" aria-hidden="true">
        <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" d="M6 6l12 12M18 6L6 18"/>
      </svg>
    </button>
  </div>

  <!-- ── THREADS tab ── -->
  {#if activeTab === "threads"}
    <div class="logos-pane-body">
      <!-- Cluster button -->
      <button
        type="button"
        class="logos-pane-btn"
        disabled={clustering}
        onclick={handleCluster}
      >
        {clustering ? "CLUSTERING…" : "CLUSTER WORKSPACE"}
      </button>

      <!-- Cross-thread retrieval -->
      <div class="logos-pane-search">
        <input
          type="text"
          class="logos-pane-input"
          placeholder="thread retrieval query…"
          bind:value={queryText}
          disabled={searching}
          onkeydown={(e: KeyboardEvent) => {
            if (e.key === "Enter") void handleSearch();
          }}
        />
      </div>

      <!-- Search results -->
      {#if hasSearchResults}
        <div class="logos-pane-section">
          <div class="logos-pane-section-title">RETRIEVAL RESULTS</div>
          {#each searchResults as node}
            <button
              type="button"
              class="logos-pane-node"
              onclick={() => focusNode(node)}
            >
              <span class="node-marker">#NODE-{node.id.slice(0, 8)}</span>
              <span class="node-score">({node.score.toFixed(2)})</span>
              <span class="node-preview">{node.content.slice(0, 60)}</span>
            </button>
          {/each}
        </div>
      {:else if searching}
        <div class="logos-pane-empty">searching…</div>
      {:else if searchError}
        <div class="logos-pane-empty is-error">{searchError}</div>
      {/if}

      <!-- Cluster list -->
      {#if hasClusters}
        <div class="logos-pane-section">
          <div class="logos-pane-section-title">SEMANTIC THREADS</div>
          {#each clusters as cluster (cluster.id)}
            <button
              type="button"
              class="logos-pane-cluster"
              onclick={() => focusCluster(cluster.id)}
            >
              <span class="cluster-label">{cluster.label}</span>
              <span class="cluster-count">({getClusterMessageCount(cluster.id)} msgs)</span>
            </button>
          {/each}
        </div>
      {:else if !clustering && !hasSearchResults}
        <div class="logos-pane-empty">
          No clusters yet. Click CLUSTER WORKSPACE to analyze the current chat.
        </div>
      {/if}
    </div>

  <!-- ── MEMORY tab ── -->
  {:else}
    <div class="logos-pane-body">
      <div class="logos-pane-section-title">MEMORY ANCHORS</div>

      {#if anchorsLoading}
        <div class="logos-pane-empty">loading…</div>
      {:else if anchorsError}
        <div class="logos-pane-empty is-error">{anchorsError}</div>
      {:else if anchors.length === 0}
        <div class="logos-pane-empty">
          No anchors found. Pin content to memory to populate.
        </div>
      {:else}
        {#each anchors as node, i}
          <button
            type="button"
            class="logos-pane-node"
            onclick={() => focusNode(node)}
          >
            <span class="node-marker">#NODE-{String(i + 1).padStart(2, "0")}</span>
            <span class="node-score">({node.score.toFixed(2)})</span>
            <span class="node-preview">{node.content.slice(0, 60)}</span>
          </button>
        {/each}
      {/if}
    </div>
  {/if}
</aside>

<style>
  .logos-semantic-pane {
    display: grid;
    grid-template-rows: auto minmax(0, 1fr);
    min-width: 0;
    overflow: hidden;
    border-left: 1px solid var(--line-strong);
    background: var(--panel);
    font-family: var(--font-mono);
    font-size: 10px;
    line-height: 1.45;
  }

  /* ── Tab strip ── */
  .logos-pane-tabs {
    display: flex;
    align-items: center;
    gap: 0;
    border-bottom: 1px solid var(--line);
    background: var(--panel-2);
  }

  .logos-pane-tab {
    padding: 6px 12px;
    border: 0;
    border-right: 1px solid var(--line);
    background: transparent;
    color: var(--muted-2);
    font-family: var(--font-mono);
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.05em;
    cursor: pointer;
    line-height: 1.4;
    text-transform: uppercase;
  }

  .logos-pane-tab:hover {
    background: var(--hover-strong);
    color: var(--text);
  }

  .logos-pane-tab.active {
    background: var(--panel);
    color: var(--text-strong);
  }

  .logos-pane-close {
    margin-left: auto;
    display: grid;
    place-items: center;
    width: 28px;
    height: 28px;
    border: 0;
    background: transparent;
    color: var(--muted);
    cursor: pointer;
  }

  .logos-pane-close:hover {
    background: var(--hover-strong);
    color: var(--text-strong);
  }

  /* ── Body ── */
  .logos-pane-body {
    padding: 8px 10px;
    overflow-y: auto;
    display: grid;
    gap: 8px;
    align-content: start;
  }

  /* ── Button ── */
  .logos-pane-btn {
    display: block;
    width: 100%;
    padding: 6px 10px;
    border: 1px solid var(--line-strong);
    background: var(--panel-2);
    color: var(--text);
    font-family: var(--font-mono);
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.04em;
    cursor: pointer;
    text-transform: uppercase;
    text-align: center;
  }

  .logos-pane-btn:hover:not(:disabled) {
    background: var(--accent-thread);
    color: var(--bg);
    border-color: var(--accent-thread);
  }

  .logos-pane-btn:disabled {
    opacity: 0.5;
    cursor: wait;
  }

  /* ── Search input ── */
  .logos-pane-search {
    display: flex;
  }

  .logos-pane-input {
    flex: 1;
    min-width: 0;
    height: 28px;
    padding: 0 8px;
    border: 1px solid var(--line);
    background: var(--panel-2);
    color: var(--text);
    font-family: var(--font-mono);
    font-size: 10px;
    outline: none;
  }

  .logos-pane-input:focus {
    border-color: var(--text-strong);
    background: var(--panel);
  }

  .logos-pane-input::placeholder {
    color: var(--muted);
  }

  /* ── Section title ── */
  .logos-pane-section {
    display: grid;
    gap: 4px;
  }

  .logos-pane-section-title {
    padding: 4px 0;
    color: var(--muted);
    font-size: 9px;
    font-weight: 700;
    letter-spacing: 0.06em;
    border-bottom: 1px solid var(--line);
    margin-bottom: 2px;
  }

  /* ── Cluster entries ── */
  .logos-pane-cluster {
    display: flex;
    align-items: baseline;
    gap: 6px;
    width: 100%;
    padding: 5px 8px;
    border: 0;
    border-left: 2px solid var(--accent-thread);
    background: var(--panel-2);
    color: var(--text);
    font-family: var(--font-mono);
    font-size: 10px;
    cursor: pointer;
    text-align: left;
    line-height: 1.4;
  }

  .logos-pane-cluster:hover {
    background: var(--hover-strong);
    border-left-color: var(--text-strong);
  }

  .cluster-label {
    color: var(--accent-thread);
    font-weight: 700;
    letter-spacing: 0.03em;
  }

  .cluster-count {
    color: var(--muted);
    font-size: 9px;
  }

  /* ── Memory node entries ── */
  .logos-pane-node {
    display: flex;
    align-items: baseline;
    gap: 4px;
    width: 100%;
    padding: 4px 6px;
    border: 0;
    border-left: 2px solid var(--line);
    background: var(--panel-2);
    color: var(--text);
    font-family: var(--font-mono);
    font-size: 10px;
    cursor: pointer;
    text-align: left;
    line-height: 1.4;
  }

  .logos-pane-node:hover {
    background: var(--hover-strong);
    border-left-color: var(--accent-thread);
  }

  .node-marker {
    color: var(--accent-thread);
    font-weight: 700;
    flex-shrink: 0;
  }

  .node-score {
    color: var(--muted-2);
    font-size: 9px;
    flex-shrink: 0;
  }

  .node-preview {
    color: var(--muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }

  /* ── Empty/status states ── */
  .logos-pane-empty {
    padding: 16px 8px;
    color: var(--muted);
    text-align: center;
    font-size: 10px;
    line-height: 1.5;
  }

  .logos-pane-empty.is-error {
    color: var(--intent-clarify);
  }

  /* ── Scrollbar ── */
  .logos-pane-body::-webkit-scrollbar {
    width: 3px;
  }

  .logos-pane-body::-webkit-scrollbar-thumb {
    background: var(--scrollbar);
  }
</style>
