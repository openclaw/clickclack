import { expect, test, type Page } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { once } from "node:events";
import { ClickClackBot, ClickClackClient } from "../../packages/sdk-ts/src";
import type { Channel, User, Workspace } from "../../apps/web/src/lib/types";
import { waitForAppReady } from "./app-ready";
import { deferred } from "./thread-fixture";

for (const action of ["remote close", "stop", "restart"] as const) {
  test(`SDK bot only reports its current connection closing: ${action}`, async ({ baseURL }) => {
    const client = new ClickClackClient({ baseUrl: baseURL! });
    const workspace = await client.workspaces.create({ name: `SDK lifecycle ${randomUUID()}` });
    const { bot, bot_token } = await client.bots.create(workspace.id, {
      display_name: "Lifecycle bot",
      scopes: ["bot:write"],
    });
    expect(Boolean(bot_token?.token)).toBe(true);
    let closes = 0;
    const runner = new ClickClackBot({
      baseUrl: baseURL!,
      workspaceId: workspace.id,
      token: bot_token!.token,
      onEvent() {},
      onClose() {
        closes++;
      },
    });
    try {
      const first = runner.start();
      expect(runner.start() === first).toBe(true);
      await once(first, "open");
      const firstClosed = once(first, "close");
      if (action === "remote close") {
        await client.bots.removeMembership(workspace.id, bot.id);
        await firstClosed;
        expect(closes).toBe(1);
      } else {
        runner.stop();
        if (action === "restart") {
          const replacement = runner.start();
          await Promise.all([firstClosed, once(replacement, "open")]);
          expect(closes).toBe(0);
          expect(runner.start() === replacement).toBe(true);
          const replacementClosed = once(replacement, "close");
          await client.bots.removeMembership(workspace.id, bot.id);
          await replacementClosed;
          expect(closes).toBe(1);
        } else {
          await firstClosed;
          expect(closes).toBe(0);
        }
      }
    } finally {
      runner.stop();
      await client.bots.delete(bot.id);
      await client.workspaces.delete(workspace.id);
    }
  });
}

async function fixture(page: Page, open = true) {
  const created = await page.request.post("/api/workspaces", {
    data: { name: `Realtime snapshots ${randomUUID()}` },
  });
  expect(created.ok()).toBe(true);
  const { workspace }: { workspace: Workspace } = await created.json();
  const channels: Channel[] = [];
  for (const name of ["active", "background"]) {
    const response = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
      data: { name },
    });
    expect(response.ok()).toBe(true);
    channels.push((await response.json()).channel);
  }
  const response = await page.request.post(`/api/workspaces/${workspace.id}/bots`, {
    data: { display_name: "Snapshot sender" },
  });
  expect(response.ok()).toBe(true);
  const { bot }: { bot: User } = await response.json();
  const [active, background] = channels;
  if (open) {
    await page.goto(`/app/${workspace.route_id}/${active.route_id}`);
    await waitForAppReady(page);
  }
  return { workspace, active, background, bot };
}

async function holdChannelSnapshot(page: Page, workspace: Workspace, channel: Channel) {
  const entered = deferred(),
    release = deferred();
  await page.route(`**/api/workspaces/${workspace.id}/channels`, async (route) => {
    entered.resolve();
    await release.promise;
    await route.continue();
  });
  const response = await page.request.patch(`/api/channels/${channel.id}`, {
    data: { display_title: channel.name },
  });
  expect(response.ok()).toBe(true);
  await entered.promise;
  return release;
}

async function installNotifications(page: Page) {
  const me = await page.request.get("/api/me");
  const { user } = await me.json();
  await page.addInitScript((id: string) => {
    Reflect.set(window, "snapshotNotifications", []);
    class TestNotification {
      static permission = "granted";
      constructor(title: string) {
        Reflect.get(window, "snapshotNotifications").push(title);
      }
      close() {}
    }
    Reflect.set(window, "Notification", TestNotification);
    localStorage.setItem(`clickclack:browser-notifications-enabled:v1:${id}`, "enabled");
  }, user.id);
}

