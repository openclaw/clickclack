<script lang="ts">
  /**
   * PROJECT LOGOS — ResultStrip (Inline Transform Result)
   *
   * Slim monochrome strip rendered below a message frame when a
   * transform or memory action completes. Non-modal, 0px radius,
   * operator aesthetic (mono, 2px accent, no popups).
   *
   * Props:
   *   result     — rendered result text
   *   op         — transform op label ("xform", "condense", etc.)
   *   model      — model name from meta (optional)
   *   messageId  — id of the parent message
   *   onApply    — (messageId, content) => replace body in-place
   *   onDismiss  — (messageId) => remove this strip
   */

  interface Props {
    result: string;
    op: string;
    model?: string | null;
    messageId: string;
    onApply: (messageId: string, content: string) => void;
    onUseAsDraft?: (content: string) => void;
    onDismiss: (messageId: string) => void;
  }

  let { result, op, model = null, messageId, onApply, onUseAsDraft, onDismiss }: Props = $props();
  let copyState = $state<"" | "COPIED" | "FAILED">("");

  const opLabel = $derived.by(() => {
    const map: Record<string, string> = {
      summarize: "SUMMARIZE",
      condense: "CONDENSE",
      expand: "EXPAND",
      memnode: "MEM-NODE",
      rewrite: "REWRITE",
      memory: "MEMORY QUERY",
      checklist: "CHECKLIST",
      plan: "PLAN",
      extract: "EXTRACT",
      diagnose: "DIAGNOSE",
      counterargument: "COUNTERARGUMENT",
      invert: "INVERT",
    };
    return map[op] ?? op.toUpperCase();
  });

  const metaText = $derived(model ? `${opLabel} · MODEL: ${model}` : opLabel);

  async function handleCopy(): Promise<void> {
    try {
      await navigator.clipboard.writeText(result);
      copyState = "COPIED";
    } catch {
      copyState = "FAILED";
    }
    window.setTimeout(() => {
      copyState = "";
    }, 1500);
  }
</script>

<div class="result-strip">
  <div class="result-body">{result}</div>
  <div class="result-footer">
    <span class="result-meta">{metaText}</span>
    {#if copyState}
      <span class="result-copy-state">{copyState}</span>
    {/if}
    <span class="result-spacer"></span>
    {#if onUseAsDraft}
      <button type="button" class="result-btn" onclick={() => onUseAsDraft(result)}>
        DRAFT
      </button>
    {/if}
    <button type="button" class="result-btn" onclick={handleCopy}>
      COPY
    </button>
    <button type="button" class="result-btn result-apply" onclick={() => onApply(messageId, result)}>
      APPLY
    </button>
    <button type="button" class="result-btn result-dismiss" onclick={() => onDismiss(messageId)}>
      DISMISS
    </button>
  </div>
</div>

<style>
  .result-strip {
    border: 1px solid var(--line-strong);
    border-top: none;
    border-radius: 0 0 var(--radius-lg) var(--radius-lg);
    background: color-mix(in srgb, var(--panel) 92%, transparent);
    font-family: var(--font-body);
    font-size: 11px;
    line-height: 1.6;
    overflow: hidden;
    box-shadow: var(--shadow-sm);
  }

  .result-body {
    padding: 12px 14px;
    color: var(--text);
    white-space: pre-wrap;
    word-break: break-word;
    border-left: 2px solid var(--accent-thread);
    max-height: 240px;
    overflow-y: auto;
  }

  .result-footer {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
    padding: 8px 12px 10px;
    border-top: 1px solid var(--line);
    background: color-mix(in srgb, var(--panel-2) 92%, transparent);
    font-size: 9px;
    letter-spacing: 0.04em;
  }

  .result-meta {
    color: var(--muted-2);
    font-weight: 600;
  }

  .result-spacer {
    flex: 1;
  }

  .result-copy-state {
    color: var(--muted);
    font-size: 9px;
  }

  .result-btn {
    min-height: 30px;
    padding: 0 10px;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius-pill);
    background: color-mix(in srgb, var(--panel-3) 82%, transparent);
    color: var(--text);
    font-family: var(--font-ui);
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.02em;
    cursor: pointer;
    text-transform: uppercase;
  }

  .result-apply {
    border-color: var(--accent-thread);
    color: var(--accent-thread);
    background: color-mix(in srgb, var(--accent-thread) 10%, transparent);
  }

  .result-apply:hover {
    background: color-mix(in srgb, var(--accent-thread) 24%, transparent);
    color: var(--text-strong);
  }

  .result-btn:hover {
    background: color-mix(in srgb, var(--panel-raised) 92%, transparent);
    color: var(--text-strong);
    box-shadow: var(--shadow-sm);
  }

  .result-dismiss:hover {
    background: var(--hover-strong);
  }
</style>
