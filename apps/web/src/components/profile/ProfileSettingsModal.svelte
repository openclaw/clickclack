<script lang="ts">
  import { avatarHue, avatarInitial, handleLabel } from "../../lib/chat/people";
  import type { User } from "../../lib/types";

  type Props = {
    user: User;
    displayName: string;
    handle: string;
    avatarURL: string;
    pushoverEnabled: boolean;
    pushoverUserKey: string;
    status: string;
    statusError: boolean;
    onDisplayName: (value: string) => void;
    onHandle: (value: string) => void;
    onAvatarURL: (value: string) => void;
    onPushoverEnabled: (value: boolean) => void;
    onPushoverUserKey: (value: string) => void;
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
    status,
    statusError,
    onDisplayName,
    onHandle,
    onAvatarURL,
    onPushoverEnabled,
    onPushoverUserKey,
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
        <span class="avatar large" style="--hue: {avatarHue(user.id)}deg">
          {#if avatarURL}
            <img src={avatarURL} alt="" loading="lazy" />
          {:else}
            {avatarInitial(displayName)}
          {/if}
        </span>
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
