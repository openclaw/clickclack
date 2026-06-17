<script lang="ts">
  import Avatar from "../avatar/Avatar.svelte";
  import { dmAvatarUser, dmTitle } from "../../lib/chat/people";
  import type { DirectConversation } from "../../lib/types";

  type Props = {
    conversations: DirectConversation[];
    currentUserID?: string;
    selectedDirectID: string;
    hrefForDirect: (conversationID: string) => string;
    onSelectDirect: (conversationID: string) => void;
    onCreateDirect: () => void;
    onHideDirect: (conversationID: string) => void;
  };

  let {
    conversations,
    currentUserID,
    selectedDirectID,
    hrefForDirect,
    onSelectDirect,
    onCreateDirect,
    onHideDirect,
  }: Props = $props();

  function shouldHandleClientNavigation(event: MouseEvent): boolean {
    return event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey;
  }
</script>

<section class="nav-section">
  <div class="section-title">
    <span class="caret" aria-hidden="true">▾</span>
    <span class="label">Direct messages</span>
    <button
      type="button"
      class="add-button"
      aria-label="Start direct message"
      title="Start direct message"
      onclick={onCreateDirect}
    >＋</button>
  </div>
  <div class="nav-list">
    {#each conversations as conversation (conversation.id)}
      {@const dmUser = dmAvatarUser(conversation, currentUserID)}
      {@const unread = conversation.unread_count || 0}
      {@const isActive = conversation.id === selectedDirectID}
      <div class="dm-row" class:active={isActive}>
        <a
          href={hrefForDirect(conversation.id)}
          class="nav-item dm"
          class:active={isActive}
          class:has-unread={unread > 0 && !isActive}
          onclick={(event) => {
            if (!shouldHandleClientNavigation(event)) return;
            event.preventDefault();
            onSelectDirect(conversation.id);
          }}
        >
          <Avatar
            class="dm-avatar"
            id={dmUser?.id || conversation.id}
            name={dmUser?.display_name}
            src={dmUser?.avatar_url}
            size={22}
          />
          <span class="nav-label">{dmTitle(conversation, currentUserID)}</span>
          {#if unread > 0 && !isActive}
            <span class="unread-badge" aria-label={`${unread} unread`}>{unread > 99 ? "99+" : unread}</span>
          {:else}
            <span class="presence-dot" aria-hidden="true"></span>
          {/if}
        </a>
        <button
          type="button"
          class="dm-close"
          aria-label={`Close ${dmTitle(conversation, currentUserID)}`}
          title="Close direct message"
          onclick={(event) => {
            event.preventDefault();
            event.stopPropagation();
            onHideDirect(conversation.id);
          }}
        >×</button>
      </div>
    {/each}
    {#if conversations.length === 0}
      <p class="nav-empty">No direct messages yet</p>
    {/if}
  </div>
</section>
