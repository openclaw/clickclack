<script lang="ts">
  /**
   * PROJECT LOGOS — Proactive Clarification Prompt
   *
   * Renders inline:
   *   ? <question text>   [ASK] [DISMISS]
   *
   * 2px --intent-clarify left accent.
   *
   * Props:
   *   question  — the clarification question text
   *   onAsk     — called with the question string when [ASK] is clicked
   *   onDismiss — called when [DISMISS] is clicked
   */

  interface Props {
    question: string;
    onAsk: (question: string) => void;
    onDismiss: () => void;
  }

  let { question, onAsk, onDismiss }: Props = $props();
</script>

<div class="logos-clarify">
  <span class="logos-clarify-glyph">?</span>
  <span class="logos-clarify-text">{question}</span>
  <span class="logos-clarify-actions">
    <button type="button" class="logos-clarify-ask" onclick={() => onAsk(question)}>ASK</button>
    <button type="button" class="logos-clarify-dismiss" onclick={onDismiss}>DISMISS</button>
  </span>
</div>

<style>
  .logos-clarify {
    display: flex;
    align-items: baseline;
    flex-wrap: wrap;
    gap: 6px;
    margin-top: 6px;
    padding: 5px 8px;
    border: 1px solid var(--line-strong);
    border-left: 2px solid var(--intent-clarify);
    background: var(--panel-2);
    font-family: var(--font-mono);
    font-size: 10px;
    line-height: 1.4;
  }

  .logos-clarify-glyph {
    color: var(--accent-thread);
    font-weight: 700;
    font-size: 12px;
    flex-shrink: 0;
  }

  .logos-clarify-text {
    flex: 1;
    min-width: 0;
    color: var(--text);
    font-style: italic;
  }

  .logos-clarify-actions {
    display: flex;
    gap: 4px;
    flex-shrink: 0;
  }

  .logos-clarify-ask,
  .logos-clarify-dismiss {
    padding: 2px 8px;
    border: 0;
    background: var(--panel-3);
    color: var(--text);
    font-family: var(--font-mono);
    font-size: 9px;
    font-weight: 600;
    letter-spacing: 0.04em;
    cursor: pointer;
    text-transform: uppercase;
  }

  .logos-clarify-ask {
    background: var(--accent-thread);
    color: var(--bg);
  }

  .logos-clarify-ask:hover {
    opacity: 0.88;
  }

  .logos-clarify-dismiss:hover {
    background: var(--hover-strong);
  }
</style>
