<script lang="ts">
  import Avatar from "../avatar/Avatar.svelte";
  import { enhanceMarkdown } from "../../lib/actions/markdown";
  import {
    handleLabel,
    isDeletedBot,
    userHandle,
  } from "../../lib/chat/people";
  import { markdown, time } from "../../lib/format";
  import { uploadURL } from "../../lib/uploads";
  import type { ReactionController } from "../../lib/reactions.svelte";
  import type { Message, ThreadState, Upload, User } from "../../lib/types";
  import ChatComposer from "../composer/ChatComposer.svelte";
  import MediaAttachment from "../MediaAttachment.svelte";
  import MessageEditor from "../messages/MessageEditor.svelte";
  import QuoteBlock from "../messages/QuoteBlock.svelte";
  import ReactionsBar from "../messages/ReactionsBar.svelte";

  type Props = {
    root: Message;
    replies: Message[];
    threadState: ThreadState | null;
    replyBody: string;
    replyTarget: Message | null;
    currentUserID?: string;
    reactionController: ReactionController;
    reactionsDisabled?: boolean;
    mentionPeople?: User[];
    replyDisabled?: boolean;
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
    editingMessageID?: string;
    editingMessageSurface?: "timeline" | "thread" | "";
    editingDraft?: string;
    editingError?: string;
    editingSaving?: boolean;
    onEditDraft?: (body: string) => void;
    onEditError?: (message: string) => void;
    onEditMessage?: (message: Message) => void;
    onSaveEdit?: (message: Message, body: string) => Promise<void>;
    onClearReply: () => void;
    onActivateThreadComposer: () => void;
    onInlineImagePointerUp: (event: PointerEvent) => void;
    onJumpToQuote: (message: Message) => void;
    onOpenImage: (url: string, title: string) => void;
    onOpenArtifact: (upload: Upload) => void;
  };

  let {
    root,
    replies,
    threadState,
    replyBody,
    replyTarget,
    currentUserID,
    reactionController,
    reactionsDisabled = false,
    mentionPeople = [],
    replyDisabled = false,
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
    editingMessageID = "",
    editingMessageSurface = "",
    editingDraft = "",
    editingError = "",
    editingSaving = false,
    onEditDraft,
    onEditError,
    onEditMessage,
    onSaveEdit,
    onClearReply,
    onActivateThreadComposer,
    onInlineImagePointerUp,
    onJumpToQuote,
    onOpenImage,
    onOpenArtifact,
  }: Props = $props();

  const canDelete = (message: Message) =>
    canDeleteAnyMessage ||
    (Boolean(currentUserID) && (message.author?.id || message.author_id) === currentUserID);
  const canEdit = (message: Message) =>
    Boolean(currentUserID) && (message.author?.id || message.author_id) === currentUserID;
  const isEditing = (message: Message) =>
    editingMessageSurface === "thread" && editingMessageID === message.id;
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
  class="thread-scroll"
  role="region"
  aria-label="Thread messages"
  onpointerdown={onActivateThreadComposer}
  onpointerup={onInlineImagePointerUp}
>
  <article class="thread-root" data-message-id={root.id}>
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
          {#if canEdit(root) && onEditMessage}
            <button
              type="button"
              class="thread-action-btn"
              aria-label="Edit message"
              data-tooltip="Edit message"
              onclick={() => onEditMessage?.(root)}
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
        {/if}
      </header>
      {#if root.deleted_at}
        <div class="message-deleted">This message was deleted.</div>
      {:else if isEditing(root) && onSaveEdit && onEditMessage}
        <MessageEditor
          message={root}
          body={editingDraft}
          errorMessage={editingError}
          saving={editingSaving}
          onBody={(body) => onEditDraft?.(body)}
          onError={(message) => onEditError?.(message)}
          onCancel={() => onEditMessage?.(root)}
          onSave={onSaveEdit}
        />
      {:else}
        <div class="markdown" use:enhanceMarkdown>{@html markdown(root.body)}</div>
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
  <div class="thread-divider"><span>{replies.length} {replies.length === 1 ? "reply" : "replies"}</span></div>
  <div class="reply-list">
    {#each replies as reply (reply.id)}
      <article class="reply" data-message-id={reply.id}>
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
              {#if canEdit(reply) && onEditMessage}
                <button
                  type="button"
                  class="thread-action-btn"
                  aria-label="Edit message"
                  data-tooltip="Edit message"
                  disabled={reply.status === "pending" || reply.status === "failed"}
                  onclick={() => onEditMessage?.(reply)}
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
            {/if}
          </header>
          {#if reply.deleted_at}
            <div class="message-deleted">This message was deleted.</div>
          {:else if isEditing(reply) && onSaveEdit && onEditMessage}
            <MessageEditor
              message={reply}
              body={editingDraft}
              errorMessage={editingError}
              saving={editingSaving}
              onBody={(body) => onEditDraft?.(body)}
              onError={(message) => onEditError?.(message)}
              onCancel={() => onEditMessage?.(reply)}
              onSave={onSaveEdit}
            />
          {:else}
            <QuoteBlock message={reply} onJump={onJumpToQuote} />
            <div class="markdown" use:enhanceMarkdown>{@html markdown(reply.body)}</div>
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
</div>
<ChatComposer
  value={replyBody}
  placeholder={replyDisabled ? "No active recipient" : "Reply in thread"}
  ariaLabel="Reply body"
  submitLabel="Reply"
  formClass="composer reply-composer"
  disabled={replyDisabled}
  replyTarget={replyTarget}
  {mentionPeople}
  onValue={onReplyBody}
  onSubmit={onSubmitReply}
  onKeydown={onReplyKeydown}
  onFocus={onReplyFocus}
  onInputRef={onReplyInputRef}
  onClearReply={onClearReply}
/>
