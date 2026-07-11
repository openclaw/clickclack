<script lang="ts">
  import Avatar from "../avatar/Avatar.svelte";
  import { enhanceMarkdownGifs } from "../../lib/actions/markdownGifs";
  import { handleLabel } from "../../lib/chat/people";
  import { markdown, time } from "../../lib/format";
  import { uploadURL } from "../../lib/uploads";
  import type { Message, ThreadState, Upload, User } from "../../lib/types";
  import ChatComposer from "../composer/ChatComposer.svelte";
  import MediaAttachment from "../MediaAttachment.svelte";
  import QuoteBlock from "../messages/QuoteBlock.svelte";

  type Props = {
    root: Message;
    replies: Message[];
    threadState: ThreadState | null;
    replyBody: string;
    replyTarget: Message | null;
    mentionPeople?: User[];
    onClose: () => void;
    onReplyBody: (value: string) => void;
    onSubmitReply: () => void;
    onReplyKeydown: (event: KeyboardEvent) => void;
    onReplyFocus: () => void;
    onReplyInputRef: (node: HTMLTextAreaElement | null) => void;
    onSetReplyTarget: (message: Message, context: "thread") => void;
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
    mentionPeople = [],
    onClose,
    onReplyBody,
    onSubmitReply,
    onReplyKeydown,
    onReplyFocus,
    onReplyInputRef,
    onSetReplyTarget,
    onClearReply,
    onActivateThreadComposer,
    onInlineImagePointerUp,
    onJumpToQuote,
    onOpenImage,
    onOpenArtifact,
  }: Props = $props();
</script>

<header>
  <div>
    <p>Thread</p>
    <strong>{threadState?.reply_count ?? replies.length} {(threadState?.reply_count ?? replies.length) === 1 ? "reply" : "replies"}</strong>
  </div>
  <button
    class="close"
    aria-label="Close thread"
    onclick={onClose}
  >×</button>
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
      src={root.author?.avatar_url}
      size={38}
    />
    <div class="group-body">
      <header>
        <strong>{root.author?.display_name || "Local User"}</strong>
        {#if root.author?.handle}<span>{handleLabel(root.author.handle)}</span>{/if}
        <time>{time(root.created_at)}</time>
        <button
          type="button"
          class="reply-quote-btn"
          aria-label="Reply"
          data-tooltip="Reply"
          onclick={() => onSetReplyTarget(root, "thread")}
        >↩</button>
      </header>
      <div class="markdown" use:enhanceMarkdownGifs>{@html markdown(root.body)}</div>
      {#if root.attachments?.length}
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
          src={reply.author?.avatar_url}
          size={30}
        />
        <div class="group-body">
          <header>
            <strong>{reply.author?.display_name || "Local User"}</strong>
            {#if reply.author?.handle}<span>{handleLabel(reply.author.handle)}</span>{/if}
            <time>{time(reply.created_at)}</time>
            <button
              type="button"
              class="reply-quote-btn"
              aria-label="Reply"
              data-tooltip="Reply"
              onclick={() => onSetReplyTarget(reply, "thread")}
            >↩</button>
          </header>
          <QuoteBlock message={reply} onJump={onJumpToQuote} />
          <div class="markdown" use:enhanceMarkdownGifs>{@html markdown(reply.body)}</div>
          {#if reply.attachments?.length}
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
  placeholder="Reply in thread"
  ariaLabel="Reply body"
  submitLabel="Reply"
  formClass="composer reply-composer"
  replyTarget={replyTarget}
  {mentionPeople}
  onValue={onReplyBody}
  onSubmit={onSubmitReply}
  onKeydown={onReplyKeydown}
  onFocus={onReplyFocus}
  onInputRef={onReplyInputRef}
  onClearReply={onClearReply}
/>
