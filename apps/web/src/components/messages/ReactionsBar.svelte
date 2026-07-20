<script lang="ts">
  import { tick } from "svelte";
  import type { ReactionSummary } from "../../lib/types";

  let {
    messageId,
    reactions = [],
    pending = false,
    error = "",
    disabled = false,
    onToggle,
  }: {
    messageId: string;
    reactions: ReactionSummary[];
    pending?: boolean;
    error?: string;
    disabled?: boolean;
    onToggle: (emoji: string) => void;
  } = $props();

  let groupedEntries = $derived(
    [...reactions].sort(
      (a, b) => b.count - a.count || a.emoji.localeCompare(b.emoji),
    ),
  );

  let showPicker = $state(false);
  let pickerRef = $state<HTMLDivElement>();
  let addButtonRef = $state<HTMLButtonElement>();
  let pickerId = $derived(`reaction-picker-${messageId}`);

  const QUICK_EMOJIS = ["👍", "❤️", "😂", "😮", "😢", "🙏", "🎉", "🔥", "💯", "👀", "🚀", "✅"];

  async function togglePicker() {
    if (disabled || pending) return;
    showPicker = !showPicker;
    if (!showPicker) return;
    await tick();
    pickerRef?.querySelector<HTMLButtonElement>(".emoji-option")?.focus();
  }

  function chooseReaction(emoji: string) {
    if (disabled || pending) return;
    onToggle(emoji);
    showPicker = false;
  }

  function handleClickOutside(e: MouseEvent) {
    if (pickerRef && !pickerRef.contains(e.target as Node)) {
      showPicker = false;
    }
  }

  function handlePickerKeydown(event: KeyboardEvent) {
    if (event.key !== "Escape") return;
    event.preventDefault();
    showPicker = false;
    addButtonRef?.focus();
  }

  $effect(() => {
    if (showPicker) {
      document.addEventListener("click", handleClickOutside);
      return () => document.removeEventListener("click", handleClickOutside);
    }
  });

  $effect(() => {
    if (disabled || pending) showPicker = false;
  });
</script>

<div class="reactions-bar">
  {#each groupedEntries as { emoji, count, reacted_by_me }}
    <button
      class="reaction-btn"
      class:me={reacted_by_me}
      onclick={() => onToggle(emoji)}
      disabled={disabled || pending}
      aria-pressed={reacted_by_me}
      aria-label="{emoji} — {count} reaction{count !== 1 ? 's' : ''}"
      title="{emoji}"
    >
      <span class="reaction-emoji">{emoji}</span>
      {#if count > 1}
        <span class="reaction-count">{count}</span>
      {/if}
    </button>
  {/each}

  <div class="picker-wrapper" bind:this={pickerRef}>
    <button
      bind:this={addButtonRef}
      class="reaction-btn add-btn"
      onclick={togglePicker}
      aria-label="Add reaction"
      aria-controls={pickerId}
      aria-expanded={showPicker}
      disabled={disabled || pending}
    >
      <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
        <path d="M8 3v10M3 8h10" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/>
      </svg>
    </button>

    {#if showPicker}
      <div
        class="emoji-grid"
        id={pickerId}
        role="group"
        aria-label="Choose a reaction"
      >
        {#each QUICK_EMOJIS as emoji}
          <button
            class="emoji-option"
            onclick={() => {
              chooseReaction(emoji);
            }}
            aria-label={`React with ${emoji}`}
            title={emoji}
            disabled={disabled || pending}
            onkeydown={handlePickerKeydown}
          >
            {emoji}
          </button>
        {/each}
      </div>
    {/if}
  </div>
  {#if error}<span class="reaction-error" role="status">{error}</span>{/if}
</div>

<style>
  .reactions-bar {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    align-items: center;
    margin-top: 4px;
    min-height: 28px;
  }

  .reaction-btn {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    padding: 2px 6px;
    border: 1px solid var(--border, color-mix(in srgb, var(--line, #e0e0e0) 50%, transparent));
    border-radius: var(--radius, 8px);
    background: transparent;
    cursor: pointer;
    font-size: 13px;
    line-height: 1.4;
    transition: background 0.1s, border-color 0.1s;
    color: var(--text-muted, #666);
  }

  .reaction-btn:hover {
    background: color-mix(in srgb, var(--accent, #5865f2) 10%, transparent);
    border-color: color-mix(in srgb, var(--accent, #5865f2) 30%, transparent);
  }

  .reaction-btn:disabled {
    cursor: wait;
    opacity: 0.55;
  }

  .reaction-btn.me {
    background: color-mix(in srgb, var(--accent, #5865f2) 15%, transparent);
    border-color: color-mix(in srgb, var(--accent, #5865f2) 40%, transparent);
  }

  .reaction-emoji {
    font-size: 14px;
    line-height: 1;
  }

  .reaction-count {
    font-size: 12px;
    font-weight: 600;
    color: var(--text-muted, #666);
  }

  .reaction-btn.me .reaction-count {
    color: var(--accent, #5865f2);
  }

  .add-btn {
    opacity: 0.6;
    padding: 2px 4px;
  }

  .add-btn:hover {
    opacity: 1;
  }

  .picker-wrapper {
    position: relative;
  }

  .emoji-grid {
    position: absolute;
    bottom: 100%;
    left: 0;
    margin-bottom: 4px;
    display: grid;
    grid-template-columns: repeat(6, 1fr);
    gap: 2px;
    padding: 6px;
    background: var(--panel, #fff);
    border: 1px solid var(--border, #e0e0e0);
    border-radius: var(--radius, 8px);
    box-shadow: 0 4px 12px rgba(0,0,0,0.12);
    z-index: 100;
    min-width: 200px;
  }

  .emoji-option {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 32px;
    height: 32px;
    padding: 0;
    border: none;
    border-radius: 4px;
    background: transparent;
    cursor: pointer;
    font-size: 18px;
    line-height: 1;
    transition: background 0.1s;
  }

  .emoji-option:hover {
    background: color-mix(in srgb, var(--accent, #5865f2) 12%, transparent);
  }

  .reaction-error {
    color: var(--danger, #b42318);
    font-size: 12px;
  }
</style>
