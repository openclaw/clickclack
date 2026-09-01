import { expect, test, type Page, type WebSocketRoute } from "@playwright/test";
import { randomUUID } from "node:crypto";
import type { Message, RealtimeEvent, User } from "../../apps/web/src/lib/types";
import { settleScrollFrames } from "./message-frames";
import { deferred } from "./thread-fixture";

const csrf = { "X-ClickClack-CSRF": "1" };

// These requests contain real session credentials, including the captured old cookie.
test.use({ trace: "off" });

async function signIn(page: Page, email: string): Promise<{ user: User; token: string }> {
  const magic = await page.request.post("/api/auth/magic/request", {
    headers: csrf,
    data: { email, display_name: "Session test reader" },
  });
  expect(magic.status()).toBe(201);
  const login = await page.request.post("/api/auth/magic/consume", {
    headers: csrf,
    data: { token: (await magic.json()).token },
  });
  expect(login.status()).toBe(200);
  return login.json();
}

async function fixture(page: Page) {
  const created = await page.request.post("/api/workspaces", {
    headers: csrf,
    data: { name: `Embed send session ${randomUUID().slice(0, 8)}` },
  });
  expect(created.status()).toBe(201);
  const { workspace } = await created.json();
  const channelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    headers: csrf,
    data: { name: "send-session" },
  });
  expect(channelResponse.status()).toBe(201);
  const { channel } = await channelResponse.json();
  const path = `/api/channels/${channel.id}/messages`;
  const seeded = await page.request.post(path, {
    headers: csrf,
    data: { body: "Existing session message" },
  });
  expect(seeded.status()).toBe(201);
  const { message: seed }: { message: Message } = await seeded.json();
  return {
    workspace,
    channel,
    path,
    seed,
    url: `/embed/channel/${workspace.route_id}/${channel.route_id}`,
    cursor: () =>
      page.evaluate((id) => localStorage.getItem(`clickclack:${id}:cursor`), workspace.id),
  };
}

