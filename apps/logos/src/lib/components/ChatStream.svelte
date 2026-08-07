<script lang="ts">
  // PROJECT LOGOS — ChatStream: the ONE piece inherited from clickclack
  //
  // Renders channel picker (mono, minimal), message list (flat rows, dense
  // typography, mono metadata line per message), and composer (Enter to send).
  //
  // Operator aesthetic: no bubbles, 0px radius, mono meta, 2px functional accents.
  // Props: none required; reads chatState store.
  //
  // Graceful states: booting (spinner text), error (mono error line), empty channel.

  import { onMount } from "svelte";
  import { chatState, boot, selectChannel, selectWorkspace, sendMessage } from "$lib/clickclack/chat";
  import type { Channel } from "$lib/clickclack/types";
  import type { MessageMetadata } from "$lib/clickclack/types";
  import MessageFrame, { type LogosMessage } from "$lib/components/MessageFrame.svelte";
  import InspectorBlade from "$lib/components/InspectorBlade.svelte";
  import { activeMessageId } from "$lib/ui";

  // ── Local state ──────────────────────────────────────────────

  let composerText = $state("");
  let composerRef: HTMLTextAreaElement | null = $state(null);
  let messageListRef: HTMLDivElement | null = $state(null);
  let inspecting: LogosMessage | null = $state(null);

  // ── Derived from chatState ───────────────────────────────────

  let snapshot = $state($chatState);
  $effect(() => {
    const unsub = chatState.subscribe((v) => {
      snapshot = v;
      // Auto-scroll to bottom on new messages
      if (messageListRef && v.messages.length > 0) {
        requestAnimationFrame(() => {
          messageListRef?.scrollTo({ top: messageListRef.scrollHeight });
        });
      }
    });
    return unsub;
  });

  // ── Metadata extraction (spec §8.4) ──────────────────────────

  function messageMeta(msg: { body?: string } & Record<string, unknown>): MessageMetadata {
    // Metadata is stored on the message object via PATCH /api/messages/{id}/metadata
    // The clickclack API stores it in the message.metadata field (JSON object).
    const raw = (msg as Record<string, unknown>).metadata;
    if (raw && typeof raw === "object") return raw as MessageMetadata;
    return {};
  }

  function metaLine(msg: { body?: string } & Record<string, unknown>): string {
    const m = messageMeta(msg);
    const parts: string[] = [];
    if (m.intent) parts.push(`[${m.intent.toUpperCase()}]`);
    if (m.persona) parts.push(`[${m.persona.toUpperCase()}]`);
    if (m.confidence !== undefined) parts.push(`[CONF: ${(m.confidence * 100).toFixed(1)}%]`);
    if (m.thread_id) parts.push(`[THREAD: ${m.thread_id}]`);
    if (m.latency_ms !== undefined) parts.push(`[${m.latency_ms}ms]`);
    return parts.join(" ");
  }

  function intentColor(intent: string | undefined): string {
    const map: Record<string, string> = {
      ask: "var(--intent-ask)",
      command: "var(--intent-command)",
      reflect: "var(--intent-reflect)",
      draft: "var(--intent-draft)",
      clarify: "var(--intent-clarify)",
      explore: "var(--intent-explore)",
    };
    return map[intent ?? ""] ?? "var(--intent-default)";
  }

  // ── Actions ──────────────────────────────────────────────────

  function onSubmit() {
    const text = composerText.trim();
    if (!text) return;
    const chId = snapshot.activeChannelId;
    if (!chId) return;
    sendMessage(chId, text);
    composerText = "";
    composerRef?.focus();
  }

  function onKeyDown(e: KeyboardEvent) {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      onSubmit();
    }
  }

  function onChannelClick(ch: Channel) {
    selectChannel(ch.id);
  }

  function onWsChange(e: Event) {
    const sel = (e.target as HTMLSelectElement).value;
    if (sel && sel !== snapshot.activeWorkspaceId) {
      selectWorkspace(sel);
    }
  }

  function messageToLogos(msg: { body?: string } & Record<string, unknown>): LogosMessage {
    const m = messageMeta(msg);
    return {
      id: String(msg.id ?? ""),
      body: String(msg.body ?? ""),
      intent: m.intent ?? null,
      persona: m.persona ?? null,
      confidence: m.confidence ?? null,
      thread_id: m.thread_id ?? null,
      execution_status: m.execution_status ?? null,
      metadata_json: (m as Record<string, unknown>) ?? null,
      created_at: msg.created_at ? String(msg.created_at) : null,
    };
  }

  function onInspect(msg: LogosMessage) {
    inspecting = inspecting?.id === msg.id ? null : msg;
    activeMessageId.set(msg.id);
  }

  function closeInspector() {
    inspecting = null;
    activeMessageId.set(null);
  }

  function onRowFocus(msg: { id?: string }) {
    activeMessageId.set(String(msg.id ?? ""));
  }

  function authorLabel(msg: { author?: { display_name?: string; handle?: string } | null }): string {
    if (msg.author?.display_name) return msg.author.display_name;
    if (msg.author?.handle) return `@${msg.author.handle}`;
    return "unknown";
  }

  // ── Bootstrap ────────────────────────────────────────────────

  onMount(() => {
    boot();
  });
