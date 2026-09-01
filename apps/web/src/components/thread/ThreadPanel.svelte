<script lang="ts">
  import { onDestroy, tick, untrack } from "svelte";
  import type { ThreadController, ThreadScrollIntent } from "../../lib/thread.svelte";
  import HistoryLoader from "../messages/HistoryLoader.svelte";
  import { readableAPIError } from "../../lib/api";
  import Avatar from "../avatar/Avatar.svelte";
  import { enhanceMarkdown } from "../../lib/actions/markdown";
  import { enhanceMentions } from "../../lib/actions/mention-highlight";
  import {
    handleLabel,
    isDeletedBot,
    userHandle,
  } from "../../lib/chat/people";
  import { markdown, time } from "../../lib/format";
  import type { MessageEdit, MessageEditController } from "../../lib/messageEditing.svelte";
  import { uploadURL } from "../../lib/uploads";
  import type { ReactionController } from "../../lib/reactions.svelte";
  import type { Message, ThreadState, Upload, User } from "../../lib/types";
  import ChatComposer from "../composer/ChatComposer.svelte";
  import MediaAttachment from "../MediaAttachment.svelte";
  import MessageEditor from "../messages/MessageEditor.svelte";
  import QuoteBlock from "../messages/QuoteBlock.svelte";
  import ReactionsBar from "../messages/ReactionsBar.svelte";
  import AddReactionButton from "../messages/AddReactionButton.svelte";
  import MessageActionSheet from "../messages/MessageActionSheet.svelte";
  import CopyLinkFallback from "../messages/CopyLinkFallback.svelte";
  import AgentResponding from "../messages/AgentResponding.svelte";

  type Props = {
    history: ThreadController;
    root: Message;
    replies: Message[];
    threadState: ThreadState | null;
    replyBody: string;
    replyTarget: Message | null;
    currentUserID?: string;
    reactionController: ReactionController;
    reactionsDisabled?: boolean;
    mentionPeople?: User[];
    mentionAttentionUserID?: string;
    agentResponding?: boolean;
    respondingAgentNames?: string[];
    replyDisabled?: boolean;
    replySending?: boolean;
    replyError?: string;
    headerLabel?: string;
    headerDetail?: string;
    openHref?: string;
    onClose?: () => void;
    onBack?: () => void;
    onReplyBody: (value: string) => void;
    onSubmitReply: () => void;
    onReplyKeydown: (event: KeyboardEvent) => void;
    onReplyFocus: () => void;
    onReplyInputRef: (node: HTMLTextAreaElement | null) => void;
    canDeleteAnyMessage?: boolean;
    deletingMessageIDs?: ReadonlySet<string>;
    onSetReplyTarget: (message: Message, context: "thread") => void;
    onDeleteMessage?: (message: Message) => void;
    channelID?: string;
    pinnedMessageIDs?: ReadonlySet<string>;
    onTogglePin?: (message: Message, pinned: boolean) => Promise<void>;
    onCopyLink?: (message: Message) => Promise<string>;
    editController?: MessageEditController;
    editScope?: string;
    onMessageEdited?: (message: MessageEdit) => void;
    onClearReply: () => void;
    onActivateThreadComposer: () => void;
    onInlineImagePointerUp: (event: PointerEvent) => void;
    onJumpToQuote: (message: Message) => void;
    onOpenImage: (url: string, title: string) => void;
    onOpenArtifact: (upload: Upload) => void;
  };

  let {
    history,
    root,
    replies,
    threadState,
    replyBody,
    replyTarget,
    currentUserID,
    reactionController,
    reactionsDisabled = false,
    mentionPeople = [],
    mentionAttentionUserID,
    agentResponding = false,
    respondingAgentNames = [],
    replyDisabled = false,
    replySending = false,
    replyError = "",
    headerLabel = "Thread",
    headerDetail,
    openHref,
    onClose,
    onBack,
    onReplyBody,
    onSubmitReply,
    onReplyKeydown,
    onReplyFocus,
    onReplyInputRef,
    canDeleteAnyMessage = false,
    deletingMessageIDs = new Set<string>(),
    onSetReplyTarget,
    onDeleteMessage,
    channelID = "",
    pinnedMessageIDs = new Set<string>(),
    onTogglePin,
    onCopyLink,
    editController,
    editScope = "",
    onMessageEdited,
    onClearReply,
    onActivateThreadComposer,
    onInlineImagePointerUp,
    onJumpToQuote,
    onOpenImage,
    onOpenArtifact,
  }: Props = $props();

  let threadScroll = $state<HTMLDivElement>();
  let scrollGeneration = 0;
  let programmaticTop = -1;
  let pendingFrame = 0;
  let pendingIntent: ThreadScrollIntent | null = null;
  const viewportRootID = $derived(root.id);
  let highlightedID = $state("");
  function captureAnchor() {
    if (!threadScroll) return;
    const top = threadScroll.getBoundingClientRect().top;
    const row = [...threadScroll.querySelectorAll<HTMLElement>("[data-message-id]")]
      .find((node) => node.getBoundingClientRect().bottom > top);
    if (row) {
      history.anchor = {
        messageID: row.dataset.messageId!,
        offset: row.getBoundingClientRect().top - top,
      };
    }
  }

  function applyScroll(intent: ThreadScrollIntent) {
    if (!threadScroll || !threadScroll.getClientRects().length) return;
    if (intent === "latest" || (intent === "preserve" && history.following)) {
      threadScroll.scrollTop = threadScroll.scrollHeight;
    } else {
      const target = intent === "preserve" ? history.anchor?.messageID : intent.messageID;
      const row = target
        ? threadScroll.querySelector<HTMLElement>(`[data-message-id="${CSS.escape(target)}"]`)
        : null;
      if (row) {
        const offset = row.getBoundingClientRect().top - threadScroll.getBoundingClientRect().top;
        const desiredOffset = intent === "preserve"
          ? history.anchor?.offset ?? 0
          : (threadScroll.clientHeight - row.offsetHeight) / 2;
        threadScroll.scrollTop += offset - desiredOffset;
        if (intent !== "preserve") highlightedID = target!;
      }
    }
    programmaticTop = threadScroll.scrollTop;
    captureAnchor();
  }

  function scheduleScroll(intent: ThreadScrollIntent, capture = true) {
    if (intent === "preserve" && pendingIntent && pendingIntent !== "preserve") return;
    pendingIntent = intent;
    if (capture) captureAnchor();
    const generation = ++scrollGeneration;
    void tick().then(() => {
      if (generation !== scrollGeneration || destroyed) return;
      cancelAnimationFrame(pendingFrame);
      pendingFrame = requestAnimationFrame(() => {
        if (generation === scrollGeneration && !destroyed) {
          applyScroll(intent);
          pendingIntent = null;
        }
      });
    });
  }

  function onThreadScroll() {
    // Layout can clamp scrollTop before the queued target/anchor restore runs.
    // Only actual input may cancel that intent; a native clamp must not.
    if (!threadScroll || pendingIntent) return;
    if (Math.abs(threadScroll.scrollTop - programmaticTop) > 1) {
      scrollGeneration++;
      pendingIntent = null;
      history.following = !history.hasNewer &&
        threadScroll.scrollHeight - threadScroll.clientHeight - threadScroll.scrollTop < 48;
    }
    programmaticTop = -1;
    captureAnchor();
  }

  function userScrollIntent() {
    scrollGeneration++;
    pendingIntent = null;
    highlightedID = "";
  }

  async function loadHistory(edge: "older" | "newer") {
    try {
      await history.loadEdge(edge);
    } catch {
      // The edge owns its visible retry.
    }
  }

  async function jumpLatest() {
    try {
      await history.latest();
    } catch {
      // The thread owns its visible load error.
    }
  }

  $effect(() => {
    const node = threadScroll, id = viewportRootID;
    if (!node || !id) return;
    history.beforeChange = (intent) => scheduleScroll(intent);
    untrack(() => scheduleScroll(history.anchor ? "preserve" : "latest", false));
    const observer = new ResizeObserver(() => scheduleScroll("preserve", false));
    observer.observe(node);
    for (const content of node.querySelectorAll(".reply-list, .thread-root")) observer.observe(content);
    const visible = () => {
      if (document.visibilityState === "visible") scheduleScroll("preserve", false);
    };
    document.addEventListener("visibilitychange", visible);
    return () => {
      history.beforeChange = undefined;
      observer.disconnect();
      document.removeEventListener("visibilitychange", visible);
      scrollGeneration++;
      cancelAnimationFrame(pendingFrame);
    };
  });
  let editSession = $derived(editController?.session(editScope));
  $effect(() => {
    history.editingID = editSession?.surface === "thread" ? editSession.messageID : "";
  });
  const editReturnFocus = new Map<string, HTMLElement>();
  const canDelete = (message: Message) =>
    canDeleteAnyMessage ||
    (Boolean(currentUserID) && (message.author?.id || message.author_id) === currentUserID);
  const canEdit = (message: Message) =>
    Boolean(currentUserID) && (message.author?.id || message.author_id) === currentUserID;
  const isEditing = (message: Message) =>
    editSession?.surface === "thread" && editSession.messageID === message.id;

  async function restoreEditButtonFocus(messageID: string) {
    await tick();
    const messageElement = threadScroll?.querySelector<HTMLElement>(
      `[data-message-id="${CSS.escape(messageID)}"]`,
    );
    const preferredTarget =
      editReturnFocus.get(messageID) ??
      messageElement?.querySelector<HTMLButtonElement>(".thread-more-actions");
    editReturnFocus.delete(messageID);
    if (preferredTarget?.isConnected && preferredTarget.getClientRects().length > 0) {
      preferredTarget.focus();
      return;
    }
    messageElement?.querySelector<HTMLButtonElement>('button[aria-label="Edit message"]')?.focus();
  }

  function startEdit(message: Message, returnFocus?: HTMLElement) {
    if (returnFocus) editReturnFocus.set(message.id, returnFocus);
    else editReturnFocus.delete(message.id);
    const result = editController?.start(editScope, message, "thread");
    if (result === "cancelled") void restoreEditButtonFocus(message.id);
  }

  function cancelEdit(message: Message) {
    if (editController?.cancel(editScope, "thread")) void restoreEditButtonFocus(message.id);
  }

  async function saveEdit(message: Message) {
    if (!editController) return;
    const result = await editController.save(editScope, message, (updated) =>
      onMessageEdited?.(updated),
    );
    if (result === "saved" || result === "cancelled") {
      await restoreEditButtonFocus(message.id);
    }
  }

  const LONG_PRESS_MS = 450;
  const LONG_PRESS_SLOP_PX = 10;
  const MESSAGE_INTERACTIVE_TARGETS =
    "a, button, input, textarea, select, .attachment-grid, .media-tile, .markdown img, .gif-player, .markdown-table-scroll";
  let actionMessage = $state<Message>();
  let actionSheetReturnFocus = $state<HTMLElement>();
  let actionCopyStatus = $state<"copied" | "failed" | "">("");
  let copyLinkStatus = $state<"pending" | "failed" | "">("");
  let copyLinkFallback = $state("");
  let copyLinkReturnFocus = $state<HTMLElement>();
  let pinSaving = $state(false);
  let pinError = $state("");
  let longPressTimer: number | undefined;
  let longPressCleanup: (() => void) | undefined;
  let sheetCloseTimer: number | undefined;
  let actionSheetGeneration = 0;
  let destroyed = false;

  function actionSheetID(message: Message) {
    return `thread-message-action-sheet-${message.id}`;
  }

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

  function openActionSheet(message: Message, returnFocus?: HTMLElement) {
    clearSheetCloseTimer();
    actionSheetGeneration += 1;
    actionMessage = message;
    actionSheetReturnFocus = returnFocus;
    actionCopyStatus = "";
  }

  function closeActionSheet() {
    clearSheetCloseTimer();
    actionSheetGeneration += 1;
    actionMessage = undefined;
  }

  function handleMessagePointerDown(event: PointerEvent, message: Message) {
    if (
      event.pointerType !== "touch" ||
      !event.isPrimary ||
      event.button !== 0 ||
      message.deleted_at ||
      message.status === "pending" ||
      message.status === "failed" ||
      isEditing(message)
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
      openActionSheet(message);
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

  function handleMessageContextMenu(event: MouseEvent) {
    if (actionMessage || longPressTimer !== undefined) event.preventDefault();
  }

  function sheetReact(emoji: string) {
    const message = actionMessage;
    closeActionSheet();
    if (
      !message ||
      reactionsDisabled ||
      !currentUserID ||
      reactionController.pending(message.id)
    ) {
      return;
    }
    void reactionController.toggle(message, emoji);
  }

  function sheetReply() {
    const message = actionMessage;
    closeActionSheet();
    if (message) onSetReplyTarget(message, "thread");
  }

  async function sheetCopy() {
    const message = actionMessage;
    if (!message) return;
    clearSheetCloseTimer();
    const generation = actionSheetGeneration;
    try {
      if (!navigator.clipboard) throw new Error("Clipboard unavailable");
      await navigator.clipboard.writeText(message.body ?? "");
      if (destroyed || actionMessage?.id !== message.id) return;
      actionCopyStatus = "copied";
      sheetCloseTimer = window.setTimeout(() => {
        sheetCloseTimer = undefined;
        if (!destroyed && generation === actionSheetGeneration) closeActionSheet();
      }, 900);
    } catch {
      if (!destroyed && actionMessage?.id === message.id) actionCopyStatus = "failed";
    }
  }

  async function writeRootLink(): Promise<{ copied: boolean; fallback?: string }> {
    if (!onCopyLink || copyLinkStatus === "pending") return { copied: false };
    copyLinkStatus = "pending";
    let url: string;
    try {
      url = await onCopyLink(root);
    } catch {
      copyLinkStatus = "failed";
      return { copied: false };
    }
    try {
      if (!navigator.clipboard) throw new Error("Clipboard unavailable");
      await navigator.clipboard.writeText(url);
      copyLinkStatus = "";
      return { copied: true };
    } catch {
      copyLinkStatus = "";
      return { copied: false, fallback: url };
    }
  }

  async function copyRootLink(returnFocus?: HTMLElement) {
    const result = await writeRootLink();
    if (!result.copied && !result.fallback) return;
    if (result.fallback) {
      copyLinkReturnFocus = returnFocus;
      copyLinkFallback = result.fallback;
    } else {
      returnFocus?.focus({ preventScroll: true });
    }
  }

  async function sheetCopyLink() {
    const result = await writeRootLink();
    if (!result.copied && !result.fallback) return;
    const returnFocus = actionSheetReturnFocus;
    closeActionSheet();
    if (result.fallback) {
      await tick();
      copyLinkReturnFocus = returnFocus;
      copyLinkFallback = result.fallback;
    }
  }

  function sheetEdit() {
    const message = actionMessage;
    const returnFocus = actionSheetReturnFocus;
    closeActionSheet();
    if (message) startEdit(message, returnFocus);
  }

  function sheetDelete() {
    const message = actionMessage;
    closeActionSheet();
    if (message) onDeleteMessage?.(message);
  }

  async function togglePin(message: Message) {
    if (!channelID || !onTogglePin || pinSaving) return;
    pinSaving = true;
    pinError = "";
    try {
      await onTogglePin(message, pinnedMessageIDs.has(message.id));
    } catch (error) {
      pinError = readableAPIError(error, "Could not update pin");
    } finally {
      pinSaving = false;
    }
  }

  async function sheetTogglePin() {
    const message = actionMessage;
    if (!message) return;
    await togglePin(message);
    if (!pinError) closeActionSheet();
  }

  onDestroy(() => {
    destroyed = true;
    clearSheetCloseTimer();
    stopLongPressTracking();
    editReturnFocus.clear();
  });
</script>

<header>
  {#if onBack}
    <button
      type="button"
      class="thread-back"
      aria-label="Back to search results"
      data-tooltip="Back to search results"
      onclick={onBack}
    >
      <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true">
        <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="m15 18-6-6 6-6"/>
      </svg>
    </button>
  {/if}
  <div>
    <p>{headerLabel}</p>
    <strong>{headerDetail ?? `${threadState?.reply_count ?? replies.length} ${(threadState?.reply_count ?? replies.length) === 1 ? "reply" : "replies"}`}</strong>
    {#if pinError}
      <span class="thread-pin-error" role="status" aria-live="polite">{pinError}</span>
    {/if}
    {#if copyLinkStatus === "failed"}
      <span class="thread-pin-error" role="status" aria-live="polite">Couldn't create link</span>
    {/if}
  </div>
  {#if openHref}
    <a class="thread-open-link" href={openHref} target="_blank" rel="noopener">Open in ClickClack</a>
  {/if}
  {#if onClose}
    <button
      class="close"
      aria-label="Close thread"
      onclick={onClose}
    >&times;</button>
  {/if}
</header>
<div
  bind:this={threadScroll}
  class="thread-scroll"
  role="region"
  aria-label="Thread messages"
  onscroll={onThreadScroll}
  onwheel={userScrollIntent}
  ontouchstart={userScrollIntent}
  onkeydown={userScrollIntent}
  onpointerdown={() => { userScrollIntent(); onActivateThreadComposer(); }}
  onpointerup={onInlineImagePointerUp}
>
  <!-- svelte-ignore a11y_no_static_element_interactions (Long-press supplements the focusable More actions button.) -->
  <article
    class="thread-root"
    data-message-id={root.id}
    onpointerdown={(event) => handleMessagePointerDown(event, root)}
    oncontextmenu={handleMessageContextMenu}
  >
    <Avatar
      class="avatar"
      id={root.author?.id || root.author_id}
      name={root.author?.display_name}
      src={isDeletedBot(root.author) ? undefined : root.author?.avatar_url}
      size={38}
    />
    <div class="group-body">
      <header>
        <strong>{root.author?.display_name || "Local User"}</strong>
        {#if isDeletedBot(root.author)}
          <span class="bot-chip bot-chip--deleted">deleted bot</span>
        {/if}
        {#if userHandle(root.author)}<span>{handleLabel(userHandle(root.author))}</span>{/if}
        <time>{time(root.created_at)}</time>
        {#if !root.deleted_at && !isEditing(root)}
          <AddReactionButton
            messageId={root.id}
            disabled={reactionsDisabled || !currentUserID}
            pending={reactionController.pending(root.id)}
            buttonClass="thread-action-btn"
            onToggle={(emoji) => void reactionController.toggle(root, emoji)}
          />
          <button
            type="button"
            class="reply-quote-btn"
            aria-label="Reply"
            data-tooltip="Reply"
            onclick={() => onSetReplyTarget(root, "thread")}
          >
            <svg viewBox="0 0 24 24" width="14" height="14" aria-hidden="true">
              <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M9 17 4 12l5-5M4 12h11a5 5 0 0 1 5 5v3"/>
            </svg>
          </button>
          {#if channelID && onCopyLink}
            <button
              type="button"
              class="thread-action-btn"
              aria-label="Copy link"
              data-tooltip={copyLinkStatus === "pending" ? "Creating link…" : "Copy link"}
              disabled={copyLinkStatus === "pending"}
              onclick={(event) => void copyRootLink(event.currentTarget)}
            >
              <svg viewBox="0 0 24 24" width="14" height="14" aria-hidden="true">
                <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/>
              </svg>
            </button>
          {/if}
          {#if channelID && onTogglePin}
            <button
              type="button"
              class="thread-action-btn"
              aria-label={pinnedMessageIDs.has(root.id) ? "Unpin message" : "Pin message"}
              data-tooltip={pinnedMessageIDs.has(root.id) ? "Unpin message" : "Pin message"}
              disabled={pinSaving}
              onclick={() => void togglePin(root)}
            >
              <svg viewBox="0 0 24 24" width="14" height="14" aria-hidden="true">
                <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="m14 4 6 6-4 4v5l-2 2-5-5-4 4-1-1 4-4-5-5 2-2h5l4-4Z"/>
              </svg>
            </button>
          {/if}
          {#if canEdit(root) && editController && editScope}
            <button
              type="button"
              class="thread-action-btn"
              aria-label="Edit message"
              data-tooltip="Edit message"
              onclick={() => startEdit(root)}
            >
              <svg viewBox="0 0 24 24" width="14" height="14" aria-hidden="true">
                <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M17 3a2.83 2.83 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5Z"/>
              </svg>
            </button>
          {/if}
          {#if canDelete(root) && onDeleteMessage}
            <button
              type="button"
              class="thread-action-btn thread-action-btn--danger"
              aria-label="Delete message"
              data-tooltip="Delete message"
              disabled={deletingMessageIDs.has(root.id)}
              onclick={() => onDeleteMessage?.(root)}
            >
              <svg viewBox="0 0 24 24" width="14" height="14" aria-hidden="true">
                <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M3 6h18M8 6V4h8v2m-1 5v6M9 11v6m-3-11 1 14h10l1-14"/>
              </svg>
            </button>
          {/if}
          <button
            type="button"
            class="thread-action-btn thread-more-actions"
            aria-label="More actions"
            aria-haspopup="dialog"
            aria-controls={actionSheetID(root)}
            aria-expanded={actionMessage?.id === root.id}
            onclick={(event) => openActionSheet(root, event.currentTarget)}
          >
            <svg viewBox="0 0 24 24" width="14" height="14" aria-hidden="true">
              <g fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                <circle cx="12" cy="5" r="1.2"/><circle cx="12" cy="12" r="1.2"/><circle cx="12" cy="19" r="1.2"/>
              </g>
            </svg>
          </button>
        {/if}
      </header>
      {#if root.deleted_at}
        <div class="message-deleted">This message was deleted.</div>
      {:else if isEditing(root) && editSession}
        <MessageEditor
          body={editSession.draft}
          errorMessage={editSession.error}
          saving={editSession.saving}
          onBody={(body) => editController?.updateDraft(editScope, body)}
          onCancel={() => cancelEdit(root)}
          onSave={() => saveEdit(root)}
        />
      {:else}
        <div
          class="markdown"
          use:enhanceMarkdown
          use:enhanceMentions={{ people: mentionPeople, attentionUserID: mentionAttentionUserID }}
        >{@html markdown(root.body)}</div>
        {#if root.edited_at}
          <span class="message-edit__indicator" title="Edited {time(root.edited_at)}">(edited)</span>
        {/if}
        <ReactionsBar
          messageId={root.id}
          reactions={reactionController.reactionsFor(root)}
          pending={reactionController.pending(root.id)}
          error={reactionController.error(root.id)}
          disabled={reactionsDisabled || !currentUserID}
          onToggle={(emoji) => void reactionController.toggle(root, emoji)}
        />
      {/if}
      {#if !root.deleted_at && root.attachments?.length}
        <div class="attachment-grid compact" aria-label="Attachments">
          {#each root.attachments as attachment (attachment.id)}
            <MediaAttachment
              upload={attachment}
              url={uploadURL(attachment)}
              onOpenImage={onOpenImage}
              onOpenArtifact={onOpenArtifact}
            />
          {/each}
        </div>
      {/if}
    </div>
  </article>
  <div class="thread-divider"><span>{replies.length} of {threadState?.reply_count ?? replies.length} replies loaded</span></div>
  {#if history.hasOlder || history.loading.older || history.edgeError.older}
    <div class="thread-history">
      {#if history.loading.older}
        <HistoryLoader direction="older" rows={1} />
      {/if}
      {#if history.edgeError.older}
        <p role="alert">{history.edgeError.older}</p>
      {/if}
      <button
        class="ghost-action"
        disabled={history.loading.older}
        onclick={() => void loadHistory("older")}
      >{history.edgeError.older ? "Retry older replies" : "Load older replies"}</button>
    </div>
  {/if}
  <div class="reply-list">
    {#each replies as reply (reply.id)}
      <!-- svelte-ignore a11y_no_static_element_interactions (Long-press supplements the focusable More actions button.) -->
      <article
        class="reply"
        class:thread-target={highlightedID === reply.id}
        data-message-id={reply.id}
        onpointerdown={(event) => handleMessagePointerDown(event, reply)}
        oncontextmenu={handleMessageContextMenu}
      >
        <Avatar
          class="avatar small"
          id={reply.author?.id || reply.author_id}
          name={reply.author?.display_name}
          src={isDeletedBot(reply.author) ? undefined : reply.author?.avatar_url}
          size={30}
        />
        <div class="group-body">
          <header>
            <strong>{reply.author?.display_name || "Local User"}</strong>
            {#if isDeletedBot(reply.author)}
              <span class="bot-chip bot-chip--deleted">deleted bot</span>
            {/if}
            {#if userHandle(reply.author)}<span>{handleLabel(userHandle(reply.author))}</span>{/if}
            <time>{time(reply.created_at)}</time>
            {#if !reply.deleted_at && !isEditing(reply)}
              <AddReactionButton
                messageId={reply.id}
                disabled={reactionsDisabled || !currentUserID}
                pending={reactionController.pending(reply.id)}
                buttonClass="thread-action-btn"
                onToggle={(emoji) => void reactionController.toggle(reply, emoji)}
              />
              <button
                type="button"
                class="reply-quote-btn"
                aria-label="Reply"
                data-tooltip="Reply"
                onclick={() => onSetReplyTarget(reply, "thread")}
              >
                <svg viewBox="0 0 24 24" width="14" height="14" aria-hidden="true">
                  <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M9 17 4 12l5-5M4 12h11a5 5 0 0 1 5 5v3"/>
                </svg>
              </button>
              {#if channelID && onTogglePin}
                <button
                  type="button"
                  class="thread-action-btn"
                  aria-label={pinnedMessageIDs.has(reply.id) ? "Unpin message" : "Pin message"}
                  data-tooltip={pinnedMessageIDs.has(reply.id) ? "Unpin message" : "Pin message"}
                  disabled={pinSaving}
                  onclick={() => void togglePin(reply)}
                >
                  <svg viewBox="0 0 24 24" width="14" height="14" aria-hidden="true">
                    <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="m14 4 6 6-4 4v5l-2 2-5-5-4 4-1-1 4-4-5-5 2-2h5l4-4Z"/>
                  </svg>
                </button>
              {/if}
              {#if canEdit(reply) && editController && editScope}
                <button
                  type="button"
                  class="thread-action-btn"
                  aria-label="Edit message"
                  data-tooltip="Edit message"
                  disabled={reply.status === "pending" || reply.status === "failed"}
                  onclick={() => startEdit(reply)}
                >
                  <svg viewBox="0 0 24 24" width="14" height="14" aria-hidden="true">
                    <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M17 3a2.83 2.83 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5Z"/>
                  </svg>
                </button>
              {/if}
              {#if canDelete(reply) && onDeleteMessage}
                <button
                  type="button"
                  class="thread-action-btn thread-action-btn--danger"
                  aria-label="Delete message"
                  data-tooltip="Delete message"
                  disabled={deletingMessageIDs.has(reply.id)}
                  onclick={() => onDeleteMessage?.(reply)}
                >
                  <svg viewBox="0 0 24 24" width="14" height="14" aria-hidden="true">
                    <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M3 6h18M8 6V4h8v2m-1 5v6M9 11v6m-3-11 1 14h10l1-14"/>
                  </svg>
                </button>
              {/if}
              <button
                type="button"
                class="thread-action-btn thread-more-actions"
                aria-label="More actions"
                aria-haspopup="dialog"
                aria-controls={actionSheetID(reply)}
                aria-expanded={actionMessage?.id === reply.id}
                disabled={reply.status === "pending" || reply.status === "failed"}
                onclick={(event) => openActionSheet(reply, event.currentTarget)}
              >
                <svg viewBox="0 0 24 24" width="14" height="14" aria-hidden="true">
                  <g fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                    <circle cx="12" cy="5" r="1.2"/><circle cx="12" cy="12" r="1.2"/><circle cx="12" cy="19" r="1.2"/>
                  </g>
                </svg>
              </button>
            {/if}
          </header>
          {#if reply.deleted_at}
            <div class="message-deleted">This message was deleted.</div>
          {:else if isEditing(reply) && editSession}
            <MessageEditor
              body={editSession.draft}
              errorMessage={editSession.error}
              saving={editSession.saving}
              onBody={(body) => editController?.updateDraft(editScope, body)}
              onCancel={() => cancelEdit(reply)}
              onSave={() => saveEdit(reply)}
            />
          {:else}
            <QuoteBlock message={reply} onJump={onJumpToQuote} />
            <div
              class="markdown"
              use:enhanceMarkdown
              use:enhanceMentions={{ people: mentionPeople, attentionUserID: mentionAttentionUserID }}
            >{@html markdown(reply.body)}</div>
            {#if reply.edited_at}
              <span class="message-edit__indicator" title="Edited {time(reply.edited_at)}">(edited)</span>
            {/if}
            <ReactionsBar
              messageId={reply.id}
              reactions={reactionController.reactionsFor(reply)}
              pending={reactionController.pending(reply.id)}
              error={reactionController.error(reply.id)}
              disabled={reactionsDisabled || !currentUserID}
              onToggle={(emoji) => void reactionController.toggle(reply, emoji)}
            />
          {/if}
          {#if !reply.deleted_at && reply.attachments?.length}
            <div class="attachment-grid compact" aria-label="Attachments">
              {#each reply.attachments as attachment (attachment.id)}
                <MediaAttachment
                  upload={attachment}
                  url={uploadURL(attachment)}
                  onOpenImage={onOpenImage}
                  onOpenArtifact={onOpenArtifact}
                />
              {/each}
            </div>
          {/if}
        </div>
      </article>
    {/each}
  </div>
  {#if history.hasNewer || history.loading.newer || history.edgeError.newer}
    <div class="thread-history">
      {#if history.loading.newer}
        <HistoryLoader direction="newer" rows={1} />
      {/if}
      {#if history.edgeError.newer}
        <p role="alert">{history.edgeError.newer}</p>
      {/if}
      <button
        class="ghost-action"
        disabled={history.loading.newer}
        onclick={() => void loadHistory("newer")}
      >{history.edgeError.newer ? "Retry newer replies" : "Load newer replies"}</button>
    </div>
  {/if}
</div>
{#if history.hasNewer || !history.following || history.error}
  <div class="thread-history thread-history--latest"><button class="ghost-action" onclick={() => void jumpLatest()}>Jump to latest</button></div>
{/if}
{#if actionMessage}
  <MessageActionSheet
    id={actionSheetID(actionMessage)}
    canReact={Boolean(currentUserID) &&
      !reactionsDisabled &&
      !reactionController.pending(actionMessage.id)}
    canReply={!replyDisabled}
    showOpenThread={false}
    canEdit={canEdit(actionMessage) && Boolean(editController) && Boolean(editScope)}
    canPin={Boolean(channelID && onTogglePin)}
    pinned={pinnedMessageIDs.has(actionMessage.id)}
    pinning={pinSaving}
    {pinError}
    canDelete={canDelete(actionMessage) && Boolean(onDeleteMessage)}
    deleting={deletingMessageIDs.has(actionMessage.id)}
    copyStatus={actionCopyStatus}
    canCopyLink={actionMessage.id === root.id && Boolean(channelID && onCopyLink)}
    {copyLinkStatus}
    onReact={sheetReact}
    onOpenThread={() => {}}
    onReply={sheetReply}
    onCopy={sheetCopy}
    onCopyLink={sheetCopyLink}
    onEdit={sheetEdit}
    onTogglePin={sheetTogglePin}
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
<AgentResponding active={agentResponding} agentNames={respondingAgentNames} />
{#if replyError}<p class="composer-notice composer-notice--error" role="alert">{replyError}</p>{/if}
<ChatComposer
  value={replyBody}
  placeholder={replyDisabled ? "No active recipient" : "Reply in thread"}
  ariaLabel="Reply body"
  submitLabel="Reply"
  formClass="composer reply-composer"
  disabled={replyDisabled || replySending}
  replyTarget={replyTarget}
  {mentionPeople}
  onValue={onReplyBody}
  onSubmit={onSubmitReply}
  onKeydown={onReplyKeydown}
  onFocus={onReplyFocus}
  onInputRef={onReplyInputRef}
  onClearReply={onClearReply}
/>
