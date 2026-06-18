<script lang="ts">
  import { onDestroy, untrack } from "svelte";
  import { Virtualizer, type VirtualizerHandle } from "virtua/svelte";
  import {
    listWorkspaceMembersPage,
    memberLoadErrorMessage,
    MEMBERS_PAGE_LIMIT,
    type WorkspaceMember,
    type WorkspaceMemberRole,
  } from "../../../../../lib/workspace-members";

  let { data } = $props();

  const ROLE_OPTIONS: { value: "" | WorkspaceMemberRole; label: string }[] = [
    { value: "", label: "All roles" },
    { value: "owner", label: "Owners" },
    { value: "moderator", label: "Moderators" },
    { value: "member", label: "Members" },
    { value: "bot", label: "Bots" },
    { value: "guest", label: "Guests" },
  ];

  const ROW_HEIGHT = 64;
  const LOAD_MORE_THRESHOLD_PX = 600;
  const SEARCH_DEBOUNCE_MS = 280;

  let members = $state<WorkspaceMember[]>(untrack(() => data.members));
  let nextCursor = $state(untrack(() => data.nextCursor));
  let hasMore = $state(untrack(() => data.hasMore));
  let totalCount = $state<number | undefined>(untrack(() => data.totalCount));
  let loadError = $state(untrack(() => data.loadError));
  let loadedWorkspaceID = $state(untrack(() => data.workspaceID));
  let searchInput = $state("");
  let activeQuery = $state("");
  let activeRole = $state<"" | WorkspaceMemberRole>("");
  let isLoadingInitial = $state(false);
  let isLoadingMore = $state(false);
  let listRevision = $state(0);
  let virtualizer: VirtualizerHandle | undefined = $state();

  let searchTimer: ReturnType<typeof setTimeout> | undefined;
  let activeFetchID = 0;

  function isFiltering() {
    return activeQuery.length > 0 || activeRole !== "";
  }

  function resetMemberListViewport() {
    listRevision++;
    virtualizer = undefined;
  }

  $effect(() => {
    if (data.workspaceID === loadedWorkspaceID) return;
    if (searchTimer) {
      clearTimeout(searchTimer);
      searchTimer = undefined;
    }
    activeFetchID++;
    resetMemberListViewport();
    loadedWorkspaceID = data.workspaceID;
    members = data.members;
    nextCursor = data.nextCursor;
    hasMore = data.hasMore;
    totalCount = data.totalCount;
    loadError = data.loadError;
    searchInput = "";
    activeQuery = "";
    activeRole = "";
    isLoadingInitial = false;
    isLoadingMore = false;
  });

  async function refetchFromStart() {
    const fetchID = ++activeFetchID;
    resetMemberListViewport();
    isLoadingInitial = true;
    isLoadingMore = false;
    loadError = "";
    try {
      const page = await listWorkspaceMembersPage({
        workspaceID: data.workspaceID,
        limit: MEMBERS_PAGE_LIMIT,
        query: activeQuery,
        role: activeRole,
      });
      if (fetchID !== activeFetchID) return;
      members = page.members;
      nextCursor = page.next_cursor ?? "";
      hasMore = page.has_more;
      totalCount = page.total_count ?? totalCount;
    } catch (err) {
      if (fetchID !== activeFetchID) return;
      loadError = memberLoadErrorMessage(err);
      members = [];
      nextCursor = "";
      hasMore = false;
    } finally {
      if (fetchID === activeFetchID) isLoadingInitial = false;
    }
  }

  async function loadMore() {
    if (isLoadingMore || isLoadingInitial || !hasMore || !nextCursor) return;
    const fetchID = activeFetchID;
    isLoadingMore = true;
    try {
      const page = await listWorkspaceMembersPage({
        workspaceID: data.workspaceID,
        limit: MEMBERS_PAGE_LIMIT,
        cursor: nextCursor,
        query: activeQuery,
        role: activeRole,
      });
      if (fetchID !== activeFetchID) return;
      members = [...members, ...page.members];
      nextCursor = page.next_cursor ?? "";
      hasMore = page.has_more;
    } catch (err) {
      if (fetchID !== activeFetchID) return;
      loadError = memberLoadErrorMessage(err);
      hasMore = false;
    } finally {
      if (fetchID === activeFetchID) isLoadingMore = false;
    }
  }

  function onSearchInput(event: Event) {
    searchInput = (event.target as HTMLInputElement).value;
    if (searchTimer) clearTimeout(searchTimer);
    searchTimer = setTimeout(() => {
      const trimmed = searchInput.trim();
      if (trimmed === activeQuery) return;
      activeQuery = trimmed;
      void refetchFromStart();
    }, SEARCH_DEBOUNCE_MS);
  }

  function onRoleChange(event: Event) {
    const value = (event.target as HTMLSelectElement).value as "" | WorkspaceMemberRole;
    if (value === activeRole) return;
    activeRole = value;
    void refetchFromStart();
  }

  function handleScroll() {
    if (!virtualizer || !hasMore || isLoadingMore) return;
    const distanceFromBottom =
      virtualizer.getScrollSize() -
      virtualizer.getScrollOffset() -
      virtualizer.getViewportSize();
    if (distanceFromBottom <= LOAD_MORE_THRESHOLD_PX) {
      void loadMore();
    }
  }

  onDestroy(() => {
    if (searchTimer) clearTimeout(searchTimer);
    activeFetchID++;
  });

  function initials(member: WorkspaceMember): string {
    const source =
      member.user.display_name?.trim() || member.user.handle?.trim() || member.user.id || "?";
    const parts = source.split(/\s+/).filter(Boolean);
    if (parts.length >= 2) return (parts[0]![0]! + parts[1]![0]!).toUpperCase();
    return source.slice(0, 2).toUpperCase();
  }

  function hueFromID(id: string): number {
    let h = 0;
    for (let i = 0; i < id.length; i++) h = (h * 31 + id.charCodeAt(i)) >>> 0;
    return h % 360;
  }

  function formatHandle(handle: string): string {
    if (!handle) return "";
    return handle.startsWith("@") ? handle : `@${handle}`;
  }

  function formatJoined(value: string): string {
    if (!value) return "";
    const d = new Date(value);
    if (Number.isNaN(d.getTime())) return "";
    return d.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
  }

  function roleLabel(role: WorkspaceMemberRole): string {
    return role.charAt(0).toUpperCase() + role.slice(1);
  }

  const countLabel = $derived.by(() => {
    if (isFiltering()) {
      const noun = members.length === 1 ? "match" : "matches";
      return hasMore ? `${members.length}+ ${noun}` : `${members.length} ${noun}`;
    }
    if (totalCount != null) return `${totalCount} ${totalCount === 1 ? "member" : "members"}`;
    return `${members.length}${hasMore ? "+" : ""} ${members.length === 1 ? "member" : "members"}`;
  });

  const showEmptyState = $derived(
    !isLoadingInitial && members.length === 0 && !loadError && !isFiltering(),
  );
  const showNoMatches = $derived(
    !isLoadingInitial && members.length === 0 && !loadError && isFiltering(),
  );
