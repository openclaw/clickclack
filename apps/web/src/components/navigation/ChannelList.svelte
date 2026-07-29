<script lang="ts">
  import { tick } from "svelte";
  import { channelDisplayTitle } from "../../lib/chat/channels";
  import type { Channel } from "../../lib/types";

  type Props = {
    workspaceID: string;
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

  type ChannelGroup = {
    key: string;
    label: string;
    channels: Channel[];
  };

  let {
    workspaceID,
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

  const GROUP_STORAGE_PREFIX = "clickclack:sidebar-channel-groups:v1:";
  const ARCHIVED_GROUP_KEY = "archived";
  const MAX_DISCLOSURE_GROUPS = 1_000;
  const MAX_DISCLOSURE_KEY_LENGTH = 256;

  let draggedChannelID = $state("");
  let draggedGroupKey = $state("");
  let dropTargetID = $state("");
  let dropBefore = $state(true);
  let dragGestureActive = $state(false);
  let moveMenuChannelID = $state("");
  let moveMenuElement = $state<HTMLDivElement>();
  let moveMenuTrigger: HTMLButtonElement | undefined;
  let moveAnnouncement = $state("");
  let groupDisclosure = $state<Record<string, boolean>>({});

  const activeChannels = $derived(channels.filter((channel) => !channel.archived_at));
  const archivedChannels = $derived(channels.filter((channel) => Boolean(channel.archived_at)));
  const unsectionedChannels = $derived(
    activeChannels.filter((channel) => !channel.sidebar_section?.trim()),
  );
  const sectionGroups = $derived.by(() => {
    const grouped = new Map<string, Channel[]>();
    for (const channel of activeChannels) {
      const label = channel.sidebar_section?.trim();
      if (!label) continue;
      const group = grouped.get(label) ?? [];
      group.push(channel);
      grouped.set(label, group);
    }
    return [...grouped.entries()]
      .map(([label, groupedChannels]): ChannelGroup => ({
        key: `section:${label}`,
        label,
        channels: groupedChannels,
      }))
      .sort((a, b) => a.label.localeCompare(b.label, undefined, { sensitivity: "base" }));
  });
  const priorityChannels = $derived(
    channels.filter(
      (channel) =>
        (channel.id === selectedChannelID && !selectedDirectID) ||
        (channel.unread_count || 0) > 0,
    ),
  );

  function parseDisclosureState(raw: string | null): Record<string, boolean> {
    if (!raw) return {};
    try {
      const value: unknown = JSON.parse(raw);
      if (!value || typeof value !== "object" || Array.isArray(value)) return {};
      const entries = Object.entries(value as Record<string, unknown>);
      if (
        entries.length > MAX_DISCLOSURE_GROUPS ||
        entries.some(
          ([key, expanded]) =>
            key.length > MAX_DISCLOSURE_KEY_LENGTH || typeof expanded !== "boolean",
        )
      ) {
        return {};
      }
      return Object.fromEntries(entries) as Record<string, boolean>;
    } catch {
      return {};
    }
  }

  function disclosureStorageKey(id: string): string {
    return `${GROUP_STORAGE_PREFIX}${id}`;
  }

  function loadDisclosureState(id: string): Record<string, boolean> {
    if (!id) return {};
    try {
      return parseDisclosureState(window.localStorage.getItem(disclosureStorageKey(id)));
    } catch {
      return {};
    }
  }

  function groupExpanded(key: string): boolean {
    const saved = groupDisclosure[key];
    return typeof saved === "boolean" ? saved : key !== ARCHIVED_GROUP_KEY;
  }

  function toggleGroup(key: string) {
    groupDisclosure = { ...groupDisclosure, [key]: !groupExpanded(key) };
    if (!workspaceID) return;
    try {
      window.localStorage.setItem(
        disclosureStorageKey(workspaceID),
        JSON.stringify(groupDisclosure),
      );
    } catch {
      // Storage is an enhancement; disclosures still work when unavailable.
    }
  }

  function handleDisclosureStorage(event: StorageEvent) {
    if (event.key !== disclosureStorageKey(workspaceID)) return;
    groupDisclosure = parseDisclosureState(event.newValue);
  }

  function visibleGroupChannels(group: ChannelGroup): Channel[] {
    return groupExpanded(group.key)
      ? group.channels
      : group.channels.filter(
          (channel) =>
            (channel.id === selectedChannelID && !selectedDirectID) ||
            (channel.unread_count || 0) > 0,
        );
  }

  function announceMove(message: string) {
    moveAnnouncement = "";
    queueMicrotask(() => {
      moveAnnouncement = message;
    });
  }

  function moveChannel(
    channelID: string,
    targetID: string,
    before: boolean,
    scopeChannels: Channel[],
  ) {
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
    const scopeOrder = scopeChannels
      .map((channel) => channel.id)
      .filter((id) => id !== channelID);
    const scopeTarget = scopeOrder.indexOf(targetID);
    scopeOrder.splice(scopeTarget + (before ? 0 : 1), 0, channelID);
    if (moved) {
      announceMove(
        `Moved #${channelDisplayTitle(moved)} to position ${scopeOrder.indexOf(channelID) + 1} of ${scopeOrder.length}`,
      );
    }
  }

  function moveBy(channelID: string, offset: number, scopeChannels: Channel[]) {
    const index = scopeChannels.findIndex((channel) => channel.id === channelID);
    const target = index + offset;
    if (index < 0 || target < 0 || target >= scopeChannels.length) return;
    moveChannel(channelID, scopeChannels[target].id, offset < 0, scopeChannels);
  }

  function handleDragStart(event: DragEvent, channelID: string, groupKey: string) {
    dragGestureActive = true;
    moveMenuChannelID = "";
    draggedChannelID = channelID;
    draggedGroupKey = groupKey;
    event.dataTransfer?.setData("text/plain", channelID);
    if (event.dataTransfer) event.dataTransfer.effectAllowed = "move";
  }

  function handleDragOver(event: DragEvent, channelID: string, groupKey: string) {
    if (
      !draggedChannelID ||
      draggedChannelID === channelID ||
      draggedGroupKey !== groupKey
    ) {
      return;
    }
    event.preventDefault();
    const row = event.currentTarget as HTMLElement;
    dropTargetID = channelID;
    dropBefore = event.clientY < row.getBoundingClientRect().top + row.offsetHeight / 2;
    if (event.dataTransfer) event.dataTransfer.dropEffect = "move";
  }

  function clearDrag() {
    draggedChannelID = "";
    draggedGroupKey = "";
    dropTargetID = "";
  }

  function finishDrag() {
    clearDrag();
    window.setTimeout(() => {
      dragGestureActive = false;
    }, 0);
  }

  async function toggleMoveMenu(channelID: string, trigger: HTMLButtonElement) {
    if (dragGestureActive) return;
    if (moveMenuChannelID === channelID) {
      moveMenuChannelID = "";
      return;
    }
    moveMenuTrigger = trigger;
    moveMenuChannelID = channelID;
    await tick();
    moveMenuElement?.querySelector<HTMLButtonElement>("button:not(:disabled)")?.focus();
  }

  async function closeMoveMenu(restoreFocus = false) {
    moveMenuChannelID = "";
    if (!restoreFocus) return;
    await tick();
    moveMenuTrigger?.focus();
  }

  function moveFromMenu(channelID: string, offset: number, scopeChannels: Channel[]) {
    moveBy(channelID, offset, scopeChannels);
    void closeMoveMenu(true);
  }

  function shouldHandleClientNavigation(event: MouseEvent): boolean {
    return (
      event.button === 0 &&
      !event.metaKey &&
      !event.ctrlKey &&
      !event.shiftKey &&
      !event.altKey
    );
  }

  $effect(() => {
    groupDisclosure = loadDisclosureState(workspaceID);
  });

  $effect(() => {
    if (!expanded) {
      moveMenuChannelID = "";
      clearDrag();
      dragGestureActive = false;
    }
  });
</script>

<svelte:window onstorage={handleDisclosureStorage} />

{#snippet channelRow(channel: Channel, scopeChannels: Channel[], groupKey: string, subdued: boolean, reorderable: boolean)}
  {@const unread = channel.unread_count || 0}
  {@const channelIndex = scopeChannels.findIndex((candidate) => candidate.id === channel.id)}
  <div
    class="channel-row"
    class:reorderable
    class:subdued
    role="listitem"
    class:dragging={draggedChannelID === channel.id}
    class:drop-before={dropTargetID === channel.id && dropBefore}
    class:drop-after={dropTargetID === channel.id && !dropBefore}
    ondragover={(event) => {
      if (reorderable) handleDragOver(event, channel.id, groupKey);
    }}
    ondrop={(event) => {
      event.preventDefault();
      if (!reorderable || draggedGroupKey !== groupKey) return;
      moveChannel(draggedChannelID, channel.id, dropBefore, scopeChannels);
      finishDrag();
    }}
    onfocusout={(event) => {
      if (!(event.currentTarget as HTMLElement).contains(event.relatedTarget as Node | null)) {
        moveMenuChannelID = "";
      }
    }}
  >
    {#if reorderable}
      <button
        type="button"
        class="channel-drag-handle"
        draggable="true"
        aria-label={`Move #${channelDisplayTitle(channel)}`}
        aria-describedby="channel-order-instructions"
        title="Move channel"
        aria-haspopup="menu"
        aria-expanded={moveMenuChannelID === channel.id}
        onclick={(event) => void toggleMoveMenu(channel.id, event.currentTarget)}
        ondragstart={(event) => handleDragStart(event, channel.id, groupKey)}
        ondragend={finishDrag}
        onkeydown={(event) => {
          if (event.key === "ArrowUp" || event.key === "ArrowDown") {
            event.preventDefault();
            moveMenuChannelID = "";
            moveBy(channel.id, event.key === "ArrowUp" ? -1 : 1, scopeChannels);
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
          tabindex="-1"
          aria-label={`Move #${channelDisplayTitle(channel)}`}
          data-handles-escape
          bind:this={moveMenuElement}
          onkeydown={(event) => {
            if (event.key === "Escape") {
              event.preventDefault();
              void closeMoveMenu(true);
            }
          }}
        >
          <button
            type="button"
            role="menuitem"
            disabled={channelIndex <= 0}
            onclick={() => moveFromMenu(channel.id, -1, scopeChannels)}
          >Move up</button>
          <button
            type="button"
            role="menuitem"
            disabled={channelIndex < 0 || channelIndex >= scopeChannels.length - 1}
            onclick={() => moveFromMenu(channel.id, 1, scopeChannels)}
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
      <span class="hash">#</span>
      <span class="nav-label">{channelDisplayTitle(channel)}</span>
      {#if channel.external_managed}
        <span class="managed-channel-marker" title="Externally managed" aria-label="Externally managed">
          <svg viewBox="0 0 24 24" width="12" height="12" aria-hidden="true">
            <path d="M10 13a5 5 0 0 0 7.54.54l2-2a5 5 0 0 0-7.07-7.07l-1.15 1.15M14 11a5 5 0 0 0-7.54-.54l-2 2a5 5 0 0 0 7.07 7.07l1.14-1.14" fill="none" stroke="currentColor" stroke-linecap="round" stroke-width="2" />
          </svg>
        </span>
      {/if}
      {#if unread > 0 && !(channel.id === selectedChannelID && !selectedDirectID)}
        <span class="unread-badge" aria-label={`${unread} unread`}>{unread > 99 ? "99+" : unread}</span>
      {/if}
    </a>
  </div>
{/snippet}

{#snippet channelSubgroup(group: ChannelGroup, domID: string, subdued: boolean)}
  {@const groupIsExpanded = groupExpanded(group.key)}
  {@const visibleChannels = visibleGroupChannels(group)}
  <section class="channel-subgroup" class:archived-channel-group={subdued}>
    <button
      type="button"
      class="channel-subgroup-toggle"
      aria-expanded={groupIsExpanded}
      aria-controls={domID}
      onclick={() => toggleGroup(group.key)}
    >
      <span class="caret" aria-hidden="true">▾</span>
      <span>{group.label}</span>
      <span class="channel-subgroup-count">{group.channels.length}</span>
    </button>
    <div
      class="channel-subgroup-list"
      id={domID}
      role="list"
      hidden={!groupIsExpanded && visibleChannels.length === 0}
    >
      {#each visibleChannels as channel (channel.id)}
        {@render channelRow(channel, group.channels, group.key, subdued, groupIsExpanded)}
      {/each}
    </div>
  </section>
{/snippet}

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
    hidden={!expanded && priorityChannels.length === 0}
  >
    {#if expanded}
      <span id="channel-order-instructions" class="sr-only">
        Drag with a pointer, use Arrow Up and Arrow Down while focused, or open the move menu. Moves stay within the current channel section.
      </span>
      {#each unsectionedChannels as channel (channel.id)}
        {@render channelRow(channel, unsectionedChannels, "unsectioned", false, true)}
      {/each}
      {#each sectionGroups as group, index (group.key)}
        {@render channelSubgroup(group, `sidebar-channel-section-${index}`, false)}
      {/each}
      {#if archivedChannels.length > 0}
        {@render channelSubgroup(
          { key: ARCHIVED_GROUP_KEY, label: "Archived", channels: archivedChannels },
          "sidebar-archived-channels",
          true,
        )}
      {/if}
      {#if channels.length === 0}
        <p class="nav-empty">No channels yet</p>
      {/if}
    {:else}
      {#each priorityChannels as channel (channel.id)}
        {@render channelRow(channel, priorityChannels, "priority", Boolean(channel.archived_at), false)}
      {/each}
    {/if}
  </div>
  <span class="sr-only" aria-live="polite" aria-atomic="true">{moveAnnouncement}</span>
</section>