test("a channel snapshot cannot consume a live timeline event", async ({ page }) => {
  const { workspace, active, bot } = await fixture(page);
  const release = await holdChannelSnapshot(page, workspace, active);
  try {
    const response = await page.request.post(`/api/channels/${active.id}/messages`, {
      headers: { "X-ClickClack-User": bot.id },
      data: { body: "Live message after the snapshot request" },
    });
    expect(response.ok()).toBe(true);
    const { message } = await response.json();
    release.resolve();
    await expect(page.locator(`.message-row[data-message-id="${message.id}"]`)).toBeVisible();
    await expect
      .poll(async () => {
        const response = await page.request.get(`/api/workspaces/${workspace.id}/channels`);
        const { channels }: { channels: Channel[] } = await response.json();
        return channels.find((channel) => channel.id === active.id)?.last_read_seq;
      })
      .toBe(message.channel_seq);
    await expect(page.getByRole("separator", { name: "New messages" })).toHaveCount(0);
    await expect(page.getByRole("button", { name: /Jump to .*new message/ })).toHaveCount(0);
  } finally {
    release.resolve();
  }
});

test("replaying a loaded message while scrolled up does not invent a newer page", async ({
  page,
}) => {
  const { workspace, active } = await fixture(page, false);
  for (let index = 0; index < 30; index++) {
    const response = await page.request.post(`/api/channels/${active.id}/messages`, {
      data: { body: `History ${index}: ${"Scrollable retained history. ".repeat(12)}` },
    });
    expect(response.ok()).toBe(true);
  }
  const tail = await page.request.get(
    `/api/realtime/events?workspace_id=${workspace.id}&include_tail=true&limit=1`,
  );
  const { tail_cursor } = await tail.json();
  const response = await page.request.post(`/api/channels/${active.id}/messages`, {
    data: { body: "Already loaded final row" },
  });
  expect(response.ok()).toBe(true);
  const { message } = await response.json();
  const cursorKey = `clickclack:${workspace.id}:cursor`;
  await page.addInitScript(({ key, cursor }) => localStorage.setItem(key, cursor), {
    key: cursorKey,
    cursor: tail_cursor,
  });
  const entered = deferred(),
    release = deferred(),
    releaseNewer = deferred();
  await page.routeWebSocket("**/api/realtime/ws?*", (socket) => {
    const server = socket.connectToServer();
    let delivery = Promise.resolve();
    server.onMessage((data) => {
      delivery = delivery.then(async () => {
        const event = JSON.parse(String(data));
        if (event.type === "message.created" && event.payload.message_id === message.id) {
          entered.resolve();
          await release.promise;
        }
        socket.send(data);
      });
    });
  });
  try {
    await page.goto(`/app/${workspace.route_id}/${active.route_id}`);
    await waitForAppReady(page);
    await expect(page.getByText("Already loaded final row", { exact: true })).toBeVisible();
    await entered.promise;
    const scroll = page.locator(".messages-scroll");
    await scroll.evaluate((element) => {
      element.scrollTop = 0;
      element.dispatchEvent(new Event("scroll", { bubbles: true }));
    });
    await expect(page.getByText(/^History 0:/)).toBeVisible();
    release.resolve();
    await expect
      .poll(() => page.evaluate((key) => localStorage.getItem(key), cursorKey))
      .not.toBe(tail_cursor);
    let newerRequests = 0;
    await page.route(`**/api/channels/${active.id}/messages?*`, async (route) => {
      if (!new URL(route.request().url()).searchParams.has("after_seq")) return route.continue();
      newerRequests++;
      await releaseNewer.promise;
      await route.continue();
    });
    await scroll.evaluate(async (element) => {
      element.scrollTop = element.scrollHeight;
      element.dispatchEvent(new Event("scroll", { bubbles: true }));
      await new Promise(requestAnimationFrame);
      await new Promise(requestAnimationFrame);
    });
    await expect(page.getByRole("status", { name: "Loading newer messages" })).toHaveCount(0);
    expect(newerRequests).toBe(0);
  } finally {
    release.resolve();
    releaseNewer.resolve();
  }
});

