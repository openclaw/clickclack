<script lang="ts">
  /**
   * PROJECT LOGOS — MessageFrame (Inspection + Telemetry Track)
   *
   * Message object anatomy (spec §8.4). Renders a single message as a
   * discrete data object with intent edge band, mono metadata header,
   * dense off-white markdown body, and inline hover action rail.
   *
   * Alt/Option inspect mode: body text drops to 60% opacity.
   * Confidence tag click: fires onInspect callback.
   */

  import { marked } from "marked";
  import DOMPurify from "dompurify";
  import { inspectMode } from "$lib/ui";

  // ── Message shape ──
  export interface LogosMessage {
    id: string;
    body: string;
    intent?: string | null;
    persona?: string | null;
    confidence?: number | null;
    thread_id?: string | null;
    execution_status?: string | null;
    metadata_json?: Record<string, unknown> | null;
    transform_history?: Array<{
      op: string;
      at: string;
      preview: string;
      persona?: string;
      model?: string;
    }> | null;
    created_at?: string | null;
  }

  interface Props {
    message: LogosMessage;
    onInspect?: (msg: LogosMessage) => void;
    active?: boolean;
  }

  let { message, onInspect, active = false }: Props = $props();

  // ── Derived state ──

  const intentColorVar = $derived.by(() => {
    const map: Record<string, string> = {
      ask: "var(--intent-ask)",
      command: "var(--intent-command)",
      reflect: "var(--intent-reflect)",
      draft: "var(--intent-draft)",
      clarify: "var(--intent-clarify)",
      explore: "var(--intent-explore)",
    };
    return message.intent ? (map[message.intent] ?? "var(--intent-default)") : "var(--intent-default)";
  });

  const intentLabel = $derived(message.intent ? message.intent.toUpperCase() : "--");
  const personaLabel = $derived(message.persona ? message.persona.toUpperCase() : "--");
  const confLabel = $derived(message.confidence != null ? (message.confidence).toFixed(2) : "--");
  const threadLabel = $derived(message.thread_id ? `#${message.thread_id.slice(0, 12)}` : "--");
  const latencyLabel = $derived.by(() => {
    const telemetry = (message.metadata_json?.telemetry as Record<string, unknown> | undefined) ?? {};
    const latency = typeof telemetry.latency_ms === "number"
      ? telemetry.latency_ms
      : typeof message.metadata_json?.latency_ms === "number"
        ? message.metadata_json.latency_ms
        : null;
    return latency != null ? `${Math.round(latency)}ms` : "n/a";
  });
  const confidenceWidth = $derived(
    message.confidence != null ? `${Math.max(0, Math.min(100, message.confidence * 100))}%` : "0%",
  );
  const transformCount = $derived(message.transform_history?.length ?? 0);

  // ── Markdown rendering ──
  const renderedHtml = $derived.by(() => {
    try {
      const raw = marked.parse(message.body, { async: false }) as string;
      return DOMPurify.sanitize(raw);
    } catch {
      return DOMPurify.sanitize(message.body);
    }
  });

  // ── Event dispatchers ──
  let dispatch: (event: string, detail?: unknown) => void;
  $effect(() => {
    dispatch = (event: string, detail?: unknown) => {
      const el = document.querySelector(`[data-msg-id="${message.id}"]`);
      if (el) {
        el.dispatchEvent(new CustomEvent(event, { detail, bubbles: true }));
      }
    };
  });

  function handleConfClick() {
    onInspect?.(message);
  }

  function handleTransform(op: "summarize" | "condense" | "expand" | "rewrite") {
    dispatch("onTransform", { op, messageId: message.id });
  }

  function handleMemory() {
    dispatch("onMemory", { messageId: message.id });
  }

  function handleAnchor() {
    dispatch("onAnchor", { messageId: message.id });
  }
</script>

<div
  class="msg-frame"
  class:msg-active={active}
  class:msg-inspect={$inspectMode}
  data-msg-id={message.id}