test("a revoked send cannot interrupt a new send after the same account signs in again", async ({
  page,
  request,
}, testInfo) => {
  test.setTimeout(90_000);
  const email = `embed-reauth-${randomUUID()}@example.com`;
  const signed = await signIn(page, email);
  const { path, seed, url, cursor } = await fixture(page);
  await page.goto(url);
  await expect(page.getByLabel("Embedded channel")).toBeVisible();
  await expect.poll(cursor).not.toBeNull();
  const oldEntered = deferred(),
    releaseOld = deferred(),
    oldDelivered = deferred();
  const getEntered = deferred(),
    releaseGet = deferred();
  const freshEntered = deferred(),
    releaseFresh = deferred(),
    freshDelivered = deferred();
  let posts = 0,
    oldStatus = 0,
    getStatus = 0,
    freshStatus = 0;
  let freshMessage: Message;
  await page.route(`**${path}`, async (route) => {
    if (route.request().method() !== "POST") return route.continue();
    const headers = await route.request().allHeaders();
    if (++posts === 1) {
      oldEntered.resolve();
      await releaseOld.promise;
      const response = await route.fetch({ headers });
      oldStatus = response.status();
      await route.fulfill({ response });
      oldDelivered.resolve();
    } else {
      const response = await route.fetch({ headers });
      freshStatus = response.status();
      freshMessage = (await response.json()).message;
      freshEntered.resolve();
      await releaseFresh.promise;
      await route.fulfill({ response });
      freshDelivered.resolve();
    }
  });
  await page.route(`**/api/messages/${seed.id}`, async (route) => {
    const headers = await route.request().allHeaders();
    getEntered.resolve();
    await releaseGet.promise;
    const response = await route.fetch({ headers });
    getStatus = response.status();
    await route.fulfill({ response });
  });
  try {
    const composer = page.getByLabel("Message body");
    const previousDraft = "Draft from the previous session";
    await composer.fill(previousDraft);
    await page.getByRole("button", { name: "Send", exact: true }).click();
    await oldEntered.promise;
    const edited = await page.request.patch(`/api/messages/${seed.id}`, {
      headers: csrf,
      data: { body: "Existing message changed" },
    });
    expect(edited.status()).toBe(200);
    await getEntered.promise;
    const revoked = await request.post("/api/auth/logout", {
      headers: { Authorization: `Bearer ${signed.token}` },
      data: {},
    });
    expect(revoked.status()).toBe(200);
    releaseGet.resolve();
    await expect(page.getByRole("region", { name: "Sign in", exact: true })).toBeVisible();
    expect(getStatus).toBe(401);
    const replacement = await signIn(page, email);
    expect(replacement.user.id).toBe(signed.user.id);
    await page.evaluate(() => window.dispatchEvent(new Event("focus")));
    await expect(page.getByLabel("Embedded channel")).toBeVisible();
    await expect(composer).toHaveValue(previousDraft);
    await expect(composer).toBeEnabled();

    const freshDraft = "Send from the renewed session";
    await composer.fill(freshDraft);
    await page.getByRole("button", { name: "Send", exact: true }).click();
    await freshEntered.promise;
    expect(freshStatus).toBe(201);
    await expect(composer).toBeDisabled();
    releaseOld.resolve();
    await oldDelivered.promise;
    expect(oldStatus).toBe(401);
    await settleScrollFrames(page);
    await expect(page.getByLabel("Embedded channel")).toBeVisible();
    await expect(composer).toHaveValue(freshDraft);
    await expect(composer).toBeDisabled();
    expect((await page.request.get("/api/me")).status()).toBe(200);
    releaseFresh.resolve();
    await freshDelivered.promise;
    await expect(composer).toHaveValue("");
    await expect(composer).toBeEnabled();
    await expect(page.locator(`[data-message-id="${freshMessage!.id}"]`)).toBeVisible();
    await page.screenshot({ path: testInfo.outputPath("after-renewed-send.png") });
  } finally {
    releaseOld.resolve();
    releaseGet.resolve();
    releaseFresh.resolve();
    await page.unrouteAll({ behavior: "wait" });
  }
});

test("reauthenticated embedded history can page after an old request finishes", async ({
  page,
  request,
}, testInfo) => {
  const email = `embed-history-reauth-${randomUUID()}@example.com`;
  const signed = await signIn(page, email);
  const { path, seed, url, cursor } = await fixture(page);
  let latest = seed;
  for (let n = 2; n <= 101; n++) {
    const response = await page.request.post(path, {
      headers: csrf,
      data: { body: `Session history message ${n}` },
    });
    expect(response.status()).toBe(201);
    latest = (await response.json()).message;
  }
  const olderEntered = deferred(),
    releaseOlder = deferred(),
    olderDelivered = deferred();
  const getEntered = deferred(),
    releaseGet = deferred();
  let olderRequests = 0,
    olderStatus = 0,
    getStatus = 0;
  await page.route(`**${path}?*`, async (route) => {
    if (!new URL(route.request().url()).searchParams.has("before_seq")) return route.continue();
    if (++olderRequests !== 1) return route.continue();
    const response = await route.fetch();
    olderStatus = response.status();
    olderEntered.resolve();
    await releaseOlder.promise;
    await route.fulfill({ response });
    olderDelivered.resolve();
  });
  await page.route(`**/api/messages/${latest.id}`, async (route) => {
    const headers = await route.request().allHeaders();
    getEntered.resolve();
    await releaseGet.promise;
    const response = await route.fetch({ headers });
    getStatus = response.status();
    await route.fulfill({ response });
  });
  try {
    await page.goto(url);
    await expect(page.locator(`[data-message-id="${latest.id}"]`)).toBeVisible();
    await expect.poll(cursor).not.toBeNull();
    await page.locator(".messages-scroll").evaluate((node) => (node.scrollTop = 0));
    await olderEntered.promise;
    expect(olderStatus).toBe(200);
    const edited = await page.request.patch(`/api/messages/${latest.id}`, {
      headers: csrf,
      data: { body: "Existing history message changed" },
    });
    expect(edited.status()).toBe(200);
    await getEntered.promise;
    const revoked = await request.post("/api/auth/logout", {
      headers: { Authorization: `Bearer ${signed.token}` },
      data: {},
    });
    expect(revoked.status()).toBe(200);
    releaseGet.resolve();
    await expect(page.getByRole("region", { name: "Sign in", exact: true })).toBeVisible();
    expect(getStatus).toBe(401);
    const replacement = await signIn(page, email);
    expect(replacement.user.id).toBe(signed.user.id);
    await page.evaluate(() => window.dispatchEvent(new Event("focus")));
    await expect(page.getByLabel("Embedded channel")).toBeVisible();
    await expect(page.locator(`[data-message-id="${latest.id}"]`)).toBeVisible();
    releaseOlder.resolve();
    await olderDelivered.promise;
    await settleScrollFrames(page);
    await page.locator(".messages-scroll").evaluate((node) => (node.scrollTop = 0));
    await expect(page.locator(`[data-message-id="${seed.id}"]`)).toBeAttached();
    expect(olderRequests).toBe(2);
    await page.screenshot({ path: testInfo.outputPath("after-session-history.png") });
  } finally {
    releaseOlder.resolve();
    releaseGet.resolve();
    await page.unrouteAll({ behavior: "wait" });
  }
});

