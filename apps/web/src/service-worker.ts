// ClickClack's service worker exists for one job: showing a notification the
// server pushed, and opening the right conversation when it is tapped.
//
// It installs no fetch handler and caches nothing, so a release behaves
// exactly as it does without a worker. The app registers it only after
// someone turns push on, never on load and never from an embed.
//
// The worker types live here rather than behind a webworker library
// reference: the app typechecks as one program, and pulling that library in
// beside the DOM one redeclares hundreds of shared names.

import { pushLandingURL } from "./lib/push-landing";

interface ExtendableEvent extends Event {
  waitUntil(promise: Promise<unknown>): void;
}

interface PushMessageData {
  json(): unknown;
}

interface PushEvent extends ExtendableEvent {
  readonly data: PushMessageData | null;
}

interface NotificationEvent extends ExtendableEvent {
  readonly notification: Notification;
}

interface WindowClient {
  readonly url: string;
  focus(): Promise<WindowClient>;
  postMessage(message: unknown): void;
}

interface WorkerClients {
  claim(): Promise<void>;
  matchAll(options?: { type?: string; includeUncontrolled?: boolean }): Promise<WindowClient[]>;
  openWindow(url: string): Promise<WindowClient | null>;
}

interface WorkerScope {
  readonly clients: WorkerClients;
  readonly registration: ServiceWorkerRegistration;
  skipWaiting(): Promise<void>;
  addEventListener(type: "install" | "activate", listener: (event: ExtendableEvent) => void): void;
  addEventListener(type: "push", listener: (event: PushEvent) => void): void;
  addEventListener(type: "notificationclick", listener: (event: NotificationEvent) => void): void;
  addEventListener(
    type: "pushsubscriptionchange",
    listener: (event: ExtendableEvent) => void,
  ): void;
}

type PushPayload = {
  user_id?: string;
  title?: string;
  body?: string;
  tag?: string;
  url?: string;
};

const worker = self as unknown as WorkerScope;

const FALLBACK_TITLE = "ClickClack";
const FALLBACK_URL = "/app";

worker.addEventListener("install", () => {
  void worker.skipWaiting();
});

worker.addEventListener("activate", (event) => {
  event.waitUntil(worker.clients.claim());
});

worker.addEventListener("push", (event) => {
  event.waitUntil(showPushNotification(readPayload(event.data)));
});

async function showPushNotification(payload: PushPayload): Promise<void> {
  // A relay can deliver after this browser switches accounts. Verify before
  // showing any preview; Safari still requires a visible alert on failure.
  let authorized = false;
  if (typeof payload.user_id === "string" && payload.user_id) {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 5000);
    try {
      const response = await fetch("/api/me", {
        credentials: "same-origin",
        cache: "no-store",
        signal: controller.signal,
      });
      if (response.ok) {
        const me = (await response.json()) as { user?: { id?: string } };
        authorized = me.user?.id === payload.user_id;
      }
    } catch {
      // Offline or unverifiable accounts receive no private preview.
    } finally {
      clearTimeout(timeout);
    }
  }
  const url =
    authorized && typeof payload.url === "string" && payload.url.startsWith("/app")
      ? payload.url
      : FALLBACK_URL;
  await worker.registration.showNotification((authorized && payload.title) || FALLBACK_TITLE, {
    body: (authorized && payload.body) || "Open ClickClack to check your messages.",
    tag: (authorized && payload.tag) || "clickclack",
    icon: "/icon-192.png",
    badge: "/icon-192.png",
    data: { url },
  });
}

worker.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const data = event.notification.data as { url?: string } | undefined;
  event.waitUntil(openApp(typeof data?.url === "string" ? data.url : FALLBACK_URL));
});

// The browser replaced or dropped this device's subscription on its own. The
// worker never registers the replacement: it cannot know which account on this
// device turned push on, and the cookies it would send belong to whoever is
// signed in now, who may never have opted in. It asks an open app window to
// run the account-scoped heal instead, which registers only for a user who
// turned push on here. The cost is deliberate: a device whose subscription
// rotates while the app is closed re-registers the next time the app opens,
// and receives nothing until then.
worker.addEventListener("pushsubscriptionchange", (event) => {
  event.waitUntil(askAppToRenew());
});

function readPayload(data: PushMessageData | null): PushPayload {
  if (!data) return {};
  try {
    const parsed = data.json();
    return parsed && typeof parsed === "object" ? (parsed as PushPayload) : {};
  } catch {
    return {};
  }
}

async function openApp(url: string): Promise<void> {
  const clients = await worker.clients.matchAll({ type: "window", includeUncontrolled: true });
  // Only the app itself is a navigation target. An embedded channel lives
  // inside someone else's page and must never be steered.
  const appClient = clients.find((client) => new URL(client.url).pathname.startsWith("/app"));
  if (appClient) {
    appClient.postMessage({ type: "clickclack:notification-click", url });
    await appClient.focus();
    return;
  }
  // A fresh window may not be listening yet when a message would arrive, so
  // the tap travels in the URL instead.
  await worker.clients.openWindow(pushLandingURL(url));
}

async function askAppToRenew(): Promise<void> {
  const clients = await worker.clients.matchAll({ type: "window", includeUncontrolled: true });
  for (const client of clients) {
    // Embedded channels live inside someone else's page; only the app heals.
    if (new URL(client.url).pathname.startsWith("/app")) {
      client.postMessage({ type: "clickclack:push-renew" });
    }
  }
}
