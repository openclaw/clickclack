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

  let draggedChannelID = $state("");
  let dropTargetID = $state("");
  let dropBefore = $state(true);
  let dragGestureActive = $state(false);
  let moveMenuChannelID = $state("");
  let moveAnnouncement = $state("");

  let visibleChannels = $derived(
    expanded
      ? channels
      : channels.filter(
          (channel) =>
            (channel.id === selectedChannelID && !selectedDirectID) ||
            (channel.unread_count || 0) > 0,
    ),
  );

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
      announceMove(
        `Moved #${moved.name} to position ${order.indexOf(channelID) + 1} of ${order.length}`,
      );
    }
  }

  function moveBy(channelID: string, offset: number) {
    const index = channels.findIndex((channel) => channel.id === channelID);
    const target = index + offset;
    if (index < 0 || target < 0 || target >= channels.length) return;
    moveChannel(channelID, channels[target].id, offset < 0);
  }

  function handleDragStart(event: DragEvent, channelID: string) {
    dragGestureActive = true;
    moveMenuChannelID = "";
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

  function finishDrag() {
    clearDrag();
    window.setTimeout(() => {
      dragGestureActive = false;
    }, 0);
  }

  function toggleMoveMenu(channelID: string) {
    if (dragGestureActive) return;
    moveMenuChannelID = moveMenuChannelID === channelID ? "" : channelID;
  }

  function moveFromMenu(channelID: string, offset: number) {
    moveBy(channelID, offset);
    moveMenuChannelID = "";
  }

  function shouldHandleClientNavigation(event: MouseEvent): boolean {
    return event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey;
  }

  $effect(() => {
    if (!expanded) {
      moveMenuChannelID = "";
      clearDrag();
    }
  });
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
    {#if expanded}
      <span id="channel-order-instructions" class="sr-only">
        Drag with a pointer, use Arrow Up and Arrow Down while focused, or open the move menu.
      </span>
    {/if}
    {#each visibleChannels as channel (channel.id)}
      {@const unread = channel.unread_count || 0}
      {@const channelIndex = channels.findIndex((candidate) => candidate.id === channel.id)}
      <div
        class="channel-row"
        role="listitem"
        class:reorderable={expanded}
        class:dragging={draggedChannelID === channel.id}
        class:drop-before={dropTargetID === channel.id && dropBefore}
        class:drop-after={dropTargetID === channel.id && !dropBefore}
        ondragover={(event) => {
          if (expanded) handleDragOver(event, channel.id);
        }}
        ondrop={(event) => {
          event.preventDefault();
          if (!expanded) return;
          moveChannel(draggedChannelID, channel.id, dropBefore);
          finishDrag();
        }}
        onfocusout={(event) => {
          if (!(event.currentTarget as HTMLElement).contains(event.relatedTarget as Node | null)) {
            moveMenuChannelID = "";
          }
        }}
      >
        {#if expanded}
          <button
            type="button"
            class="channel-drag-handle"
            draggable="true"
            aria-label={`Move #${channel.name}`}
            aria-describedby="channel-order-instructions"
            title="Move channel"
            aria-haspopup="menu"
            aria-expanded={moveMenuChannelID === channel.id}
            onclick={() => toggleMoveMenu(channel.id)}
            ondragstart={(event) => handleDragStart(event, channel.id)}
            ondragend={finishDrag}
            onkeydown={(event) => {
              if (event.key === "ArrowUp" || event.key === "ArrowDown") {
                event.preventDefault();
                moveMenuChannelID = "";
                moveBy(channel.id, event.key === "ArrowUp" ? -1 : 1);
              } else if (event.key === "Escape") {
                moveMenuChannelID = "";
              }
            }}
          >
            <svg viewBox="0 0 12 16" width="12" height="16" aria-hidden="true">
              <circle cx="3" cy="4" r="1" /><circle cx="9" cy="4" r="1" />
              <circle cx="3" cy="8" r="1" /><circle cx="9" cy="8" r="1" />
              <circle cx="3" cy="12" r="1" /><circle cx="9" cy="12" r="1" />
            </svg>
          </button>
          {#if moveMenuChannelID === channel.id}
            <div
              class="channel-move-menu"
              role="menu"
              aria-label={`Move #${channel.name}`}
              onkeydown={(event) => {
                if (event.key === "Escape") moveMenuChannelID = "";
              }}
            >
              <button
                type="button"
                role="menuitem"
                disabled={channelIndex <= 0}
                onclick={() => moveFromMenu(channel.id, -1)}
              >Move up</button>
              <button
                type="button"
                role="menuitem"
                disabled={channelIndex < 0 || channelIndex >= channels.length - 1}
                onclick={() => moveFromMenu(channel.id, 1)}
              >Move down</button>
            </div>
          {/if}
        {/if}
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
