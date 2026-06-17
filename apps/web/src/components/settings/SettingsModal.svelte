<script lang="ts">
  import { onMount } from "svelte";
  import Avatar from "../avatar/Avatar.svelte";
  import ProfileSettingsForm from "../profile/ProfileSettingsForm.svelte";
  import NotificationSettingsForm from "../profile/NotificationSettingsForm.svelte";
  import { api, APIError } from "../../lib/api";
  import {
    ACCOUNT_SETTINGS_SECTIONS,
    DEFAULT_ACCOUNT_SETTINGS_SECTION,
    type AccountSettingsSectionId,
  } from "../../lib/settings";
  import type { User } from "../../lib/types";

  type Props = {
    user: User;
    initialSection?: AccountSettingsSectionId;
    onClose: () => void;
    onUserUpdated?: (user: User) => void;
  };

  let {
    user: initialUser,
    initialSection = DEFAULT_ACCOUNT_SETTINGS_SECTION,
    onClose,
    onUserUpdated,
  }: Props = $props();

  let activeSection = $state<AccountSettingsSectionId>(initialSection);
  let user = $state<User>(initialUser);
  let userStatus = $state<"ready" | "loading" | "error">("ready");
  let userError = $state("");

  $effect(() => {
    user = initialUser;
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
      user = data.user;
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
    user = updated;
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
</script>

<svelte:window onkeydown={handleKeydown} />

<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
<div class="settings-modal-scrim" onclick={handleScrimClick}>
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
                {/if}
              </button>
            </li>
          {/each}
        </ul>
      </div>
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
      {/if}
    </main>
  </div>
</div>