test("a background snapshot preserves the live alert without doubling unread", async ({ page }) => {
  await installNotifications(page);
  const { workspace, background, bot } = await fixture(page);
  const release = await holdChannelSnapshot(page, workspace, background);
  try {
    const response = await page.request.post(`/api/channels/${background.id}/messages`, {
      headers: { "X-ClickClack-User": bot.id },
      data: { body: "Notify after the snapshot request" },
    });
    expect(response.ok()).toBe(true);
    release.resolve();
    await expect
      .poll(() => page.evaluate(() => Reflect.get(window, "snapshotNotifications").length))
      .toBe(1);
    await expect(
      page.getByRole("link", { name: "# background" }).getByLabel("1 unread"),
    ).toBeVisible();
  } finally {
    release.resolve();
  }
});

test("cold snapshots suppress old alerts and failed event replay does not repeat a new alert", async ({
  page,
}) => {
  await installNotifications(page);
  let connections = 0;
  await page.routeWebSocket("**/api/realtime/ws?*", (socket) => {
    socket.connectToServer();
    connections++;
  });
  const { workspace, active, background, bot } = await fixture(page);
  const cursorKey = `clickclack:${workspace.id}:cursor`;
  const previousCursor = await page.evaluate((key) => localStorage.getItem(key), cursorKey);
  expect(previousCursor).toBeTruthy();
  const history = await page.request.post(`/api/channels/${background.id}/messages`, {
    headers: { "X-ClickClack-User": bot.id },
    data: { body: "Already visible in the startup snapshot" },
  });
  expect(history.ok()).toBe(true);
  await expect
    .poll(() => page.evaluate(() => Reflect.get(window, "snapshotNotifications").length))
    .toBe(1);
  await page.evaluate(({ key, cursor }) => localStorage.setItem(key, cursor!), {
    key: cursorKey,
    cursor: previousCursor,
  });
  await page.reload();
  await waitForAppReady(page);
  const barrier = await page.request.post(`/api/channels/${active.id}/messages`, {
    data: { body: "Processed after startup replay" },
  });
  expect(barrier.ok()).toBe(true);
  await expect(page.getByText("Processed after startup replay", { exact: true })).toBeVisible();
  expect(await page.evaluate(() => Reflect.get(window, "snapshotNotifications").length)).toBe(0);

  const created = await page.request.post("/api/dms", {
    data: { workspace_id: workspace.id, member_ids: [bot.id] },
  });
  expect(created.ok()).toBe(true);
  const { conversation } = await created.json();
  let lookups = 0;
  const failed = deferred(),
    release = deferred();
  await page.route(`**/api/dms?workspace_id=${workspace.id}`, async (route) => {
    if (++lookups > 1) return route.continue();
    failed.resolve();
    await release.promise;
    await route.fulfill({ status: 503, json: { error: "Transient conversation lookup failure" } });
  });
  try {
    const response = await page.request.post(`/api/dms/${conversation.id}/messages`, {
      headers: { "X-ClickClack-User": bot.id },
      data: { body: "Deliver this alert once across recovery" },
    });
    expect(response.ok()).toBe(true);
    await failed.promise;
    await expect
      .poll(() => page.evaluate(() => Reflect.get(window, "snapshotNotifications").length))
      .toBe(1);
    const previousConnections = connections;
    const beforeFailureCursor = await page.evaluate((key) => localStorage.getItem(key), cursorKey);
    release.resolve();
    await expect.poll(() => connections).toBeGreaterThan(previousConnections);
    await expect(
      page.getByRole("link", { name: /Snapshot sender/ }).getByLabel("1 unread"),
    ).toBeVisible();
    await expect
      .poll(() => page.evaluate((key) => localStorage.getItem(key), cursorKey))
      .not.toBe(beforeFailureCursor);
    expect(await page.evaluate(() => Reflect.get(window, "snapshotNotifications").length)).toBe(1);
  } finally {
    release.resolve();
  }
});
