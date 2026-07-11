<script lang="ts">
  import Avatar from "../avatar/Avatar.svelte";
  import { dmAvatarUser, dmTitle } from "../../lib/chat/people";
  import type { DirectConversation } from "../../lib/types";

  type Props = {
    expanded: boolean;
    conversations: DirectConversation[];
    currentUserID?: string;
    selectedDirectID: string;
    hrefForDirect: (conversationID: string) => string;
    onSelectDirect: (conversationID: string) => void;
    onCreateDirect: () => void;
    onHideDirect: (conversationID: string) => void;
    hiddenDirectTitle?: string;
    onUndoHideDirect: () => void;
    onToggle: () => void;
  };

  let {
    expanded,
    conversations,
    currentUserID,
    selectedDirectID,
    hrefForDirect,
    onSelectDirect,
    onCreateDirect,
    onHideDirect,
    hiddenDirectTitle,
    onUndoHideDirect,
    onToggle,
  }: Props = $props();

  let openActionsID = $state("");
  let unreadTotal = $derived(conversations.reduce((total, conversation) => total + (conversation.unread_count || 0), 0));

  function shouldHandleClientNavigation(event: MouseEvent): boolean {
    return event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey;
  }

  function toggleActions(conversationID: string) {
    openActionsID = openActionsID === conversationID ? "" : conversationID;
  }

  function closeActions() {
    openActionsID = "";
  }
</script>

<section class="nav-section" class:collapsed={!expanded}>
  <div class="section-title">
    <button type="button" class="section-toggle" aria-expanded={expanded} aria-controls="sidebar-direct-messages-list" onclick={onToggle}>
      <span class="caret" aria-hidden="true">▾</span>
      <span class="label">Direct messages</span>
    </button>
    {#if !expanded && unreadTotal > 0}
      <span class="section-unread-badge" aria-label={`${unreadTotal} unread`}>{unreadTotal > 99 ? "99+" : unreadTotal}</span>
    {/if}
    <button
      type="button"
      class="add-button"
      aria-label="Start direct message"
      title="Start direct message"
      onclick={onCreateDirect}
    >＋</button>
  </div>
  <div class="nav-list" id="sidebar-direct-messages-list" hidden={!expanded}>
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
          class="dm-actions-trigger"
          aria-label={`Direct message actions for ${dmTitle(conversation, currentUserID)}`}
          title="Direct message actions"
          aria-haspopup="menu"
          aria-expanded={openActionsID === conversation.id}
          onclick={(event) => {
            event.preventDefault();
            event.stopPropagation();
            toggleActions(conversation.id);
          }}
          onkeydown={(event) => {
            if (event.key === "Escape") closeActions();
          }}
        >…</button>
        {#if openActionsID === conversation.id}
          <div class="dm-actions-menu" role="menu">
            <button
              type="button"
              class="dm-actions-item"
              role="menuitem"
              onclick={(event) => {
                event.preventDefault();
                event.stopPropagation();
                closeActions();
                onHideDirect(conversation.id);
              }}
            >Close direct message</button>
          </div>
        {/if}
      </div>
    {/each}
    {#if conversations.length === 0}
      <p class="nav-empty">No direct messages yet</p>
    {/if}
    {#if hiddenDirectTitle}
      <div class="dm-undo" role="status">
        <span>Closed {hiddenDirectTitle}</span>
        <button type="button" onclick={onUndoHideDirect}>Undo</button>
      </div>
    {/if}
  </div>
</section>
