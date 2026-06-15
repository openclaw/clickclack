<script lang="ts">
  import Avatar from "../avatar/Avatar.svelte";
  import { handleLabel } from "../../lib/chat/people";
  import type { User } from "../../lib/types";

  type Props = {
    user: User;
    displayName: string;
    handle: string;
    avatarURL: string;
    pushoverEnabled: boolean;
    pushoverUserKey: string;
    browserNotificationsSupported: boolean;
    browserNotificationsEnabled: boolean;
    browserNotificationPermission: NotificationPermission | "unsupported";
    status: string;
    statusError: boolean;
    onDisplayName: (value: string) => void;
    onHandle: (value: string) => void;
    onAvatarURL: (value: string) => void;
    onPushoverEnabled: (value: boolean) => void;
    onPushoverUserKey: (value: string) => void;
    onBrowserNotificationsEnabled: (value: boolean) => void;
    onClose: () => void;
    onSave: () => void;
  };

  let {
    user,
    displayName,
    handle,
    avatarURL,
    pushoverEnabled,
    pushoverUserKey,
    browserNotificationsSupported,
    browserNotificationsEnabled,
    browserNotificationPermission,
    status,
    statusError,
    onDisplayName,
    onHandle,
    onAvatarURL,
    onPushoverEnabled,
    onPushoverUserKey,
    onBrowserNotificationsEnabled,
    onClose,
    onSave,
  }: Props = $props();
</script>

<div class="modal-scrim" role="presentation">
  <button class="modal-backdrop" type="button" aria-label="Close account settings" onclick={onClose}></button>
  <section class="profile-modal" aria-label="Account settings">
    <header>
      <div>
        <p>Account</p>
        <h2>Profile settings</h2>
      </div>
      <button type="button" aria-label="Close account settings" onclick={onClose}>×</button>
    </header>
    <form
      class="profile-form"
      onsubmit={(event) => {
        event.preventDefault();
        onSave();
      }}
    >
      <div class="profile-preview">
        <Avatar
          class="avatar large"
          id={user.id}
          name={displayName}
          src={avatarURL}
          size={56}
          loading="eager"
          fetchPriority="auto"
        />
        <div>
          <strong>{displayName || user.display_name}</strong>
          <span>{handle || handleLabel(user.handle) || "No handle set"}</span>
        </div>
      </div>
      <label class="field">
        <span>Display name</span>
        <input
          value={displayName}
          aria-label="Display name"
          maxlength="80"
          autocomplete="name"
          oninput={(event) => onDisplayName(event.currentTarget.value)}
        />
      </label>
      <label class="field">
        <span>Handle</span>
        <input
          value={handle}
          aria-label="Handle"
          placeholder="@steipete"
          autocomplete="username"
          oninput={(event) => onHandle(event.currentTarget.value)}
        />
      </label>
      <label class="field">
        <span>Avatar URL</span>
        <input
          value={avatarURL}
          aria-label="Avatar URL"
          placeholder="https://example.com/avatar.png"
          inputmode="url"
          oninput={(event) => onAvatarURL(event.currentTarget.value)}
        />
      </label>
      <label class="field check-field">
        <input
          type="checkbox"
          disabled={!browserNotificationsSupported || browserNotificationPermission === "denied"}
          checked={browserNotificationsEnabled}
          onchange={(event) => onBrowserNotificationsEnabled(event.currentTarget.checked)}
        />
        <span>Browser notifications</span>
      </label>
      {#if !browserNotificationsSupported}
        <p class="profile-status error">Browser notifications are not supported</p>
      {:else if browserNotificationPermission === "denied"}
        <p class="profile-status error">Browser notifications are blocked by this browser</p>
      {/if}
      <label class="field check-field">
        <input
          type="checkbox"
          checked={pushoverEnabled}
          onchange={(event) => onPushoverEnabled(event.currentTarget.checked)}
        />
        <span>Pushover notifications</span>
      </label>
      <label class="field">
        <span>Pushover user key</span>
        <input
          value={pushoverUserKey}
          aria-label="Pushover user key"
          maxlength="30"
          autocomplete="off"
          oninput={(event) => onPushoverUserKey(event.currentTarget.value)}
        />
      </label>
      {#if status}<p class="profile-status" class:error={statusError}>{status}</p>{/if}
      <div class="profile-actions">
        <button type="button" class="ghost-action" onclick={onClose}>Cancel</button>
        <button type="submit" class="primary-action">Save profile</button>
      </div>
    </form>
  </section>
</div>
