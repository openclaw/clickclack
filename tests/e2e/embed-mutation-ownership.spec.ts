import { expect, test, type Locator, type Page, type WebSocketRoute } from "@playwright/test";
import { randomUUID } from "node:crypto";
import type { Message, MessagePage, RealtimeEvent } from "../../apps/web/src/lib/types";
import { settleScrollFrames } from "./message-frames";
import { deferred } from "./thread-fixture";

async function channelFixture(page: Page) {
  const created = await page.request.post("/api/workspaces", {
    data: { name: `Embed mutation ${randomUUID().slice(0, 8)}` },
  });
  expect(created.status()).toBe(201);
  const { workspace } = await created.json();
  const channelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: "send-mutation" },
  });
  expect(channelResponse.status()).toBe(201);
  const { channel } = await channelResponse.json();
  const messagePath = `/api/channels/${channel.id}/messages`;
  const seeded = await page.request.post(messagePath, { data: { body: "Existing message" } });
  expect(seeded.status()).toBe(201);
  const { message: seed }: { message: Message } = await seeded.json();
  return {
    workspace,
    channel,
    messagePath,
    seed,
    url: `/embed/channel/${workspace.route_id}/${channel.route_id}`,
    cursor: () =>
      page.evaluate((id) => localStorage.getItem(`clickclack:${id}:cursor`), workspace.id),
  };
}

async function editMessage(row: Locator, body: string) {
  await row.hover();
  await row.getByRole("button", { name: "More actions" }).click();
  await row.getByRole("menuitem", { name: "Edit message" }).click();
  await row.getByLabel("Edit message").fill(body);
  await row.getByRole("button", { name: "Save" }).click();
  await expect(row.locator(".markdown")).toHaveText(body);
}

for (const mutation of ["edit", "delete"] as const) {
  test(`embedded send recovery preserves an acknowledged ${mutation}`, async ({
    page,
  }, testInfo) => {
    const { channel, messagePath, seed, url, cursor } = await channelFixture(page);
    let heldCreate: { socket: WebSocketRoute; raw: string | Buffer; event: RealtimeEvent };
    let mutationCursor = "";
    await page.routeWebSocket("**/api/realtime/ws?*", (socket) => {
      socket.connectToServer().onMessage((raw) => {
        const event = JSON.parse(String(raw)) as RealtimeEvent;
        if (event.channel_id === channel.id) {
          if (event.type === "message.created") {
            heldCreate = { socket, raw, event };
            return;
          }
          if (event.type === (mutation === "edit" ? "message.updated" : "message.deleted")) {
            mutationCursor = event.cursor || "";
          }
        }
        socket.send(raw);
      });
    });
    await page.goto(url);
    await expect(page.locator(`[data-message-id="${seed.id}"]`)).toBeVisible();
    await expect.poll(cursor).not.toBeNull();
    const snapshotEntered = deferred(),
      releaseSnapshot = deferred();
    let pageReads = 0;
    let snapshot: MessagePage;
    await page.route(`**${messagePath}?*`, async (route) => {
      if (!new URL(route.request().url()).searchParams.has("after_seq")) return route.continue();
      if (++pageReads !== 1) return route.continue();
      const response = await route.fetch();
      expect(response.status()).toBe(200);
      snapshot = await response.json();
      snapshotEntered.resolve();
      await releaseSnapshot.promise;
      await route.fulfill({ response });
    });
    try {
      const originalBody = "Own message before delayed recovery";
      const editedBody = "Newer edit after the recovery snapshot";
      const receiptPromise = page.waitForResponse(
        (response) =>
          new URL(response.url()).pathname === messagePath &&
          response.request().method() === "POST",
      );
      const composer = page.getByLabel("Message body");
      await composer.fill(originalBody);
      await page.getByRole("button", { name: "Send", exact: true }).click();
      const receipt = await receiptPromise;
      expect(receipt.status()).toBe(201);
      const { message: own }: { message: Message } = await receipt.json();
      const row = page.locator(`[data-message-id="${own.id}"]`);
      await expect(row).toContainText(originalBody);
      await expect(composer).toHaveValue("");
      await snapshotEntered.promise;
      expect(snapshot!.messages.find((message) => message.id === own.id)?.body).toBe(originalBody);
      await expect.poll(() => heldCreate?.event.payload.message_id).toBe(own.id);
      // Complete the independent realtime fetch before editing, leaving only the send read held.
      heldCreate!.socket.send(heldCreate!.raw);
      await expect.poll(cursor).toBe(heldCreate!.event.cursor);
      expect(pageReads).toBe(2);

      if (mutation === "edit") {
        await editMessage(row, editedBody);
      } else {
        const deleted = await page.request.delete(`/api/messages/${own.id}`);
        expect(deleted.status()).toBe(200);
        await expect(row.locator(".message-deleted")).toHaveText("This message was deleted.");
      }
      await expect.poll(() => mutationCursor).not.toBe("");
      await expect.poll(cursor).toBe(mutationCursor);
      await page.screenshot({ path: testInfo.outputPath("before-delayed-page.png") });
      const recovery = page.waitForResponse(
        (response) =>
          new URL(response.url()).pathname === messagePath &&
          new URL(response.url()).searchParams.has("after_seq"),
      );
      releaseSnapshot.resolve();
      await (await recovery).finished();
      await settleScrollFrames(page);
      await page.screenshot({ path: testInfo.outputPath("after-delayed-page.png") });
      if (mutation === "edit") await expect(row.locator(".markdown")).toHaveText(editedBody);
      else await expect(row.locator(".message-deleted")).toHaveText("This message was deleted.");
      await expect.poll(cursor).toBe(mutationCursor);
    } finally {
      releaseSnapshot.resolve();
      await page.unrouteAll({ behavior: "wait" });
    }
  });
}

