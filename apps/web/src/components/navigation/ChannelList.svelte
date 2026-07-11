<script lang="ts">
  import type { Channel } from "../../lib/types";

  type Props = {
    expanded: boolean;
    channels: Channel[];
    selectedChannelID: string;
    selectedDirectID: string;
    hrefForChannel: (channelID: string) => string;
    onSelectChannel: (channelID: string) => void;
    onCreateChannel: () => void;
    onToggle: () => void;
  };

  let {
    expanded,
    channels,
    selectedChannelID,
    selectedDirectID,
    hrefForChannel,
    onSelectChannel,
    onCreateChannel,
    onToggle,
  }: Props = $props();

  let unreadTotal = $derived(channels.reduce((total, channel) => total + (channel.unread_count || 0), 0));

  function shouldHandleClientNavigation(event: MouseEvent): boolean {
    return event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey;
  }
</script>

<section class="nav-section" class:collapsed={!expanded}>
  <div class="section-title">
    <button type="button" class="section-toggle" aria-expanded={expanded} aria-controls="sidebar-channels-list" onclick={onToggle}>
      <span class="caret" aria-hidden="true">▾</span>
      <span class="label">Channels</span>
    </button>
    {#if !expanded && unreadTotal > 0}
      <span class="section-unread-badge" aria-label={`${unreadTotal} unread`}>{unreadTotal > 99 ? "99+" : unreadTotal}</span>
    {/if}
    <button
      type="button"
      class="add-button"
      aria-label="Create channel"
      title="Create channel"
      onclick={onCreateChannel}
    >＋</button>
  </div>
  <div class="nav-list" id="sidebar-channels-list" hidden={!expanded}>
    {#each channels as channel (channel.id)}
      {@const unread = channel.unread_count || 0}
      <a
        href={hrefForChannel(channel.id)}
        class="nav-item channel"
        class:active={channel.id === selectedChannelID && !selectedDirectID}
        class:has-unread={unread > 0 && !(channel.id === selectedChannelID && !selectedDirectID)}
        onclick={(event) => {
          if (!shouldHandleClientNavigation(event)) return;
          event.preventDefault();
          onSelectChannel(channel.id);
        }}
      >
        <span class="hash">#</span> <span class="nav-label">{channel.name}</span>
        {#if unread > 0 && !(channel.id === selectedChannelID && !selectedDirectID)}
          <span class="unread-badge" aria-label={`${unread} unread`}>{unread > 99 ? "99+" : unread}</span>
        {/if}
      </a>
    {/each}
    {#if channels.length === 0}
      <p class="nav-empty">No channels yet</p>
    {/if}
  </div>
</section>
