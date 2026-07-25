<script lang="ts">
  import { onDestroy, tick } from "svelte";
  import { threadActivityLabel, threadActivityTime, threadSummary } from "../../lib/chat/messages";
  import { enhanceMarkdown } from "../../lib/actions/markdown";
  import { time, markdown } from "../../lib/format";
  import type { MessageEditController } from "../../lib/messageEditing.svelte";
  import { uploadURL } from "../../lib/uploads";
  import ReactionsBar from "./ReactionsBar.svelte";
  import EmojiPicker, { QUICK_REACTS } from "./EmojiPicker.svelte";
  import MessageActionSheet from "./MessageActionSheet.svelte";
  import CopyLinkFallback from "./CopyLinkFallback.svelte";
  import { shouldOpenUpward } from "../../lib/popover";
  import type { ReactionController } from "../../lib/reactions.svelte";
  import type { Message, Upload } from "../../lib/types";
  import MediaAttachment from "../MediaAttachment.svelte";
  import MessageEditor from "./MessageEditor.svelte";
  import QuoteBlock from "./QuoteBlock.svelte";
  import PreambleBlock from "./PreambleBlock.svelte";

  type Props = {
    message: Message;
    index: number;
    previousMessage?: Message;
    nextMessage?: Message;
    selected: boolean;
    replyContext: "channel" | "dm";
    selectedThreadID?: string;
    currentUserID?: string;
    reactionController: ReactionController;
    reactionsDisabled?: boolean;
    canDeleteAnyMessage?: boolean;
    deleting?: boolean;
    editController?: MessageEditController;
    editScope?: string;
    onMessageEdited?: (message: Message) => void;
    onReply: (message: Message, context: "channel" | "dm") => void;
    onOpenThread: (message: Message) => void;
    onJumpToQuote: (message: Message) => void;
    onOpenImage: (url: string, title: string) => void;
    onOpenArtifact: (upload: Upload) => void;
    onRetry?: (message: Message) => void;
    onDiscard?: (message: Message) => void;
    onDeleteMessage?: (message: Message) => void;
    citationURL?: string;
  };

  let {
    message,
    index,
    previousMessage,
    nextMessage,
    selected,
    replyContext,
    selectedThreadID,
    currentUserID,
    reactionController,
    reactionsDisabled = false,
    canDeleteAnyMessage = false,
    deleting = false,
    editController,
    editScope = "",
    onMessageEdited,
    onReply,
    onOpenThread,
    onJumpToQuote,
    onOpenImage,
    onOpenArtifact,
    onRetry,
    onDiscard,
    onDeleteMessage,
    citationURL = "",
  }: Props = $props();

  let editSession = $derived(editController?.session(editScope));
  let editing = $derived(
    editSession?.surface === "timeline" && editSession.messageID === message.id,
  );

  // Editing now starts from the ⋮ menu, so focus lands back on its trigger.
  async function restoreEditEntryFocus() {
    await tick();
    moreButton?.focus();
  }

  function handleEditStart() {
    const result = editController?.start(editScope, message, "timeline");
    if (result === "cancelled") void restoreEditEntryFocus();
  }

  function handleEditCancel() {
    if (editController?.cancel(editScope, "timeline")) void restoreEditEntryFocus();
  }

  async function handleEditSave() {
    if (!editController) return;
    const result = await editController.save(editScope, message, (updated) =>
      onMessageEdited?.(updated),
    );
    if (result === "saved" || result === "cancelled") await restoreEditEntryFocus();
  }

  let isPending = $derived(message.status === "pending");
  let isFailed = $derived(message.status === "failed");
  let isDeleted = $derived(Boolean(message.deleted_at));
  let canDeleteMessage = $derived(
    canDeleteAnyMessage ||
      (Boolean(currentUserID) && (message.author?.id || message.author_id) === currentUserID),
  );
  let canEditMessage = $derived(
    Boolean(currentUserID) && (message.author?.id || message.author_id) === currentUserID,
  );
  // Coalesced agent activity: consecutive same-turn agent_commentary/agent_tool
  // rows are collapsed (client-side) into one synthetic row carrying a
  // preamble_block. When present, the row renders as a single preamble block
  // (incrementing commentary + collapsed tool sub-items, collapse-to-one-line
  // when the turn ends) instead of the final-answer treatment.
  let preambleBlock = $derived(message.preamble_block);
  // Boxed preamble<->answer cohesion. Within an agent message group the
  // synthetic preamble row is immediately followed by the same author's final
  // answer (coalesceAgentActivity anchors the block at the turn, ordinary
  // messages pass through), so within-group adjacency alone identifies the
  // pair. The preamble that precedes a final answer and the answer that follows
  // a preamble share one bordered card with a flat internal seam, mirroring the
  // ClawCanvas inline model so the activity log and the answer read as one unit.
  let followsPreamble = $derived(Boolean(previousMessage?.preamble_block) && !preambleBlock);
  let precedesFinalMessage = $derived(
    Boolean(preambleBlock) && Boolean(nextMessage) && !nextMessage?.preamble_block,
  );
  let threadReplyCount = $derived(message.thread_state?.reply_count || 0);
  let hasThreadReplies = $derived(threadReplyCount > 0);
  let threadTime = $derived(threadActivityTime(message));
  let isThreadOpen = $derived(selectedThreadID === message.id);
  let canOpenThread = $derived(
    !preambleBlock && !isPending && !isFailed && (!isDeleted || hasThreadReplies || isThreadOpen),
  );

  function openThreadFromRow(event: MouseEvent) {
    if (suppressRowClick || showActionSheet) {
      // A long-press just opened the action sheet; swallow the synthetic click.
      suppressRowClick = false;
      event.preventDefault();
      event.stopPropagation();
      return;
    }
    if (!canOpenThread) return;
    if (window.getSelection()?.toString()) return;
    const target = event.target as HTMLElement | null;
    if (target?.closest(MESSAGE_INTERACTIVE_TARGETS)) return;
    onOpenThread(message);
  }

  function openThreadOnClick(node: HTMLElement) {
    node.addEventListener("click", openThreadFromRow);
    return {
      destroy() {
        node.removeEventListener("click", openThreadFromRow);
      },
    };
  }

  // ---- Hover toolbar: quick reacts + full picker + ⋮ overflow menu ----

  let showReactPicker = $state(false);
  let showMenu = $state(false);
  let copyStatus = $state<"copied" | "failed" | "">("");
  let copyLinkFallback = $state("");
  let copyLinkReturnFocus = $state<HTMLElement>();
  let reactPickerUp = $state(false);
  let menuUp = $state(false);
  let rowEl = $state<HTMLDivElement>();
  let rowHovered = $state(false);
  let rowFocused = $state(false);
  let rowActive = $derived(rowHovered || rowFocused || selected);
  /* The hover toolbar straddles the row's top edge (top: -24px). When the row
     is flush with the scrollport's top edge that straddle would be clipped by
     overflow, so flip it to straddle the bottom edge instead. Measured when
     the row becomes hovered/focused — scrolling moves the pointer to another
     row, which re-measures. */
  let actionsFlipped = $state(false);

  function updateActionsFlip() {
    const scroller = rowEl?.closest(".messages-scroll");
    if (!rowEl || !scroller) {
      actionsFlipped = false;
      return;
    }
    const headroom = rowEl.getBoundingClientRect().top - scroller.getBoundingClientRect().top;
    actionsFlipped = headroom < 26;
  }
  let reactPickerWrap = $state<HTMLDivElement>();
  let menuWrap = $state<HTMLDivElement>();
  let addReactionButton = $state<HTMLButtonElement>();
  let moreButton = $state<HTMLButtonElement>();
  let copyStatusTimer: number | undefined;
  let sheetCloseTimer: number | undefined;
  let destroyed = false;
  let reactPickerId = $derived(`toolbar-reaction-picker-${message.id}`);
  let reactionPending = $derived(reactionController.pending(message.id));
  let cannotReact = $derived(
    reactionsDisabled || !currentUserID || isPending || isFailed || reactionPending,
  );

  function quickReact(emoji: string) {
    if (cannotReact) return;
    void reactionController.toggle(message, emoji);
  }

  function toggleReactPicker() {
    if (cannotReact) return;
    if (!showReactPicker) reactPickerUp = shouldOpenUpward(reactPickerWrap, 130);
    showReactPicker = !showReactPicker;
  }

  function chooseToolbarReaction(emoji: string) {
    if (cannotReact) return;
    void reactionController.toggle(message, emoji);
    showReactPicker = false;
  }

  function closeReactPicker() {
    showReactPicker = false;
    addReactionButton?.focus();
  }

  async function toggleMenu() {
    if (!showMenu) menuUp = shouldOpenUpward(menuWrap, 160);
    showMenu = !showMenu;
    if (!showMenu) return;
    await tick();
    menuItems()[0]?.focus();
  }

  function handleMoreActions() {
    if (coarsePointer) {
      openActionSheet(moreButton);
      return;
    }
    void toggleMenu();
  }

  function closeMenu(refocus = true) {
    showMenu = false;
    if (refocus) moreButton?.focus();
  }

  function menuItems() {
    return [...(menuWrap?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]:not(:disabled)') ?? [])]
      .filter((item) => item.getClientRects().length > 0);
  }

  function handleMenuKeydown(event: KeyboardEvent) {
    if (event.key === "Escape") {
      event.preventDefault();
      closeMenu();
      return;
    }
    if (event.key === "Tab") {
      closeMenu(false);
      return;
    }

    const items = menuItems();
    if (items.length === 0) return;
    const currentIndex = items.indexOf(document.activeElement as HTMLButtonElement);
    let nextIndex: number | undefined;
    if (event.key === "ArrowDown") {
      nextIndex = currentIndex < 0 ? 0 : (currentIndex + 1) % items.length;
    } else if (event.key === "ArrowUp") {
      nextIndex = currentIndex <= 0 ? items.length - 1 : currentIndex - 1;
    } else if (event.key === "Home") {
      nextIndex = 0;
    } else if (event.key === "End") {
      nextIndex = items.length - 1;
    }
    if (nextIndex === undefined) return;
    event.preventDefault();
    items[nextIndex]?.focus();
  }

  function setCopyStatus(status: "copied" | "failed") {
    if (destroyed) return;
    copyStatus = status;
    if (copyStatusTimer) window.clearTimeout(copyStatusTimer);
    copyStatusTimer = window.setTimeout(() => {
      copyStatus = "";
      copyStatusTimer = undefined;
    }, 1800);
  }

  async function copyMessageText() {
    closeMenu();
    await writeMessageToClipboard();
  }

  async function copyMessageLink(returnFocus?: HTMLElement) {
    closeMenu(false);
    if (!citationURL) return;
    copyLinkReturnFocus = returnFocus;
    try {
      if (!navigator.clipboard) throw new Error("Clipboard unavailable");
      await navigator.clipboard.writeText(citationURL);
      setCopyStatus("copied");
      returnFocus?.focus();
    } catch {
      setCopyStatus("failed");
      copyLinkFallback = citationURL;
    }
  }

  async function sheetCopyLink() {
    const returnFocus = actionSheetReturnFocus;
    closeActionSheet();
    await copyMessageLink(returnFocus);
  }

  async function writeMessageToClipboard(): Promise<boolean> {
    try {
      if (!navigator.clipboard) throw new Error("Clipboard unavailable");
      await navigator.clipboard.writeText(message.body ?? "");
      setCopyStatus("copied");
      return true;
    } catch {
      setCopyStatus("failed");
      return false;
    }
  }

  function menuEdit() {
    closeMenu(false);
    handleEditStart();
  }

  function menuDelete() {
    closeMenu(false);
    onDeleteMessage?.(message);
  }

  // ---- Touch: long-press opens a bottom action sheet ----
  const LONG_PRESS_MS = 450;
  const LONG_PRESS_SLOP_PX = 10;
  const MESSAGE_INTERACTIVE_TARGETS =
    "a, button, input, textarea, select, .attachment-grid, .media-tile, .markdown img, .gif-player, .markdown-table-scroll, .message-actions, .message-failed";
  const coarseQuery =
    typeof window !== "undefined" ? window.matchMedia("(hover: none), (pointer: coarse)") : null;
  let coarsePointer = $state(coarseQuery?.matches ?? false);
  let showActionSheet = $state(false);
  let longPressTimer: number | undefined;
  let longPressCleanup: (() => void) | undefined;
  let actionSheetGeneration = 0;
  let actionSheetReturnFocus = $state<HTMLElement>();
  let suppressRowClick = false;
  let actionSheetId = $derived(`message-action-sheet-${message.id}`);

  $effect(() => {
    if (!coarseQuery) return;
    const onChange = () => {
      coarsePointer = coarseQuery.matches;
    };
    coarseQuery.addEventListener("change", onChange);
    return () => coarseQuery.removeEventListener("change", onChange);
  });

  function clearLongPressTimer() {
    if (longPressTimer === undefined) return;
    window.clearTimeout(longPressTimer);
    longPressTimer = undefined;
  }

  function stopLongPressTracking() {
    longPressCleanup?.();
    longPressCleanup = undefined;
  }

  function clearSheetCloseTimer() {
    if (sheetCloseTimer === undefined) return;
    window.clearTimeout(sheetCloseTimer);
    sheetCloseTimer = undefined;
  }

  function openActionSheet(returnFocus?: HTMLElement) {
    clearSheetCloseTimer();
    actionSheetGeneration += 1;
    actionSheetReturnFocus = returnFocus;
    showMenu = false;
    showReactPicker = false;
    showActionSheet = true;
  }

  function handleRowPointerDown(event: PointerEvent) {
    if (
      event.pointerType !== "touch" ||
      !event.isPrimary ||
      event.button !== 0 ||
      preambleBlock ||
      isDeleted ||
      isPending ||
      isFailed ||
      editing
    ) {
      return;
    }
    const target = event.target as HTMLElement | null;
    if (target?.closest(MESSAGE_INTERACTIVE_TARGETS)) return;
    stopLongPressTracking();
    const pointerID = event.pointerId;
    const startX = event.clientX;
    const startY = event.clientY;
    longPressTimer = window.setTimeout(() => {
      longPressTimer = undefined;
      suppressRowClick = true;
      openActionSheet();
    }, LONG_PRESS_MS);
    const onMove = (moveEvent: PointerEvent) => {
      if (moveEvent.pointerId !== pointerID) return;
      if (
        Math.abs(moveEvent.clientX - startX) > LONG_PRESS_SLOP_PX ||
        Math.abs(moveEvent.clientY - startY) > LONG_PRESS_SLOP_PX
      ) {
        cleanup();
      }
    };
    const stop = (endEvent: PointerEvent) => {
      if (endEvent.pointerId !== pointerID) return;
      cleanup();
    };
    const cleanup = () => {
      clearLongPressTimer();
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", stop);
      window.removeEventListener("pointercancel", stop);
      if (longPressCleanup === cleanup) longPressCleanup = undefined;
    };
    longPressCleanup = cleanup;
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", stop);
    window.addEventListener("pointercancel", stop);
  }

  function handleRowContextMenu(event: MouseEvent) {
    // Long-press must not additionally pop the native context menu on touch.
    if (showActionSheet || longPressTimer !== undefined) {
      event.preventDefault();
    }
  }

  function closeActionSheet() {
    clearSheetCloseTimer();
    actionSheetGeneration += 1;
    showActionSheet = false;
    // The sheet's scrim can swallow the long-press mouseup, so the suppressed
    // click may never reach the row — clear the flag on close either way.
    suppressRowClick = false;
  }

  function sheetReact(emoji: string) {
    closeActionSheet();
    if (cannotReact) return;
    void reactionController.toggle(message, emoji);
  }

  function sheetOpenThread() {
    closeActionSheet();
    onOpenThread(message);
  }

  function sheetReply() {
    closeActionSheet();
    onReply(message, replyContext);
  }

  async function sheetCopy() {
    clearSheetCloseTimer();
    const generation = actionSheetGeneration;
    const copied = await writeMessageToClipboard();
    if (!copied || !showActionSheet || generation !== actionSheetGeneration) return;
    sheetCloseTimer = window.setTimeout(() => {
      sheetCloseTimer = undefined;
      if (!destroyed && generation === actionSheetGeneration) closeActionSheet();
    }, 900);
  }

  function sheetEdit() {
    closeActionSheet();
    handleEditStart();
  }

  function sheetDelete() {
    closeActionSheet();
    onDeleteMessage?.(message);
  }

  $effect(() => {
    if (!showReactPicker && !showMenu) return;
    const onDocClick = (event: MouseEvent) => {
      const target = event.target as Node;
      if (showReactPicker && reactPickerWrap && !reactPickerWrap.contains(target)) {
        showReactPicker = false;
      }
      if (showMenu && menuWrap && !menuWrap.contains(target)) {
        showMenu = false;
      }
    };
    document.addEventListener("click", onDocClick);
    return () => document.removeEventListener("click", onDocClick);
  });

  $effect(() => {
    if (cannotReact) showReactPicker = false;
  });

  onDestroy(() => {
    destroyed = true;
    if (copyStatusTimer) window.clearTimeout(copyStatusTimer);
    clearSheetCloseTimer();
    stopLongPressTracking();
  });

  // Virtua item wrappers carry `contain: layout style`, so each is its own
  // stacking context — a z-index inside one row can never beat a later
  // sibling item's toolbar. While a popover is open or the toolbar is shown
  // (it straddles the row's top edge, overlapping the neighboring item),
  // lift the enclosing virtualized item itself.
  $effect(() => {
    if (!(showMenu || showReactPicker || rowActive) || !rowEl) return;
    let item: HTMLElement | null = rowEl.parentElement;
    while (item && item.style.position !== "absolute") item = item.parentElement;
    if (!item) return;
    const previous = item.style.zIndex;
    item.style.zIndex = "10";
    return () => {
      item.style.zIndex = previous;
    };
  });
</script>

<!-- svelte-ignore a11y_no_static_element_interactions (Long-press supplements the focusable More actions button.) -->
<div
  bind:this={rowEl}
  class="message-row"
  class:selected
  class:is-pending={isPending}
  class:is-failed={isFailed}
  class:is-deleted={isDeleted}
  class:is-preamble={Boolean(preambleBlock)}
  class:is-preamble-collapsed={preambleBlock?.final === true}
  class:is-preamble-live={preambleBlock?.final === false}
  class:before-final-message={precedesFinalMessage}
  class:after-preamble={followsPreamble}
  class:can-open-thread={canOpenThread}
  class:editing={editing}
  class:menu-open={showMenu || showReactPicker}
  class:actions-flip={actionsFlipped}
  data-message-id={message.id}
  use:openThreadOnClick
  onpointerdown={handleRowPointerDown}
  oncontextmenu={handleRowContextMenu}
  onmouseenter={() => {
    // Measure only on the inactive → active transition. Re-measuring while
    // already active (e.g. focusin from clicking a toolbar button) could flip
    // the toolbar mid-click and teleport the button out from under the pointer.
    if (!rowHovered && !rowFocused) updateActionsFlip();
    rowHovered = true;
  }}
  onmouseleave={() => (rowHovered = false)}
  onfocusin={() => {
    if (!rowHovered && !rowFocused) updateActionsFlip();
    rowFocused = true;
  }}
  onfocusout={(event) => {
    if (!rowEl?.contains(event.relatedTarget as Node | null)) rowFocused = false;
  }}
>
  <span class="row-stamp" aria-hidden="true">{index === 0 ? "" : time(message.created_at)}</span>
  <div class="message-content">
    {#if preambleBlock}
      <PreambleBlock block={preambleBlock} />
    {:else if isDeleted}
      <div class="message-deleted">This message was deleted.</div>
    {:else if editing}
      {#if editSession}
        <MessageEditor
          body={editSession.draft}
          errorMessage={editSession.error}
          saving={editSession.saving}
          onBody={(body) => editController?.updateDraft(editScope, body)}
          onCancel={handleEditCancel}
          onSave={handleEditSave}
        />
      {/if}
    {:else}
    <QuoteBlock {message} onJump={onJumpToQuote} />
    <div class="markdown" use:enhanceMarkdown>{@html markdown(message.body)}</div>
    {#if message.edited_at}
      <span class="message-edit__indicator" title="Edited {time(message.edited_at)}">(edited)</span>
    {/if}
    {#if !isPending && !isFailed}
      <ReactionsBar
        messageId={message.id}
        reactions={reactionController.reactionsFor(message)}
        pending={reactionController.pending(message.id)}
        error={reactionController.error(message.id)}
        disabled={reactionsDisabled || !currentUserID}
        onToggle={(emoji) => void reactionController.toggle(message, emoji)}
      />
    {/if}
    {#if message.attachments?.length}
      <div class="attachment-grid" aria-label="Attachments">
        {#each message.attachments as attachment (attachment.id)}
          <MediaAttachment
            upload={attachment}
            url={uploadURL(attachment)}
            onOpenImage={onOpenImage}
            onOpenArtifact={onOpenArtifact}
          />
        {/each}
      </div>
    {/if}
    {#if isFailed}
      <div class="message-failed" role="alert">
        <span class="message-failed__label">Couldn't send.</span>
        {#if onRetry}
          <button type="button" class="message-failed__action" onclick={() => onRetry?.(message)}>Retry</button>
        {/if}
        {#if onDiscard}
          <button type="button" class="message-failed__action message-failed__action--ghost" onclick={() => onDiscard?.(message)}>Discard</button>
        {/if}
      </div>
    {/if}
    {/if}
    {#if canOpenThread}
    <button
      type="button"
      class:has-replies={hasThreadReplies}
      class:is-open={isThreadOpen}
      class="thread-hint tooltip"
      data-tooltip={threadSummary(message, selectedThreadID)}
      aria-label={threadSummary(message, selectedThreadID)}
      onclick={() => onOpenThread(message)}
    >
      <svg viewBox="0 0 24 24" width="13" height="13" aria-hidden="true">
        <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M21 12a8 8 0 0 1-11.6 7.16L3 21l1.84-6.4A8 8 0 1 1 21 12Z"/>
      </svg>
      {#if hasThreadReplies || isThreadOpen}
        <span>{threadActivityLabel(message)}</span>
        {#if threadTime}
          <time datetime={message.thread_state?.last_reply_at}>{threadTime}</time>
        {/if}
      {/if}
    </button>
    {/if}
  </div>
  {#if !preambleBlock && !isDeleted}
  <div class="message-actions" aria-label="Message actions">
    {#if copyStatus}
      <span
        class="message-copy-status"
        class:is-error={copyStatus === "failed"}
        role="status"
        aria-live="polite"
      >{copyStatus === "copied" ? "Copied" : "Couldn't copy"}</span>
    {/if}
    {#each QUICK_REACTS as emoji}
      <button
        type="button"
        class="message-action-react tooltip"
        aria-label={`React with ${emoji}`}
        data-tooltip={`React with ${emoji}`}
        disabled={cannotReact}
        onclick={() => quickReact(emoji)}
      >{emoji}</button>
    {/each}
    <div class="picker-wrapper" bind:this={reactPickerWrap}>
      <button
        bind:this={addReactionButton}
        type="button"
        aria-label="Add reaction"
        class="tooltip"
        data-tooltip="Add reaction"
        aria-controls={reactPickerId}
        aria-expanded={showReactPicker}
        disabled={cannotReact}
        onclick={toggleReactPicker}
      >
        <svg viewBox="0 0 24 24" width="15" height="15" aria-hidden="true">
          <g fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
            <circle cx="12" cy="12" r="9"/>
            <path d="M8 14s1.5 2 4 2 4-2 4-2M9 9h.01M15 9h.01"/>
          </g>
        </svg>
      </button>
      {#if showReactPicker}
        <EmojiPicker
          id={reactPickerId}
          placement={reactPickerUp ? "above-right" : "below"}
          disabled={cannotReact}
          onPick={chooseToolbarReaction}
          onEscape={closeReactPicker}
        />
      {/if}
    </div>
    <span class="action-sep" aria-hidden="true"></span>
    <button
      type="button"
      aria-label="Open thread"
      class="message-action-thread tooltip"
      data-tooltip={threadSummary(message, selectedThreadID)}
      disabled={isPending || isFailed}
      onclick={() => onOpenThread(message)}
    >
      <svg viewBox="0 0 24 24" width="14" height="14" aria-hidden="true">
        <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M21 12a8 8 0 0 1-11.6 7.16L3 21l1.84-6.4A8 8 0 1 1 21 12Z"/>
      </svg>
    </button>
    <button
      type="button"
      aria-label="Reply"
      class="message-action-reply tooltip"
      data-tooltip="Reply"
      disabled={isPending || isFailed}
      onclick={() => onReply(message, replyContext)}
    >
      <svg viewBox="0 0 24 24" width="14" height="14" aria-hidden="true">
        <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M9 17 4 12l5-5M4 12h11a5 5 0 0 1 5 5v3"/>
      </svg>
    </button>
    <span class="action-sep" aria-hidden="true"></span>
    <div class="message-more" bind:this={menuWrap}>
      <button
        bind:this={moreButton}
        type="button"
        aria-label="More actions"
        class="message-actions-trigger"
        class:tooltip={!coarsePointer}
        class:tooltip-align-end={!coarsePointer}
        data-tooltip={coarsePointer ? undefined : "More actions"}
        aria-haspopup={coarsePointer ? "dialog" : "menu"}
        aria-controls={coarsePointer ? actionSheetId : undefined}
        aria-expanded={coarsePointer ? showActionSheet : showMenu}
        disabled={isPending || isFailed}
        onclick={handleMoreActions}
      >
        <svg viewBox="0 0 24 24" width="14" height="14" aria-hidden="true">
          <g fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
            <circle cx="12" cy="5" r="1.2"/><circle cx="12" cy="12" r="1.2"/><circle cx="12" cy="19" r="1.2"/>
          </g>
        </svg>
      </button>
      {#if showMenu}
        <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
        <div
          class="message-menu"
          class:above={menuUp}
          role="menu"
          aria-label="More actions"
          tabindex="-1"
          onkeydown={handleMenuKeydown}
        >
          <button type="button" role="menuitem" onclick={copyMessageText}>
            <svg viewBox="0 0 24 24" width="14" height="14" aria-hidden="true">
              <g fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <rect x="9" y="9" width="11" height="11" rx="2"/><path d="M5 15V5a2 2 0 0 1 2-2h10"/>
              </g>
            </svg>
            Copy text
          </button>
          {#if citationURL}
            <button
              type="button"
              role="menuitem"
              onclick={() => void copyMessageLink(moreButton)}
            >
              <svg viewBox="0 0 24 24" width="14" height="14" aria-hidden="true">
                <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/>
              </svg>
              Copy link
            </button>
          {/if}
          {#if canEditMessage && editController && editScope && !editing}
            <div class="menu-sep" role="separator"></div>
            <button type="button" role="menuitem" onclick={menuEdit}>
              <svg viewBox="0 0 24 24" width="14" height="14" aria-hidden="true">
                <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M17 3a2.83 2.83 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5Z"/>
              </svg>
              Edit message
            </button>
          {/if}
          {#if canDeleteMessage && onDeleteMessage}
            <div class="menu-sep" role="separator"></div>
            <button
              type="button"
              role="menuitem"
              class="menu-danger"
              aria-label="Delete message"
              disabled={deleting}
              onclick={menuDelete}
            >
              <svg viewBox="0 0 24 24" width="14" height="14" aria-hidden="true">
                <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M3 6h18M8 6V4h8v2m-1 5v6M9 11v6m-3-11 1 14h10l1-14"/>
              </svg>
              Delete message…
            </button>
          {/if}
        </div>
      {/if}
    </div>
  </div>
  {/if}
  {#if showActionSheet}
    <MessageActionSheet
      id={actionSheetId}
      canReact={!cannotReact}
      canReply={!isPending && !isFailed}
      canOpenThread={canOpenThread}
      canEdit={canEditMessage && Boolean(editController) && Boolean(editScope) && !editing}
      canDelete={canDeleteMessage && Boolean(onDeleteMessage)}
      {deleting}
      {copyStatus}
      canCopyLink={Boolean(citationURL)}
      onReact={sheetReact}
      onOpenThread={sheetOpenThread}
      onReply={sheetReply}
      onCopy={sheetCopy}
      onCopyLink={sheetCopyLink}
      onEdit={sheetEdit}
      onDelete={sheetDelete}
      onClose={closeActionSheet}
      returnFocus={actionSheetReturnFocus}
    />
  {/if}
  {#if copyLinkFallback}
    <CopyLinkFallback
      url={copyLinkFallback}
      onClose={() => (copyLinkFallback = "")}
      returnFocus={copyLinkReturnFocus}
    />
  {/if}
</div>
