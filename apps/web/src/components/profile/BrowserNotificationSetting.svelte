<script lang="ts">
  import {
    readBrowserNotificationsEnabled,
    writeBrowserNotificationsEnabled,
  } from "../../lib/browserNotifications";
  import type { User } from "../../lib/types";

  type Props = {
    user: User;
    isDesktop?: boolean;
    onChanged?: (enabled: boolean) => void;
  };

  let { user, isDesktop = false, onChanged }: Props = $props();

  let supported = $state(false);
  let enabled = $state(false);
  let permission = $state<NotificationPermission | "unsupported">("default");
  let status = $state("");
  let statusError = $state(false);

  $effect(() => {
    syncState(user.id, isDesktop);
  });

  function syncState(userID: string, desktop: boolean) {
    if (desktop) {
      supported = true;
      permission = "granted";
      enabled = readBrowserNotificationsEnabled(userID);
      onChanged?.(enabled);
      return;
    }

    supported = typeof Notification !== "undefined";
    permission = supported ? Notification.permission : "unsupported";
    const storedEnabled = readBrowserNotificationsEnabled(userID);
    enabled = permission === "granted" && storedEnabled;
    if (storedEnabled && permission !== "granted") {
      writeBrowserNotificationsEnabled(userID, false);
    }
    onChanged?.(enabled);
  }

  async function setEnabled(nextEnabled: boolean) {
    status = "";
    statusError = false;
    if (!nextEnabled) {
      writeBrowserNotificationsEnabled(user.id, false);
      enabled = false;
      onChanged?.(false);
      status = isDesktop ? "Desktop notifications disabled" : "Browser notifications disabled";
      return;
    }

    if (isDesktop) {
      enabled = writeBrowserNotificationsEnabled(user.id, true);
      onChanged?.(enabled);
      status = enabled
        ? "Desktop notifications enabled"
        : "Desktop notification preference could not be saved";
      statusError = !enabled;
      return;
    }

    if (typeof Notification === "undefined") {
      supported = false;
      permission = "unsupported";
      enabled = false;
      onChanged?.(false);
      status = "Browser notifications are not supported";
      statusError = true;
      return;
    }

    permission =
      Notification.permission === "default"
        ? await Notification.requestPermission()
        : Notification.permission;
    supported = true;
    if (permission === "granted") {
      enabled = writeBrowserNotificationsEnabled(user.id, true);
      onChanged?.(enabled);
      status = enabled
        ? "Browser notifications enabled"
        : "Browser notification preference could not be saved";
      statusError = !enabled;
      return;
    }

    writeBrowserNotificationsEnabled(user.id, false);
    enabled = false;
    onChanged?.(false);
    status =
      permission === "denied"
        ? "Browser notifications are blocked by this browser"
        : "Browser notifications were not enabled";
    statusError = true;
  }
</script>

<div class="settings-row2 settings-row2--toggle">
  <div class="settings-row2__desc">
    <label class="settings-row2__label" for="notifications-browser">
      {isDesktop ? "Desktop notifications" : "Browser notifications"}
    </label>
    <p class="settings-row2__hint">Show alerts when ClickClack is in the background.</p>
    {#if !isDesktop && !supported}
      <p class="settings-row2__hint is-error">Browser notifications are not supported on this device.</p>
    {:else if !isDesktop && permission === "denied"}
      <p class="settings-row2__hint is-error">Browser notifications are blocked by this browser.</p>
    {/if}
    {#if status}
      <p class="settings-row2__hint" class:is-error={statusError} role="status">{status}</p>
    {/if}
  </div>
  <div class="settings-row2__control settings-row2__control--end">
    <input
      id="notifications-browser"
      type="checkbox"
      class="settings-switch"
      aria-label={isDesktop ? "Desktop notifications" : "Browser notifications"}
      disabled={!supported || permission === "denied"}
      checked={enabled}
      onchange={(event) => void setEnabled(event.currentTarget.checked)}
    />
  </div>
</div>
