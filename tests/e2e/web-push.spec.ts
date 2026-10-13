import {
  expect,
  test,
  type APIRequestContext,
  type Page,
  type Request,
  type Worker,
} from "@playwright/test";
import { createHash, randomUUID } from "node:crypto";
import { createGeneralChannel } from "./channel-fixture";
import { waitForAppReady } from "./app-ready";

const relayOrigin = `http://127.0.0.1:${Number(process.env.CLICKCLACK_E2E_PORT || "18082") + 2}`;

// The signed-in session case carries a real session cookie, and a trace would
// keep it. Playwright sets tracing per worker, so it is off for the file.
test.use({ trace: "off" });

type Delivery = {
  hasVapidToken: boolean;
  contentEncoding?: string;
  contentType?: string;
  ttl?: string;
  urgency?: string;
  encryptedBytes: number;
};

// The browser will not hand out a real push subscription in a headless run, so
// the push manager is replaced with one that points at the fake relay. The
// service worker, the settings row, the API, and the server's encryption and
// delivery are all the real ones.
//
// The stand-in keeps the two browser rules this feature depends on: subscribe
// refuses while the registration has no active worker, the way Chromium does
// on first activation, and a subscription reports the application server key
// it was made under. A test can also start the page holding an older
// subscription, or drop the current one the way a browser rotating it would.
// An older subscription with no key stands in for Safari, which never reports
// the key a subscription was made under.
type StubOptions = { stale?: { endpoint: string; key: string | null } };

// The RFC 8291 example subscription keys: a real point on P-256 and a 16 byte
// auth secret, so the server can encrypt for them.
const exampleSubscriptionKeys = {
  p256dh: "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4",
  auth: "BTBZMqHH6r4Tts7J_aSIgg",
};

async function stubPushManager(page: Page, endpoint: string, options: StubOptions = {}) {
  await page.addInitScript(
    ({ endpoint, stale, subscriptionKeys }) => {
      const calls: string[] = [];
      let subscribed = 0;
      type StubSubscription = {
        endpoint: string;
        options: { applicationServerKey: ArrayBuffer | null };
        toJSON: () => unknown;
        unsubscribe: () => Promise<boolean>;
      };
      let current: StubSubscription | null = null;
      const makeSubscription = (subscriptionEndpoint: string, key: ArrayBuffer | null) => {
        const subscription: StubSubscription = {
          endpoint: subscriptionEndpoint,
          options: { applicationServerKey: key },
          toJSON: () => ({ endpoint: subscriptionEndpoint, keys: subscriptionKeys }),
          unsubscribe: async () => {
            calls.push(`unsubscribe:${subscriptionEndpoint}`);
            if (current === subscription) current = null;
            return true;
          },
        };
        return subscription;
      };
      if (stale) {
        let key: ArrayBuffer | null = null;
        if (stale.key) {
          const padded = stale.key.replace(/-/g, "+").replace(/_/g, "/");
          const binary = atob(padded + "=".repeat((4 - (padded.length % 4)) % 4));
          key = Uint8Array.from(binary, (character) => character.charCodeAt(0)).buffer;
        }
        current = makeSubscription(stale.endpoint, key);
      }
      const managers = new WeakMap<ServiceWorkerRegistration, unknown>();
      const managerFor = (registration: ServiceWorkerRegistration) => {
        let manager = managers.get(registration);
        if (!manager) {
          manager = {
            subscribe: async (subscribeOptions: { applicationServerKey?: BufferSource }) => {
              if (!registration.active) {
                throw new DOMException(
                  "Subscription failed - no active Service Worker",
                  "AbortError",
                );
              }
              subscribed += 1;
              const next = subscribed === 1 ? endpoint : `${endpoint}-${subscribed}`;
              const key = subscribeOptions.applicationServerKey
                ? new Uint8Array(subscribeOptions.applicationServerKey as ArrayBuffer).slice()
                    .buffer
                : null;
              current = makeSubscription(next, key);
              calls.push(`subscribe:${next}`);
              return current;
            },
            getSubscription: async () => current,
            permissionState: async () => "granted",
          };
          managers.set(registration, manager);
        }
        return manager;
      };
      Object.defineProperty(ServiceWorkerRegistration.prototype, "pushManager", {
        configurable: true,
        get(this: ServiceWorkerRegistration) {
          return managerFor(this);
        },
      });
      (window as unknown as { __pushStub: unknown }).__pushStub = {
        calls,
        drop: () => {
          current = null;
        },
      };
      // Chromium denies notifications to an automated profile, and the
      // permission is not what this test is about.
      class GrantedNotification {
        static permission: NotificationPermission = "granted";
        static requestPermission = () => Promise.resolve("granted" as NotificationPermission);
        close() {}
      }
      (window as unknown as { Notification: unknown }).Notification = GrantedNotification;
    },
    { endpoint, stale: options.stale, subscriptionKeys: exampleSubscriptionKeys },
  );
}

async function stubCalls(page: Page): Promise<string[]> {
  return page.evaluate(
    () => (window as unknown as { __pushStub: { calls: string[] } }).__pushStub.calls,
  );
}

async function openNotificationSettings(page: Page) {
  // At phone width the rail with the account button lives behind the nav
  // toggle, exactly as a person on a phone finds it.
  const navToggle = page.locator(".mobile-nav-toggle");
  if (await navToggle.isVisible()) await navToggle.click();
  await page.getByRole("button", { name: /Account settings for/ }).click();
  const modal = page.getByRole("dialog", { name: "Account settings" });
  await expect(modal.getByRole("heading", { name: "Profile settings" })).toBeVisible();
  await modal.getByRole("button", { name: "Notifications", exact: true }).click();
  await expect(modal.getByRole("heading", { name: "Notifications" })).toBeVisible();
  return modal;
}

// The page's own fetch carries the signed-in identity that page.request does
// not, so every assertion about this account reads through the app.
async function pushState(page: Page): Promise<{
  enabled: boolean;
  vapid_public_key: string;
  subscriptions: { id: string; user_agent: string }[];
}> {
  return page.evaluate(() => fetch("/api/me/push").then((response) => response.json()));
}

async function relayDeliveries(page: Page): Promise<Delivery[]> {
  const response = await page.request.get(`${relayOrigin}/deliveries`);
  expect(response.ok()).toBe(true);
  return (await response.json()) as Delivery[];
}

