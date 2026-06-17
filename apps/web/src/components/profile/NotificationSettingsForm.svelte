<script lang="ts">
  import { api } from "../../lib/api";
  import {
    readBrowserNotificationsEnabled,
    writeBrowserNotificationsEnabled,
  } from "../../lib/browserNotifications";
  import type { User } from "../../lib/types";

  type Props = {
    user: User;
    onUserUpdated?: (user: User) => void;
  };

  let { user, onUserUpdated }: Props = $props();

  let pushoverEnabled = $state(user.notification_settings?.pushover_enabled ?? false);
  let pushoverUserKey = $state(user.notification_settings?.pushover_user_key ?? "");

  let browserNotificationsSupported = $state(false);
  let browserNotificationsEnabled = $state(false);
  let browserNotificationPermission = $state<NotificationPermission | "unsupported">("default");

  let status = $state("");
  let statusError = $state(false);
  let saving = $state(false);

  $effect(() => {
    syncBrowserNotificationState();
  });

  function syncBrowserNotificationState() {
    browserNotificationsSupported = typeof Notification !== "undefined";
    browserNotificationPermission = browserNotificationsSupported ? Notification.permission : "unsupported";
    const storedEnabled = readBrowserNotificationsEnabled(user.id);
    browserNotificationsEnabled = browserNotificationPermission === "granted" && storedEnabled;
    if (storedEnabled && browserNotificationPermission !== "granted") {
      writeBrowserNotificationsEnabled(user.id, false);
    }
  }

  async function setBrowserNotifications(enabled: boolean) {
    status = "";
    statusError = false;
    if (!enabled) {
      writeBrowserNotificationsEnabled(user.id, false);
      browserNotificationsEnabled = false;
      status = "Browser notifications disabled";
      return;
    }
    if (typeof Notification === "undefined") {
      browserNotificationsSupported = false;
      browserNotificationPermission = "unsupported";
      browserNotificationsEnabled = false;
      status = "Browser notifications are not supported";
      statusError = true;
      return;
    }
    const permission =
      Notification.permission === "default" ? await Notification.requestPermission() : Notification.permission;
    browserNotificationsSupported = true;
    browserNotificationPermission = permission;
    if (permission === "granted") {
      browserNotificationsEnabled = writeBrowserNotificationsEnabled(user.id, true);
      status = browserNotificationsEnabled
        ? "Browser notifications enabled"
        : "Browser notification preference could not be saved";
      statusError = !browserNotificationsEnabled;
      return;
    }
    writeBrowserNotificationsEnabled(user.id, false);
    browserNotificationsEnabled = false;
    status =
      permission === "denied"
        ? "Browser notifications are blocked by this browser"
        : "Browser notifications were not enabled";
    statusError = true;
  }

  async function savePushover() {
    if (saving) return;
    saving = true;
    status = "";
    statusError = false;
    try {
      const data = await api<{ user: User }>("/api/me", {
        method: "PATCH",
        body: JSON.stringify({
          display_name: user.display_name,
          handle: user.handle ? `@${user.handle}` : "",
          avatar_url: user.avatar_url,
          notification_settings: {
            pushover_enabled: pushoverEnabled,
            pushover_user_key: pushoverUserKey,
          },
        }),
      });
      user = data.user;
      pushoverEnabled = data.user.notification_settings?.pushover_enabled ?? false;
      pushoverUserKey = data.user.notification_settings?.pushover_user_key ?? "";
      onUserUpdated?.(data.user);
      status = "Saved";
    } catch (error) {
      status = error instanceof Error ? error.message : "Could not save notifications";
      statusError = true;
    } finally {
      saving = false;
    }
  }
</script>

<form
  class="settings-form"
  onsubmit={(event) => {
    event.preventDefault();
    void savePushover();
  }}
>
  <section class="settings-section">
    <h3 class="settings-section__h">Desktop</h3>
    <div class="settings-row">
      <div class="settings-row__main">
        <div class="settings-row__label">Browser notifications</div>
        <div class="settings-row__hint">Show desktop alerts when ClickClack is in the background.</div>
        {#if !browserNotificationsSupported}
          <div class="settings-row__hint is-error">Browser notifications are not supported on this device.</div>
        {:else if browserNotificationPermission === "denied"}
          <div class="settings-row__hint is-error">Browser notifications are blocked by this browser.</div>
        {/if}
      </div>
      <input
        type="checkbox"
        class="settings-switch"
        aria-label="Browser notifications"
        disabled={!browserNotificationsSupported || browserNotificationPermission === "denied"}
        checked={browserNotificationsEnabled}
        onchange={(event) => void setBrowserNotifications(event.currentTarget.checked)}
      />
    </div>
  </section>

  <section class="settings-section">
    <h3 class="settings-section__h">Mobile push</h3>
    <div class="settings-row">
      <div class="settings-row__main">
        <div class="settings-row__label">Pushover</div>
        <div class="settings-row__hint">Send push notifications to your phone via Pushover.</div>
      </div>
      <input
        type="checkbox"
        class="settings-switch"
        aria-label="Pushover notifications"
        bind:checked={pushoverEnabled}
      />
    </div>
    <div class="settings-field">
      <label class="settings-field__label" for="notifications-pushover-key">Pushover user key</label>
      <p class="settings-field__hint">Find this in your Pushover dashboard under "Your User Key".</p>
      <input
        id="notifications-pushover-key"
        class="settings-input"
        bind:value={pushoverUserKey}
        aria-label="Pushover user key"
        maxlength="30"
        placeholder="u..."
        autocomplete="off"
      />
    </div>
  </section>

  <div class="settings-actions">
    {#if status}
      <p class="settings-status" class:is-error={statusError} role="status">{status}</p>
    {/if}
    <button type="submit" class="settings-button settings-button--primary" disabled={saving}>
      {saving ? "Saving..." : "Save notifications"}
    </button>
  </div>
</form>
