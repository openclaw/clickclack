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
    compact?: boolean;
  }

  let { message, onInspect, active = false, compact = false }: Props = $props();

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
  const summaryText = $derived.by(() => {
    const text = message.body.replace(/\s+/g, " ").trim();
    if (text.length <= 220) return text;
    return `${text.slice(0, 217).trimEnd()}...`;
  });
  const hasLongBody = $derived(message.body.replace(/\s+/g, " ").trim().length > 220);
  const timestampLabel = $derived.by(() => {
    if (!message.created_at) return null;
    const date = new Date(message.created_at);
    if (Number.isNaN(date.getTime())) return null;
    return new Intl.DateTimeFormat(undefined, {
      hour: "numeric",
      minute: "2-digit",
      month: "short",
      day: "numeric",
    }).format(date);
  });
  const primaryLine = $derived.by(() => {
    const text = message.body.replace(/\s+/g, " ").trim();
    if (!text) return "";
    return text.length > 420 ? `${text.slice(0, 417).trimEnd()}...` : text;
  });

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

  function handleTransform(
    op:
      | "summarize"
      | "condense"
      | "expand"
      | "rewrite"
      | "checklist"
      | "plan"
      | "extract"
      | "diagnose"
      | "counterargument"
      | "invert",
  ) {
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
    <div class="msg-header">
      <div class="msg-header-main">
        <span class="msg-persona">{personaLabel === "--" ? "COMPANION" : personaLabel}</span>
        {#if timestampLabel}
          <span class="msg-time">{timestampLabel}</span>
        {/if}
      </div>
      <div class="msg-header-meta">
        <span class="msg-intent-pill" style={`--intent-color: ${intentColorVar}`}>{intentLabel}</span>
        {#if message.confidence != null}
          <button
            type="button"
            class="msg-confidence-pill"
            onclick={handleConfClick}
            title={onInspect ? "Open details" : undefined}
          >
            {Math.round(message.confidence * 100)}%
          </button>
        {/if}
      </div>
    </div>

    {#if compact}
      <div class="msg-summary-wrap">
        <div class="msg-summary">{summaryText}</div>
        {#if hasLongBody}
          <button type="button" class="msg-summary-expand" onclick={handleConfClick}>
            OPEN THREAD
          </button>
        {/if}
      </div>
    {:else}
      <div class="msg-body">
        {@html renderedHtml}
      </div>
    {/if}

    {#if !compact}
      <div class="msg-footer">
        <div class="msg-footer-summary">{primaryLine}</div>
        <div class="msg-footer-meta">
          {#if transformCount > 0}<span>{transformCount} transforms</span>{/if}
          {#if latencyLabel !== "n/a"}<span>{latencyLabel}</span>{/if}
          {#if threadLabel !== "--"}<span>{threadLabel}</span>{/if}
        </div>
      </div>
    {/if}

    <!-- Inline action rail (hover reveal) -->
    <div class="msg-actions">
      <button type="button" class="msg-action-btn" onclick={() => handleTransform("summarize")}>
        Summarize
      </button>
      <button type="button" class="msg-action-btn" onclick={() => handleTransform("condense")}>
        Shorten
      </button>
      <button type="button" class="msg-action-btn" onclick={() => handleTransform("expand")}>
        Expand
      </button>
      <button type="button" class="msg-action-btn" onclick={handleMemory}>
        Related
      </button>
      <button type="button" class="msg-action-btn" onclick={handleAnchor}>
        Save
      </button>
      <button type="button" class="msg-action-btn" onclick={() => handleTransform("checklist")}>
        Checklist
      </button>
      <button type="button" class="msg-action-btn" onclick={() => handleTransform("plan")}>
        Plan
      </button>
      <button type="button" class="msg-action-btn" onclick={() => handleTransform("extract")}>
        Extract
      </button>
      <button type="button" class="msg-action-btn" onclick={() => handleTransform("diagnose")}>
        Diagnose
      </button>
      <button type="button" class="msg-action-btn" onclick={() => handleTransform("counterargument")}>
        Counter
      </button>
      <button type="button" class="msg-action-btn" onclick={() => handleTransform("invert")}>
        Invert
      </button>
      <button type="button" class="msg-action-btn" onclick={() => handleTransform("rewrite")}>
        Rewrite
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
    border-radius: var(--radius-lg);
    background: color-mix(in srgb, var(--panel-2) 92%, transparent);
    position: relative;
    overflow: hidden;
    box-shadow: var(--shadow-sm);
  }

  .msg-frame.msg-active {
    border-color: color-mix(in srgb, var(--accent-thread) 32%, var(--line-strong));
    background: var(--panel);
    box-shadow: var(--shadow-md);
  }

  .msg-frame:hover {
    border-color: color-mix(in srgb, var(--accent-thread) 20%, var(--line-strong));
    background: color-mix(in srgb, var(--panel-raised) 78%, var(--panel));
  }

  /* ── Intent edge band ── */
  .msg-intent-band {
    width: 4px;
    min-height: 100%;
    background: var(--intent-color, var(--intent-default));
    flex-shrink: 0;
    opacity: 0.8;
  }

  /* ── Content area ── */
  .msg-content {
    padding: var(--space-4);
    min-width: 0;
  }

  .msg-header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 12px;
    margin-bottom: 10px;
  }

  .msg-header-main {
    display: flex;
    align-items: center;
    gap: 10px;
    min-width: 0;
  }

  .msg-persona {
    color: var(--text-strong);
    font-family: var(--font-ui);
    font-size: 12px;
    font-weight: 650;
    letter-spacing: 0.02em;
  }

  .msg-time {
    color: var(--muted);
    font-size: 11px;
  }

  .msg-header-meta {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .msg-intent-pill,
  .msg-confidence-pill {
    display: inline-flex;
    align-items: center;
    min-height: 28px;
    padding: 0 10px;
    border: 1px solid color-mix(in srgb, var(--line) 75%, transparent);
    border-radius: var(--radius-pill);
    background: color-mix(in srgb, var(--panel-3) 78%, transparent);
    white-space: nowrap;
    font-size: 11px;
    font-weight: 600;
  }

  .msg-intent-pill {
    color: color-mix(in srgb, var(--intent-color) 82%, white);
    border-color: color-mix(in srgb, var(--intent-color) 32%, var(--line));
    background: color-mix(in srgb, var(--intent-color) 12%, var(--panel-3));
  }

  .msg-confidence-pill {
    color: var(--text-strong);
    cursor: pointer;
  }

  /* ── Body ── */
  .msg-body {
    color: var(--cog-fg);
    font-family: var(--font-body);
    font-size: 14px;
    line-height: 1.72;
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
    background: color-mix(in srgb, var(--panel) 88%, transparent);
    border: 1px solid color-mix(in srgb, var(--line-strong) 70%, transparent);
    border-radius: var(--radius);
    padding: 10px 12px;
    font-family: var(--font-mono);
    font-size: 12px;
    line-height: 1.5;
    overflow-x: auto;
    margin: 8px 0;
  }

  .msg-body :global(code) {
    font-family: var(--font-mono);
    font-size: 0.9em;
    background: color-mix(in srgb, var(--panel-3) 86%, transparent);
    padding: 2px 6px;
    border-radius: var(--radius-sm);
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
    margin: 8px 0;
    padding: 4px 0 4px 12px;
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

  .msg-summary-wrap {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .msg-summary {
    color: var(--text);
    font-family: var(--font-body);
    font-size: 14px;
    line-height: 1.65;
    display: -webkit-box;
    -webkit-line-clamp: 4;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }

  .msg-summary-expand {
    align-self: flex-start;
    min-height: 28px;
    padding: 0 10px;
    border: 1px solid color-mix(in srgb, var(--accent-thread) 30%, var(--line));
    border-radius: var(--radius-pill);
    background: color-mix(in srgb, var(--accent-thread) 10%, transparent);
    color: var(--text-strong);
    font-family: var(--font-ui);
    font-size: 10px;
    font-weight: 600;
    cursor: pointer;
  }

  .msg-footer {
    margin-top: 12px;
    display: grid;
    gap: 8px;
  }

  .msg-footer-summary {
    color: var(--muted);
    font-size: 12px;
    line-height: 1.55;
    display: none;
  }

  .msg-footer-meta {
    display: flex;
    flex-wrap: wrap;
    gap: 10px;
    color: var(--muted);
    font-size: 11px;
  }

  /* ── Inspect mode: body 60% opacity ── */
  .msg-frame.msg-inspect .msg-body {
    opacity: 0.6;
  }

  /* ── Inline action rail ── */
  .msg-actions {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px;
    padding-top: var(--space-3);
    margin-top: var(--space-3);
    border-top: 1px solid var(--line);
    opacity: 0;
    transform: translateY(6px);
    transition:
      opacity var(--motion-fast),
      transform var(--motion-fast);
    font-family: var(--font-ui);
    font-size: 10px;
    letter-spacing: 0.01em;
  }

  .msg-frame:hover .msg-actions,
  .msg-frame:focus-within .msg-actions,
  .msg-frame.msg-active .msg-actions {
    opacity: 1;
    transform: translateY(0);
  }

  .msg-action-btn {
    min-height: 30px;
    padding: 0 10px;
    border: 1px solid var(--line);
    border-radius: var(--radius-pill);
    background: color-mix(in srgb, var(--panel-3) 72%, transparent);
    color: var(--muted);
    font-family: var(--font-ui);
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.02em;
    cursor: pointer;
  }

  .msg-action-btn:hover {
    background: color-mix(in srgb, var(--panel-raised) 90%, transparent);
    color: var(--text-strong);
    border-color: color-mix(in srgb, var(--accent-thread) 42%, var(--line-strong));
    box-shadow: var(--shadow-sm);
  }

  .msg-action-btn:focus-visible {
    outline-offset: 2px;
  }

  @media (max-width: 640px) {
    .msg-header {
      flex-direction: column;
      align-items: stretch;
    }
    .msg-header-meta {
      flex-wrap: wrap;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .msg-actions {
      transition: none;
    }
    .msg-body {
      transition: none;
    }
  }
</style>
