<script lang="ts">
  /**
   * COGNITIVE OS — Inline Message Utility Bar (T4)
   *
   * Non-modal, no popups/modals. Revealed on hover/tap per message.
   * All six actions wired to real cognition service calls via callback props.
   * In-flight state communicated for the execution marker.
   */

  interface Props {
    messageId: string;
    visible: boolean;
    flip?: boolean;
    /** Callbacks for each action. If undefined, button is hidden. */
    onTransform?: (messageId: string) => void;
    onSummarize?: (messageId: string) => void;
    onExpand?: (messageId: string) => void;
    onThreadLink?: (messageId: string) => void;
    onMemoryLink?: (messageId: string) => void;
    onPersonaSwitch?: (messageId: string) => void;
    /** Transient in-flight states. */
    transforming?: boolean;
    querying?: boolean;
  }

  let {
    messageId,
    visible = false,
    flip = false,
    onTransform,
    onSummarize,
    onExpand,
    onThreadLink,
    onMemoryLink,
    onPersonaSwitch,
    transforming = false,
    querying = false,
  }: Props = $props();

  const hasCognition = $derived(true); // always shown in T4 (cognition service is live)
  const busy = $derived(transforming || querying);
</script>

{#if visible}
  <div
    class="cog-utilities"
    class:cog-utilities-flip={flip}
    class:busy
    aria-label="Message utilities"
    role="toolbar"
  >
    {#if onTransform}
      <button
        type="button"
        class="cog-util-btn"
        title="Transform message"
        aria-label="Transform message"
        disabled={busy}
        onclick={() => onTransform(messageId)}
      >
        <svg viewBox="0 0 24 24" width="12" height="12" aria-hidden="true">
          <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/>
          <circle cx="12" cy="12" r="3" fill="none" stroke="currentColor" stroke-width="2"/>
        </svg>
      </button>
    {/if}
    {#if onSummarize}
      <button
        type="button"
        class="cog-util-btn"
        title="Summarize"
        aria-label="Summarize message"
        disabled={busy}
        onclick={() => onSummarize(messageId)}
      >
        <svg viewBox="0 0 24 24" width="12" height="12" aria-hidden="true">
          <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M4 6h16M4 12h10M4 18h6"/>
        </svg>
      </button>
    {/if}
    {#if onExpand}
      <button
        type="button"
        class="cog-util-btn"
        title="Expand"
        aria-label="Expand message"
        disabled={busy}
        onclick={() => onExpand(messageId)}
      >
        <svg viewBox="0 0 24 24" width="12" height="12" aria-hidden="true">
          <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M15 3h6v6M9 21H3v-6M21 3l-7 7M3 21l7-7"/>
        </svg>
      </button>
    {/if}
    <span class="cog-util-sep" aria-hidden="true"></span>
    {#if onThreadLink}
      <button
        type="button"
        class="cog-util-btn"
        title="Thread link"
        aria-label="Navigate to associated thread"
        onclick={() => onThreadLink(messageId)}
      >
        <svg viewBox="0 0 24 24" width="12" height="12" aria-hidden="true">
          <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M21 12a8 8 0 0 1-11.6 7.16L3 21l1.84-6.4A8 8 0 1 1 21 12Z"/>
        </svg>
      </button>
    {/if}
    {#if onMemoryLink}
      <button
        type="button"
        class="cog-util-btn"
        title="Memory link"
        aria-label="Reveal memory graph nodes"
        disabled={busy}
        onclick={() => onMemoryLink(messageId)}
      >
        <svg viewBox="0 0 24 24" width="12" height="12" aria-hidden="true">
          <circle cx="12" cy="5" r="3" fill="none" stroke="currentColor" stroke-width="2"/>
          <circle cx="5" cy="19" r="3" fill="none" stroke="currentColor" stroke-width="2"/>
          <circle cx="19" cy="19" r="3" fill="none" stroke="currentColor" stroke-width="2"/>
          <path fill="none" stroke="currentColor" stroke-width="1.5" d="M10.5 7.5 7 16.5M14 7l3.5 9"/>
        </svg>
      </button>
    {/if}
    {#if onPersonaSwitch}
      <button
        type="button"
        class="cog-util-btn"
        title="Persona switch"
        aria-label="Switch persona filter"
        disabled={busy}
        onclick={() => onPersonaSwitch(messageId)}
      >
        <svg viewBox="0 0 24 24" width="12" height="12" aria-hidden="true">
          <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2M12 3a4 4 0 1 0 0 8 4 4 0 0 0 0-8z"/>
        </svg>
      </button>
    {/if}
    <span class="cog-util-indicator" title={busy ? "Cognition active" : "Cognition connected"} aria-hidden="true"></span>
  </div>
{/if}

<style>
  .cog-utilities {
    position: absolute;
    right: 0;
    top: -30px;
    display: flex;
    align-items: center;
    gap: 1px;
    padding: 2px;
    background: var(--panel);
    border: 1px solid var(--line);
    z-index: 6;
    opacity: 1;
    pointer-events: auto;
    transition: opacity 100ms ease;
  }

  .cog-utilities-flip {
    top: auto;
    bottom: -30px;
  }

  .cog-util-btn {
    display: grid;
    place-items: center;
    width: 26px;
    height: 26px;
    border: 0;
    background: transparent;
    color: var(--muted);
    cursor: pointer;
  }

  .cog-util-btn:hover:not(:disabled) {
    background: var(--hover-strong);
    color: var(--text-strong);
  }

  .cog-util-btn:disabled {
    opacity: 0.35;
    cursor: not-allowed;
  }

  .cog-util-btn:focus-visible {
    outline: 1px solid var(--text-strong);
    outline-offset: -1px;
  }

  .cog-util-sep {
    width: 1px;
    height: 14px;
    background: var(--line);
    margin: 0 3px;
  }

  .cog-util-indicator {
    width: 5px;
    height: 5px;
    background: var(--status-success);
    margin-left: 4px;
    margin-right: 2px;
  }

  .busy .cog-util-indicator {
    background: var(--status-executing);
    animation: cog-indicator-pulse 0.8s ease-in-out infinite;
  }

  @keyframes cog-indicator-pulse {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.3; }
  }

  @media (prefers-reduced-motion: reduce) {
    .cog-util-indicator {
      animation: none;
    }
  }

  @media (hover: none), (pointer: coarse) {
    .cog-utilities {
      width: 1px;
      height: 1px;
      top: 1px;
      bottom: auto;
      padding: 0;
      border: 0;
      background: transparent;
      overflow: visible;
    }

    .cog-utilities > * {
      display: none;
    }

    .cog-utilities-flip {
      top: 1px;
      bottom: auto;
    }
  }
</style>