</script>

<div class="ws-members-page">
  <header class="ws-page__header ws-members-page__header">
    <h1 class="ws-page__h1">Members</h1>
    <p class="ws-page__lead">Everyone with access to this workspace.</p>
  </header>

  <div class="ws-members__toolbar">
    <div class="ws-members__count">{countLabel}</div>

    <div class="ws-members__controls">
      <div class="ws-members__search">
        <span class="ws-members__search-icon" aria-hidden="true">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="7"/><path d="m21 21-4.3-4.3"/></svg>
        </span>
        <input
          class="ws-members__search-input"
          type="search"
          placeholder="Search name or handle"
          value={searchInput}
          oninput={onSearchInput}
          aria-label="Search members"
        />
      </div>

      <select
        class="ws-members__role-select"
        value={activeRole}
        onchange={onRoleChange}
        aria-label="Filter by role"
      >
        {#each ROLE_OPTIONS as option (option.value)}
          <option value={option.value}>{option.label}</option>
        {/each}
      </select>

      <button class="ws-btn" type="button" disabled title="Invitations are not available yet">
        Invite people
      </button>
    </div>
  </div>

  {#if loadError}
    <div class="ws-settings__error">{loadError}</div>
  {:else if showEmptyState}
    <div class="ws-members__empty">
      No one's here yet. Invite teammates to get this workspace started.
    </div>
  {:else if showNoMatches}
    <div class="ws-members__empty">
      No members match your search. Try a different query or filter.
    </div>
  {:else}
    <div class="ws-members__list" aria-busy={isLoadingInitial}>
    {#if isLoadingInitial && members.length === 0}
      {#each Array(8) as _, i (i)}
        <div class="ws-members__row ws-members__row--skeleton">
          <div class="ws-members__avatar ws-members__skeleton"></div>
          <div class="ws-members__main">
            <div class="ws-members__skeleton ws-members__skeleton--line" style="width: 38%"></div>
            <div class="ws-members__skeleton ws-members__skeleton--line" style="width: 62%"></div>
          </div>
          <div class="ws-members__skeleton ws-members__skeleton--pill"></div>
        </div>
      {/each}
    {:else}
      {#key listRevision}
        <Virtualizer
          bind:this={virtualizer}
          data={members}
          getKey={(m: WorkspaceMember) => m.user.id}
          itemSize={ROW_HEIGHT}
          onscroll={handleScroll}
        >
          {#snippet children(member: WorkspaceMember, _index: number)}
            <div class="ws-members__row" style="height: {ROW_HEIGHT}px">
              <span
                class="ws-members__avatar"
                style="--hue: {hueFromID(member.user.id)}deg"
                aria-hidden="true"
              >
                {#if member.user.avatar_url}
                  <img src={member.user.avatar_url} alt="" />
                {:else}
                  {initials(member)}
                {/if}
              </span>
              <div class="ws-members__main">
                <div class="ws-members__name">{member.user.display_name || "Unknown"}</div>
                <div class="ws-members__meta">
                  <span class="ws-members__handle">{formatHandle(member.user.handle)}</span>
                  {#if member.joined_at}
                    <span class="ws-members__dot" aria-hidden="true">·</span>
                    <span>Joined {formatJoined(member.joined_at)}</span>
                  {/if}
                </div>
              </div>
              <span class="ws-members__pill ws-members__pill--{member.role}">{roleLabel(member.role)}</span>
              <button
                class="ws-members__actions"
                type="button"
                disabled
                aria-label="Member actions"
                title="Coming soon"
              >
                <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                  <circle cx="12" cy="5" r="1"/><circle cx="12" cy="12" r="1"/><circle cx="12" cy="19" r="1"/>
                </svg>
              </button>
            </div>
          {/snippet}
        </Virtualizer>
      {/key}
    {/if}

    {#if isLoadingMore}
      <div class="ws-members__loading">Loading more…</div>
    {/if}
  </div>
  {/if}
</div>
