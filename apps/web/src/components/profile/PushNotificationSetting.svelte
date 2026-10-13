<script lang="ts">
  import { readableAPIError } from "../../lib/api";
  import {
    appleDevice,
    deviceLabel,
    installedToHomeScreen,
    pushSupported,
  } from "../../lib/push-capability";
  import {
    currentPushSubscription,
    ensurePushSubscription,
    fetchPushState,
    forgetSubscription,
    readPushEnabled,
    registerPushWorker,
    signedInUserID,
    storeSubscription,
    writePushEnabled,
    writeSubscribedKey,
  } from "../../lib/webPush";
  import type { User } from "../../lib/types";

  type Props = {
    user: User;
    isDesktop?: boolean;
  };

  let { user, isDesktop = false }: Props = $props();

  // Another tab may have signed this browser in to a different account.
  // Whoever is signed in now did not flip this switch.
  const accountChanged = "This browser is now signed in to a different account. Reload to continue.";

  let available = $state(false);
  let supported = $state(false);
  let enabled = $state(false);
  let busy = $state(false);
  let status = $state("");
  let statusError = $state(false);

  $effect(() => {
    void load(user.id, isDesktop);
  });

  async function load(userID: string, desktop: boolean) {
    if (desktop) {
      // The desktop app denies every permission request and shows its own
      // notifications, so the row would be a dead switch there.
      available = false;
      return;
    }
    supported = pushSupported();
    try {
      const subscription = supported ? await currentPushSubscription() : null;
      const state = await fetchPushState(subscription);
      // Asked after the state, not beside it: a sign-in that landed before the
      // state was read shows up here, and that state is the new account's.
      const accountMoved = state.enabled && (await signedInUserID()) !== userID;
      available = state.enabled;
      if (!state.enabled) return;
      if (accountMoved) {
        enabled = false;
        status = accountChanged;
        statusError = true;
        return;
      }
      // One browser subscription serves every account on this device, and the
      // server keeps it for whichever account registered it last. The switch
      // is on only while the server says this browser's subscription is one
      // of this account's devices; a device elsewhere does not count.
      enabled = Boolean(subscription) && readPushEnabled(userID) && state.this_device;
      // A device whose subscription is gone is off here, whatever this
      // browser remembered.
      if (!subscription && readPushEnabled(userID)) writePushEnabled(userID, false);
    } catch {
      available = false;
    }
  }

  async function setEnabled(control: HTMLInputElement) {
    const next = control.checked;
    status = "";
    statusError = false;
    busy = true;
    try {
      if (next) await turnOn();
      else await turnOff();
    } catch (error) {
      // A failed turn-off reads on until the server has removed the device,
      // as when it refuses because another account signed in after the check.
      if (next) enabled = false;
      status = readableAPIError(
        error,
        next ? "Push notifications could not be turned on" : "Push notifications could not be turned off",
      );
      statusError = true;
    } finally {
      busy = false;
      // The click has already moved the box, and `checked` rewrites it only
      // when `enabled` changes. A refusal that leaves `enabled` as it was
      // puts the box back here.
      control.checked = enabled;
    }
  }

  async function turnOn() {
    // The switch click is the user gesture the permission prompt needs.
    const permission = Notification.permission === "default"
      ? await Notification.requestPermission()
      : Notification.permission;
    if (permission !== "granted") {
      enabled = false;
      status = permission === "denied"
        ? "Notifications are blocked for this site. Allow them in your browser settings, then try again."
        : "Notifications were not enabled.";
      statusError = true;
      return;
    }
    const [state, signedIn] = await Promise.all([
      currentPushSubscription().then(fetchPushState),
      signedInUserID(),
    ]);
    if (!state.enabled) {
      available = false;
      return;
    }
    if (signedIn !== user.id) {
      enabled = false;
      status = accountChanged;
      statusError = true;
      return;
    }
    const registration = await registerPushWorker();
    const subscription = await ensurePushSubscription(registration, state, user.id);
    await storeSubscription(subscription, user.id);
    writePushEnabled(user.id, true);
    enabled = true;
    status = "On for this device";
  }

  async function turnOff() {
    // The subscription is the device of the account this row shows. The
    // signed-in account cannot remove it, and dropping it from the browser
    // would leave that account's row pointing at nothing.
    if ((await signedInUserID()) !== user.id) {
      status = accountChanged;
      statusError = true;
      return;
    }
    const subscription = await currentPushSubscription();
    // The server goes first: a sign-in landing after the check above is
    // refused there, and the browser keeps its subscription.
    if (subscription) await forgetSubscription(subscription.endpoint, user.id);
    writePushEnabled(user.id, false);
    writeSubscribedKey(user.id, "");
    enabled = false;
    if (!subscription) return;
    await subscription.unsubscribe();
    status = "Off for this device";
  }

  const needsInstall = $derived(!supported && appleDevice() && !installedToHomeScreen());
</script>

{#if available}
  <div class="settings-row2 settings-row2--toggle">
    <div class="settings-row2__desc">
      <label class="settings-row2__label" for="notifications-push">Push notifications on this device</label>
      <p class="settings-row2__hint">Receive alerts when the app is closed.</p>
      {#if needsInstall}
        <p class="settings-row2__hint">
          Add ClickClack to your Home Screen from the Share menu, then open it from there to turn this on.
        </p>
      {:else if !supported}
        <p class="settings-row2__hint is-error">This browser does not support push notifications.</p>
      {:else if enabled}
        <p class="settings-row2__hint">On for {deviceLabel()}</p>
      {/if}
      {#if status}
        <p class="settings-row2__hint" class:is-error={statusError} role="status">{status}</p>
      {/if}
    </div>
    <div class="settings-row2__control settings-row2__control--end">
      <input
        id="notifications-push"
        type="checkbox"
        class="settings-switch"
        aria-label="Push notifications on this device"
        disabled={!supported || busy}
        checked={enabled}
        onchange={(event) => void setEnabled(event.currentTarget)}
      />
    </div>
  </div>
{/if}
