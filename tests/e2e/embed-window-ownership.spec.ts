import { expect, test, type WebSocketRoute } from "@playwright/test";
import { randomUUID } from "node:crypto";
import type { Message, RealtimeEvent } from "../../apps/web/src/lib/types";
import { deferred } from "./thread-fixture";

test("embedded history remains reachable when an older page arrives after authoritative resync", async ({
  page,
}, testInfo) => {
  test.setTimeout(90_000);
  const created = await page.request.post("/api/workspaces", {
    data: { name: `Embed window ownership ${randomUUID().slice(0, 8)}` },
  });
  expect(created.ok()).toBe(true);
  const { workspace } = await created.json();
  const channelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: "resync-history" },
  });
  expect(channelResponse.ok()).toBe(true);
  const { channel } = await channelResponse.json();
  const messagePath = `/api/channels/${channel.id}/messages`;
  const messages: Message[] = [];
  const publish = async (seq: number) => {
    const response = await page.request.post(messagePath, {
      data: { body: `History row ${seq}` },
    });
    expect(response.status()).toBe(201);
    const { message }: { message: Message } = await response.json();
    expect(message.channel_seq).toBe(seq);
    messages.push(message);
    return message;
  };
  for (let seq = 1; seq <= 120; seq++) await publish(seq);

  let socket: WebSocketRoute | undefined, serverSocket: WebSocketRoute | undefined;
  let connections = 0,
    mutedCreates = 0,
    muted = false;
  await page.routeWebSocket("**/api/realtime/ws?*", (client) => {
    socket = client;
    const server = client.connectToServer();
    serverSocket = server;
    connections++;
    server.onMessage((raw) => {
      const event = JSON.parse(String(raw)) as RealtimeEvent;
      if (muted && event.type === "message.created" && event.channel_id === channel.id)
        mutedCreates++;
      else client.send(raw);
    });
  });
  const cursor = () =>
    page.evaluate((id) => localStorage.getItem(`clickclack:${id}:cursor`), workspace.id);
  await page.goto(`/embed/channel/${workspace.route_id}/${channel.route_id}`);
  await expect(page.locator(`[data-message-id="${messages[119].id}"]`)).toBeInViewport();
  await expect.poll(cursor).not.toBeNull();

  const entered = deferred(),
    release = deferred(),
    delivered = deferred();
  let held = false;
  await page.route(`**${messagePath}?*`, async (route) => {
    if (held || !new URL(route.request().url()).searchParams.has("before_seq"))
      return route.continue();
    held = true;
    const response = await route.fetch();
    entered.resolve();
    await release.promise;
    await route.fulfill({ response });
    delivered.resolve();
  });
  try {
    const viewport = page.locator(".messages-scroll");
    await viewport.hover();
    await page.mouse.wheel(0, -10000);
    await entered.promise;
    muted = true;
    for (let seq = 121; seq <= 240; seq++) await publish(seq);
    await expect.poll(() => mutedCreates).toBe(120);
    const tailResponse = await page.request.get(
      `/api/realtime/events?workspace_id=${workspace.id}&include_tail=true&limit=1`,
    );
    const { tail_cursor } = await tailResponse.json();
    const upstream = serverSocket!;
    await socket!.close({ code: 4001, reason: "Held history page resync regression" });
    await upstream.close();
    muted = false;
    await expect.poll(() => connections).toBe(2);
    await expect.poll(cursor).toBe(tail_cursor);
    await expect(page.locator(`[data-message-id="${messages[239].id}"]`)).toBeAttached();

    // The old page must not replace the new window's history cursor or edge flags.
    release.resolve();
    await delivered.promise;
    await page.evaluate(
      () =>
        new Promise<void>((resolve) =>
          requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
        ),
    );
    const live = await publish(241);
    await expect(page.locator(`[data-message-id="${live.id}"]`)).toBeAttached();
    await viewport.hover();
    await page.mouse.wheel(0, 10000);
    await expect(page.locator(`[data-message-id="${live.id}"]`)).toBeInViewport();
    await page.mouse.wheel(0, -10000);
    try {
      await expect(
        page.locator(`[data-message-id="${messages[119].id}"]`),
        "Paging after resync must recover the intervening persisted history",
      ).toBeAttached();
    } finally {
      await page.screenshot({ path: testInfo.outputPath("after-resync-history.png") });
    }
    expect(connections, "Another resync must not mask the obsolete page result").toBe(2);
  } finally {
    release.resolve();
  }
});
