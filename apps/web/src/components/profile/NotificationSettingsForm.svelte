<script lang="ts">
  import { api } from "../../lib/api";
  import {
    readBrowserNotificationsEnabled,
    writeBrowserNotificationsEnabled,
  } from "../../lib/browserNotifications";
  import type { User } from "../../lib/types";

  type Props = {
    user: User;
    isDesktop?: boolean;
    onUserUpdated?: (user: User) => void;
    onBrowserNotificationsChanged?: (enabled: boolean) => void;
  };

  let {
    user,
    isDesktop = false,
    onUserUpdated,
    onBrowserNotificationsChanged,
  }: Props = $props();

  let savedUser = $state<User | null>(null);
  const currentUser = $derived(savedUser ?? user);
  let pushoverEnabled = $state(false);
  let pushoverUserKey = $state("");

  let browserNotificationsSupported = $state(false);
  let browserNotificationsEnabled = $state(false);
  let browserNotificationPermission = $state<NotificationPermission | "unsupported">("default");

  let status = $state("");
  let statusError = $state(false);
  let saving = $state(false);

  $effect(() => {
    pushoverEnabled = currentUser.notification_settings?.pushover_enabled ?? false;
    pushoverUserKey = currentUser.notification_settings?.pushover_user_key ?? "";
    syncBrowserNotificationState();
  });

  function syncBrowserNotificationState() {
    if (isDesktop) {
      browserNotificationsSupported = true;
      browserNotificationPermission = "granted";
      browserNotificationsEnabled = readBrowserNotificationsEnabled(currentUser.id);
      onBrowserNotificationsChanged?.(browserNotificationsEnabled);
      return;
    }
    browserNotificationsSupported = typeof Notification !== "undefined";
    browserNotificationPermission = browserNotificationsSupported ? Notification.permission : "unsupported";
    const storedEnabled = readBrowserNotificationsEnabled(currentUser.id);
    browserNotificationsEnabled = browserNotificationPermission === "granted" && storedEnabled;
    if (storedEnabled && browserNotificationPermission !== "granted") {
      writeBrowserNotificationsEnabled(currentUser.id, false);
    }
    onBrowserNotificationsChanged?.(browserNotificationsEnabled);
  }

  async function setBrowserNotifications(enabled: boolean) {
    status = "";
    statusError = false;
    if (!enabled) {
      writeBrowserNotificationsEnabled(currentUser.id, false);
      browserNotificationsEnabled = false;
      onBrowserNotificationsChanged?.(false);
      status = isDesktop ? "Desktop notifications disabled" : "Browser notifications disabled";
      return;
    }
    if (isDesktop) {
      browserNotificationsEnabled = writeBrowserNotificationsEnabled(currentUser.id, true);
      onBrowserNotificationsChanged?.(browserNotificationsEnabled);
      status = browserNotificationsEnabled
        ? "Desktop notifications enabled"
        : "Desktop notification preference could not be saved";
      statusError = !browserNotificationsEnabled;
      return;
    }
    if (typeof Notification === "undefined") {
      browserNotificationsSupported = false;
      browserNotificationPermission = "unsupported";
      browserNotificationsEnabled = false;
      onBrowserNotificationsChanged?.(false);
      status = "Browser notifications are not supported";
      statusError = true;
      return;
    }
    const permission =
      Notification.permission === "default" ? await Notification.requestPermission() : Notification.permission;
    browserNotificationsSupported = true;
    browserNotificationPermission = permission;
    if (permission === "granted") {
      browserNotificationsEnabled = writeBrowserNotificationsEnabled(currentUser.id, true);
      onBrowserNotificationsChanged?.(browserNotificationsEnabled);
      status = browserNotificationsEnabled
        ? "Browser notifications enabled"
        : "Browser notification preference could not be saved";
      statusError = !browserNotificationsEnabled;
      return;
    }
    writeBrowserNotificationsEnabled(currentUser.id, false);
    browserNotificationsEnabled = false;
    onBrowserNotificationsChanged?.(false);
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
          display_name: currentUser.display_name,
          handle: currentUser.handle ? `@${currentUser.handle}` : "",
          avatar_url: currentUser.avatar_url,
          notification_settings: {
            pushover_enabled: pushoverEnabled,
            pushover_user_key: pushoverUserKey,
          },
        }),
      });
      savedUser = data.user;
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
  <div class="settings-rows settings-rows--sectioned">
    <h3 class="settings-rows__head">Desktop</h3>

    <div class="settings-row2 settings-row2--toggle">
      <div class="settings-row2__desc">
        <label class="settings-row2__label" for="notifications-browser">
          {isDesktop ? "Desktop notifications" : "Browser notifications"}
        </label>
        <p class="settings-row2__hint">Show alerts when ClickClack is in the background.</p>
        {#if !isDesktop && !browserNotificationsSupported}
          <p class="settings-row2__hint is-error">Browser notifications are not supported on this device.</p>
        {:else if !isDesktop && browserNotificationPermission === "denied"}
          <p class="settings-row2__hint is-error">Browser notifications are blocked by this browser.</p>
        {/if}
      </div>
      <div class="settings-row2__control settings-row2__control--end">
        <input
          id="notifications-browser"
          type="checkbox"
          class="settings-switch"
          aria-label="Browser notifications"
          disabled={!browserNotificationsSupported || browserNotificationPermission === "denied"}
          checked={browserNotificationsEnabled}
          onchange={(event) => void setBrowserNotifications(event.currentTarget.checked)}
        />
      </div>
    </div>

    <h3 class="settings-rows__head">Mobile push</h3>

    <div class="settings-row2 settings-row2--toggle">
      <div class="settings-row2__desc">
        <label class="settings-row2__label" for="notifications-pushover">Pushover</label>
        <p class="settings-row2__hint">Send push notifications to your phone via Pushover.</p>
      </div>
      <div class="settings-row2__control settings-row2__control--end">
        <input
          id="notifications-pushover"
          type="checkbox"
          class="settings-switch"
          aria-label="Pushover notifications"
          bind:checked={pushoverEnabled}
        />
      </div>
    </div>

    <div class="settings-row2">
      <div class="settings-row2__desc">
        <label class="settings-row2__label" for="notifications-pushover-key">Pushover user key</label>
        <p class="settings-row2__hint">Find this in your Pushover dashboard under "Your User Key".</p>
      </div>
      <div class="settings-row2__control">
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
    </div>
  </div>

  <footer class="settings-footer">
    {#if status}
      <p class="settings-status" class:is-error={statusError} role="status">{status}</p>
    {:else}
      <span class="settings-footer__spacer" aria-hidden="true"></span>
    {/if}
    <button type="submit" class="settings-button settings-button--primary" disabled={saving}>
      {saving ? "Saving..." : "Save notifications"}
    </button>
  </footer>
</form>
