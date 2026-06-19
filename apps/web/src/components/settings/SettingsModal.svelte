<script lang="ts">
  import { goto } from "$app/navigation";
  import { onMount } from "svelte";
  import Avatar from "../avatar/Avatar.svelte";
  import ProfileSettingsForm from "../profile/ProfileSettingsForm.svelte";
  import NotificationSettingsForm from "../profile/NotificationSettingsForm.svelte";
  import MyBotsSection from "./MyBotsSection.svelte";
  import { api, APIError } from "../../lib/api";
  import {
    ACCOUNT_SETTINGS_SECTIONS,
    DEFAULT_ACCOUNT_SETTINGS_SECTION,
    WORKSPACE_SETTINGS_SECTIONS,
    workspaceSettingsPath,
    type AccountSettingsSectionId,
  } from "../../lib/settings";
  import { isWorkspaceManager } from "../../lib/permissions";
  import type { User, Workspace } from "../../lib/types";

  type Props = {
    user: User;
    workspaces?: Workspace[];
    initialSection?: AccountSettingsSectionId;
    onClose: () => void;
    onUserUpdated?: (user: User) => void;
  };

  let {
    user: initialUser,
    workspaces = [],
    initialSection = DEFAULT_ACCOUNT_SETTINGS_SECTION,
    onClose,
    onUserUpdated,
  }: Props = $props();

  let activeSection = $state<AccountSettingsSectionId>(DEFAULT_ACCOUNT_SETTINGS_SECTION);
  let refreshedUser = $state<User | null>(null);
  const user = $derived(refreshedUser?.id === initialUser.id ? refreshedUser : initialUser);
  let userStatus = $state<"ready" | "loading" | "error">("ready");
  let userError = $state("");

  $effect(() => {
    activeSection = initialSection;
  });

  // Refresh user from the API on mount so the modal always reflects
  // server-side truth, not whatever's stale in ChatApp state.
  onMount(() => {
    void refreshUser();
  });

  async function refreshUser() {
    userStatus = "loading";
    try {
      const data = await api<{ user: User }>("/api/me");
      refreshedUser = data.user;
      onUserUpdated?.(data.user);
      userStatus = "ready";
    } catch (err) {
      if (err instanceof APIError && (err.status === 401 || err.status === 403)) {
        userStatus = "error";
        userError = "Sign in to manage your account";
        return;
      }
      userStatus = "error";
      userError = err instanceof Error ? err.message : "Could not load your account";
    }
  }

  function handleUserUpdated(updated: User) {
    refreshedUser = updated;
    onUserUpdated?.(updated);
  }

  function handleScrimClick(event: MouseEvent) {
    if (event.target === event.currentTarget) onClose();
  }

  function handleKeydown(event: KeyboardEvent) {
    if (event.key !== "Escape") return;
    const target = event.target as HTMLElement | null;
    if (target && (target.tagName === "INPUT" || target.tagName === "TEXTAREA" || target.isContentEditable)) {
      return;
    }
    event.preventDefault();
    onClose();
  }

  function openWorkspaceSection(workspace: Workspace, slug: string) {
    onClose();
    void goto(workspaceSettingsPath(workspace.route_id || workspace.id, slug));
  }
</script>

<svelte:window onkeydown={handleKeydown} />

<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
<div class="settings-modal-scrim" role="presentation" onclick={handleScrimClick}>
  <div class="settings-modal" role="dialog" aria-modal="true" aria-label="Settings">
    <button type="button" class="settings-modal__close" onclick={onClose} aria-label="Close">
      <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <path d="M18 6L6 18M6 6l12 12" />
      </svg>
    </button>

    <aside class="settings-modal__rail">
      <div class="settings-modal__rail-group">
        <p class="settings-modal__rail-heading">Account</p>
        <ul>
          {#each ACCOUNT_SETTINGS_SECTIONS as section (section.id)}
            <li>
              <button
                type="button"
                class="settings-modal__rail-item"
                class:is-active={activeSection === section.id}
                aria-current={activeSection === section.id ? "page" : undefined}
                onclick={() => (activeSection = section.id)}
              >
                {#if section.id === "profile"}
                  <Avatar
                    class="settings-modal__rail-avatar"
                    id={user.id}
                    name={user.display_name}
                    src={user.avatar_url}
                    size={18}
                  />
                  <span class="settings-modal__rail-label">{user.display_name || section.label}</span>
                {:else if section.id === "notifications"}
                  <span class="settings-modal__rail-icon" aria-hidden="true">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                      <path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" />
                      <path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" />
                    </svg>
                  </span>
                  <span class="settings-modal__rail-label">{section.label}</span>
                {:else if section.id === "bots"}
                  <span class="settings-modal__rail-icon" aria-hidden="true">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                      <path d="M12 8V4H8" />
                      <path d="M5 4h14a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2Z" />
                      <path d="M2 14h2" />
                      <path d="M20 14h2" />
                      <path d="M15 13v2" />
                      <path d="M9 13v2" />
                    </svg>
                  </span>
                  <span class="settings-modal__rail-label">{section.label}</span>
                {/if}
              </button>
            </li>
          {/each}
        </ul>
      </div>

      {#each workspaces as workspace (workspace.id)}
        <div class="settings-modal__rail-group">
          <p class="settings-modal__rail-heading" title={workspace.name}>
            Workspace · {workspace.name}
          </p>
          <ul>
            {#each WORKSPACE_SETTINGS_SECTIONS as section (section.id)}
              {#if !section.managersOnly || isWorkspaceManager(workspace.role)}
                <li>
                  <button
                    type="button"
                    class="settings-modal__rail-item"
                    onclick={() => openWorkspaceSection(workspace, section.slug)}
                  >
                    <span class="settings-modal__rail-icon" aria-hidden="true">
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                        <path d="M5 12h14M13 5l7 7-7 7" />
                      </svg>
                    </span>
                    <span class="settings-modal__rail-label">{section.label}</span>
                  </button>
                </li>
              {/if}
            {/each}
          </ul>
        </div>
      {/each}
    </aside>

    <main class="settings-modal__content">
      {#if userStatus === "loading"}
        <p class="settings-status">Loading...</p>
      {:else if userStatus === "error"}
        <p class="settings-status is-error">{userError}</p>
      {:else if activeSection === "profile"}
        <header class="settings-page__header">
          <p class="settings-page__eyebrow">Account</p>
          <h2 class="settings-page__h1">Profile settings</h2>
          <p class="settings-page__lead">How you appear across ClickClack.</p>
        </header>
        <ProfileSettingsForm {user} onUserUpdated={handleUserUpdated} />
      {:else if activeSection === "notifications"}
        <header class="settings-page__header">
          <p class="settings-page__eyebrow">Account</p>
          <h2 class="settings-page__h1">Notifications</h2>
          <p class="settings-page__lead">Decide when and how ClickClack should reach you.</p>
        </header>
        <NotificationSettingsForm {user} onUserUpdated={handleUserUpdated} />
      {:else if activeSection === "bots"}
        <MyBotsSection {onClose} />
      {/if}
    </main>
  </div>
</div>
