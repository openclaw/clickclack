<script lang="ts">
  /**
   * PROJECT LOGOS — Semantic Thread & Memory Graph Pane (Track B)
   *
   * Reuses the col 4 grid slot (same as thread/search/artifact).
   * Two tabs: THREADS | MEMORY.
   *
   * THREADS: cluster button, cluster list, cross-thread memory retrieval.
   * MEMORY: anchor list, "anchor current message" affordance.
   *
   * Chassis aesthetic: monochrome, 0px radius, mono, 2px accents.
   */

  import type { Message } from "../../lib/types";
  import {
    runClustering,
    getClusters,
    getClusterLabel,
    getClusterMessageCount,
    getClusterMessageIds,
    isRunning as isClustering,
    clear as clearClusters,
  } from "../../lib/semanticThreads";
  import {
    memoryQuery,
    memoryAnchor,
    listMemoryAnchors,
    type MemoryNode,
    cognitionAvailable,
  } from "../../lib/cognition";
  import { semanticPaneTab, activeMessageId } from "../../lib/ui";
  import { get } from "svelte/store";

  interface Props {
    messages: Message[];
    onClose: () => void;
    onScrollToMessage?: (messageId: string) => void;
  }

  let {
    messages,
    onClose,
    onScrollToMessage,
  }: Props = $props();

  // ── Tab state ──
  let activeTab = $state<"threads" | "memory">("threads");
  $effect(() => {
    semanticPaneTab.set(activeTab);
  });

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
  let anchoring = $state(false);
  let anchorSuccess = $state("");

  // ── Derived ──
  let clusters = $derived(getClusters());
  let hasClusters = $derived(clusters.length > 0);
  let hasSearchResults = $derived(searchResults.length > 0);
  let available = $derived(cognitionAvailable());

  // ── Scroll to first message in a cluster ──
  function focusCluster(clusterId: string) {
    const ids = getClusterMessageIds(clusterId);
    const firstId = ids[0];
    if (firstId) {
      activeMessageId.set(firstId);
      onScrollToMessage?.(firstId);
    }
  }

  // ── Run clustering ──
  async function handleCluster() {
    if (clustering) return;
    clustering = true;
    try {
      clearClusters();
      const items = messages
        .filter(m => m.body && !m.deleted_at && !m.preamble_block)
        .map(m => ({ id: m.id, content: m.body }));
      await runClustering(items);
    } finally {
      clustering = false;
    }
  }

  // ── Cross-thread retrieval ──
  async function handleSearch() {
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

  function scrollToNode(node: MemoryNode) {
    // Try to match by content in visible messages
    const matched = messages.find(m => m.body === node.content);
    if (matched) {
      activeMessageId.set(matched.id);
      onScrollToMessage?.(matched.id);
    }
  }

  // ── Load memory anchors ──
  async function loadAnchors() {
    anchorsLoading = true;
    anchorsError = "";
    try {
      const result = await listMemoryAnchors(20);
      if (result?.nodes) {
        anchors = result.nodes;
      } else {
        anchors = [];
      }
    } catch (err) {
      anchorsError = "Could not load anchors";
    } finally {
      anchorsLoading = false;
    }
  }

  // Initial load when tab switches to memory
  $effect(() => {
    if (activeTab === "memory" && anchors.length === 0 && !anchorsLoading) {
      void loadAnchors();
    }
  });

  // ── Anchor current message ──
  async function handleAnchor(content: string, messageId: string) {
    if (anchoring) return;
    anchoring = true;
    anchorSuccess = "";
    try {
      const result = await memoryAnchor(content, messageId);
      if (result?.id) {
        anchorSuccess = `Anchored as ${result.id.slice(0, 8)}`;
        void loadAnchors();
      }
    } catch {
      anchorError = "Anchor failed";
    } finally {
      anchoring = false;
    }
  }

  let anchorError = $state("");

  // ── Keyboard ──
  function handleKeydown(e: KeyboardEvent) {
    if (e.key === "Escape") {
      e.preventDefault();
      onClose();
    }
  }
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<aside
  class="semantic-pane open"
  onkeydown={handleKeydown}
  aria-label="Semantic thread pane"
  role="complementary"
>
  <!-- Tab strip -->
  <div class="semantic-pane-tabs">
    <button
      type="button"
      class="semantic-pane-tab"
      class:active={activeTab === "threads"}
      onclick={() => (activeTab = "threads")}
    >THREADS</button>
    <button
      type="button"
      class="semantic-pane-tab"
      class:active={activeTab === "memory"}
      onclick={() => (activeTab = "memory")}
    >MEMORY</button>
    <button
      type="button"
      class="semantic-pane-close"
      onclick={onClose}
      aria-label="Close semantic pane"
      title="Close semantic pane (Esc)"
    >
      <svg viewBox="0 0 24 24" width="11" height="11" aria-hidden="true">
        <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" d="M6 6l12 12M18 6L6 18"/>
      </svg>
    </button>
  </div>

  <!-- ── THREADS tab ── -->
  {#if activeTab === "threads"}
    <div class="semantic-pane-body">
      <!-- Cluster button -->
      <button
        type="button"
        class="semantic-pane-btn"
        disabled={clustering || !available}
        onclick={handleCluster}
      >
        {clustering ? "CLUSTERING…" : "CLUSTER WORKSPACE"}
      </button>

      <!-- Cross-thread retrieval -->
      <div class="semantic-pane-search">
        <input
          type="text"
          class="semantic-pane-input"
          placeholder="thread retrieval query…"
          bind:value={queryText}
          disabled={searching || !available}
          onkeydown={(e: KeyboardEvent) => {
            if (e.key === "Enter") void handleSearch();
          }}
        />
      </div>

      <!-- Search results -->
      {#if hasSearchResults}
        <div class="semantic-pane-section">
          <div class="semantic-pane-section-title">RETRIEVAL RESULTS</div>
          {#each searchResults as node}
            <button
              type="button"
              class="semantic-pane-node"
              onclick={() => scrollToNode(node)}
            >
              <span class="node-marker">#NODE-{node.content.slice(0, 8)}</span>
              <span class="node-score">({node.score.toFixed(2)})</span>
              <span class="node-preview">{node.content.slice(0, 60)}</span>
            </button>
          {/each}
        </div>
      {:else if searching}
        <div class="semantic-pane-empty">searching…</div>
      {:else if searchError}
        <div class="semantic-pane-empty is-error">{searchError}</div>
      {/if}

      <!-- Cluster list -->
      {#if hasClusters}
        <div class="semantic-pane-section">
          <div class="semantic-pane-section-title">SEMANTIC THREADS</div>
          {#each clusters as cluster (cluster.id)}
            <button
              type="button"
              class="semantic-pane-cluster"
              onclick={() => focusCluster(cluster.id)}
            >
              <span class="cluster-label">{cluster.label}</span>
              <span class="cluster-count">({getClusterMessageCount(cluster.id)} msgs)</span>
            </button>
          {/each}
        </div>
      {:else if !clustering && !hasSearchResults}
        <div class="semantic-pane-empty">No clusters yet. Click CLUSTER WORKSPACE to analyze the current chat.</div>
      {/if}
    </div>

  <!-- ── MEMORY tab ── -->
  {:else}
    <div class="semantic-pane-body">
      <div class="semantic-pane-section-title">MEMORY ANCHORS</div>

      {#if anchorsLoading}
        <div class="semantic-pane-empty">loading…</div>
      {:else if anchorsError}
        <div class="semantic-pane-empty is-error">{anchorsError}</div>
      {:else if anchors.length === 0}
        <div class="semantic-pane-empty">
          No anchors found. Use "anchor current message" from a message row to pin content.
        </div>
      {:else}
        {#each anchors as node, i}
          <div class="semantic-pane-node-wrap">
            <button
              type="button"
              class="semantic-pane-node"
              onclick={() => scrollToNode(node)}
            >
              <span class="node-marker">#NODE-{String(i + 1).padStart(2, '0')}</span>
              <span class="node-score">({node.score.toFixed(2)})</span>
              <span class="node-preview">{node.content.slice(0, 60)}</span>
            </button>
          </div>
        {/each}
      {/if}

      <!-- Anchor status -->
      {#if anchorSuccess}
        <div class="semantic-pane-status">{anchorSuccess}</div>
      {:else if anchorError}
        <div class="semantic-pane-status is-error">{anchorError}</div>
      {/if}
    </div>
  {/if}
</aside>

<style>
  .semantic-pane {
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
  .semantic-pane-tabs {
    display: flex;
    align-items: center;
    gap: 0;
    border-bottom: 1px solid var(--line);
    background: var(--panel-2);
  }

  .semantic-pane-tab {
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

  .semantic-pane-tab:hover {
    background: var(--hover-strong);
    color: var(--text);
  }

  .semantic-pane-tab.active {
    background: var(--panel);
    color: var(--text-strong);
  }

  .semantic-pane-close {
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

  .semantic-pane-close:hover {
    background: var(--hover-strong);
    color: var(--text-strong);
  }

  /* ── Body ── */
  .semantic-pane-body {
    padding: 8px 10px;
    overflow-y: auto;
    display: grid;
    gap: 8px;
    align-content: start;
  }

  /* ── Button ── */
  .semantic-pane-btn {
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

  .semantic-pane-btn:hover:not(:disabled) {
    background: var(--accent-thread);
    color: var(--bg);
    border-color: var(--accent-thread);
  }

  .semantic-pane-btn:disabled {
    opacity: 0.5;
    cursor: wait;
  }

  /* ── Search input ── */
  .semantic-pane-search {
    display: flex;
  }

  .semantic-pane-input {
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

  .semantic-pane-input:focus {
    border-color: var(--text-strong);
    background: var(--panel);
  }

  .semantic-pane-input::placeholder {
    color: var(--muted);
  }

  /* ── Section title ── */
  .semantic-pane-section {
    display: grid;
    gap: 4px;
  }

  .semantic-pane-section-title {
    padding: 4px 0;
    color: var(--muted);
    font-size: 9px;
    font-weight: 700;
    letter-spacing: 0.06em;
    border-bottom: 1px solid var(--line);
    margin-bottom: 2px;
  }

  /* ── Cluster entries ── */
  .semantic-pane-cluster {
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

  .semantic-pane-cluster:hover {
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
  .semantic-pane-node-wrap {
    display: grid;
  }

  .semantic-pane-node {
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

  .semantic-pane-node:hover {
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
  .semantic-pane-empty {
    padding: 16px 8px;
    color: var(--muted);
    text-align: center;
    font-size: 10px;
    line-height: 1.5;
  }

  .semantic-pane-empty.is-error {
    color: var(--status-danger);
  }

  .semantic-pane-status {
    padding: 4px 8px;
    border-left: 2px solid var(--accent-verified);
    background: var(--panel-2);
    color: var(--accent-verified);
    font-size: 10px;
    font-weight: 600;
  }

  .semantic-pane-status.is-error {
    border-left-color: var(--status-danger);
    color: var(--status-danger);
  }

  /* ── Scrollbar ── */
  .semantic-pane-body::-webkit-scrollbar {
    width: 3px;
  }

  .semantic-pane-body::-webkit-scrollbar-thumb {
    background: var(--scrollbar);
  }
</style>