for (const surface of ["desktop", "phone"] as const) {
  test(`a ${surface} device turns push on and receives a delivery`, async ({ page }, testInfo) => {
    if (surface === "phone") await page.setViewportSize({ width: 390, height: 844 });
    const endpoint = `${relayOrigin}/push/${randomUUID()}`;
    await stubPushManager(page, endpoint);

    // An isolated account keeps this test's devices out of every other
    // test's subscription list.
    const { workspace, channel, route } = await createGeneralChannel(page, `Push ${surface}`, true);
    await page.goto(route);
    await waitForAppReady(page);

    const before = (await relayDeliveries(page)).length;
    const modal = await openNotificationSettings(page);
    const control = modal.getByLabel("Push notifications on this device");
    await expect(control).toBeEnabled();

    const stored = page.waitForResponse(
      (response) =>
        response.url().includes("/api/me/push/subscriptions") &&
        response.request().method() === "PUT" &&
        response.status() === 200,
    );
    await control.check();
    await stored;
    await expect(modal.getByText("On for this device")).toBeVisible();
    await page.screenshot({ path: testInfo.outputPath(`push-on-${surface}.png`) });

    const body = await pushState(page);
    expect(body.enabled).toBe(true);
    expect(body.vapid_public_key).not.toBe("");
    expect(body.subscriptions).toHaveLength(1);
    expect(body.subscriptions[0].user_agent).not.toBe("");
    // The summary is the whole story the API tells about a device.
    expect(JSON.stringify(body.subscriptions[0])).not.toContain(endpoint);

    await modal.getByRole("button", { name: "Close", exact: true }).click();

    // A second member posts, because the author never notifies themselves.
    const poster = await page.request.post(`/api/workspaces/${workspace.id}/bots`, {
      data: { display_name: `Poster ${randomUUID().slice(0, 8)}` },
    });
    expect(poster.ok()).toBe(true);
    const { bot, bot_token: botToken } = (await poster.json()) as {
      bot: { id: string };
      bot_token: { token: string };
    };
    const posted = await page.request.post(`/api/channels/${channel.id}/messages`, {
      headers: { Authorization: `Bearer ${botToken.token}` },
      data: { body: `hello from ${bot.id}` },
    });
    expect(posted.ok()).toBe(true);

    await expect
      .poll(async () => (await relayDeliveries(page)).length, { timeout: 15_000 })
      .toBeGreaterThan(before);
    const delivery = (await relayDeliveries(page)).at(-1)!;
    expect(delivery.hasVapidToken).toBe(true);
    expect(delivery.contentEncoding).toBe("aes128gcm");
    expect(delivery.contentType).toBe("application/octet-stream");
    expect(delivery.ttl).toBe("86400");
    expect(delivery.urgency).toBe("normal");
    // Salt, the payload, the sender key, and the tag: an empty body would be
    // far smaller than this.
    expect(delivery.encryptedBytes).toBeGreaterThan(100);

    const reopened = await openNotificationSettings(page);
    const control2 = reopened.getByLabel("Push notifications on this device");
    await expect(control2).toBeChecked();
    await control2.uncheck();
    await expect(reopened.getByText("Off for this device")).toBeVisible();
    expect((await pushState(page)).subscriptions).toHaveLength(0);
  });
}

// rememberPushEnabled marks push as turned on for the signed-in account on
// this device, the state a returning user opens the app in. The page's own
// fetch is the one that carries the account's identity.
async function rememberPushEnabled(page: Page) {
  await page.evaluate(async () => {
    const response = await fetch("/api/me");
    const { user } = (await response.json()) as { user: { id: string } };
    window.localStorage.setItem(`clickclack:web-push-enabled:v1:${user.id}`, "enabled");
  });
}

function isPushRegistration(request: Request): boolean {
  return request.url().includes("/api/me/push/subscriptions") && request.method() === "PUT";
}

test("a device holding a subscription under an old key replaces it when the app opens", async ({
  page,
}) => {
  const endpoint = `${relayOrigin}/push/${randomUUID()}`;
  const staleEndpoint = `${relayOrigin}/push/stale-${randomUUID()}`;
  // A second real P-256 point, standing in for the key the server used before
  // its operator rotated the pair.
  const staleKey =
    "BP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A8";
  await stubPushManager(page, endpoint, { stale: { endpoint: staleEndpoint, key: staleKey } });
  const { route } = await createGeneralChannel(page, "Push rotation", true);
  await page.goto(route);
  await waitForAppReady(page);
  await rememberPushEnabled(page);

  const registered = page.waitForRequest(
    (request) =>
      isPushRegistration(request) &&
      (JSON.parse(request.postData() ?? "{}") as { endpoint?: string }).endpoint === endpoint,
  );
  await page.reload();
  await waitForAppReady(page);
  await registered;

  await expect
    .poll(() => stubCalls(page))
    .toEqual([`unsubscribe:${staleEndpoint}`, `subscribe:${endpoint}`]);
  await expect.poll(async () => (await pushState(page)).subscriptions.length).toBe(1);
});

