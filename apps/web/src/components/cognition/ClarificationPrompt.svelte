<script lang="ts">
  /**
   * PROJECT LOGOS — Proactive Clarification Prompt (Track B, spec §5.1)
   *
   * Renders inline under a message:
   *   ? <question text>
   *   [ASK] [DISMISS]
   *
   * [ASK] sends the question as a follow-up to the channel.
   * [DISMISS] hides the prompt.
   */

  interface Props {
    question: string;
    onAsk: (question: string) => void;
    onDismiss: () => void;
  }

  let {
    question,
    onAsk,
    onDismiss,
  }: Props = $props();
</script>

<div class="clarification-prompt">
  <span class="clarification-glyph">?</span>
  <span class="clarification-question">{question}</span>
  <span class="clarification-actions">
    <button type="button" class="clarification-ask" onclick={() => onAsk(question)}>ASK</button>
    <button type="button" class="clarification-dismiss" onclick={onDismiss}>DISMISS</button>
  </span>
</div>

<style>
  .clarification-prompt {
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

  .clarification-glyph {
    color: var(--accent-thread);
    font-weight: 700;
    font-size: 12px;
    flex-shrink: 0;
  }

  .clarification-question {
    flex: 1;
    min-width: 0;
    color: var(--text);
    font-style: italic;
  }

  .clarification-actions {
    display: flex;
    gap: 4px;
    flex-shrink: 0;
  }

  .clarification-ask,
  .clarification-dismiss {
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

  .clarification-ask {
    background: var(--accent-thread);
    color: var(--bg);
  }

  .clarification-ask:hover {
    opacity: 0.88;
  }

  .clarification-dismiss:hover {
    background: var(--hover-strong);
  }
</style>
