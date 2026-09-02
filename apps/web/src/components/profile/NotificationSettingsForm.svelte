<script lang="ts">
  import AccountSettingsForm from "./AccountSettingsForm.svelte";
  import BrowserNotificationSetting from "./BrowserNotificationSetting.svelte";
  import type { User } from "../../lib/types";

  type Props = {
    user: User;
    isDesktop?: boolean;
    onUserUpdated: (user: User) => void;
    onBrowserNotificationsChanged?: (enabled: boolean) => void;
  };

  let {
    user,
    isDesktop = false,
    onUserUpdated,
    onBrowserNotificationsChanged,
  }: Props = $props();

  let pushoverEnabled = $state(false);
  let pushoverUserKey = $state("");

  $effect(() => {
    pushoverEnabled = user.notification_settings?.pushover_enabled ?? false;
    pushoverUserKey = user.notification_settings?.pushover_user_key ?? "";
  });

</script>

<AccountSettingsForm
  section="notifications"
  {onUserUpdated}
  payload={() => ({
    notification_settings: {
      pushover_enabled: pushoverEnabled,
      pushover_user_key: pushoverUserKey,
    },
  })}
>
  <div class="settings-rows settings-rows--sectioned">
    <h3 class="settings-rows__head">Desktop</h3>

    <BrowserNotificationSetting
      {user}
      {isDesktop}
      onChanged={onBrowserNotificationsChanged}
    />

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

</AccountSettingsForm>
