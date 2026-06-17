<script lang="ts">
  import { goto } from "$app/navigation";
  import { page } from "$app/state";
  import {
    WORKSPACE_SETTINGS_SECTIONS,
    workspaceSettingsPath,
    type WorkspaceSettingsSection,
  } from "../../../../lib/settings";
  import { isWorkspaceManager } from "../../../../lib/permissions";

  let { data, children } = $props();

  const workspaceID = $derived(data.workspaceID);
  const workspace = $derived(data.workspace);
  const role = $derived(workspace?.role);

  const visibleSections = $derived<WorkspaceSettingsSection[]>(
    WORKSPACE_SETTINGS_SECTIONS.filter((section) => !section.managersOnly || isWorkspaceManager(role)),
  );

  const activeSlug = $derived.by(() => {
    const segments = page.url.pathname.split("/").filter(Boolean);
    // /app/{ws}/settings/{slug}
    return segments[3] ?? "";
  });

  function backToChat() {
    void goto(`/app/${workspaceID}`);
  }

  function navigate(slug: string) {
    void goto(workspaceSettingsPath(workspaceID, slug));
  }

  function handleKeydown(event: KeyboardEvent) {
    if (event.key !== "Escape") return;
    const target = event.target as HTMLElement | null;
    if (target && (target.tagName === "INPUT" || target.tagName === "TEXTAREA" || target.isContentEditable)) {
      return;
    }
    event.preventDefault();
    backToChat();
  }
</script>

<svelte:window onkeydown={handleKeydown} />

<div class="ws-settings">
  <div class="ws-settings__panel" role="dialog" aria-label="Workspace settings">
    <aside class="ws-settings__rail">
    <div class="ws-settings__rail-search">
      <span class="ws-settings__rail-search-icon" aria-hidden="true">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="7"/><path d="m21 21-4.3-4.3"/></svg>
      </span>
      <input
        class="ws-settings__rail-search-input"
        type="text"
        placeholder="Search settings…"
        aria-label="Search settings"
        disabled
      />
    </div>

    <div class="ws-settings__rail-group">
      <p class="ws-settings__rail-heading">
        Workspace · <span class="ws-settings__rail-heading-name">{workspace?.name ?? "—"}</span>
      </p>
      <ul class="ws-settings__rail-list">
        {#each visibleSections as section (section.id)}
          <li>
            <button
              type="button"
              class="ws-settings__rail-item"
              class:is-active={activeSlug === section.slug}
              aria-current={activeSlug === section.slug ? "page" : undefined}
              onclick={() => navigate(section.slug)}
            >
              <span class="ws-settings__rail-item-icon" aria-hidden="true">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  {#each section.icon as d, i (i)}
                    <path {d} />
                  {/each}
                </svg>
              </span>
              <span class="ws-settings__rail-item-label">{section.label}</span>
            </button>
          </li>
        {/each}
      </ul>
    </div>
  </aside>

  <main class="ws-settings__content">
    <div class="ws-settings__content-inner">
      {#if data.loadError}
        <div class="ws-settings__error">{data.loadError}</div>
      {:else}
        {@render children?.()}
      {/if}
    </div>
  </main>
  </div>

  <button
    type="button"
    class="ws-settings__close"
    onclick={backToChat}
    aria-label="Close workspace settings"
    title="Close (Esc)"
  >
    <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
      <path d="M18 6L6 18M6 6l12 12" />
    </svg>
  </button>
</div>
