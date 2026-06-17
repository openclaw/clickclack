<script lang="ts">
  import Avatar from "../avatar/Avatar.svelte";
  import { api } from "../../lib/api";
  import type { User } from "../../lib/types";

  type Props = {
    user: User;
    onUserUpdated?: (user: User) => void;
  };

  let { user, onUserUpdated }: Props = $props();

  let savedUser = $state<User | null>(null);
  const currentUser = $derived(savedUser ?? user);
  let displayName = $state("");
  let handle = $state("");
  let avatarURL = $state("");
  let status = $state("");
  let statusError = $state(false);
  let saving = $state(false);

  const previewName = $derived(displayName.trim() || currentUser.display_name || "Your name");
  const previewHandle = $derived(handle.trim().replace(/^@+/, "") || currentUser.handle || "");

  $effect(() => {
    displayName = currentUser.display_name;
    handle = currentUser.handle ?? "";
    avatarURL = currentUser.avatar_url;
  });

  function normalizedHandleForSave(): string {
    const trimmed = handle.trim().replace(/^@+/, "");
    return trimmed ? `@${trimmed}` : "";
  }

  async function save() {
    if (saving) return;
    saving = true;
    status = "";
    statusError = false;
    try {
      const data = await api<{ user: User }>("/api/me", {
        method: "PATCH",
        body: JSON.stringify({
          display_name: displayName,
          handle: normalizedHandleForSave(),
          avatar_url: avatarURL,
          notification_settings: currentUser.notification_settings,
        }),
      });
      savedUser = data.user;
      onUserUpdated?.(data.user);
      status = "Saved";
    } catch (error) {
      status = error instanceof Error ? error.message : "Could not save profile";
      statusError = true;
    } finally {
      saving = false;
    }
  }

  function clearAvatar() {
    avatarURL = "";
  }
</script>

<form
  class="settings-form"
  onsubmit={(event) => {
    event.preventDefault();
    void save();
  }}
>
  <section class="settings-identity" aria-label="Profile preview">
    <Avatar
      id={currentUser.id}
      name={previewName}
      src={avatarURL}
      size={52}
      loading="eager"
      fetchPriority="auto"
    />
    <div class="settings-identity__meta">
      <strong class="settings-identity__name">{previewName}</strong>
      <span class="settings-identity__handle">
        {previewHandle ? `@${previewHandle}` : "No handle set"}
      </span>
    </div>
    <span class="settings-identity__tag">Preview</span>
  </section>

  <div class="settings-rows">
    <div class="settings-row2">
      <div class="settings-row2__desc">
        <label class="settings-row2__label" for="profile-display-name">Display name</label>
        <p class="settings-row2__hint">Shown in messages, mentions, and your profile card.</p>
      </div>
      <div class="settings-row2__control">
        <input
          id="profile-display-name"
          class="settings-input"
          bind:value={displayName}
          aria-label="Display name"
          maxlength="80"
          autocomplete="name"
        />
      </div>
    </div>

    <div class="settings-row2">
      <div class="settings-row2__desc">
        <label class="settings-row2__label" for="profile-handle">Handle</label>
        <p class="settings-row2__hint">Used in mentions and the quick switcher. Must be unique.</p>
      </div>
      <div class="settings-row2__control">
        <div class="settings-input-group">
          <span class="settings-input-group__prefix" aria-hidden="true">@</span>
          <input
            id="profile-handle"
            class="settings-input settings-input--in-group"
            bind:value={handle}
            aria-label="Handle"
            placeholder="steipete"
            autocomplete="username"
          />
        </div>
      </div>
    </div>

    <div class="settings-row2">
      <div class="settings-row2__desc">
        <label class="settings-row2__label" for="profile-avatar-url">Profile photo</label>
        <p class="settings-row2__hint">Paste a public image URL. Your initials show when empty.</p>
      </div>
      <div class="settings-row2__control">
        <input
          id="profile-avatar-url"
          class="settings-input"
          bind:value={avatarURL}
          aria-label="Avatar URL"
          placeholder="https://example.com/avatar.png"
          inputmode="url"
        />
        {#if avatarURL}
          <button
            type="button"
            class="settings-linklike"
            onclick={clearAvatar}
            aria-label="Remove avatar"
          >
            Remove
          </button>
        {/if}
      </div>
    </div>
  </div>

  <footer class="settings-footer">
    {#if status}
      <p class="settings-status" class:is-error={statusError} role="status">{status}</p>
    {:else}
      <span class="settings-footer__spacer" aria-hidden="true"></span>
    {/if}
    <button type="submit" class="settings-button settings-button--primary" disabled={saving}>
      {saving ? "Saving..." : "Save profile"}
    </button>
  </footer>
</form>
