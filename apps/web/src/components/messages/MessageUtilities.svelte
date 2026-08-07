<script lang="ts">
  /**
   * PROJECT LOGOS — Inline Action Rail (Track B)
   *
   * Non-modal, no popups/modals. Flush to bottom edge of message frame on
   * hover/select. Monospace text-trigger blocks per spec §8.4:
   *   > [XFORM] [CONDENSE] [EXPAND] [MEM-NODE] [REWRITE]
   *
   * All actions wired to real cognition service calls via callback props.
   */

  interface Props {
    messageId: string;
    visible: boolean;
    flip?: boolean;
    onTransform?: (messageId: string) => void;
    onSummarize?: (messageId: string) => void;
    onExpand?: (messageId: string) => void;
    onThreadLink?: (messageId: string) => void;
    onMemoryLink?: (messageId: string) => void;
    onPersonaSwitch?: (messageId: string) => void;
    transforming?: boolean;
    querying?: boolean;
  }

  let {
    messageId,
    visible = false,
    onTransform,
    onSummarize,
    onExpand,
    onThreadLink,
    onMemoryLink,
    onPersonaSwitch,
    transforming = false,
    querying = false,
  }: Props = $props();

  const busy = $derived(transforming || querying);
  const hasAny = $derived(
    onTransform || onSummarize || onExpand || onThreadLink || onMemoryLink || onPersonaSwitch,
  );
</script>

{#if visible && hasAny}
  <div
    class="action-rail"
    class:busy
    aria-label="Message utilities"
    role="toolbar"
  >
    <span class="rail-prompt" aria-hidden="true">&gt;</span>

    {#if onTransform}
      <button
        type="button"
        class="rail-btn"
        title="Transform message"
        aria-label="Transform message"
        disabled={busy}
        onclick={() => onTransform(messageId)}
      >[XFORM]</button>
    {/if}

    {#if onSummarize}
      <button
        type="button"
        class="rail-btn"
        title="Condense message"
        aria-label="Condense message"
        disabled={busy}
        onclick={() => onSummarize(messageId)}
      >[CONDENSE]</button>
    {/if}

    {#if onExpand}
      <button
        type="button"
        class="rail-btn"
        title="Expand message"
        aria-label="Expand message"
        disabled={busy}
        onclick={() => onExpand(messageId)}
      >[EXPAND]</button>
    {/if}

    {#if onMemoryLink}
      <button
        type="button"
        class="rail-btn"
        title="Memory node lookup"
        aria-label="Memory node lookup"
        disabled={busy}
        onclick={() => onMemoryLink(messageId)}
      >[MEM-NODE]</button>
    {/if}

    {#if onPersonaSwitch}
      <button
        type="button"
        class="rail-btn"
        title="Rewrite with alternate persona"
        aria-label="Rewrite with alternate persona"
        disabled={busy}
        onclick={() => onPersonaSwitch(messageId)}
      >[REWRITE]</button>
    {/if}

    {#if busy}
      <span class="rail-busy" title="Cognition active" aria-hidden="true"></span>
    {/if}
  </div>
{/if}

<style>
  .action-rail {
    display: flex;
    align-items: center;
    gap: 4px;
    margin-top: 4px;
    padding-top: 4px;
    border-top: 1px solid var(--line);
    font-family: var(--font-mono);
    font-size: 9.5px;
    letter-spacing: 0.04em;
    opacity: 0;
    pointer-events: none;
    transition: opacity var(--motion-fast);
  }

  /* Reveal on parent hover/select */
  .message-row:hover .action-rail,
  .message-row.selected .action-rail,
  .message-row.menu-open .action-rail {
    opacity: 1;
    pointer-events: auto;
  }

  .rail-prompt {
    color: var(--muted);
    font-weight: 600;
    flex-shrink: 0;
  }

  .rail-btn {
    padding: 2px 4px;
    border: 0;
    background: transparent;
    color: var(--muted-2);
    font-family: var(--font-mono);
    font-size: 9px;
    font-weight: 600;
    letter-spacing: 0.04em;
    cursor: pointer;
    white-space: nowrap;
    line-height: 1.4;
  }

  .rail-btn:hover:not(:disabled) {
    background: var(--hover-strong);
    color: var(--text-strong);
  }

  .rail-btn:disabled {
    opacity: 0.35;
    cursor: not-allowed;
  }

  .rail-btn:focus-visible {
    outline: 1px solid var(--text-strong);
    outline-offset: 1px;
  }

  .rail-busy {
    width: 5px;
    height: 5px;
    background: var(--accent-thread);
    margin-left: 4px;
    flex-shrink: 0;
    animation: rail-pulse 0.8s steps(2, jump-none) infinite;
  }

  @keyframes rail-pulse {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.3; }
  }

  @media (prefers-reduced-motion: reduce) {
    .rail-busy { animation: none; }
    .action-rail { transition: none; }
  }

  /* Touch: hide action rail (rely on long-press action sheet) */
  @media (hover: none), (pointer: coarse) {
    .action-rail { display: none; }
  }
</style>
