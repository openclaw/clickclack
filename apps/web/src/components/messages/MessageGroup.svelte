<script lang="ts">
  import Avatar from "../avatar/Avatar.svelte";
  import { handleLabel } from "../../lib/chat/people";
  import { time } from "../../lib/format";
  import type { Message, Upload } from "../../lib/types";
  import type { MessageGroup as MessageGroupType } from "../../lib/chat/messages";
  import MessageRow from "./MessageRow.svelte";

  type Props = {
    group: MessageGroupType;
    currentUserID?: string;
    selectedThreadID?: string;
    canDeleteAnyMessage?: boolean;
    deletingMessageIDs?: ReadonlySet<string>;
    replyContext: "channel" | "dm";
    onOpenProfile: (profile?: Message["author"]) => void;
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
    group,
    currentUserID,
    selectedThreadID,
    canDeleteAnyMessage = false,
    deletingMessageIDs = new Set<string>(),
    replyContext,
    onOpenProfile,
    onReply,
    onOpenThread,
    onJumpToQuote,
    onOpenImage,
    onOpenArtifact,
    onRetry,
    onDiscard,
    onDeleteMessage,
  }: Props = $props();

  const author = $derived(group.messages[0]?.author);
  const isBot = $derived(author?.kind === "bot");
  // Self = the current user's own messages. Bots never match the current user
  // id, so an agent group is never marked self even in a DM with the agent.
  const isSelf = $derived(
    !isBot && Boolean(currentUserID) && group.authorID === currentUserID,
  );
</script>

<article class="message-group" class:is-agent={isBot} class:is-self={isSelf}>
  <Avatar
    class={group.authorDeleted ? "avatar" : "avatar avatar-button"}
    id={group.authorID}
    name={group.authorName}
    src={group.authorAvatarURL}
    size={38}
    buttonLabel={group.authorDeleted ? undefined : `View profile for ${group.authorName}`}
    onclick={() => onOpenProfile(group.messages[0]?.author)}
  />
  <div class="group-body">
    <header>
      {#if group.authorDeleted}
        <span class="author-name author-name--static">{group.authorName}</span>
        <span class="bot-chip bot-chip--deleted">deleted bot</span>
      {:else}
        <button
          type="button"
          class="author-name"
          onclick={() => onOpenProfile(group.messages[0]?.author)}
        >{group.authorName}</button>
        {#if isBot}<span class="bot-chip">bot</span>{/if}
      {/if}
      {#if group.authorHandle}<span>{handleLabel(group.authorHandle)}</span>{/if}
      <time>{time(group.timestamp)}</time>
    </header>
    {#each group.messages as message, index (message.id)}
      <MessageRow
        {message}
        {index}
        previousMessage={group.messages[index - 1]}
        nextMessage={group.messages[index + 1]}
        selected={selectedThreadID === message.id}
        {replyContext}
        {selectedThreadID}
        {currentUserID}
        {canDeleteAnyMessage}
        deleting={deletingMessageIDs.has(message.id)}
        {onReply}
        {onOpenThread}
        {onJumpToQuote}
        {onOpenImage}
        {onOpenArtifact}
        {onRetry}
        {onDiscard}
        {onDeleteMessage}
      />
    {/each}
  </div>
</article>
