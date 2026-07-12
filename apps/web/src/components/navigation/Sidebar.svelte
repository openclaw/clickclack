<script lang="ts">
  import Avatar from "../avatar/Avatar.svelte";
  import { avatarHue, directConversationForUser, handleLabel } from "../../lib/chat/people";
  import type { Channel, DirectConversation, User } from "../../lib/types";
  import ChannelList from "./ChannelList.svelte";
  import DirectMessageList from "./DirectMessageList.svelte";

  type Props = {
    workspaceID: string;
    workspaceName?: string;
    workspaceIconURL?: string;
    status: string;
    connected: boolean;
    sidebarCollapsed: boolean;
    showCollapse?: boolean;
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
    onOpenWorkspaceSettings: () => void;
  };

  let {
    workspaceID,
    workspaceName,
    workspaceIconURL,
    status,
    connected,
    sidebarCollapsed,
    showCollapse = true,
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
    onOpenWorkspaceSettings,
  }: Props = $props();

  type SectionState = { channels: boolean; directMessages: boolean; people: boolean };
  const SECTION_STORAGE_PREFIX = "clickclack:sidebar-sections:v1:";
  const DEFAULT_SECTION_STATE: SectionState = { channels: true, directMessages: true, people: true };
  let sections = $state<SectionState>({ ...DEFAULT_SECTION_STATE });

  function isSectionState(value: unknown): value is SectionState {
    if (!value || typeof value !== "object") return false;
    const candidate = value as Record<string, unknown>;
    return typeof candidate.channels === "boolean" && typeof candidate.directMessages === "boolean" && typeof candidate.people === "boolean";
  }

  function loadSections(id: string): SectionState {
    if (!id) return { ...DEFAULT_SECTION_STATE };
    try {
      const raw = window.localStorage.getItem(`${SECTION_STORAGE_PREFIX}${id}`);
      if (!raw) return { ...DEFAULT_SECTION_STATE };
      const parsed: unknown = JSON.parse(raw);
      return isSectionState(parsed) ? parsed : { ...DEFAULT_SECTION_STATE };
    } catch {
      return { ...DEFAULT_SECTION_STATE };
    }
  }

  function toggleSection(section: keyof SectionState) {
    sections = { ...sections, [section]: !sections[section] };
    if (!workspaceID) return;
    try {
      window.localStorage.setItem(`${SECTION_STORAGE_PREFIX}${workspaceID}`, JSON.stringify(sections));
    } catch {
      // Storage is an enhancement; disclosures still work when it is unavailable.
    }
  }

  $effect(() => {
    sections = loadSections(workspaceID);
  });

  const CHANNEL_ORDER_STORAGE_PREFIX = "clickclack:sidebar-channel-order:v1:";
  const MAX_CHANNEL_ORDER_STORAGE_LENGTH = 1_000_000;
  const MAX_CHANNEL_ORDER_IDS = 10_000;
  const MAX_CHANNEL_ID_LENGTH = 128;
  let channelOrder = $state<string[]>([]);

  function channelOrderStorageKey(workspaceID: string, userID: string): string {
    return `${CHANNEL_ORDER_STORAGE_PREFIX}${userID}:${workspaceID}`;
  }

  function parseChannelOrder(raw: string | null): string[] {
    if (!raw || raw.length > MAX_CHANNEL_ORDER_STORAGE_LENGTH) return [];
    try {
      const parsed: unknown = JSON.parse(raw);
      return Array.isArray(parsed) &&
        parsed.length <= MAX_CHANNEL_ORDER_IDS &&
        parsed.every((id) => typeof id === "string" && id.length <= MAX_CHANNEL_ID_LENGTH)
        ? [...new Set(parsed)]
        : [];
    } catch {
      return [];
    }
  }

  function loadChannelOrder(workspaceID: string, userID: string): string[] {
    if (!workspaceID || !userID) return [];
    try {
      return parseChannelOrder(window.localStorage.getItem(channelOrderStorageKey(workspaceID, userID)));
    } catch {
      return [];
    }
  }

  function saveChannelOrder(order: string[]) {
    channelOrder = order;
    if (!workspaceID || !currentUser?.id) return;
    try {
      const key = channelOrderStorageKey(workspaceID, currentUser.id);
      const serialized = JSON.stringify(order);
      if (serialized.length > MAX_CHANNEL_ORDER_STORAGE_LENGTH) {
        window.localStorage.removeItem(key);
        return;
      }
      window.localStorage.setItem(key, serialized);
    } catch {
      // Storage is an enhancement; reordering still works for this session.
    }
  }

  function handleStorage(event: StorageEvent) {
    if (!workspaceID || !currentUser?.id) return;
    if (event.key !== channelOrderStorageKey(workspaceID, currentUser.id)) return;
    channelOrder = parseChannelOrder(event.newValue);
  }

  let orderedChannels = $derived.by(() => {
    const byID = new Map(channels.map((channel) => [channel.id, channel]));
    const saved = channelOrder.flatMap((id) => {
      const channel = byID.get(id);
      if (!channel) return [];
      byID.delete(id);
      return [channel];
    });
    return [...saved, ...byID.values()];
  });

  $effect(() => {
    channelOrder = loadChannelOrder(workspaceID, currentUser?.id || "");
  });

  function shouldHandleClientNavigation(event: MouseEvent): boolean {
    return event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey;
  }
</script>

<svelte:window onstorage={handleStorage} />

<aside class="sidebar" aria-label="Channels and DMs">
  <header class="workspace-header">
    {#if workspaceIconURL}
      <img class="workspace-header-icon" src={workspaceIconURL} alt="" />
    {/if}
    <div class="workspace-name">
      <strong>{workspaceName || "Pick a workspace"}</strong>
      <span class="presence" class:online={connected}>{connected ? "Connected" : status}</span>
    </div>
    <div class="workspace-header-actions">
      <button
        type="button"
        class="workspace-settings"
        aria-label="Workspace settings"
        title="Workspace settings"
        onclick={onOpenWorkspaceSettings}
      >
        <svg viewBox="0 0 24 24" width="15" height="15" aria-hidden="true" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2">
          <circle cx="12" cy="12" r="3" />
          <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09a1.65 1.65 0 0 0-1-1.51 1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09a1.65 1.65 0 0 0 1.51-1 1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1Z" />
        </svg>
      </button>
      {#if showCollapse}
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
      {/if}
    </div>
  </header>

  <div class="sidebar-scroll">
    <ChannelList
      expanded={sections.channels}
      channels={orderedChannels}
      {selectedChannelID}
      {selectedDirectID}
      {hrefForChannel}
      {onSelectChannel}
      {onCreateChannel}
      onToggle={() => toggleSection("channels")}
      onReorder={saveChannelOrder}
    />

    <DirectMessageList
      expanded={sections.directMessages}
      conversations={directConversations}
      currentUserID={currentUser?.id}
      {selectedDirectID}
      {hrefForDirect}
      {onSelectDirect}
      {onCreateDirect}
      {onHideDirect}
      {hiddenDirectTitle}
      {onUndoHideDirect}
      onToggle={() => toggleSection("directMessages")}
    />

    <section class="nav-section" class:collapsed={!sections.people}>
      <div class="section-title">
        <button type="button" class="section-toggle" aria-expanded={sections.people} aria-controls="sidebar-people-list" onclick={() => toggleSection("people")}>
          <span class="caret" aria-hidden="true">▾</span>
          <span class="label">People</span>
        </button>
      </div>
      <div class="nav-list" id="sidebar-people-list" hidden={!sections.people}>
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
