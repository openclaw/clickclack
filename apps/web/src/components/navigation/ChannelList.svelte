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
    onReorder: (channelIDs: string[]) => void;
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
    onReorder,
  }: Props = $props();

  let visibleChannels = $derived(
    expanded
      ? channels
      : channels.filter(
          (channel) =>
            (channel.id === selectedChannelID && !selectedDirectID) ||
            (channel.unread_count || 0) > 0,
        ),
  );

  let draggedChannelID = $state("");
  let dropTargetID = $state("");
  let dropBefore = $state(true);
  let moveAnnouncement = $state("");

  function announceMove(message: string) {
    moveAnnouncement = "";
    queueMicrotask(() => {
      moveAnnouncement = message;
    });
  }

  function moveChannel(channelID: string, targetID: string, before: boolean) {
    if (!channelID || !targetID || channelID === targetID) return;
    const order = channels.map((channel) => channel.id);
    const from = order.indexOf(channelID);
    if (from < 0) return;
    order.splice(from, 1);
    const target = order.indexOf(targetID);
    if (target < 0) return;
    order.splice(target + (before ? 0 : 1), 0, channelID);
    onReorder(order);
    const moved = channels.find((channel) => channel.id === channelID);
    if (moved) {
      announceMove(`Moved #${moved.name} to position ${order.indexOf(channelID) + 1} of ${order.length}`);
    }
  }

  function moveBy(channelID: string, offset: number) {
    const index = channels.findIndex((channel) => channel.id === channelID);
    const target = index + offset;
    if (index < 0 || target < 0 || target >= channels.length) return;
    moveChannel(channelID, channels[target].id, offset < 0);
  }

  function handleDragStart(event: DragEvent, channelID: string) {
    draggedChannelID = channelID;
    event.dataTransfer?.setData("text/plain", channelID);
    if (event.dataTransfer) event.dataTransfer.effectAllowed = "move";
  }

  function handleDragOver(event: DragEvent, channelID: string) {
    if (!draggedChannelID || draggedChannelID === channelID) return;
    event.preventDefault();
    const row = event.currentTarget as HTMLElement;
    dropTargetID = channelID;
    dropBefore = event.clientY < row.getBoundingClientRect().top + row.offsetHeight / 2;
    if (event.dataTransfer) event.dataTransfer.dropEffect = "move";
  }

  function clearDrag() {
    draggedChannelID = "";
    dropTargetID = "";
  }

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
    <button
      type="button"
      class="add-button"
      aria-label="Create channel"
      title="Create channel"
      onclick={onCreateChannel}
    >＋</button>
  </div>
  <div
    class="nav-list"
    id="sidebar-channels-list"
    role="list"
    hidden={!expanded && visibleChannels.length === 0}
  >
    <span id="channel-order-instructions" class="sr-only">
      Drag with a pointer, or use Arrow Up and Arrow Down while focused.
    </span>
    {#each visibleChannels as channel (channel.id)}
      {@const index = channels.findIndex((candidate) => candidate.id === channel.id)}
      {@const unread = channel.unread_count || 0}
      <div
        class="channel-row"
        role="listitem"
        class:dragging={draggedChannelID === channel.id}
        class:drop-before={dropTargetID === channel.id && dropBefore}
        class:drop-after={dropTargetID === channel.id && !dropBefore}
        ondragover={(event) => handleDragOver(event, channel.id)}
        ondrop={(event) => {
          event.preventDefault();
          moveChannel(draggedChannelID, channel.id, dropBefore);
          clearDrag();
        }}
      >
        <button
          type="button"
          class="channel-drag-handle"
          draggable="true"
          aria-label={`Move #${channel.name}`}
          aria-describedby="channel-order-instructions"
          title="Drag to reorder; use arrow keys to move"
          ondragstart={(event) => handleDragStart(event, channel.id)}
          ondragend={clearDrag}
          onkeydown={(event) => {
            if (event.key === "ArrowUp" || event.key === "ArrowDown") {
              event.preventDefault();
              moveBy(channel.id, event.key === "ArrowUp" ? -1 : 1);
            }
          }}
        >
          <svg viewBox="0 0 12 16" width="12" height="16" aria-hidden="true">
            <circle cx="3" cy="4" r="1" /><circle cx="9" cy="4" r="1" />
            <circle cx="3" cy="8" r="1" /><circle cx="9" cy="8" r="1" />
            <circle cx="3" cy="12" r="1" /><circle cx="9" cy="12" r="1" />
          </svg>
        </button>
        <div class="channel-touch-controls">
          <button
            type="button"
            aria-label={`Move #${channel.name} up`}
            disabled={index === 0}
            onclick={() => moveBy(channel.id, -1)}
          >↑</button>
          <button
            type="button"
            aria-label={`Move #${channel.name} down`}
            disabled={index === channels.length - 1}
            onclick={() => moveBy(channel.id, 1)}
          >↓</button>
        </div>
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
      </div>
    {/each}
    {#if expanded && channels.length === 0}
      <p class="nav-empty">No channels yet</p>
    {/if}
  </div>
  <span class="sr-only" aria-live="polite" aria-atomic="true">{moveAnnouncement}</span>
</section>
