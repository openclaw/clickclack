<script lang="ts">
  import Avatar from "../avatar/Avatar.svelte";
  import { avatarHue, directConversationForUser, handleLabel } from "../../lib/chat/people";
  import type { Channel, DirectConversation, User } from "../../lib/types";
  import ChannelList from "./ChannelList.svelte";
  import DirectMessageList from "./DirectMessageList.svelte";

  type Props = {
    workspaceName?: string;
    status: string;
    connected: boolean;
    sidebarCollapsed: boolean;
    channels: Channel[];
    directConversations: DirectConversation[];
    recentPeople: User[];
    currentUser: User | null;
    selectedChannelID: string;
    selectedDirectID: string;
    selectedProfile: User | null;
    onToggleCollapse: () => void;
    hrefForChannel: (channelID: string) => string;
    hrefForDirect: (conversationID: string) => string;
    onSelectChannel: (channelID: string) => void;
    onCreateChannel: () => void;
    onSelectDirect: (conversationID: string) => void;
    onCreateDirect: () => void;
    onHideDirect: (conversationID: string) => void;
    hiddenDirectTitle?: string;
    onUndoHideDirect: () => void;
    onOpenProfile: (profile: User) => void;
    onOpenSettings: () => void;
  };

  let {
    workspaceName,
    status,
    connected,
    sidebarCollapsed,
    channels,
    directConversations,
    recentPeople,
    currentUser,
    selectedChannelID,
    selectedDirectID,
    selectedProfile,
    onToggleCollapse,
    hrefForChannel,
    hrefForDirect,
    onSelectChannel,
    onCreateChannel,
    onSelectDirect,
    onCreateDirect,
    onHideDirect,
    hiddenDirectTitle,
    onUndoHideDirect,
    onOpenProfile,
    onOpenSettings,
  }: Props = $props();

  function shouldHandleClientNavigation(event: MouseEvent): boolean {
    return event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey;
  }
</script>

<aside class="sidebar" aria-label="Channels and DMs">
  <header class="workspace-header">
    <div class="workspace-name">
      <strong>{workspaceName || "Pick a workspace"}</strong>
      <span class="presence" class:online={connected}>{connected ? "Connected" : status}</span>
    </div>
    <button
      type="button"
      class="sidebar-collapse"
      aria-label={sidebarCollapsed ? "Expand sidebar" : "Collapse sidebar"}
      title={sidebarCollapsed ? "Expand sidebar" : "Collapse sidebar"}
      onclick={onToggleCollapse}
    >
      <svg viewBox="0 0 24 24" width="15" height="15" aria-hidden="true">
        <path
          fill="none"
          stroke="currentColor"
          stroke-linecap="round"
          stroke-linejoin="round"
          stroke-width="2"
          d={sidebarCollapsed ? "m9 6 6 6-6 6" : "m15 6-6 6 6 6"}
        />
      </svg>
    </button>
  </header>

  <div class="sidebar-scroll">
    <ChannelList
      {channels}
      {selectedChannelID}
      {selectedDirectID}
      {hrefForChannel}
      {onSelectChannel}
      {onCreateChannel}
    />

    <DirectMessageList
      conversations={directConversations}
      currentUserID={currentUser?.id}
      {selectedDirectID}
      {hrefForDirect}
      {onSelectDirect}
      {onCreateDirect}
      {onHideDirect}
      {hiddenDirectTitle}
      {onUndoHideDirect}
    />

    <section class="nav-section">
      <div class="section-title">
        <span class="caret" aria-hidden="true">▾</span>
        <span class="label">People</span>
      </div>
      <div class="nav-list">
        {#each recentPeople as person (person.id)}
          {@const conversation = directConversationForUser(directConversations, person.id)}
          <a
            href={conversation ? hrefForDirect(conversation.id) : "#"}
            class="nav-item dm"
            class:active={conversation?.id === selectedDirectID || selectedProfile?.id === person.id}
            onclick={(event) => {
              if (conversation) {
                if (!shouldHandleClientNavigation(event)) return;
                event.preventDefault();
                onSelectDirect(conversation.id);
              } else {
                event.preventDefault();
                onOpenProfile(person);
              }
            }}
          >
            <Avatar
              class="dm-avatar"
              id={person.id}
              name={person.display_name}
              src={person.avatar_url}
              size={22}
            />
            <span class="nav-label">{person.display_name}</span>
            <span class="presence-dot active" aria-hidden="true"></span>
          </a>
        {/each}
        {#if recentPeople.length === 0}
          <p class="nav-empty">People appear here as you chat</p>
        {/if}
      </div>
    </section>
  </div>

  {#if currentUser}
    <button
      class="user-card"
      type="button"
      onclick={onOpenSettings}
      oncontextmenu={(event) => {
        event.preventDefault();
        onOpenSettings();
      }}
      aria-label={`Account settings for ${currentUser.display_name} ${handleLabel(currentUser.handle)}`}
    >
      <Avatar
        class="dm-avatar"
        id={currentUser.id}
        name={currentUser.display_name}
        src={currentUser.avatar_url}
        size={28}
        loading="eager"
        fetchPriority="auto"
      />
      <div class="user-meta">
        <strong>{currentUser.display_name}</strong>
        <span>{currentUser.handle ? handleLabel(currentUser.handle) : connected ? "Active" : "Reconnecting…"}</span>
      </div>
      <span class="presence-dot active" aria-hidden="true"></span>
    </button>
  {/if}
</aside>
