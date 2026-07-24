<script lang="ts">
  import { dmTitle } from "../../lib/chat/people";
  import { safeExternalChannelURL } from "../../lib/chat/channels";
  import type { Channel, DirectConversation } from "../../lib/types";
  import ChannelPurpose from "../channels/ChannelPurpose.svelte";

  type Props = {
    selectedDirect?: DirectConversation;
    selectedChannel?: Channel;
    workspaceName?: string;
    currentUserID?: string;
    searchQuery: string;
    sidePanelOpen: boolean;
    threadOpen: boolean;
    onSearchQuery: (value: string) => void;
    onSearch: () => void;
    onResetSearch: () => void;
    onToggleThread: () => void;
    onPinnedItems: () => void;
  };

  let {
    selectedDirect,
    selectedChannel,
    workspaceName,
    currentUserID,
    searchQuery,
    sidePanelOpen,
    threadOpen,
    onSearchQuery,
    onSearch,
    onResetSearch,
    onToggleThread,
    onPinnedItems,
  }: Props = $props();

  const externalHref = $derived(selectedDirect ? undefined : safeExternalChannelURL(selectedChannel?.external_url));
</script>

<header class="topbar">
  <div class="topbar-title">
    {#if selectedDirect}
      <h1 class="with-glyph dm">{`@${dmTitle(selectedDirect, currentUserID)}`}</h1>
    {:else if selectedChannel}
      <div class="topbar-channel-copy">
        <h1 class="with-glyph channel">{`#${selectedChannel.name}`}</h1>
        <ChannelPurpose description={selectedChannel.description} />
      </div>
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
      class:active={sidePanelOpen}
      onclick={onToggleThread}
    >
      <svg viewBox="0 0 24 24" width="15" height="15" aria-hidden="true">
        <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M21 12a8 8 0 0 1-11.6 7.16L3 21l1.84-6.4A8 8 0 1 1 21 12Z" />
      </svg>
    </button>
    <button type="button" title="Pinned items" aria-label="Pinned items" onclick={onPinnedItems}>
      <svg viewBox="0 0 24 24" width="15" height="15" aria-hidden="true">
        <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="m14 4 6 6-4 4v5l-2 2-5-5-4 4-1-1 4-4-5-5 2-2h5l4-4Z" />
      </svg>
    </button>
  </div>
</header>

<style>
  .topbar-channel-copy {
    display: grid;
    min-width: 0;
    gap: 2px;
  }
</style>