test("embedded snapshot preserves an edit acknowledged while channel metadata is held", async ({
  page,
}, testInfo) => {
  const { workspace, channel, messagePath, seed, url, cursor } = await channelFixture(page);
  let clientSocket: WebSocketRoute | undefined, serverSocket: WebSocketRoute | undefined;
  const heldUpdates: { socket: WebSocketRoute; raw: string | Buffer }[] = [];
  let barrierCursor = "";
  await page.routeWebSocket("**/api/realtime/ws?*", (socket) => {
    clientSocket = socket;
    const server = socket.connectToServer();
    serverSocket = server;
    server.onMessage((raw) => {
      const event = JSON.parse(String(raw)) as RealtimeEvent;
      if (event.channel_id === channel.id) {
        if (event.type === "message.updated" && event.payload.message_id === seed.id) {
          heldUpdates.push({ socket, raw });
          return;
        }
        if (event.type === "message.created") barrierCursor = event.cursor || "";
      }
      socket.send(raw);
    });
  });
  await page.goto(url);
  const row = page.locator(`[data-message-id="${seed.id}"]`);
  await expect(row).toContainText(seed.body);
  await expect.poll(cursor).not.toBeNull();
  const metadataEntered = deferred(),
    releaseMetadata = deferred();
  let metadataReads = 0;
  await page.route(`**/api/workspaces/${workspace.id}/channels`, async (route) => {
    if (++metadataReads !== 1) return route.continue();
    const response = await route.fetch();
    expect(response.status()).toBe(200);
    metadataEntered.resolve();
    await releaseMetadata.promise;
    await route.fulfill({ response });
  });
  try {
    const snapshotReady = page.waitForResponse(
      (response) =>
        new URL(response.url()).pathname === messagePath &&
        !new URL(response.url()).searchParams.has("after_seq"),
    );
    const upstream = serverSocket!;
    await clientSocket!.close({ code: 4001, reason: "Snapshot metadata ownership regression" });
    await upstream.close();
    await metadataEntered.promise;
    await (await snapshotReady).finished();
    await settleScrollFrames(page);
    // This create is queued behind the resync; its cursor proves snapshot application finished.
    const barrier = await page.request.post(messagePath, {
      data: { body: "After snapshot barrier" },
    });
    expect(barrier.status()).toBe(201);
    await expect.poll(() => barrierCursor).not.toBe("");
    const editedBody = "Edit acknowledged while metadata waits";
    await editMessage(row, editedBody);
    await expect.poll(() => heldUpdates.length).toBe(1);
    await page.screenshot({ path: testInfo.outputPath("before-metadata-release.png") });
    releaseMetadata.resolve();
    await expect.poll(cursor).toBe(barrierCursor);
    await settleScrollFrames(page);
    await page.screenshot({ path: testInfo.outputPath("after-metadata-release.png") });
    await expect(row.locator(".markdown")).toHaveText(editedBody);
  } finally {
    releaseMetadata.resolve();
    for (const held of heldUpdates) held.socket.send(held.raw);
    await page.unrouteAll({ behavior: "wait" });
  }
});

