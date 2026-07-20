<script lang="ts">
  import { tick } from "svelte";
  import { threadActivityLabel, threadActivityTime, threadSummary } from "../../lib/chat/messages";
  import { enhanceMarkdown } from "../../lib/actions/markdown";
  import { time, markdown } from "../../lib/format";
  import type { MessageEditController } from "../../lib/messageEditing.svelte";
  import { uploadURL } from "../../lib/uploads";
  import ReactionsBar from "./ReactionsBar.svelte";
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
  }: Props = $props();

  let editButton = $state<HTMLButtonElement>();
  let editSession = $derived(editController?.session(editScope));
  let editing = $derived(
    editSession?.surface === "timeline" && editSession.messageID === message.id,
  );

  async function restoreEditButtonFocus() {
    await tick();
    editButton?.focus();
  }

  function handleEditStart() {
    const result = editController?.start(editScope, message, "timeline");
    if (result === "cancelled") void restoreEditButtonFocus();
  }

  function handleEditCancel() {
    if (editController?.cancel(editScope, "timeline")) void restoreEditButtonFocus();
  }

  async function handleEditSave() {
    if (!editController) return;
    const result = await editController.save(editScope, message, (updated) =>
      onMessageEdited?.(updated),
    );
    if (result === "saved" || result === "cancelled") await restoreEditButtonFocus();
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
    if (!canOpenThread) return;
    if (window.getSelection()?.toString()) return;
    const target = event.target as HTMLElement | null;
    if (
      target?.closest(
        "a, button, input, textarea, select, .attachment-grid, .media-tile, .markdown img, .gif-player, .markdown-table-scroll, .message-actions, .message-failed"
      )
    ) {
      return;
    }
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
</script>

<div
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
  data-message-id={message.id}
  use:openThreadOnClick
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
    <button
      type="button"
      aria-label="Reply"
      class="tooltip"
      data-tooltip="Reply"
      disabled={isPending || isFailed}
      onclick={() => onReply(message, replyContext)}
    >
      <svg viewBox="0 0 24 24" width="14" height="14" aria-hidden="true">
        <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M9 17 4 12l5-5M4 12h11a5 5 0 0 1 5 5v3"/>
      </svg>
    </button>
    <button
      type="button"
      aria-label="Open thread"
      class="tooltip"
      data-tooltip={threadSummary(message, selectedThreadID)}
      disabled={isPending || isFailed}
      onclick={() => onOpenThread(message)}
    >
      <svg viewBox="0 0 24 24" width="14" height="14" aria-hidden="true">
        <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M21 12a8 8 0 0 1-11.6 7.16L3 21l1.84-6.4A8 8 0 1 1 21 12Z"/>
      </svg>
    </button>
    {#if canEditMessage && editController && editScope && !editing}
        <button
          bind:this={editButton}
          type="button"
          aria-label="Edit message"
          class="tooltip message-action-edit"
          data-tooltip="Edit message"
          disabled={isPending || isFailed}
          onclick={handleEditStart}
        >
          <svg viewBox="0 0 24 24" width="14" height="14" aria-hidden="true">
            <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M17 3a2.83 2.83 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5Z"/>
          </svg>
        </button>
    {/if}
    {#if canDeleteMessage && onDeleteMessage}
        <button
          type="button"
          aria-label="Delete message"
          class="tooltip message-action-danger"
          data-tooltip="Delete message"
          disabled={isPending || isFailed || deleting}
          onclick={() => onDeleteMessage?.(message)}
        >
          <svg viewBox="0 0 24 24" width="14" height="14" aria-hidden="true">
            <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M3 6h18M8 6V4h8v2m-1 5v6M9 11v6m-3-11 1 14h10l1-14"/>
          </svg>
        </button>
    {/if}
  </div>
  {/if}
</div>
