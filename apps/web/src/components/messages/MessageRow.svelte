<script lang="ts">
  import { threadActivityLabel, threadActivityTime, threadSummary } from "../../lib/chat/messages";
  import { enhanceMarkdownGifs } from "../../lib/actions/markdownGifs";
  import { time, markdown } from "../../lib/format";
  import { uploadURL } from "../../lib/uploads";
  import type { Message, Upload } from "../../lib/types";
  import MediaAttachment from "../MediaAttachment.svelte";
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
    onReply: (message: Message, context: "channel" | "dm") => void;
    onOpenThread: (message: Message) => void;
    onJumpToQuote: (message: Message) => void;
    onOpenImage: (url: string, title: string) => void;
    onOpenArtifact: (upload: Upload) => void;
    onRetry?: (message: Message) => void;
    onDiscard?: (message: Message) => void;
  };

  let {
    message,
    index,
    previousMessage,
    nextMessage,
    selected,
    replyContext,
    selectedThreadID,
    onReply,
    onOpenThread,
    onJumpToQuote,
    onOpenImage,
    onOpenArtifact,
    onRetry,
    onDiscard,
  }: Props = $props();

  let isPending = $derived(message.status === "pending");
  let isFailed = $derived(message.status === "failed");
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

  function openThreadFromRow(event: MouseEvent) {
    if (preambleBlock || isPending || isFailed) return;
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
  class:is-preamble={Boolean(preambleBlock)}
  class:is-preamble-collapsed={preambleBlock?.final === true}
  class:is-preamble-live={preambleBlock?.final === false}
  class:before-final-message={precedesFinalMessage}
  class:after-preamble={followsPreamble}
  class:can-open-thread={!preambleBlock && !isPending && !isFailed}
  data-message-id={message.id}
  use:openThreadOnClick
>
  <span class="row-stamp" aria-hidden="true">{index === 0 ? "" : time(message.created_at)}</span>
  <div class="message-content">
    {#if preambleBlock}
      <PreambleBlock block={preambleBlock} />
    {:else}
    <QuoteBlock {message} onJump={onJumpToQuote} />
    <div class="markdown" use:enhanceMarkdownGifs>{@html markdown(message.body)}</div>
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
    {/if}
  </div>
  {#if !preambleBlock}
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
  {/if}
</div>
