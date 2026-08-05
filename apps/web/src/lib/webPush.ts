import { api } from "./api";

type WebPushPublicKeyResponse = {
  public_key: string;
};

export type BrowserPushSubscriptionJSON = {
  endpoint: string;
  expirationTime: string | null;
  keys: {
    auth: string;
    p256dh: string;
  };
};

const WEB_PUSH_SUPPORTED =
  typeof window !== "undefined" &&
  window.isSecureContext &&
  "serviceWorker" in navigator &&
  "PushManager" in window;

function urlBase64ToUint8Array(value: string): Uint8Array {
  const padding = "=".repeat((4 - (value.length % 4 || 4)) % 4);
  const base64 = (value + padding).replace(/-/g, "+").replace(/_/g, "/");
  const raw = atob(base64);
  const out = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i++) {
    out[i] = raw.charCodeAt(i);
  }
  return out;
}

export function isWebPushSupported(): boolean {
  return WEB_PUSH_SUPPORTED;
}

export async function enableWebPushSubscription(): Promise<BrowserPushSubscriptionJSON | null> {
  if (!WEB_PUSH_SUPPORTED) return null;

  const [{ public_key }, registration] = await Promise.all([
    api<WebPushPublicKeyResponse>("/web-push/public-key"),
    navigator.serviceWorker.ready,
  ]);

  const existing = await registration.pushManager.getSubscription();
  const subscription =
    existing ??
    (await registration.pushManager.subscribe({
      userVisibleOnly: true,
      applicationServerKey: urlBase64ToUint8Array(public_key) as BufferSource,
    }));

  const payload = subscription.toJSON() as BrowserPushSubscriptionJSON;
  await api<{ subscription: BrowserPushSubscriptionJSON }>("/me/web-push-subscriptions", {
    method: "PUT",
    body: JSON.stringify(payload),
  });
  return payload;
}

export async function disableWebPushSubscription(): Promise<boolean> {
  if (!WEB_PUSH_SUPPORTED) return false;

  const registration = await navigator.serviceWorker.ready;
  const subscription = await registration.pushManager.getSubscription();
  if (!subscription) return false;

  const payload = subscription.toJSON() as BrowserPushSubscriptionJSON;
  await api("/me/web-push-subscriptions", {
    method: "DELETE",
    body: JSON.stringify(payload),
  });
  return subscription.unsubscribe();
}