>
  <!-- Intent edge band — 2px vertical bar at far left -->
  <div
    class="msg-intent-band"
    style="--intent-color: {intentColorVar}"
    title="Intent: {intentLabel}"
  ></div>

  <!-- Message content area -->
  <div class="msg-content">
    <!-- Mono metadata header -->
    <div class="msg-meta">
      <span class="meta-tag">INTENT: {intentLabel}</span>
      <span class="meta-tag">PERSONA: {personaLabel}</span>
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <span
        class="meta-tag meta-conf"
        class:meta-clickable={onInspect != null}
        onclick={handleConfClick}
        onkeydown={(e: KeyboardEvent) => e.key === "Enter" && handleConfClick()}
        role={onInspect ? "button" : undefined}
        tabindex={onInspect ? 0 : undefined}
        title={onInspect ? "Click to inspect" : undefined}
      >
        CONF: {confLabel}
      </span>
      <span class="meta-tag">THREAD: {threadLabel}</span>
      <span class="meta-tag meta-latency">LATENCY: {latencyLabel}</span>
      {#if transformCount > 0}
        <span class="meta-tag">XFORM: {transformCount}</span>
      {/if}
    </div>
    <div class="msg-confidence-track" aria-hidden="true">
      <div class="msg-confidence-bar" style={`width: ${confidenceWidth}; background: ${intentColorVar};`}></div>
    </div>

    <!-- Dense off-white body -->
    <div class="msg-body">
      {@html renderedHtml}
    </div>

    <!-- Inline action rail (hover reveal) -->
    <div class="msg-actions">
      <span class="msg-action-prompt">&gt;</span>
      <button type="button" class="msg-action-btn" onclick={() => handleTransform("summarize")}>
        SUMMARIZE
      </button>
      <button type="button" class="msg-action-btn" onclick={() => handleTransform("condense")}>
        CONDENSE
      </button>
      <button type="button" class="msg-action-btn" onclick={() => handleTransform("expand")}>
        EXPAND
      </button>
      <button type="button" class="msg-action-btn" onclick={handleMemory}>
        MEM-NODE
      </button>
      <button type="button" class="msg-action-btn" onclick={handleAnchor}>
        ANCHOR
      </button>
      <button type="button" class="msg-action-btn" onclick={() => handleTransform("rewrite")}>
        REWRITE
      </button>
    </div>
  </div>
</div>

<style>
  .msg-frame {
    display: grid;
    grid-template-columns: 4px 1fr;
    gap: 0;
    border: 1px solid var(--line);
    background: var(--panel-2);
    position: relative;
  }

  .msg-frame.msg-active {
    border-color: var(--line-strong);
    background: var(--panel);
  }

  /* ── Intent edge band ── */
  .msg-intent-band {
    width: 4px;
    min-height: 100%;
    background: var(--intent-color, var(--intent-default));
    flex-shrink: 0;
  }

  /* ── Content area ── */
  .msg-content {
    padding: 10px 12px;
    min-width: 0;
  }

  /* ── Mono metadata header ── */
  .msg-meta {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 2px;
    margin-bottom: 6px;
    font-family: var(--font-mono);
    font-size: 9.5px;
    font-weight: 600;
    letter-spacing: 0.04em;
    color: var(--muted);
    line-height: 1.5;
  }

  .meta-tag {
    display: inline-flex;
    align-items: center;
    padding: 1px 4px;
    border: 1px solid transparent;
    white-space: nowrap;
  }

  .meta-conf {
    color: var(--text-strong);
    font-weight: 700;
    border-color: var(--line);
  }

  .meta-clickable {
    cursor: pointer;
    transition: background var(--motion-fast);
  }

  .meta-clickable:hover {
    background: var(--hover-strong);
    border-color: var(--line-strong);
  }

  .meta-clickable:focus-visible {
    outline: 1px solid var(--text-strong);
    outline-offset: -1px;
  }

  .meta-latency {
    color: var(--muted-2);
    font-weight: 400;
  }

  .msg-confidence-track {
    height: 4px;
    margin: 0 0 10px;
    border: 1px solid var(--line);
    background: var(--panel);
  }

  .msg-confidence-bar {
    height: 100%;
  }

  /* ── Body ── */
  .msg-body {
    color: var(--cog-fg);
    font-family: var(--font-body);
    font-size: 14px;
    line-height: 1.6;
    word-wrap: break-word;
    overflow-wrap: break-word;
    transition: opacity var(--motion-med);
  }

  /* Markdown overrides inside body */
  .msg-body :global(p) {
    margin: 0 0 8px 0;
  }

  .msg-body :global(p:last-child) {
    margin-bottom: 0;
  }

  .msg-body :global(pre) {
    background: var(--panel);
    border: 1px solid var(--line);
    padding: 8px 10px;
    font-family: var(--font-mono);
    font-size: 12px;
    line-height: 1.5;
    overflow-x: auto;
    margin: 6px 0;
  }

  .msg-body :global(code) {
    font-family: var(--font-mono);
    font-size: 0.9em;
    background: var(--panel);
    padding: 1px 4px;
  }

  .msg-body :global(pre code) {
    background: none;
    padding: 0;
  }

  .msg-body :global(ul),
  .msg-body :global(ol) {
    padding-left: 1.5em;
    margin: 4px 0;
  }

  .msg-body :global(li) {
    margin-bottom: 2px;
  }

  .msg-body :global(blockquote) {
    border-left: 2px solid var(--line-strong);
    margin: 6px 0;
    padding: 2px 0 2px 10px;
    color: var(--cog-cloud);
  }

  .msg-body :global(a) {
    color: var(--accent-thread);
    text-decoration: none;
  }

  .msg-body :global(a:hover) {
    text-decoration: underline;
  }

  .msg-body :global(strong) {
    color: var(--text-strong);
  }

  /* ── Inspect mode: body 60% opacity ── */
  .msg-frame.msg-inspect .msg-body {
    opacity: 0.6;
  }

  /* ── Inline action rail ── */
  .msg-actions {
    display: flex;
    align-items: center;
    gap: 4px;
    padding-top: 8px;
    margin-top: 6px;
    border-top: 1px solid var(--line);
    opacity: 0;
    transition: opacity var(--motion-fast);
    font-family: var(--font-mono);
    font-size: 10px;
    letter-spacing: 0.04em;
  }

  .msg-frame:hover .msg-actions,
  .msg-frame:focus-within .msg-actions,
  .msg-frame.msg-active .msg-actions {
    opacity: 1;
  }

  .msg-action-prompt {
    color: var(--muted-2);
    margin-right: 2px;
    flex-shrink: 0;
  }

  .msg-action-btn {
    padding: 2px 6px;
    border: 1px solid var(--line);
    background: transparent;
    color: var(--muted);
    font-family: var(--font-mono);
    font-size: 9px;
    font-weight: 600;
    letter-spacing: 0.05em;
    cursor: pointer;
  }

  .msg-action-btn:hover {
    background: var(--hover-strong);
    color: var(--text-strong);
    border-color: var(--line-strong);
  }

  .msg-action-btn:focus-visible {
    outline: 1px solid var(--text-strong);
    outline-offset: -1px;
  }

  @media (prefers-reduced-motion: reduce) {
    .msg-actions {
      transition: none;
    }
    .msg-body {
      transition: none;
    }
    .meta-clickable {
      transition: none;
    }
  }
</style>
