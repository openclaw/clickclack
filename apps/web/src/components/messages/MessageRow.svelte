<script lang="ts">
  import { threadSummary } from "../../lib/chat/messages";
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
  // Durable agent activity rows render inline but with a theme-tied accent and a
  // small badge so operators can tell commentary/tool output from the final
  // answer. The accent color comes from --activity-accent (blue on light, amber
  // on dark), never a hardcoded hex.
  let activityKind = $derived(
    message.kind === "agent_commentary" || message.kind === "agent_tool" ? message.kind : "",
  );
  let activityLabel = $derived(activityKind === "agent_tool" ? "tool" : "commentary");
</script>

<div
  class="message-row"
  class:selected
  class:is-pending={isPending}
  class:is-failed={isFailed}
  class:is-activity={activityKind !== ""}
  data-message-id={message.id}
  data-message-kind={activityKind || undefined}
>
  <span class="row-stamp" aria-hidden="true">{index === 0 ? "" : time(message.created_at)}</span>
  <div class="message-content">
    {#if activityKind !== ""}
      <span class="activity-badge" data-activity-kind={activityKind}>
        {#if activityKind === "agent_tool"}
          <svg viewBox="0 0 24 24" width="11" height="11" aria-hidden="true">
            <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M14.7 6.3a4 4 0 0 0-5.4 5.4L3 18v3h3l6.3-6.3a4 4 0 0 0 5.4-5.4l-2.5 2.5-2-2 2.5-2.5Z"/>
          </svg>
        {:else}
          <svg viewBox="0 0 24 24" width="11" height="11" aria-hidden="true">
            <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M21 12a8 8 0 0 1-11.6 7.16L3 21l1.84-6.4A8 8 0 1 1 21 12Z"/>
          </svg>
        {/if}
        <span>{activityLabel}</span>
      </span>
    {/if}
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
