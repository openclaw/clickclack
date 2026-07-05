<script lang="ts">
  type Props = {
    connected: boolean;
    mobileNavigation: boolean;
    mobileNavOpen: boolean;
    platform: string;
    searchQuery: string;
    sidebarCollapsed: boolean;
    status: string;
    workspaceName?: string;
    onOpenSettings: () => void;
    onResetSearch: () => void;
    onSearch: () => void;
    onSearchQuery: (value: string) => void;
    onToggleSidebar: () => void;
  };

  let {
    connected,
    mobileNavigation,
    mobileNavOpen,
    platform,
    searchQuery,
    sidebarCollapsed,
    status,
    workspaceName,
    onOpenSettings,
    onResetSearch,
    onSearch,
    onSearchQuery,
    onToggleSidebar,
  }: Props = $props();
</script>

<header class="desktop-titlebar" data-platform={platform}>
  <div class="desktop-titlebar-safe-area">
    <div class="desktop-titlebar-leading">
      <button
        type="button"
        class="desktop-sidebar-toggle"
        aria-label={mobileNavigation
          ? mobileNavOpen
            ? "Close navigation"
            : "Open navigation"
          : sidebarCollapsed
            ? "Expand sidebar"
            : "Collapse sidebar"}
        title={mobileNavigation
          ? mobileNavOpen
            ? "Close navigation"
            : "Open navigation"
          : sidebarCollapsed
            ? "Expand sidebar"
            : "Collapse sidebar"}
        onclick={onToggleSidebar}
      >
        <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true">
          <rect x="3" y="4" width="18" height="16" rx="3" fill="none" stroke="currentColor" stroke-width="1.8" />
          <path d="M9 4v16" fill="none" stroke="currentColor" stroke-width="1.8" />
          <path
            d={mobileNavigation
              ? mobileNavOpen
                ? "m15 9-3 3 3 3"
                : "m9 9 3 3-3 3"
              : sidebarCollapsed
                ? "m13 9 3 3-3 3"
                : "m16 9-3 3 3 3"}
            fill="none"
            stroke="currentColor"
            stroke-linecap="round"
            stroke-linejoin="round"
            stroke-width="1.8"
          />
        </svg>
      </button>
      <span class="desktop-titlebar-workspace" title={workspaceName || "ClickClack"}>
        {workspaceName || "ClickClack"}
      </span>
    </div>

    <form
      class="search desktop-titlebar-search"
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

    <div class="desktop-titlebar-actions">
      <span class="desktop-titlebar-presence" title={connected ? "Connected" : status}>
        <i class:online={connected} aria-hidden="true"></i>
        <span>{connected ? "Connected" : status}</span>
      </span>
      <button type="button" aria-label="Open settings" title="Open settings" onclick={onOpenSettings}>
        <svg viewBox="0 0 24 24" width="15" height="15" aria-hidden="true">
          <path
            d="M12 8.5a3.5 3.5 0 1 0 0 7 3.5 3.5 0 0 0 0-7Zm8 3.5-.1-1.1 2-1.5-2-3.4-2.4 1a8.4 8.4 0 0 0-1.9-1.1L15.3 3h-4l-.4 2.9A8.4 8.4 0 0 0 9 7L6.6 6 4.6 9.4l2 1.5a8.4 8.4 0 0 0 0 2.2l-2 1.5 2 3.4L9 17a8.4 8.4 0 0 0 1.9 1.1l.4 2.9h4l.4-2.9a8.4 8.4 0 0 0 1.9-1.1l2.4 1 2-3.4-2-1.5.1-1.1Z"
            fill="none"
            stroke="currentColor"
            stroke-linecap="round"
            stroke-linejoin="round"
            stroke-width="1.5"
          />
        </svg>
      </button>
    </div>
  </div>
</header>
