<script lang="ts">
  import { threadActivityLabel, threadActivityTime, threadSummary } from "../../lib/chat/messages";
  import { enhanceMarkdownGifs } from "../../lib/actions/markdownGifs";
  import { time, markdown } from "../../lib/format";
  import { uploadURL } from "../../lib/uploads";
  import type { Message } from "../../lib/types";
  import MediaAttachment from "../MediaAttachment.svelte";
  import QuoteBlock from "./QuoteBlock.svelte";

  type Props = {
    message: Message;
    index: number;
    selected: boolean;
    replyContext: "channel" | "dm";
    selectedThreadID?: string;
    onReply: (message: Message, context: "channel" | "dm") => void;
    onOpenThread: (message: Message) => void;
    onJumpToQuote: (message: Message) => void;
    onOpenImage: (url: string, title: string) => void;
    onRetry?: (message: Message) => void;
    onDiscard?: (message: Message) => void;
  };

  let {
    message,
    index,
    selected,
    replyContext,
    selectedThreadID,
    onReply,
    onOpenThread,
    onJumpToQuote,
    onOpenImage,
    onRetry,
    onDiscard,
  }: Props = $props();

  let isPending = $derived(message.status === "pending");
  let isFailed = $derived(message.status === "failed");
  let threadReplyCount = $derived(message.thread_state?.reply_count || 0);
  let hasThreadReplies = $derived(threadReplyCount > 0);
  let threadTime = $derived(threadActivityTime(message));

  function openThreadFromRow(event: MouseEvent) {
    if (isPending || isFailed) return;
    if (window.getSelection()?.toString()) return;
    const target = event.target as HTMLElement | null;
    if (
      target?.closest(
        "a, button, input, textarea, select, .attachment-grid, .media-tile, .markdown img, .gif-player, .message-actions, .message-failed"
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
  class:can-open-thread={!isPending && !isFailed}
  data-message-id={message.id}
  use:openThreadOnClick
>
  <span class="row-stamp" aria-hidden="true">{index === 0 ? "" : time(message.created_at)}</span>
  <div class="message-content">
    <QuoteBlock {message} onJump={onJumpToQuote} />
    <div class="markdown" use:enhanceMarkdownGifs>{@html markdown(message.body)}</div>
    {#if message.attachments?.length}
      <div class="attachment-grid" aria-label="Attachments">
        {#each message.attachments as attachment (attachment.id)}
          <MediaAttachment
            upload={attachment}
            url={uploadURL(attachment)}
            onOpenImage={onOpenImage}
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
    <button
      type="button"
      class:has-replies={hasThreadReplies}
      class:is-open={selectedThreadID === message.id}
      class="thread-hint tooltip"
      data-tooltip={threadSummary(message, selectedThreadID)}
      aria-label={threadSummary(message, selectedThreadID)}
      disabled={isPending || isFailed}
      onclick={() => onOpenThread(message)}
    >
      <svg viewBox="0 0 24 24" width="13" height="13" aria-hidden="true">
        <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M21 12a8 8 0 0 1-11.6 7.16L3 21l1.84-6.4A8 8 0 1 1 21 12Z"/>
      </svg>
      {#if hasThreadReplies || selectedThreadID === message.id}
        <span>{threadActivityLabel(message)}</span>
        {#if threadTime}
          <time datetime={message.thread_state?.last_reply_at}>{threadTime}</time>
        {/if}
      {/if}
    </button>
  </div>
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
  </div>
</div>
