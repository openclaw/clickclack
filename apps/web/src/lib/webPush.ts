// Web push, the third notification leg beside the in-page alerts and Pushover.
// The server holds the subscription; this module only talks to the browser and
// keeps a per-user note of which devices the user turned on.

import { api } from "./api";
import {
  applicationServerKey,
  deviceLabel,
  pushDeviceKey,
  pushSupported,
  subscriptionIsStale,
} from "./push-capability";

export type PushDevice = {
  id: string;
  user_agent: string;
  created_at: string;
  updated_at: string;
  last_success_at?: string;
  failure_count: number;
};

export type PushState = {
  enabled: boolean;
  vapid_public_key: string;
  subscriptions: PushDevice[];
  this_device: boolean;
  this_device_stale: boolean;
};

const STORAGE_PREFIX = "clickclack:web-push-enabled:v1:";
// The server key this device last subscribed under, per user. Safari does not
// expose a subscription's applicationServerKey, so this is how a rotation is
// noticed there.
const KEY_PREFIX = "clickclack:web-push-key:v1:";
const WORKER_URL = "/service-worker.js";

function storageKey(userID: string): string {
  return `${STORAGE_PREFIX}${userID}`;
}

export function readPushEnabled(userID: string): boolean {
  if (!userID) return false;
  try {
    return window.localStorage.getItem(storageKey(userID)) === "enabled";
  } catch {
    return false;
  }
}

export function writePushEnabled(userID: string, enabled: boolean): void {
  if (!userID) return;
  try {
    if (enabled) window.localStorage.setItem(storageKey(userID), "enabled");
    else window.localStorage.removeItem(storageKey(userID));
  } catch {
    // A blocked storage jar only costs the reminder to re-register on start.
  }
}

export function readSubscribedKey(userID: string): string {
  if (!userID) return "";
  try {
    return window.localStorage.getItem(`${KEY_PREFIX}${userID}`) ?? "";
  } catch {
    return "";
  }
}

export function writeSubscribedKey(userID: string, key: string): void {
  if (!userID) return;
  try {
    if (key) window.localStorage.setItem(`${KEY_PREFIX}${userID}`, key);
    else window.localStorage.removeItem(`${KEY_PREFIX}${userID}`);
  } catch {
    // Without storage the browser's own key report is the only rotation signal.
  }
}

// fetchPushState reads the account's push state. Given the subscription this
// browser holds, the answer also says whether that subscription is one of the
// account's devices, named by a digest so the endpoint never leaves the
// browser.
export async function fetchPushState(subscription?: PushSubscription | null): Promise<PushState> {
  const key = subscription ? await pushDeviceKey(subscription.endpoint) : "";
  return api<PushState>(key ? `/api/me/push?device=${encodeURIComponent(key)}` : "/api/me/push");
}

// registerPushWorker installs the worker only when a user asks for push. The
// app never registers it on load, and it is never registered from an embed.
// It resolves once a worker is active: on first activation register() returns
// while the worker is still installing, and a push manager without an active
// worker refuses to subscribe.
export async function registerPushWorker(): Promise<ServiceWorkerRegistration> {
  const existing = await navigator.serviceWorker.getRegistration("/");
  if (!existing) await navigator.serviceWorker.register(WORKER_URL, { scope: "/" });
  return navigator.serviceWorker.ready;
}