// Safari hides the key a subscription was made under, and a device that
// subscribed before the app remembered keys, or lost its storage, remembers
// none, so only the server can say the device is under a retired key. The
// server's answer is set here for this browser's device alone: a headless run
// has one server key, and the Go tests cover how the server decides.
for (const serverSaysStale of [true, false]) {
  test(`a device that cannot report its key ${serverSaysStale ? "replaces its subscription when the server names it stale" : "keeps its subscription when the server does not"}`, async ({
    page,
  }) => {
    const endpoint = `${relayOrigin}/push/${randomUUID()}`;
    const heldEndpoint = `${relayOrigin}/push/held-${randomUUID()}`;
    await stubPushManager(page, endpoint, { stale: { endpoint: heldEndpoint, key: null } });
    const { route } = await createGeneralChannel(page, `Push hidden key ${serverSaysStale}`, true);
    await page.goto(route);
    await waitForAppReady(page);
    const userID = await page.evaluate(async () => {
      const response = await fetch("/api/me");
      return ((await response.json()) as { user: { id: string } }).user.id;
    });
    await rememberPushEnabled(page);
    // Push was on here before, so the worker is installed and holds the
    // subscription, and the server holds the device for this account.
    await page.evaluate(async () => {
      await navigator.serviceWorker.register("/service-worker.js", { scope: "/" });
      await navigator.serviceWorker.ready;
    });
    const registered = await page.evaluate(
      async ({ endpoint, userID, keys }) =>
        (
          await fetch("/api/me/push/subscriptions", {
            method: "PUT",
            headers: { "Content-Type": "application/json", "X-ClickClack-CSRF": "1" },
            body: JSON.stringify({ user_id: userID, endpoint, keys, user_agent: "held" }),
          })
        ).status,
      { endpoint: heldEndpoint, userID, keys: exampleSubscriptionKeys },
    );
    expect(registered).toBe(200);

    const heldDevice = createHash("sha256").update(heldEndpoint).digest("base64url");
    const reads: { device: string; thisDevice: boolean; stale: boolean }[] = [];
    await page.route(
      (url) => url.pathname === "/api/me/push",
      async (route) => {
        if (route.request().method() !== "GET") return route.continue();
        const device = new URL(route.request().url()).searchParams.get("device") ?? "";
        const response = await route.fetch();
        const state = (await response.json()) as {
          this_device: boolean;
          this_device_stale: boolean;
        };
        reads.push({ device, thisDevice: state.this_device, stale: state.this_device_stale });
        if (serverSaysStale && device === heldDevice && state.this_device) {
          state.this_device_stale = true;
        }
        await route.fulfill({ response, json: state });
      },
    );
    const removals: string[] = [];
    page.on("request", (request) => {
      if (request.url().includes("/api/me/push/subscriptions") && request.method() === "DELETE") {
        removals.push(request.postData() ?? "");
      }
    });
    const next = serverSaysStale ? endpoint : heldEndpoint;
    const stored = page.waitForResponse(
      (response) =>
        isPushRegistration(response.request()) &&
        (JSON.parse(response.request().postData() ?? "{}") as { endpoint?: string }).endpoint ===
          next &&
        response.status() === 200,
    );
    await page.reload();
    await waitForAppReady(page);
    await stored;
    await page.unroute((url) => url.pathname === "/api/me/push");

    // The heal named this browser's device when it read the state, and the
    // server, holding it under its own key, did not call it stale.
    expect(reads).toContainEqual({ device: heldDevice, thisDevice: true, stale: false });
    if (serverSaysStale) {
      await expect
        .poll(() => stubCalls(page))
        .toEqual([`unsubscribe:${heldEndpoint}`, `subscribe:${endpoint}`]);
      expect(removals).toHaveLength(1);
      expect(JSON.parse(removals[0])).toEqual({ user_id: userID, endpoint: heldEndpoint });
    } else {
      expect(await stubCalls(page)).toEqual([]);
      expect(removals).toEqual([]);
    }
    const state = await pushState(page);
    expect(state.subscriptions).toHaveLength(1);
  });
}

test("a renewed subscription registers only for the account that turned push on", async ({
  page,
  context,
}) => {
  const workerRequests: string[] = [];
  context.on("request", (request) => {
    if (request.serviceWorker() && request.url().includes("/api/me/push")) {
      workerRequests.push(`${request.method()} ${new URL(request.url()).pathname}`);
    }
  });
  const endpoint = `${relayOrigin}/push/${randomUUID()}`;
  await stubPushManager(page, endpoint);
  const { route } = await createGeneralChannel(page, "Push renewal", true);
  await page.goto(route);
  await waitForAppReady(page);
  await rememberPushEnabled(page);

  // The returning user's device registers on open, which also installs the
  // worker whose renewal message this test sends.
  const firstRegistration = page.waitForRequest(isPushRegistration);
  await page.reload();
  await waitForAppReady(page);
  await firstRegistration;
  const worker = context.serviceWorkers()[0] ?? (await context.waitForEvent("serviceworker"));

  // The browser drops the subscription and tells the worker. The worker only
  // asks the app; the app's heal registers the replacement for this user.
  await page.evaluate(() =>
    (window as unknown as { __pushStub: { drop: () => void } }).__pushStub.drop(),
  );
  const renewed = page.waitForRequest(
    (request) =>
      isPushRegistration(request) &&
      (JSON.parse(request.postData() ?? "{}") as { endpoint?: string }).endpoint ===
        `${endpoint}-2`,
  );
  await fireSubscriptionChange(worker);
  const renewal = await renewed;
  expect(renewal.serviceWorker()).toBeNull();
  expect(workerRequests).toEqual([]);
});

test("a renewal on a device where this account never turned push on registers nothing", async ({
  page,
  context,
}) => {
  const endpoint = `${relayOrigin}/push/${randomUUID()}`;
  await stubPushManager(page, endpoint);
  const { route } = await createGeneralChannel(page, "Push other account", true);
  await page.goto(route);
  await waitForAppReady(page);
  // Another account turned push on here earlier, so the worker is installed;
  // this account never did.
  await page.evaluate(async () => {
    await navigator.serviceWorker.register("/service-worker.js", { scope: "/" });
    await navigator.serviceWorker.ready;
  });
  const worker = context.serviceWorkers()[0] ?? (await context.waitForEvent("serviceworker"));
  const registrations: string[] = [];
  context.on("request", (request) => {
    if (request.url().includes("/api/me/push")) registrations.push(request.method());
  });
  await page.evaluate(() => {
    const seen: string[] = [];
    (window as unknown as { __workerMessages: string[] }).__workerMessages = seen;
    navigator.serviceWorker.addEventListener("message", (event) => {
      seen.push(String((event.data as { type?: string } | null)?.type));
    });
  });

  await fireSubscriptionChange(worker);
  await expect
    .poll(() =>
      page.evaluate(() => (window as unknown as { __workerMessages: string[] }).__workerMessages),
    )
    .toContain("clickclack:push-renew");
  // The heal returns before any request for an account without the opt-in;
  // this settle only gives a wrong implementation room to show itself.
  await page.waitForTimeout(500);
  expect(registrations).toEqual([]);
  expect(await stubCalls(page)).toEqual([]);
});

