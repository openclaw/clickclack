import { expect, test, type Page } from "@playwright/test";
import { randomUUID } from "node:crypto";
import type { Channel, User, Workspace } from "../../apps/web/src/lib/types";
import { waitForAppReady } from "./app-ready";
import { deferred } from "./thread-fixture";

async function fixture(page: Page) {
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
  await page.goto(`/app/${workspace.route_id}/${active.route_id}`);
  await waitForAppReady(page);
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
  } finally {
    release.resolve();
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