// ensurePushSubscription returns this device's subscription under the key the
// server signs with now. One made under an earlier key is replaced, because
// its push service refuses everything signed with the new one, and its row on
// the server is removed for userID since it can never deliver again. state is
// the push state the caller read naming this browser's subscription, so the
// server can say the device is stale when the browser cannot.
export async function ensurePushSubscription(
  registration: ServiceWorkerRegistration,
  state: PushState,
  userID: string,
): Promise<PushSubscription> {
  const vapidPublicKey = state.vapid_public_key;
  const key = applicationServerKey(vapidPublicKey);
  const existing = await registration.pushManager.getSubscription();
  if (
    existing &&
    !subscriptionIsStale(
      existing.options.applicationServerKey,
      readSubscribedKey(userID),
      vapidPublicKey,
      state.this_device_stale,
    )
  ) {
    writeSubscribedKey(userID, vapidPublicKey);
    return existing;
  }
  if (existing) {
    const staleEndpoint = existing.endpoint;
    await existing.unsubscribe();
    await forgetSubscription(staleEndpoint, userID).catch(() => undefined);
  }
  const fresh = await registration.pushManager.subscribe({
    userVisibleOnly: true,
    applicationServerKey: key,
  });
  writeSubscribedKey(userID, vapidPublicKey);
  return fresh;
}

export async function currentPushSubscription(): Promise<PushSubscription | null> {
  if (!pushSupported()) return null;
  const registration = await navigator.serviceWorker.getRegistration("/");
  return registration ? await registration.pushManager.getSubscription() : null;
}

// signedInUserID asks the server which account this browser is signed in to
// now. Every tab shares one cookie jar, so another tab signing in changes it
// underneath a tab still showing the account before.
export async function signedInUserID(): Promise<string> {
  const me = await api<{ user: { id: string } }>("/api/me");
  return me.user.id;
}

// storeSubscription registers this device for userID, the account whose
// opt-in the caller checked. The server refuses it with 409 when the cookie
// now belongs to someone else, so a switch between the check and this write
// registers nothing.
export async function storeSubscription(
  subscription: PushSubscription,
  userID: string,
): Promise<PushDevice> {
  const payload = subscription.toJSON() as { endpoint?: string; keys?: Record<string, string> };
  const result = await api<{ subscription: PushDevice }>("/api/me/push/subscriptions", {
    method: "PUT",
    body: JSON.stringify({
      user_id: userID,
      endpoint: payload.endpoint ?? subscription.endpoint,
      keys: { p256dh: payload.keys?.p256dh ?? "", auth: payload.keys?.auth ?? "" },
      user_agent: deviceLabel(),
    }),
  });
  return result.subscription;
}

// forgetSubscription removes this device for userID, the account the caller
// is turning it off for. The server refuses it with 409 when the cookie now
// belongs to someone else, and removes nothing.
export async function forgetSubscription(endpoint: string, userID: string): Promise<void> {
  await api<void>("/api/me/push/subscriptions", {
    method: "DELETE",
    body: JSON.stringify({ user_id: userID, endpoint }),
  });
}

// healPushSubscription re-registers this device on app start, and when the
// service worker reports the browser replaced its subscription, but only for a
// user who turned push on here. A key rotation changes the endpoint, so
// without this a device goes quiet with nothing on screen to explain it. A
// reinstall wipes the note that push was on, so it is a fresh opt-in from the
// switch, not a heal. This is the only path that registers a device without
// the switch.
//
// userID is the account this tab shows, which may no longer be the one the
// browser is signed in to. The heal goes ahead only when the server names the
// same account; otherwise the opt-in is not the signed-in account's to use,
// and both accounts' notes are left as they are.
export async function healPushSubscription(userID: string): Promise<void> {
  if (!userID || !pushSupported()) return;
  try {
    const current = await currentPushSubscription();
    const optedIn = readPushEnabled(userID);
    if (!optedIn && !current) return;
    if ((await signedInUserID()) !== userID) return;
    const state = await fetchPushState(current);
    if (!state.enabled) return;
    if (!optedIn) {
      if (current && !state.this_device) {
        await current.unsubscribe();
        const registration = await navigator.serviceWorker.getRegistration("/");
        for (const notification of (await registration?.getNotifications()) ?? [])
          notification.close();
      }
      return;
    }
    const registration = await registerPushWorker();
    const subscription = await ensurePushSubscription(registration, state, userID);
    await storeSubscription(subscription, userID);
  } catch {
    // The settings row is the recovery path; a failed heal must never break
    // the app's boot.
  }
}