// turnPushOn flips the settings switch and waits for the device to be stored,
// then closes the dialog.
async function turnPushOn(page: Page) {
  const modal = await openNotificationSettings(page);
  const stored = page.waitForResponse(
    (response) =>
      response.url().includes("/api/me/push/subscriptions") &&
      response.request().method() === "PUT" &&
      response.status() === 200,
  );
  await modal.getByLabel("Push notifications on this device").check();
  await stored;
  await expect(modal.getByText("On for this device")).toBeVisible();
  await modal.getByRole("button", { name: "Close", exact: true }).click();
}

// expectPushSwitch opens the settings row fresh, the way a person checks
// whether this device will ring, and asserts what the switch shows once the
// row has read the server.
async function expectPushSwitch(page: Page, checked: boolean) {
  const stateRead = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === "/api/me/push" && response.request().method() === "GET",
  );
  const modal = await openNotificationSettings(page);
  const control = modal.getByLabel("Push notifications on this device");
  await stateRead;
  if (checked) {
    await expect(control).toBeChecked();
  } else {
    // Off is the row's starting state, so give a wrong reading room to land
    // after the server answered before asserting it never did.
    await page.waitForTimeout(500);
    await expect(control).not.toBeChecked();
  }
  await modal.getByRole("button", { name: "Close", exact: true }).click();
}

test("a device two accounts turned push on shows it on only for the account the server delivers to", async ({
  page,
  context,
}) => {
  // One browser, one push subscription, two accounts in two tabs.
  const endpoint = `${relayOrigin}/push/${randomUUID()}`;
  await stubPushManager(page, endpoint);
  const first = await createGeneralChannel(page, "Push shared first", true);
  await page.goto(first.route);
  await waitForAppReady(page);
  await turnPushOn(page);

  const second = await context.newPage();
  await stubPushManager(second, endpoint);
  const other = await createGeneralChannel(second, "Push shared second", true);
  await second.goto(other.route);
  await waitForAppReady(second);
  await turnPushOn(second);
  // The endpoint is unique, so the second account now holds the device.
  expect((await pushState(page)).subscriptions).toHaveLength(0);
  expect((await pushState(second)).subscriptions).toHaveLength(1);

  await expectPushSwitch(page, false);
  await expectPushSwitch(second, true);

  // Opening the app as the first account re-registers the device it turned
  // on here, so the device moves back and the second account's switch reads
  // off.
  const healed = page.waitForResponse(
    (response) => isPushRegistration(response.request()) && response.status() === 200,
  );
  await page.reload();
  await waitForAppReady(page);
  await healed;
  await expectPushSwitch(second, false);
  await expectPushSwitch(page, true);
});

test("an account with a device elsewhere reads off on a shared device another account holds", async ({
  page,
  browser,
  baseURL,
}) => {
  // Device one: account A turns push on with its own endpoint.
  const phoneEndpoint = `${relayOrigin}/push/${randomUUID()}`;
  await stubPushManager(page, phoneEndpoint);
  const own = await createGeneralChannel(page, "Push elsewhere", true);
  await page.goto(own.route);
  await waitForAppReady(page);
  await turnPushOn(page);
  const accountA = await page.evaluate(() =>
    fetch("/api/me").then(
      async (response) => ((await response.json()) as { user: { id: string } }).user.id,
    ),
  );

  // Device two, a shared laptop with one browser subscription: A turns push
  // on there too, then B does, and the server gives the laptop to B.
  const laptop = await browser.newContext({ baseURL });
  try {
    const laptopEndpoint = `${relayOrigin}/push/${randomUUID()}`;
    const laptopA = await laptop.newPage();
    await laptopA.setExtraHTTPHeaders({ "X-ClickClack-User": accountA });
    await stubPushManager(laptopA, laptopEndpoint);
    await laptopA.goto(own.route);
    await waitForAppReady(laptopA);
    await turnPushOn(laptopA);

    const laptopB = await laptop.newPage();
    await stubPushManager(laptopB, laptopEndpoint);
    const other = await createGeneralChannel(laptopB, "Push laptop owner", true);
    await laptopB.goto(other.route);
    await waitForAppReady(laptopB);
    await turnPushOn(laptopB);

    // A still has its phone, so it has a device; just not this one.
    expect((await pushState(laptopA)).subscriptions).toHaveLength(1);
    await expectPushSwitch(laptopA, false);
    await expectPushSwitch(laptopB, true);
    // The phone is still A's, and reads on there.
    await expectPushSwitch(page, true);
  } finally {
    await laptop.close();
  }
});

test("a notification tap lands at the conversation's newest message", async ({ page }) => {
  const { workspace, channel, route } = await createGeneralChannel(page, "Push tap", true);
  const elsewhere = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: "elsewhere", kind: "public" },
  });
  expect(elsewhere.ok()).toBe(true);
  const { channel: away } = (await elsewhere.json()) as { channel: { route_id: string } };
  await page.goto(`/app/${workspace.route_id}/${away.route_id}`);
  await waitForAppReady(page);

  const poster = await page.request.post(`/api/workspaces/${workspace.id}/bots`, {
    data: { display_name: `Tapper ${randomUUID().slice(0, 8)}` },
  });
  expect(poster.ok()).toBe(true);
  const { bot_token: botToken } = (await poster.json()) as { bot_token: { token: string } };
  let newest = "";
  for (let index = 1; index <= 60; index += 1) {
    const posted = await page.request.post(`/api/channels/${channel.id}/messages`, {
      headers: { Authorization: `Bearer ${botToken.token}` },
      data: { body: `backlog ${index}` },
    });
    expect(posted.ok()).toBe(true);
    newest = ((await posted.json()) as { message: { id: string } }).message.id;
  }

  // The worker's notificationclick handler posts this message to the app
  // window; dispatching it on the container reaches the same listener. The
  // URL is the one the server puts in the push: storage ids, which the app
  // canonicalizes to the channel's route.
  await page.evaluate((url) => {
    navigator.serviceWorker.dispatchEvent(
      new MessageEvent("message", { data: { type: "clickclack:notification-click", url } }),
    );
  }, `/app/${workspace.id}/${channel.id}`);

  await expect(page).toHaveURL(new RegExp(`${route}$`));
  await expect(page.locator(`[data-message-id="${newest}"]`)).toBeInViewport();
});

