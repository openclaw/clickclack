<script lang="ts">
  import { dmTitle } from "../../lib/chat/people";
  import { channelDisplayTitle, safeExternalChannelURL } from "../../lib/chat/channels";
  import type { Channel, ChannelNotificationPreference, DirectConversation } from "../../lib/types";

  type Props = {
    selectedDirect?: DirectConversation;
    selectedChannel?: Channel;
    workspaceName?: string;
    currentUserID?: string;
    searchQuery: string;
    threadOpen: boolean;
    pinnedOpen?: boolean;
    channelNotifPreference?: ChannelNotificationPreference | null;
    channelNotifSaving?: boolean;
    onSearchQuery: (value: string) => void;
    onSearch: () => void;
    onResetSearch: () => void;
    onToggleThread: () => void;
    onPinnedItems: () => void;
    onToggleChannelNotifications?: () => void;
    semanticPaneOpen?: boolean;
    onToggleSemanticPane?: () => void;
  };

  function notifTitle(pref: ChannelNotificationPreference): string {
    if (pref === "muted") return "Channel muted - click to change";
    if (pref === "mentions") return "Notifications for @mentions only - click to change";
    return "All notifications enabled - click to change";
  }

  let {
    selectedDirect,
    selectedChannel,
    workspaceName,
    currentUserID,
    searchQuery,
    threadOpen,
    pinnedOpen = false,
    channelNotifPreference = undefined,
    channelNotifSaving = false,
    onSearchQuery,
    onSearch,
    onResetSearch,
    onToggleThread,
    onPinnedItems,
    onToggleChannelNotifications = () => {},
    semanticPaneOpen = false,
    onToggleSemanticPane = () => {},
  }: Props = $props();

  const externalHref = $derived(selectedDirect ? undefined : safeExternalChannelURL(selectedChannel?.external_url));
</script>

<header class="topbar">
  <div class="topbar-title">
    {#if selectedDirect}
      <h1 class="with-glyph dm">{`@${dmTitle(selectedDirect, currentUserID)}`}</h1>
    {:else if selectedChannel}
      <h1 class="with-glyph channel">{`#${channelDisplayTitle(selectedChannel)}`}</h1>
      {#if channelNotifPreference}
        <button
          type="button"
          class="notif-toggle"
          title={notifTitle(channelNotifPreference)}
          aria-label={notifTitle(channelNotifPreference)}
          aria-busy={channelNotifSaving}
          disabled={channelNotifSaving}
          onclick={onToggleChannelNotifications}
        >
          {#if channelNotifPreference === "muted"}
            <span aria-hidden="true">M</span>
          {:else if channelNotifPreference === "mentions"}
            <span aria-hidden="true">@</span>
          {:else}
            <span aria-hidden="true">A</span>
          {/if}
        </button>
      {/if}
    {:else}
      <h1 class="with-glyph">ClickClack</h1>
    {/if}
    <span class="topbar-divider" aria-hidden="true"></span>
    <p class="topbar-meta">{workspaceName || "no workspace"}</p>
  </div>
  <form
    class="search"
    onsubmit={(event) => {
      event.preventDefault();
      onSearch();
    }}
  >
    <svg viewBox="0 0 24 24" width="14" height="14" aria-hidden="true">
      <circle cx="11" cy="11" r="7" fill="none" stroke="currentColor" stroke-width="2" />
      <path d="m20 20-3.5-3.5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
    </svg>
    <input
      value={searchQuery}
      placeholder="Search messages"
      aria-label="Search messages"
      oninput={(event) => onSearchQuery(event.currentTarget.value)}
    />
    {#if searchQuery}
      <button type="button" class="search-clear" aria-label="Reset" onclick={onResetSearch}>×</button>
    {/if}
    <button type="submit" class="search-submit">Search</button>
  </form>
  <div class="topbar-actions" aria-label="Channel tools">
    {#if externalHref}
      <a href={externalHref} target="_blank" rel="noopener" title="Open external channel" aria-label="Open external channel">
        <svg viewBox="0 0 24 24" width="15" height="15" aria-hidden="true">
          <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M14 4h6v6m0-6-9 9m7 0v6a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h6" />
        </svg>
      </a>
    {/if}
    <button
      type="button"
      title={threadOpen ? "Close thread" : "Open a message thread"}
      aria-label={threadOpen ? "Close thread" : "Open a message thread"}
      class:active={threadOpen}
      onclick={onToggleThread}
    >
      <svg viewBox="0 0 24 24" width="15" height="15" aria-hidden="true">
        <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M21 12a8 8 0 0 1-11.6 7.16L3 21l1.84-6.4A8 8 0 1 1 21 12Z" />
      </svg>
    </button>
    <button
      type="button"
      title={semanticPaneOpen ? "Close cognition pane" : "Cognition"}
      aria-label={semanticPaneOpen ? "Close cognition pane" : "Cognition"}
      class:active={semanticPaneOpen}
      onclick={onToggleSemanticPane}
    >
      <svg viewBox="0 0 24 24" width="15" height="15" aria-hidden="true">
        <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M4 6h16M4 12h10M4 18h16"/>
      </svg>
    </button>
    {#if selectedChannel}
      <button
        type="button"
        title={pinnedOpen ? "Close pinned items" : "Pinned items"}
        aria-label={pinnedOpen ? "Close pinned items" : "Pinned items"}
        class:active={pinnedOpen}
        onclick={onPinnedItems}
      >
        <svg viewBox="0 0 24 24" width="15" height="15" aria-hidden="true">
          <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="m14 4 6 6-4 4v5l-2 2-5-5-4 4-1-1 4-4-5-5 2-2h5l4-4Z" />
        </svg>
      </button>
    {/if}
  </div>
</header>