test("authoritative window resync preserves completion of a valid embedded send", async ({
  page,
}) => {
  const { workspace, channel, path, url, cursor } = await fixture(page);
  let socket: WebSocketRoute | undefined, serverSocket: WebSocketRoute | undefined;
  let connections = 0,
    heldCreates = 0,
    holding = true;
  await page.routeWebSocket("**/api/realtime/ws?*", (client) => {
    socket = client;
    const server = client.connectToServer();
    serverSocket = server;
    connections++;
    server.onMessage((raw) => {
      const event = JSON.parse(String(raw)) as RealtimeEvent;
      if (holding && event.type === "message.created" && event.channel_id === channel.id)
        heldCreates++;
      else client.send(raw);
    });
  });
  await page.goto(url);
  await expect(page.getByLabel("Embedded channel")).toBeVisible();
  await expect.poll(cursor).not.toBeNull();
  const entered = deferred(),
    release = deferred();
  let sendStatus = 0;
  let sent: Message;
  await page.route(`**${path}`, async (route) => {
    if (route.request().method() !== "POST") return route.continue();
    const response = await route.fetch();
    sendStatus = response.status();
    sent = (await response.json()).message;
    entered.resolve();
    await release.promise;
    await route.fulfill({ response });
  });
  try {
    const composer = page.getByLabel("Message body");
    const body = "Valid send across a window resync";
    await composer.fill(body);
    await page.getByRole("button", { name: "Send", exact: true }).click();
    await entered.promise;
    expect(sendStatus).toBe(201);
    await expect.poll(() => heldCreates).toBe(1);
    const tail = await page.request.get(
      `/api/realtime/events?workspace_id=${workspace.id}&include_tail=true&limit=1`,
    );
    const { tail_cursor } = await tail.json();
    const upstream = serverSocket!;
    await socket!.close({ code: 4001, reason: "In-flight send ownership regression" });
    await upstream.close();
    holding = false;
    await expect.poll(() => connections).toBe(2);
    await expect.poll(cursor).toBe(tail_cursor);
    await expect(page.locator(`[data-message-id="${sent!.id}"]`)).toBeVisible();
    await expect(composer).toHaveValue(body);
    await expect(composer).toBeDisabled();
    release.resolve();
    await expect(composer).toHaveValue("");
    await expect(composer).toBeEnabled();
    await expect(page.locator(`[data-message-id="${sent!.id}"]`)).toHaveCount(1);
  } finally {
    release.resolve();
    await page.unrouteAll({ behavior: "wait" });
  }
});