test("a notification tap with no app window open lands at the newest message", async ({
  page,
  context,
}) => {
  const { workspace, channel, route } = await createGeneralChannel(page, "Push cold tap", true);
  const elsewhere = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: "elsewhere", kind: "public" },
  });
  expect(elsewhere.ok()).toBe(true);
  const { channel: away } = (await elsewhere.json()) as { channel: { route_id: string } };
  await page.goto(`/app/${workspace.route_id}/${away.route_id}`);
  await waitForAppReady(page);
  const userID = await page.evaluate(() =>
    fetch("/api/me").then(
      async (response) => ((await response.json()) as { user: { id: string } }).user.id,
    ),
  );
  // The worker a device that turned push on has installed.
  await page.evaluate(async () => {
    await navigator.serviceWorker.register("/service-worker.js", { scope: "/" });
    await navigator.serviceWorker.ready;
  });
  const worker = context.serviceWorkers()[0] ?? (await context.waitForEvent("serviceworker"));

  const poster = await page.request.post(`/api/workspaces/${workspace.id}/bots`, {
    data: { display_name: `Cold tapper ${randomUUID().slice(0, 8)}` },
  });
  expect(poster.ok()).toBe(true);
  const { bot_token: botToken } = (await poster.json()) as { bot_token: { token: string } };
  let newest = "";
  for (let index = 1; index <= 60; index += 1) {
    const posted = await page.request.post(`/api/channels/${channel.id}/messages`, {
      headers: { Authorization: `Bearer ${botToken.token}` },
      data: { body: `unread ${index}` },
    });
    expect(posted.ok()).toBe(true);
    newest = ((await posted.json()) as { message: { id: string } }).message.id;
  }

  // The app is closed: nothing in this browser is an app window.
  await page.goto("about:blank");

  // Only a real tap lets a worker open a window, so the window this synthetic
  // tap asks for is recorded here and then opened the way the browser would.
  const opened = await worker.evaluate(async (url) => {
    const scope = self as unknown as {
      clients: { openWindow: (target: string) => Promise<null> };
      dispatchEvent: (event: Event) => boolean;
      ExtendableEvent: new (type: string) => Event;
    };
    let requested = "";
    scope.clients.openWindow = async (target) => {
      requested = target;
      return null;
    };
    const click = new scope.ExtendableEvent("notificationclick");
    Object.defineProperty(click, "notification", { value: { data: { url }, close() {} } });
    try {
      scope.dispatchEvent(click);
    } catch {
      // A synthetic event cannot extend its lifetime; the handler has already
      // started, which is all this needs.
    }
    for (let attempt = 0; attempt < 100 && !requested; attempt += 1) {
      await new Promise((resolve) => setTimeout(resolve, 20));
    }
    return requested;
  }, `/app/${workspace.id}/${channel.id}`);
  expect(opened).toBe(`/app/${workspace.id}/${channel.id}?from=push`);

  const fresh = await context.newPage();
  await fresh.setExtraHTTPHeaders({ "X-ClickClack-User": userID });
  await fresh.goto(opened);
  await expect(fresh).toHaveURL(new RegExp(`${route}$`));
  await expect(fresh.locator(`[data-message-id="${newest}"]`)).toBeInViewport();
});

