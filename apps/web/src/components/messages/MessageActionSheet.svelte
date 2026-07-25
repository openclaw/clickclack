<script lang="ts">
  import { onDestroy, onMount } from "svelte";
  import { portal } from "../../lib/actions/portal";
  import { SHEET_QUICK_REACTS } from "./EmojiPicker.svelte";

  type CopyStatus = "copied" | "failed" | "";

  let {
    id,
    canReact = false,
    canReply = false,
    showOpenThread = true,
    canOpenThread = false,
    canEdit = false,
    canDelete = false,
    deleting = false,
    canCopyLink = false,
    copyStatus = "",
    onReact,
    onOpenThread,
    onReply,
    onCopy,
    onCopyLink,
    onEdit,
    onDelete,
    onClose,
    returnFocus,
  }: {
    id: string;
    canReact?: boolean;
    canReply?: boolean;
    showOpenThread?: boolean;
    canOpenThread?: boolean;
    canEdit?: boolean;
    canDelete?: boolean;
    deleting?: boolean;
    canCopyLink?: boolean;
    copyStatus?: CopyStatus;
    onReact: (emoji: string) => void;
    onOpenThread: () => void;
    onReply: () => void;
    onCopy: () => void;
    onCopyLink: () => void;
    onEdit: () => void;
    onDelete: () => void;
    onClose: () => void;
    returnFocus?: HTMLElement;
  } = $props();

  let scrimRef = $state<HTMLDivElement>();
  let sheetRef = $state<HTMLDivElement>();
  let restoreFocus = true;
  const inertSiblings = new Set<HTMLElement>();

  function handleKeydown(event: KeyboardEvent) {
    if (event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      onClose();
      return;
    }
    if (event.key !== "Tab" || !sheetRef) return;
    const focusable = [...sheetRef.querySelectorAll<HTMLElement>(
      'a[href], button:not([disabled]), [tabindex]:not([tabindex="-1"])',
    )].filter((element) => !element.inert && element.getClientRects().length > 0);
    if (focusable.length === 0) {
      event.preventDefault();
      sheetRef.focus();
      return;
    }
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    if (event.shiftKey && (document.activeElement === first || !sheetRef.contains(document.activeElement))) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && (document.activeElement === last || !sheetRef.contains(document.activeElement))) {
      event.preventDefault();
      first.focus();
    }
  }

  function runAndClose(action: () => void, shouldRestoreFocus = false) {
    restoreFocus = shouldRestoreFocus;
    action();
  }

  onMount(() => {
    const parent = scrimRef?.parentElement;
    if (parent) {
      for (const sibling of parent.children) {
        if (!(sibling instanceof HTMLElement) || sibling === scrimRef || sibling.inert) continue;
        sibling.inert = true;
        inertSiblings.add(sibling);
      }
    }
    sheetRef?.querySelector<HTMLButtonElement>("button:not(:disabled)")?.focus({
      preventScroll: true,
    });
  });

  onDestroy(() => {
    for (const sibling of inertSiblings) sibling.inert = false;
    inertSiblings.clear();
    if (restoreFocus && returnFocus?.isConnected) {
      returnFocus.focus({ preventScroll: true });
    }
  });
</script>

<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<div
  bind:this={scrimRef}
  class="message-sheet-scrim"
  role="presentation"
  use:portal
  onkeydown={handleKeydown}
>
  <button
    type="button"
    class="message-sheet-backdrop"
    tabindex="-1"
    aria-label="Close message actions"
    onclick={onClose}
  ></button>
  <div
    {id}
    bind:this={sheetRef}
    class="message-action-sheet"
    role="dialog"
    aria-modal="true"
    aria-label="Message actions"
    tabindex="-1"
  >
    <div class="sheet-grabber" aria-hidden="true"></div>
    <div class="sheet-emoji-row" role="group" aria-label="Choose a reaction">
      {#each SHEET_QUICK_REACTS as emoji}
        <button
          type="button"
          aria-label={`React with ${emoji}`}
          disabled={!canReact}
          onclick={() => runAndClose(() => onReact(emoji), true)}
        >{emoji}</button>
      {/each}
    </div>
    <div class="sheet-actions">
      {#if showOpenThread}
        <button
          type="button"
          disabled={!canOpenThread}
          onclick={() => runAndClose(onOpenThread)}
        >
          <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true">
            <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M21 12a8 8 0 0 1-11.6 7.16L3 21l1.84-6.4A8 8 0 1 1 21 12Z"/>
          </svg>
          Open thread
        </button>
      {/if}
      <button type="button" disabled={!canReply} onclick={() => runAndClose(onReply)}>
        <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true">
          <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M9 17 4 12l5-5M4 12h11a5 5 0 0 1 5 5v3"/>
        </svg>
        Reply
      </button>
      <button type="button" onclick={onCopy}>
        <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true">
          <g fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <rect x="9" y="9" width="11" height="11" rx="2"/><path d="M5 15V5a2 2 0 0 1 2-2h10"/>
          </g>
        </svg>
        Copy text
        {#if copyStatus}
          <span
            class="sheet-copy-status"
            class:is-error={copyStatus === "failed"}
            role="status"
            aria-live="polite"
          >{copyStatus === "copied" ? "Copied" : "Couldn't copy"}</span>
        {/if}
      </button>
      {#if canCopyLink}
        <button type="button" onclick={onCopyLink}>
          <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true">
            <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/>
          </svg>
          Copy link
        </button>
      {/if}
      {#if canEdit}
        <button type="button" onclick={() => runAndClose(onEdit)}>
          <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true">
            <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M17 3a2.83 2.83 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5Z"/>
          </svg>
          Edit message
        </button>
      {/if}
      {#if canDelete}
        <button
          type="button"
          class="sheet-danger"
          aria-label="Delete message"
          disabled={deleting}
          onclick={() => runAndClose(onDelete)}
        >
          <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true">
            <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M3 6h18M8 6V4h8v2m-1 5v6M9 11v6m-3-11 1 14h10l1-14"/>
          </svg>
          Delete message…
        </button>
      {/if}
    </div>
  </div>
</div>
