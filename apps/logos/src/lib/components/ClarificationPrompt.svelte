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
    gap: 8px;
    margin-top: var(--space-3);
    padding: 12px 14px;
    border: 1px solid var(--line-strong);
    border-left: 2px solid var(--intent-clarify);
    border-radius: var(--radius-lg);
    background: color-mix(in srgb, var(--panel-2) 92%, transparent);
    font-family: var(--font-body);
    font-size: 12px;
    line-height: 1.6;
    box-shadow: var(--shadow-sm);
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
    gap: 8px;
    flex-shrink: 0;
  }

  .logos-clarify-ask,
  .logos-clarify-dismiss {
    min-height: 32px;
    padding: 0 12px;
    border: 1px solid var(--line);
    border-radius: var(--radius-pill);
    background: color-mix(in srgb, var(--panel-3) 84%, transparent);
    color: var(--text);
    font-family: var(--font-ui);
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.02em;
    cursor: pointer;
    text-transform: uppercase;
  }

  .logos-clarify-ask {
    background: color-mix(in srgb, var(--intent-clarify) 20%, transparent);
    border-color: color-mix(in srgb, var(--intent-clarify) 45%, var(--line-strong));
    color: var(--text-strong);
  }

  .logos-clarify-ask:hover {
    background: color-mix(in srgb, var(--intent-clarify) 30%, transparent);
  }

  .logos-clarify-dismiss:hover {
    background: color-mix(in srgb, var(--panel-raised) 92%, transparent);
  }
</style>