test.describe("a device registered under a signed-in session", () => {
  const csrf = { "X-ClickClack-CSRF": "1" };
  // What the settings row says when the browser is signed in to someone else.
  const accountChangedStatus =
    "This browser is now signed in to a different account. Reload to continue.";

  // signInByMagicLink signs the page's browser context in as a new account.
  // The cookie it sets is shared by every tab in the context, the way one
  // browser shares it.
  async function signInByMagicLink(page: Page, label: string): Promise<string> {
    const magic = await page.request.post("/api/auth/magic/request", {
      headers: csrf,
      data: { email: `push-session-${randomUUID()}@example.com`, display_name: label },
    });
    expect(magic.status()).toBe(201);
    const login = await page.request.post("/api/auth/magic/consume", {
      headers: csrf,
      data: { token: ((await magic.json()) as { token: string }).token },
    });
    expect(login.status()).toBe(200);
    const me = await page.request.get("/api/me");
    expect(me.ok()).toBe(true);
    return ((await me.json()) as { user: { id: string } }).user.id;
  }

  // sessionRoom gives the signed-in account its own workspace and channel,
  // and a bot there whose messages notify the account.
  async function sessionRoom(page: Page, label: string) {
    const created = await page.request.post("/api/workspaces", {
      headers: csrf,
      data: { name: `${label} ${randomUUID().slice(0, 8)}` },
    });
    expect(created.status()).toBe(201);
    const { workspace } = (await created.json()) as { workspace: { id: string; route_id: string } };
    const channelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
      headers: csrf,
      data: { name: "general", kind: "public" },
    });
    expect(channelResponse.status()).toBe(201);
    const { channel } = (await channelResponse.json()) as {
      channel: { id: string; route_id: string };
    };
    const poster = await page.request.post(`/api/workspaces/${workspace.id}/bots`, {
      headers: csrf,
      data: { display_name: `Poster ${randomUUID().slice(0, 8)}` },
    });
    expect(poster.ok()).toBe(true);
    const { bot_token: botToken } = (await poster.json()) as { bot_token: { token: string } };
    const post = async (body: string) => {
      const posted = await page.request.post(`/api/channels/${channel.id}/messages`, {
        headers: { Authorization: `Bearer ${botToken.token}` },
        data: { body },
      });
      expect(posted.ok()).toBe(true);
    };
    return { route: `/app/${workspace.route_id}/${channel.route_id}`, post };
  }

  async function devicesOf(request: APIRequestContext): Promise<number> {
    const response = await request.get("/api/me/push");
    expect(response.ok()).toBe(true);
    return ((await response.json()) as { subscriptions: unknown[] }).subscriptions.length;
  }

  async function expectDeliveries(page: Page, count: number) {
    await expect
      .poll(async () => (await relayDeliveries(page)).length, { timeout: 15_000 })
      .toBe(count);
  }

  // expectNoDeliveryAfter posts and then gives a push that was going to leave
  // the time a real one takes, well inside the control's window.
  async function expectNoDeliveryAfter(page: Page, post: () => Promise<void>) {
    const before = (await relayDeliveries(page)).length;
    await post();
    await page.waitForTimeout(2_000);
    expect((await relayDeliveries(page)).length).toBe(before);
  }

  test("a service worker hides a previous account's private preview", async ({ page, context }) => {
    const accountA = await signInByMagicLink(page, "Preview owner");
    const room = await sessionRoom(page, "Preview owner");
    await page.goto(room.route);
    await waitForAppReady(page);
    await page.evaluate(async () => {
      await navigator.serviceWorker.register("/service-worker.js", { scope: "/" });
      await navigator.serviceWorker.ready;
    });
    const worker = context.serviceWorkers()[0] ?? (await context.waitForEvent("serviceworker"));
    const preview = () =>
      worker.evaluate(async (userID) => {
        const scope = self as unknown as {
          registration: {
            showNotification: (title: string, options: NotificationOptions) => Promise<void>;
          };
          dispatchEvent: (event: Event) => boolean;
        };
        let notice: { title: string; body?: string; url?: string } | undefined;
        const original = scope.registration.showNotification;
        scope.registration.showNotification = async (title, options) => {
          notice = { title, body: options.body, url: options.data?.url };
        };
        const payload = {
          user_id: userID,
          title: "Private sender",
          body: "Private account A message",
          url: "/app/private",
          tag: "private-message",
        };
        const event = new Event("push");
        let pending: Promise<unknown> = Promise.resolve();
        Object.defineProperty(event, "data", { value: { json: () => payload } });
        Object.defineProperty(event, "waitUntil", {
          value: (promise: Promise<unknown>) => {
            pending = promise;
          },
        });
        try {
          scope.dispatchEvent(event);
          await pending;
          return notice;
        } finally {
          scope.registration.showNotification = original;
        }
      }, accountA);
    expect(await preview()).toMatchObject({
      title: "Private sender",
      body: "Private account A message",
    });
    await signInByMagicLink(page, "Other account");
    const hidden = await preview();
    expect(hidden?.title).toBe("ClickClack");
    expect(hidden?.body).not.toContain("Private account A");
    expect(hidden?.url).toBe("/app");
  });

  test("first opt-in requests permission before account network checks", async ({ page }) => {
    await stubPushManager(page, relayOrigin + "/push/" + randomUUID());
    await signInByMagicLink(page, "Permission owner");
    const room = await sessionRoom(page, "Permission owner");
    await page.goto(room.route);
    await waitForAppReady(page);
    const modal = await openNotificationSettings(page);
    const control = modal.getByLabel("Push notifications on this device");
    await expect(control).toBeEnabled();
    await page.evaluate(() => {
      Object.defineProperty(Notification, "permission", { configurable: true, value: "default" });
      let networkStarted = false;
      const fetch = window.fetch.bind(window);
      window.fetch = (...args) => {
        networkStarted = true;
        return fetch(...args);
      };
      Notification.requestPermission = async () => {
        document.documentElement.dataset.permissionBeforeFetch = String(!networkStarted);
        return networkStarted ? "denied" : "granted";
      };
    });
    await control.check();
    await expect(page.locator("html")).toHaveAttribute("data-permission-before-fetch", "true");
    await expect(modal.getByText("On for this device")).toBeVisible();
  });

  test("stops receiving pushes when that session signs out", async ({ page }) => {
    const endpoint = `${relayOrigin}/push/${randomUUID()}`;
    await stubPushManager(page, endpoint);
    await signInByMagicLink(page, "Push session");
    const room = await sessionRoom(page, "Push session");

    await page.goto(room.route);
    await waitForAppReady(page);
    await turnPushOn(page);

    // Control: the session-bound device receives while the session lives.
    const before = (await relayDeliveries(page)).length;
    await room.post("while signed in");
    await expectDeliveries(page, before + 1);

    const loggedOut = await page.request.post("/api/auth/logout", { headers: csrf, data: {} });
    expect(loggedOut.status()).toBe(200);
    await expectNoDeliveryAfter(page, () => room.post("after signing out"));
  });

  // One browser, one cookie jar. Account A turns push on and its tab stays
  // open; a second tab signs the browser in as B, who never turned push on.
  // A renewal reaching A's tab, and A's tab opening again, must not register
  // the device for B.
  test("a tab left open for an account another tab replaced registers nothing for the new account", async ({
    page,
    context,
    playwright,
    baseURL,
  }) => {
    const endpoint = `${relayOrigin}/push/${randomUUID()}`;
    await stubPushManager(page, endpoint);
    const accountA = await signInByMagicLink(page, "Push stale first");
    const roomA = await sessionRoom(page, "Push stale first");
    await page.goto(roomA.route);
    await waitForAppReady(page);
    await turnPushOn(page);
    const worker = context.serviceWorkers()[0] ?? (await context.waitForEvent("serviceworker"));
    let delivered = (await relayDeliveries(page)).length;
    await roomA.post("to the first account, before the switch");
    await expectDeliveries(page, delivered + 1);
    // A's own session, kept aside so A's rows can be read after the jar moves.
    const asA = await playwright.request.newContext({
      baseURL,
      storageState: await context.storageState(),
    });
    try {
      const second = await context.newPage();
      const accountB = await signInByMagicLink(second, "Push stale second");
      const roomB = await sessionRoom(second, "Push stale second");
      expect(accountB).not.toBe(accountA);
      // A's tab still shows A, but its requests now carry B's cookie.
      const cookieOwner = await page.evaluate(() =>
        fetch("/api/me").then(
          async (response) => ((await response.json()) as { user: { id: string } }).user.id,
        ),
      );
      expect(cookieOwner).toBe(accountB);

      const registrations: number[] = [];
      context.on("response", (response) => {
        if (isPushRegistration(response.request())) registrations.push(response.status());
      });
      await page.evaluate(() => {
        const seen: string[] = [];
        (window as unknown as { __workerMessages: string[] }).__workerMessages = seen;
        navigator.serviceWorker.addEventListener("message", (event) => {
          seen.push(String((event.data as { type?: string } | null)?.type));
        });
      });

      // The renewal path: the browser replaces the subscription and the
      // worker tells A's tab.
      await page.evaluate(() =>
        (window as unknown as { __pushStub: { drop: () => void } }).__pushStub.drop(),
      );
      await fireSubscriptionChange(worker);
      await expect
        .poll(() =>
          page.evaluate(
            () => (window as unknown as { __workerMessages: string[] }).__workerMessages,
          ),
        )
        .toContain("clickclack:push-renew");
      // Give a wrong implementation room to register before asserting it did not.
      await page.waitForTimeout(1_000);
      expect(registrations.filter((status) => status < 300)).toEqual([]);
      expect(await stubCalls(page)).not.toContain(`subscribe:${endpoint}-2`);
      expect(await devicesOf(second.request)).toBe(0);
      await expectNoDeliveryAfter(page, () =>
        roomB.post("to the second account, which never turned push on"),
      );
      // A's device is still A's: its row, its session, its delivery.
      expect(await devicesOf(asA)).toBe(1);
      delivered = (await relayDeliveries(page)).length;
      await roomA.post("to the first account, after the switch");
      await expectDeliveries(page, delivered + 1);

      // The server holds the same line on its own: a registration naming A
      // from this browser, whose cookie is B's, writes nothing.
      const refused = await page.evaluate(
        async ({ userID, probe, keys }) => {
          const response = await fetch("/api/me/push/subscriptions", {
            method: "PUT",
            headers: { "Content-Type": "application/json", "X-ClickClack-CSRF": "1" },
            body: JSON.stringify({ user_id: userID, endpoint: probe, keys, user_agent: "probe" }),
          });
          return { status: response.status, body: await response.text() };
        },
        { userID: accountA, probe: `${endpoint}-probe`, keys: exampleSubscriptionKeys },
      );
      expect(refused.status).toBe(409);
      expect(refused.body).toContain("different account");
      expect(await devicesOf(second.request)).toBe(0);
      expect(await devicesOf(asA)).toBe(1);

      // The boot path: A's tab opens again under B's cookie. It becomes B's
      // tab, and B never turned push on here.
      await page.reload();
      await waitForAppReady(page);
      await page.waitForTimeout(1_000);
      expect(registrations.filter((status) => status < 300)).toEqual([]);
      expect(await devicesOf(second.request)).toBe(0);
      expect(await devicesOf(asA)).toBe(1);
    } finally {
      await asA.dispose();
    }
  });

  // The same switch landing mid-registration: A's tab opens, has checked A is
  // signed in and is part way through registering, and a second tab signs in
  // as B before the write. The server refuses the write, so the device stays
  // with A.
  test("a registration another tab's sign-in overtakes is refused and the device stays with its account", async ({
    page,
    context,
    playwright,
    baseURL,
  }) => {
    const endpoint = `${relayOrigin}/push/${randomUUID()}`;
    await stubPushManager(page, endpoint);
    const accountA = await signInByMagicLink(page, "Push raced first");
    const roomA = await sessionRoom(page, "Push raced first");
    await page.goto(roomA.route);
    await waitForAppReady(page);
    await turnPushOn(page);
    let delivered = (await relayDeliveries(page)).length;
    await roomA.post("to the first account, before the switch");
    await expectDeliveries(page, delivered + 1);
    const asA = await playwright.request.newContext({
      baseURL,
      storageState: await context.storageState(),
    });
    try {
      // Hold the heal's read of the push state, the last await before the
      // write, until the other tab has signed in.
      let reached!: () => void;
      const healReached = new Promise<void>((resolve) => (reached = resolve));
      let release!: () => void;
      const released = new Promise<void>((resolve) => (release = resolve));
      await page.route(
        (url) => url.pathname === "/api/me/push",
        async (route) => {
          if (route.request().method() === "GET") {
            reached();
            await released;
          }
          await route.continue();
        },
      );
      const registration = page.waitForResponse((response) =>
        isPushRegistration(response.request()),
      );
      await page.reload();
      await healReached;

      const second = await context.newPage();
      const accountB = await signInByMagicLink(second, "Push raced second");
      const roomB = await sessionRoom(second, "Push raced second");
      expect(accountB).not.toBe(accountA);
      release();

      const response = await registration;
      expect(response.status()).toBe(409);
      expect(
        (JSON.parse(response.request().postData() ?? "{}") as { user_id?: string }).user_id,
      ).toBe(accountA);
      await page.unroute((url) => url.pathname === "/api/me/push");
      expect(await devicesOf(second.request)).toBe(0);
      await expectNoDeliveryAfter(page, () =>
        roomB.post("to the second account, which never turned push on"),
      );
      expect(await devicesOf(asA)).toBe(1);
      delivered = (await relayDeliveries(page)).length;
      await roomA.post("to the first account, after the switch");
      await expectDeliveries(page, delivered + 1);
    } finally {
      await asA.dispose();
    }
  });

  // Opening the settings reads the signed-in account, so a row can only still
  // be A's if it was open before a second tab signed the browser in as B.
  // Switching it on there refuses before asking the browser for anything.
  test("the switch in a tab another tab's sign-in replaced turns nothing on and says why", async ({
    page,
    context,
  }) => {
    const endpoint = `${relayOrigin}/push/${randomUUID()}`;
    await stubPushManager(page, endpoint);
    const accountA = await signInByMagicLink(page, "Push switch on first");
    const roomA = await sessionRoom(page, "Push switch on first");
    await page.goto(roomA.route);
    await waitForAppReady(page);
    const modal = await openNotificationSettings(page);
    const control = modal.getByLabel("Push notifications on this device");
    await expect(control).toBeEnabled();
    await expect(control).not.toBeChecked();

    const second = await context.newPage();
    const accountB = await signInByMagicLink(second, "Push switch on second");
    expect(accountB).not.toBe(accountA);
    const registrations: number[] = [];
    context.on("response", (response) => {
      if (isPushRegistration(response.request())) registrations.push(response.status());
    });

    await control.click();
    await expect(modal.getByText(accountChangedStatus)).toBeVisible();
    await expect(control).toBeEnabled();
    await expect(control).not.toBeChecked();
    expect(registrations).toEqual([]);
    expect(await stubCalls(page)).toEqual([]);
    expect(await devicesOf(second.request)).toBe(0);
  });

  // The switch off in a tab whose row was open before another tab signed in
  // as B. B's cookie would remove nothing of A's, so turning off refuses
  // outright: the browser keeps its subscription and A keeps its device.
  test("turning push off in a tab another tab's sign-in replaced removes nothing and says why", async ({
    page,
    context,
    playwright,
    baseURL,
  }) => {
    const endpoint = `${relayOrigin}/push/${randomUUID()}`;
    await stubPushManager(page, endpoint);
    const accountA = await signInByMagicLink(page, "Push switch off first");
    const roomA = await sessionRoom(page, "Push switch off first");
    await page.goto(roomA.route);
    await waitForAppReady(page);
    await turnPushOn(page);
    const asA = await playwright.request.newContext({
      baseURL,
      storageState: await context.storageState(),
    });
    try {
      // A's row opens while A is signed in, so it reads on, and stays open.
      const modal = await openNotificationSettings(page);
      const control = modal.getByLabel("Push notifications on this device");
      await expect(control).toBeChecked();

      const second = await context.newPage();
      const accountB = await signInByMagicLink(second, "Push switch off second");
      expect(accountB).not.toBe(accountA);
      const removals: number[] = [];
      context.on("response", (response) => {
        if (
          response.url().includes("/api/me/push/subscriptions") &&
          response.request().method() === "DELETE"
        ) {
          removals.push(response.status());
        }
      });

      await control.click();
      await expect(modal.getByText(accountChangedStatus)).toBeVisible();
      await expect(control).toBeEnabled();
      await expect(control).toBeChecked();
      expect(await stubCalls(page)).not.toContain(`unsubscribe:${endpoint}`);
      expect(removals).toEqual([]);
      expect(await devicesOf(asA)).toBe(1);
      expect(await devicesOf(second.request)).toBe(0);
    } finally {
      await asA.dispose();
    }
  });

  // Opening the settings reads the signed-in account, so a row can only open
  // for A under B's cookie when B signs in after that read and before the row
  // reads the push state. B has turned push on here by then, so the state says
  // this browser's subscription is one of the signed-in account's devices,
  // and none of it is A's.
  test("a row that reads the push state after another tab's sign-in shows why instead of on", async ({
    page,
    context,
  }) => {
    const endpoint = `${relayOrigin}/push/${randomUUID()}`;
    await stubPushManager(page, endpoint);
    const accountA = await signInByMagicLink(page, "Push row read first");
    const roomA = await sessionRoom(page, "Push row read first");
    await page.goto(roomA.route);
    await waitForAppReady(page);
    await turnPushOn(page);

    // Hold the row's read of the push state until the other tab is done.
    let reached!: () => void;
    const rowReached = new Promise<void>((resolve) => (reached = resolve));
    let release!: () => void;
    const released = new Promise<void>((resolve) => (release = resolve));
    await page.route(
      (url) => url.pathname === "/api/me/push",
      async (route) => {
        if (route.request().method() === "GET") {
          reached();
          await released;
        }
        await route.continue();
      },
    );
    const modal = await openNotificationSettings(page);
    await rowReached;

    const second = await context.newPage();
    await stubPushManager(second, endpoint);
    const accountB = await signInByMagicLink(second, "Push row read second");
    const roomB = await sessionRoom(second, "Push row read second");
    expect(accountB).not.toBe(accountA);
    await second.goto(roomB.route);
    await waitForAppReady(second);
    await turnPushOn(second);
    expect(await devicesOf(second.request)).toBe(1);
    release();

    const control = modal.getByLabel("Push notifications on this device");
    await expect(modal.getByText(accountChangedStatus)).toBeVisible();
    await expect(control).toBeEnabled();
    await expect(control).not.toBeChecked();
    await expect(modal.getByText(/^On for /)).toHaveCount(0);
    // The row is still A's: the tab never took B's account.
    await expect(
      page.getByRole("button", { name: /Account settings for Push row read first/ }),
    ).toBeAttached();
    await page.unroute((url) => url.pathname === "/api/me/push");
    expect(await stubCalls(page)).toEqual([`subscribe:${endpoint}`]);
    expect(await devicesOf(second.request)).toBe(1);
  });

  // On a shared browser the account that does not hold the device reads off
  // and cannot turn it off. Turning it on moves the device to that account,
  // and turning it off then removes it, so the first account's pushes stop
  // arriving there.
  test("the other account on a shared browser stops the first account's pushes by turning on then off", async ({
    page,
    context,
    playwright,
    baseURL,
  }) => {
    const endpoint = `${relayOrigin}/push/${randomUUID()}`;
    await stubPushManager(page, endpoint);
    const accountA = await signInByMagicLink(page, "Push handover first");
    const roomA = await sessionRoom(page, "Push handover first");
    await page.goto(roomA.route);
    await waitForAppReady(page);
    await turnPushOn(page);
    const asA = await playwright.request.newContext({
      baseURL,
      storageState: await context.storageState(),
    });
    try {
      const second = await context.newPage();
      await stubPushManager(second, endpoint);
      const accountB = await signInByMagicLink(second, "Push handover second");
      const roomB = await sessionRoom(second, "Push handover second");
      expect(accountB).not.toBe(accountA);
      await second.goto(roomB.route);
      await waitForAppReady(second);
      await turnPushOn(second);
      expect(await devicesOf(asA)).toBe(0);
      expect(await devicesOf(second.request)).toBe(1);

      const modal = await openNotificationSettings(second);
      const control = modal.getByLabel("Push notifications on this device");
      await expect(control).toBeChecked();
      const removed = second.waitForResponse(
        (response) =>
          response.url().includes("/api/me/push/subscriptions") &&
          response.request().method() === "DELETE" &&
          response.status() === 204,
      );
      await control.uncheck();
      await removed;
      await expect(modal.getByText("Off for this device")).toBeVisible();

      expect(await stubCalls(second)).toContain(`unsubscribe:${endpoint}`);
      expect(await devicesOf(asA)).toBe(0);
      expect(await devicesOf(second.request)).toBe(0);
      await expectNoDeliveryAfter(page, () =>
        roomA.post("to the first account, after the handover"),
      );
    } finally {
      await asA.dispose();
    }
  });
});

// fireSubscriptionChange raises the event a browser raises when it replaces a
// subscription. A synthetic event cannot extend its lifetime, so the worker's
// waitUntil throws after the handler has already started its work; that is
// the browser's rule, not the handler failing.
async function fireSubscriptionChange(worker: Worker) {
  await worker.evaluate(() => {
    const scope = self as unknown as {
      dispatchEvent: (event: Event) => boolean;
      ExtendableEvent: new (type: string) => Event;
    };
    scope.dispatchEvent(new scope.ExtendableEvent("pushsubscriptionchange"));
  });
}