test("embedded send receipt preserves an edit already applied through realtime", async ({
  page,
}, testInfo) => {
  const { channel, messagePath, seed, url, cursor } = await channelFixture(page);
  let createdCursor = "",
    editedCursor = "";
  await page.routeWebSocket("**/api/realtime/ws?*", (socket) => {
    socket.connectToServer().onMessage((raw) => {
      const event = JSON.parse(String(raw)) as RealtimeEvent;
      if (event.channel_id === channel.id) {
        if (event.type === "message.created") createdCursor = event.cursor || "";
        if (event.type === "message.updated") editedCursor = event.cursor || "";
      }
      socket.send(raw);
    });
  });
  await page.goto(url);
  await expect(page.locator(`[data-message-id="${seed.id}"]`)).toBeVisible();
  await expect.poll(cursor).not.toBeNull();
  const receiptEntered = deferred(),
    releaseReceipt = deferred();
  let own: Message;
  await page.route(`**${messagePath}`, async (route) => {
    if (route.request().method() !== "POST") return route.continue();
    const response = await route.fetch();
    expect(response.status()).toBe(201);
    own = (await response.json()).message;
    receiptEntered.resolve();
    await releaseReceipt.promise;
    await route.fulfill({ response });
  });
  try {
    const composer = page.getByLabel("Message body");
    const originalBody = "Message with a delayed POST receipt";
    const editedBody = "Edit applied before the POST receipt";
    await composer.fill(originalBody);
    await page.getByRole("button", { name: "Send", exact: true }).click();
    await receiptEntered.promise;
    await expect.poll(() => createdCursor).not.toBe("");
    await expect.poll(cursor).toBe(createdCursor);
    const row = page.locator(`[data-message-id="${own!.id}"]`);
    await expect(row.locator(".markdown")).toHaveText(originalBody);
    await expect(composer).toBeDisabled();
    await editMessage(row, editedBody);
    await expect.poll(() => editedCursor).not.toBe("");
    await expect.poll(cursor).toBe(editedCursor);
    await page.screenshot({ path: testInfo.outputPath("before-post-receipt.png") });
    const receiptReady = page.waitForResponse(
      (response) =>
        new URL(response.url()).pathname === messagePath && response.request().method() === "POST",
    );
    releaseReceipt.resolve();
    await (await receiptReady).finished();
    await settleScrollFrames(page);
    await page.screenshot({ path: testInfo.outputPath("after-post-receipt.png") });
    await expect(row.locator(".markdown")).toHaveText(editedBody);
    await expect(composer).toHaveValue("");
    await expect(composer).toBeEnabled();
  } finally {
    releaseReceipt.resolve();
    await page.unrouteAll({ behavior: "wait" });
  }
});