</script>

<div class="chatstream">
  <!-- ═══ TOP BAR: workspace + channel picker ═══ -->
  <div class="cs-topbar logos-mono">
    {#if snapshot.workspaces.length > 0}
      <select
        class="cs-select"
        value={snapshot.activeWorkspaceId ?? ""}
        onchange={onWsChange}
        aria-label="Workspace"
      >
        {#each snapshot.workspaces as ws (ws.id)}
          <option value={ws.id}>{ws.name}</option>
        {/each}
      </select>
      <span class="cs-sep">/</span>
    {/if}
    <div class="cs-channels">
      {#each snapshot.channels as ch (ch.id)}
        <button
          class="cs-channel-btn"
          class:active={ch.id === snapshot.activeChannelId}
          onclick={() => onChannelClick(ch)}
        >
          # {ch.name}
        </button>
      {/each}
    </div>
    <span class="cs-spacer"></span>
    {#if snapshot.status === "booting"}
      <span class="cs-status-booting">CONNECTING…</span>
    {:else if snapshot.status === "error"}
      <span class="cs-status-error" title={snapshot.error ?? ""}>ERR</span>
    {:else if snapshot.activeChannelId}
      <span class="cs-status-ready accent-verified">LIVE</span>
    {/if}
  </div>

  <!-- ═══ MESSAGE LIST ═══ -->
  <div class="cs-messages" bind:this={messageListRef}>
    {#if snapshot.status === "booting"}
      <div class="cs-state logos-mono">SUBSTRATE BOOTING — awaiting workspace connection…</div>
    {:else if snapshot.status === "error"}
      <div class="cs-state cs-error logos-mono">
        <span class="accent-intent">[ERR]</span> {snapshot.error ?? "Unknown error"}
      </div>
    {:else if snapshot.messages.length === 0}
      <div class="cs-state logos-mono">
        {snapshot.activeChannelId
          ? "No messages in this channel."
          : "Select a channel to begin."}
      </div>
    {:else}
      {#each snapshot.messages as msg (msg.id)}
        <div
          class="cs-row"
          data-msg-id={msg.id}
          class:cs-active={$activeMessageId === msg.id}
          onmouseenter={() => onRowFocus(msg)}
        >
          <MessageFrame message={messageToLogos(msg)} onInspect={onInspect} />
          {#if inspecting?.id === msg.id}
            <InspectorBlade message={inspecting} open={true} onClose={closeInspector} />
          {/if}
        </div>
      {/each}
    {/if}
  </div>

  <!-- ═══ COMPOSER ═══ -->
  {#if snapshot.status === "ready" && snapshot.activeChannelId}
    <div class="cs-composer">
      <textarea
        bind:this={composerRef}
        bind:value={composerText}
        class="cs-input logos-mono"
        placeholder="Message…"
        rows={2}
        onkeydown={onKeyDown}
      ></textarea>
      <button class="cs-send-btn logos-mono" onclick={onSubmit} disabled={!composerText.trim()}>
        SEND
      </button>
    </div>
  {/if}
</div>

<style>
  .chatstream {
    display: grid;
    grid-template-rows: 32px minmax(0, 1fr) auto;
    height: 100%;
    background: var(--bg);
  }

  /* ── Top bar ──────────────────────────────── */
  .cs-topbar {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 0 8px;
    border-bottom: 1px solid var(--line);
    background: var(--panel);
    overflow: hidden;
  }
  .cs-select {
    background: var(--panel-2);
    color: var(--text);
    border: 1px solid var(--line-strong);
    font-family: var(--font-mono);
    font-size: 11px;
    padding: 2px 6px;
    max-width: 160px;
    cursor: pointer;
  }
  .cs-sep {
    color: var(--muted-2);
    font-family: var(--font-mono);
    font-size: 11px;
  }
  .cs-channels {
    display: flex;
    gap: 2px;
    overflow-x: auto;
  }
  .cs-channel-btn {
    background: transparent;
    border: 1px solid var(--line);
    color: var(--muted);
    font-family: var(--font-mono);
    font-size: 11px;
    padding: 2px 8px;
    cursor: pointer;
    white-space: nowrap;
    transition: color var(--motion-fast), border-color var(--motion-fast);
  }
  .cs-channel-btn:hover,
  .cs-channel-btn.active {
    color: var(--text-strong);
    border-color: var(--line-strong);
  }
  .cs-channel-btn.active {
    background: var(--panel-2);
  }
  .cs-spacer { flex: 1; }
  .cs-status-booting {
    color: var(--muted-2);
    animation: cs-pulse 1.5s steps(2, jump-none) infinite;
  }
  .cs-status-error { color: var(--accent-intent); }
  .cs-status-ready { font-weight: 700; }
  @keyframes cs-pulse {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.4; }
  }

  /* ── Message list ─────────────────────────── */
  .cs-messages {
    overflow-y: auto;
    padding: 0;
    min-height: 0;
  }
  .cs-state {
    padding: 20px;
    color: var(--muted);
    text-align: center;
    line-height: 1.7;
  }
  .cs-error {
    color: var(--intent-clarify);
  }

  /* ── Message row ──────────────────────────── */
  .cs-row {
    display: flex;
    border-bottom: 1px solid var(--line);
    min-height: 0;
  }
  .cs-row:last-child {
    border-bottom: none;
  }
  .cs-intent-bar {
    width: 2px;
    flex-shrink: 0;
    background: var(--intent-bar-color, var(--intent-default));
  }
  .cs-row-body {
    flex: 1;
    min-width: 0;
    padding: 8px 12px;
  }
  .cs-meta {
    color: var(--muted-2);
    margin-bottom: 4px;
    letter-spacing: 0.03em;
    word-break: break-all;
  }
  .cs-byline {
    display: flex;
    gap: 10px;
    align-items: baseline;
    margin-bottom: 4px;
    color: var(--muted);
  }
  .cs-author {
    color: var(--text-strong);
    font-weight: 600;
  }
  .cs-time {
    color: var(--muted-2);
  }
  .cs-body {
    color: var(--text);
    line-height: 1.6;
    white-space: pre-wrap;
    word-break: break-word;
    font-size: 13px;
  }

  /* ── Composer ─────────────────────────────── */
  .cs-composer {
    display: flex;
    gap: 0;
    border-top: 1px solid var(--line);
    background: var(--panel);
    padding: 6px 8px;
  }
  .cs-input {
    flex: 1;
    background: var(--panel-2);
    border: 1px solid var(--line-strong);
    color: var(--text);
    font-family: var(--font-mono);
    font-size: 12px;
    padding: 6px 8px;
    resize: none;
    outline: none;
    line-height: 1.5;
  }
  .cs-input:focus {
    border-color: var(--accent-intent);
  }
  .cs-input::placeholder {
    color: var(--muted-2);
  }
  .cs-send-btn {
    background: var(--panel-2);
    border: 1px solid var(--line-strong);
    border-left: none;
    color: var(--muted);
    font-family: var(--font-mono);
    font-size: 11px;
    padding: 4px 14px;
    cursor: pointer;
    transition: color var(--motion-fast), border-color var(--motion-fast);
    white-space: nowrap;
  }
  .cs-send-btn:hover:not(:disabled) {
    color: var(--accent-intent);
    border-color: var(--accent-intent);
  }
  .cs-send-btn:disabled {
    opacity: 0.3;
    cursor: default;
  }
</style>
