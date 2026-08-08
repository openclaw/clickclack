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

  import { onMount, onDestroy } from "svelte";
  import {
    chatState,
    boot,
    selectChannel,
    selectWorkspace,
    sendMessage,
    applyMessageUpdate,
  } from "$lib/clickclack/chat";
  import { updateMessageMetadata } from "$lib/clickclack/api";
  import type { Channel } from "$lib/clickclack/types";
  import {
    readMessageMetadata,
    type CognitiveMessage,
    type MessageMetadata,
    type TransformHistoryEntry,
  } from "$lib/clickclack/types";
  import MessageFrame, { type LogosMessage } from "$lib/components/MessageFrame.svelte";
  import InspectorBlade from "$lib/components/InspectorBlade.svelte";
  import ResultStrip from "$lib/components/ResultStrip.svelte";
  import ClarificationPrompt from "$lib/components/ClarificationPrompt.svelte";
  import { activeMessageId, currentPersona } from "$lib/ui";
  import { transform, memoryQuery, respond } from "$lib/cognition";

  // ── Local state ──────────────────────────────────────────────

  let composerText = $state("");
  let composerRef: HTMLTextAreaElement | null = $state(null);
  let messageListRef: HTMLDivElement | null = $state(null);
  let inspecting: LogosMessage | null = $state(null);

  // ── Transform & clarification state ───────────────────────────

  /** Active transform results: messageId → { content, op, model, loading } */
  let activeTransforms = $state<Map<string, {
    content: string;
    op: string;
    model: string | null;
    loading: boolean;
  }>>(new Map());

  /** Dismissed clarification prompts: messageId → true */
  let dismissedClarifications = $state<Set<string>>(new Set());

  /** Non-persistent body overrides from applied transforms: messageId → newBody */
  let appliedOverrides = $state<Map<string, string>>(new Map());

  let companionSuggestion = $state<{
    content: string;
    model: string | null;
    loading: boolean;
    clarificationQuestion: string | null;
  } | null>(null);

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

  let persona = $state("operator");
  $effect(() => {
    const unsub = currentPersona.subscribe((v) => (persona = v));
    return unsub;
  });

  // ── Metadata extraction (spec §8.4) ──────────────────────────

  function messageMeta(msg: Record<string, unknown>): MessageMetadata {
    return readMessageMetadata(msg);
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

  // ── Message → LogosMessage adapter ─────────────────────────────

  function messageToLogos(msg: CognitiveMessage): LogosMessage {
    const m = messageMeta(msg);
    const id = String(msg.id ?? "");
    return {
      id,
      body: appliedOverrides.get(id) ?? String(msg.body ?? ""),
      intent: m.intent ?? null,
      persona: m.persona ?? null,
      confidence: m.confidence ?? null,
      thread_id: m.thread_id ?? null,
      execution_status: m.execution_status ?? null,
      metadata_json: (m as Record<string, unknown>) ?? null,
      transform_history: m.transform_history ?? [],
      created_at: msg.created_at ? String(msg.created_at) : null,
    };
  }

  // ── Inspector ──────────────────────────────────────────────────

  function onInspect(msg: LogosMessage) {
    inspecting = inspecting?.id === msg.id ? null : msg;
    activeMessageId.set(msg.id);
  }

  function closeInspector() {
    inspecting = null;
    activeMessageId.set(null);
  }

  // ── Row focus for keyboard / hover ─────────────────────────────

  function onRowFocus(msg: { id?: string }) {
    activeMessageId.set(String(msg.id ?? ""));
  }

  function authorLabel(msg: { author?: { display_name?: string; handle?: string } | null }): string {
    if (msg.author?.display_name) return msg.author.display_name;
    if (msg.author?.handle) return `@${msg.author.handle}`;
    return "unknown";
  }

  // ── Action rail wiring: transforms + memory ────────────────────

  interface TransformEventDetail {
    op: "summarize" | "condense" | "expand" | "rewrite";
    messageId: string;
  }

  function buildTransformHistoryEntry(
    op: string,
    content: string,
    model: string | null,
  ): TransformHistoryEntry {
    return {
      op,
      at: new Date().toISOString(),
      preview: content.slice(0, 120),
      ...(persona ? { persona } : {}),
      ...(model ? { model } : {}),
    };
  }

  async function persistTransformHistory(
    message: CognitiveMessage,
    op: string,
    content: string,
    model: string | null,
  ): Promise<void> {
    const metadata = messageMeta(message);
    const nextHistory = [
      ...(metadata.transform_history ?? []),
      buildTransformHistoryEntry(op, content, model),
    ];

    try {
      const updated = await updateMessageMetadata(message.id, {
        transform_history: nextHistory,
      });
      applyMessageUpdate(updated);
    } catch (err) {
      console.warn("[logos/chatstream] transform history patch failed:", err);
      applyMessageUpdate({
        ...message,
        transform_history: nextHistory,
      } as CognitiveMessage);
    }
  }

  async function handleTransformEvent(detail: TransformEventDetail) {
    const msg = snapshot.messages.find((m) => m.id === detail.messageId) as CognitiveMessage | undefined;
    if (!msg) return;
    const body = (msg as Record<string, unknown>).body;
    if (typeof body !== "string" || !body.trim()) return;

    // Show loading strip
    activeTransforms.set(detail.messageId, { content: "", op: detail.op, model: null, loading: true });
    activeTransforms = new Map(activeTransforms); // trigger reactivity

    const result = await transform(body, detail.op, persona);
    if (result) {
      const model = (result.meta && typeof result.meta === "object"
        ? (result.meta as Record<string, unknown>).model as string | undefined
        : undefined) ?? null;
      await persistTransformHistory(msg, detail.op, result.transformed_content, model);
      activeTransforms.set(detail.messageId, {
        content: result.transformed_content,
        op: detail.op,
        model,
        loading: false,
      });
    } else {
      activeTransforms.set(detail.messageId, {
        content: "[ERR] Cognition service returned no result.",
        op: detail.op,
        model: null,
        loading: false,
      });
    }
    activeTransforms = new Map(activeTransforms);
  }

  async function handleMemoryEvent(detail: { messageId: string }) {
    const msg = snapshot.messages.find((m) => m.id === detail.messageId);
    if (!msg) return;
    const body = (msg as Record<string, unknown>).body;
    if (typeof body !== "string" || !body.trim()) return;

    // Show loading strip
    activeTransforms.set(detail.messageId, { content: "", op: "memory", model: null, loading: true });
    activeTransforms = new Map(activeTransforms);

    const result = await memoryQuery(body.slice(0, 500));
    if (result?.nodes?.length) {
      const lines = result.nodes.map(
        (n) => `#NODE-${n.id.slice(0, 12)}  (${n.score.toFixed(3)})  ${n.content.slice(0, 80)}`
      );
      activeTransforms.set(detail.messageId, {
        content: lines.join("\n"),
        op: "memory",
        model: null,
        loading: false,
      });
    } else {
      activeTransforms.set(detail.messageId, {
        content: "[MEMORY] No matching anchors found.",
        op: "memory",
        model: null,
        loading: false,
      });
    }
    activeTransforms = new Map(activeTransforms);
  }

  function dismissTransform(messageId: string) {
    activeTransforms.delete(messageId);
    activeTransforms = new Map(activeTransforms);
  }

  function applyTransform(messageId: string, content: string) {
    appliedOverrides.set(messageId, content);
    appliedOverrides = new Map(appliedOverrides);
    dismissTransform(messageId);
  }

  // ── Clarification handling ─────────────────────────────────────

  function getClarificationQuestion(msg: Record<string, unknown>): string | null {
    const m = messageMeta(msg);
    const cq = m.clarification_question;
    if (typeof cq === "string" && cq.trim()) return cq.trim();
    return null;
  }

  function buildContextMessages(): Array<{ role: "user" | "assistant"; content: string }> | undefined {
    const userId = snapshot.user?.id;
    if (!userId) return undefined;
    return snapshot.messages
      .slice(-8)
      .filter((message) => typeof message.body === "string" && message.body.trim().length > 0)
      .map((message) => ({
        role: message.author_id === userId ? "user" : "assistant",
        content: message.body,
      }));
  }

  async function handleSuggestReply() {
    const text = composerText.trim();
    if (!text) return;

    companionSuggestion = {
      content: "",
      model: null,
      loading: true,
      clarificationQuestion: null,
    };

    const result = await respond({
      content: text,
      persona,
      context_messages: buildContextMessages(),
    });

    if (!result) {
      companionSuggestion = {
        content: "[ERR] Cognition service returned no companion reply.",
        model: null,
        loading: false,
        clarificationQuestion: null,
      };
      return;
    }

    companionSuggestion = {
      content: result.content,
      model: result.meta?.model ?? null,
      loading: false,
      clarificationQuestion: result.clarification_question ?? null,
    };
  }

  function applyCompanionSuggestion() {
    if (!companionSuggestion || companionSuggestion.loading) return;
    composerText = companionSuggestion.content;
    companionSuggestion = null;
    composerRef?.focus();
  }

  function dismissCompanionSuggestion() {
    companionSuggestion = null;
  }

  function handleClarifyAsk(messageId: string, question: string) {
    const chId = snapshot.activeChannelId;
    if (!chId) return;
    sendMessage(chId, question);
    dismissedClarifications.add(messageId);
    dismissedClarifications = new Set(dismissedClarifications);
  }

  function dismissClarification(messageId: string) {
    dismissedClarifications.add(messageId);
    dismissedClarifications = new Set(dismissedClarifications);
  }

  // ── Keyboard navigation (spec §8.6) ────────────────────────────

  function getVisibleMessageIds(): string[] {
    if (!messageListRef) return [];
    const rows = messageListRef.querySelectorAll("[data-msg-id]");
    return Array.from(rows).map((el) => el.getAttribute("data-msg-id") ?? "").filter(Boolean);
  }

  function scrollToMessageId(id: string) {
    const el = messageListRef?.querySelector(`[data-msg-id="${id}"]`);
    el?.scrollIntoView({ block: "nearest" });
  }

  function navigateMessage(direction: 1 | -1) {
    const ids = getVisibleMessageIds();
    if (ids.length === 0) return;
    const current = $activeMessageId;
    const idx = current ? ids.indexOf(current) : -1;
    const nextIdx = idx === -1 ? 0 : Math.min(Math.max(idx + direction, 0), ids.length - 1);
    const nextId = ids[nextIdx];
    if (nextId) {
      activeMessageId.set(nextId);
      scrollToMessageId(nextId);
    }
  }

  function openInspectorForActive() {
    const aid = $activeMessageId;
    if (!aid) return;
    const msg = snapshot.messages.find((m) => m.id === aid);
    if (msg) {
      inspecting = messageToLogos(msg);
    }
  }

  function handleListKeydown(e: KeyboardEvent) {
    // Only handle when not typing in composer
    if (e.target instanceof HTMLTextAreaElement || e.target instanceof HTMLInputElement) return;

    if (e.key === "j" || e.key === "ArrowDown") {
      e.preventDefault();
      navigateMessage(1);
    } else if (e.key === "k" || e.key === "ArrowUp") {
      e.preventDefault();
      navigateMessage(-1);
    } else if (e.key === "Enter") {
      e.preventDefault();
      openInspectorForActive();
    } else if (e.key === "Escape") {
      closeInspector();
    }
  }

  // ── Event listeners for MessageFrame CustomEvents ──────────────

  function handleBubbledTransform(e: Event) {
    const ce = e as CustomEvent<TransformEventDetail>;
    if (ce.detail?.messageId && ce.detail?.op) {
      e.stopPropagation();
      void handleTransformEvent(ce.detail);
    }
  }

  function handleBubbledMemory(e: Event) {
    const ce = e as CustomEvent<{ messageId: string }>;
    if (ce.detail?.messageId) {
      e.stopPropagation();
      void handleMemoryEvent(ce.detail);
    }
  }

  onMount(() => {
    boot();

    // Listen for bubbled CustomEvents from MessageFrame
    const listEl = messageListRef;
    if (listEl) {
      listEl.addEventListener("onTransform", handleBubbledTransform);
      listEl.addEventListener("onMemory", handleBubbledMemory);
    }

    // Focus the message list for keyboard nav
    messageListRef?.focus();
  });

  onDestroy(() => {
    messageListRef?.removeEventListener("onTransform", handleBubbledTransform);
    messageListRef?.removeEventListener("onMemory", handleBubbledMemory);
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
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="cs-messages"
    bind:this={messageListRef}
    tabindex="0"
    role="list"
    aria-label="Message stream"
    onkeydown={handleListKeydown}
  >
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
        {@const msgId = String(msg.id)}
        {@const logosMsg = messageToLogos(msg as CognitiveMessage)}
        {@const rawMsg = msg as Record<string, unknown>}
        {@const clarifyQ = getClarificationQuestion(rawMsg)}
        {@const showClarify = clarifyQ != null && !dismissedClarifications.has(msgId)}
        {@const hasTransform = activeTransforms.has(msgId)}
        {@const tform = activeTransforms.get(msgId)}
        <div
          class="cs-row"
          data-msg-id={msgId}
          class:cs-active={$activeMessageId === msgId}
          onmouseenter={() => onRowFocus(msg)}
        >
          <div class="cs-row-inner">
            <MessageFrame message={logosMsg} onInspect={onInspect} active={$activeMessageId === msgId} />
            {#if inspecting?.id === msgId}
              <InspectorBlade message={inspecting} open={true} onClose={closeInspector} />
            {/if}
            {#if showClarify && clarifyQ}
              <ClarificationPrompt
                question={clarifyQ}
                onAsk={(q) => handleClarifyAsk(msgId, q)}
                onDismiss={() => dismissClarification(msgId)}
              />
            {/if}
            {#if hasTransform && tform}
              <ResultStrip
                result={tform.loading ? "…" : tform.content}
                op={tform.op}
                model={tform.model}
                messageId={msgId}
                onApply={applyTransform}
                onDismiss={dismissTransform}
              />
            {/if}
          </div>
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
      <button
        class="cs-send-btn cs-suggest-btn logos-mono"
        onclick={handleSuggestReply}
        disabled={!composerText.trim()}
      >
        SUGGEST
      </button>
      <button class="cs-send-btn logos-mono" onclick={onSubmit} disabled={!composerText.trim()}>
        SEND
      </button>
    </div>
    {#if companionSuggestion}
      <div class="cs-companion">
        <div class="cs-companion-body">
          {companionSuggestion.loading ? "…" : companionSuggestion.content}
        </div>
        {#if companionSuggestion.clarificationQuestion}
          <ClarificationPrompt
            question={companionSuggestion.clarificationQuestion}
            onAsk={(question) => {
              composerText = question;
              composerRef?.focus();
            }}
            onDismiss={() => {
              if (companionSuggestion) {
                companionSuggestion = { ...companionSuggestion, clarificationQuestion: null };
              }
            }}
          />
        {/if}
        <div class="cs-companion-footer logos-mono">
          <span>
            COMPANION REPLY{companionSuggestion.model ? ` · MODEL: ${companionSuggestion.model}` : ""}
          </span>
          <span class="cs-spacer"></span>
          <button
            type="button"
            class="cs-companion-btn"
            onclick={applyCompanionSuggestion}
            disabled={companionSuggestion.loading}
          >
            APPLY
          </button>
          <button
            type="button"
            class="cs-companion-btn"
            onclick={dismissCompanionSuggestion}
          >
            DISMISS
          </button>
        </div>
      </div>
    {/if}
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
    outline: none;
  }
  .cs-messages:focus-visible {
    outline: 1px solid var(--line-strong);
    outline-offset: -2px;
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
    border-bottom: 1px solid var(--line);
  }
  .cs-row:last-child {
    border-bottom: none;
  }
  .cs-row-inner {
    display: flex;
    flex-direction: column;
  }
  .cs-row.cs-active > .cs-row-inner > :global(.msg-frame) {
    border-color: var(--line-strong);
    background: var(--panel);
  }

  .cs-composer {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto auto;
    gap: 8px;
    padding: 10px;
    border-top: 1px solid var(--line);
    background: var(--panel);
  }

  .cs-input {
    min-height: 56px;
    padding: 8px 10px;
    border: 1px solid var(--line-strong);
    background: var(--panel-2);
    color: var(--text);
    resize: vertical;
  }

  .cs-send-btn {
    padding: 0 12px;
    border: 1px solid var(--line-strong);
    background: var(--panel-2);
    color: var(--text);
    cursor: pointer;
  }

  .cs-send-btn:disabled {
    opacity: 0.45;
    cursor: not-allowed;
  }

  .cs-suggest-btn {
    color: var(--accent-thread);
    border-color: var(--accent-thread);
  }

  .cs-companion {
    border-top: 1px solid var(--line);
    background: var(--panel-2);
  }

  .cs-companion-body {
    padding: 10px 12px;
    white-space: pre-wrap;
    color: var(--text);
    border-left: 2px solid var(--accent-thread);
  }

  .cs-companion-footer {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 12px 10px;
    color: var(--muted);
    font-size: 9px;
    letter-spacing: 0.04em;
  }

  .cs-companion-btn {
    padding: 2px 8px;
    border: 1px solid var(--line-strong);
    background: transparent;
    color: var(--text);
    font-family: var(--font-mono);
    font-size: 9px;
    cursor: pointer;
  }

  .cs-companion-btn:hover:not(:disabled) {
    background: var(--hover-strong);
  }
</style>
