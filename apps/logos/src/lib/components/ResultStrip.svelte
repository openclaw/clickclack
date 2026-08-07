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
    onDismiss: (messageId: string) => void;
  }

  let { result, op, model = null, messageId, onApply, onDismiss }: Props = $props();

  const opLabel = $derived.by(() => {
    const map: Record<string, string> = {
      xform: "TRANSFORM",
      condense: "CONDENSE",
      expand: "EXPAND",
      memnode: "MEM-NODE",
      rewrite: "REWRITE",
      memory: "MEMORY QUERY",
    };
    return map[op] ?? op.toUpperCase();
  });

  const metaText = $derived(model ? `${opLabel} · MODEL: ${model}` : opLabel);
</script>

<div class="result-strip">
  <div class="result-body">{result}</div>
  <div class="result-footer">
    <span class="result-meta">{metaText}</span>
    <span class="result-spacer"></span>
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
    background: var(--panel);
    font-family: var(--font-mono);
    font-size: 11px;
    line-height: 1.5;
  }

  .result-body {
    padding: 8px 12px;
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
    gap: 8px;
    padding: 4px 12px;
    border-top: 1px solid var(--line);
    background: var(--panel-2);
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

  .result-btn {
    padding: 2px 8px;
    border: 1px solid var(--line-strong);
    background: var(--panel-3);
    color: var(--text);
    font-family: var(--font-mono);
    font-size: 9px;
    font-weight: 600;
    letter-spacing: 0.04em;
    cursor: pointer;
    text-transform: uppercase;
  }

  .result-apply {
    border-color: var(--accent-thread);
    color: var(--accent-thread);
    background: transparent;
  }

  .result-apply:hover {
    background: var(--accent-thread);
    color: var(--bg);
  }

  .result-dismiss:hover {
    background: var(--hover-strong);
  }
</style>
