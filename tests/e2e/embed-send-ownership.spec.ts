import { expect, test, type WebSocketRoute } from "@playwright/test";
import { randomUUID } from "node:crypto";
import type { Message, RealtimeEvent } from "../../apps/web/src/lib/types";

for (const scenario of ["receipt", "older page", "empty channel"] as const) {
  test(`embedded ${scenario} cannot skip an earlier remote message after a confirmed send`, async ({
    page,
  }, testInfo) => {
    test.setTimeout(90_000);
    const workspaceResponse = await page.request.post("/api/workspaces", {
      data: { name: `Send ownership ${randomUUID().slice(0, 8)}` },
    });
    expect(workspaceResponse.ok()).toBe(true);
    const { workspace } = await workspaceResponse.json();
    const channelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
      data: { name: "receipt-history" },
    });
    expect(channelResponse.ok()).toBe(true);
    const { channel } = await channelResponse.json();
    const messagePath = `/api/channels/${channel.id}/messages`;
    const post = async (body: string): Promise<Message> => {
      const response = await page.request.post(messagePath, { data: { body } });
      expect(response.status()).toBe(201);
      return (await response.json()).message;
    };
    const initial: Message[] = [];
    const initialCount = scenario === "older page" ? 101 : scenario === "empty channel" ? 0 : 1;
    for (let n = 1; n <= initialCount; n++) {
      initial.push(await post(`Existing message ${n}`));
    }
    const held: { socket: WebSocketRoute; raw: string | Buffer; event: RealtimeEvent }[] = [];
    let holding = true;
    let connections = 0;
    await page.routeWebSocket("**/api/realtime/ws?*", (socket) => {
      connections++;
      socket.connectToServer().onMessage((raw) => {
        const event = JSON.parse(String(raw)) as RealtimeEvent;
        if (holding && event.type === "message.created" && event.channel_id === channel.id) {
          held.push({ socket, raw, event });
        } else socket.send(raw);
      });
    });
    await page.goto(`/embed/channel/${workspace.route_id}/${channel.route_id}`);
    await expect(page.getByLabel("Embedded channel")).toBeVisible();
    if (initial.length)
      await expect(page.locator(`[data-message-id="${initial.at(-1)!.id}"]`)).toBeVisible();
    else await expect(page.locator(".empty")).toBeVisible();
    await expect.poll(() => connections).toBe(1);
    const cursor = () =>
      page.evaluate((id) => localStorage.getItem(`clickclack:${id}:cursor`), workspace.id);
    await expect.poll(cursor).not.toBeNull();

    let failRefresh = scenario === "older page";
    await page.route(`**${messagePath}?*`, (route) => {
      if (failRefresh && new URL(route.request().url()).searchParams.has("after_seq"))
        return route.fulfill({ status: 503, json: { error: "Message refresh unavailable" } });
      return route.continue();
    });
    const remote = await post("Remote message before our receipt");
    await expect
      .poll(() => held.some((item) => item.event.payload.message_id === remote.id))
      .toBe(true);
    const ownResponse = page.waitForResponse(
      (response) =>
        new URL(response.url()).pathname === messagePath && response.request().method() === "POST",
    );
    const composer = page.getByLabel("Message body");
    await composer.fill("Confirmed own message");
    await page.getByRole("button", { name: "Send", exact: true }).click();
    const receipt = await ownResponse;
    expect(receipt.status()).toBe(201);
    const own: Message = (await receipt.json()).message;
    await expect(page.locator(`[data-message-id="${own.id}"]`)).toBeVisible();
    await expect(composer).toHaveValue("");
    await expect(composer).toBeEnabled();
    await expect
      .poll(() => held.some((item) => item.event.payload.message_id === own.id))
      .toBe(true);

    if (scenario === "older page") {
      await expect(page.getByRole("status")).toContainText("Message refresh unavailable");
      // An unrelated older page must not treat the displayed receipt as fetched history.
      const older = page.waitForResponse(
        (response) =>
          new URL(response.url()).pathname === messagePath &&
          new URL(response.url()).searchParams.has("before_seq"),
      );
      await page.locator(".messages-scroll").evaluate((node) => (node.scrollTop = 0));
      await (await older).finished();
      await expect(page.locator(`[data-message-id="${initial[0].id}"]`)).toBeAttached();
    }
    failRefresh = false;
    holding = false;
    const ownEvent = held.find((item) => item.event.payload.message_id === own.id)!.event;
    for (const item of held.splice(0)) item.socket.send(item.raw);
    await expect.poll(cursor).toBe(ownEvent.cursor);
    await page.locator(".messages-scroll").evaluate((node) => (node.scrollTop = node.scrollHeight));
    await expect(page.locator(`[data-message-id="${own.id}"]`)).toBeVisible();
    await page.screenshot({ path: testInfo.outputPath("after-realtime-receipt.png") });
    await expect(page.locator(`[data-message-id="${remote.id}"]`)).toBeAttached();
    await expect(page.locator(`[data-message-id="${own.id}"]`)).toHaveCount(1);
    expect(connections, "Recovery must use the original durable event stream").toBe(1);
    const persisted = await page.request.get(`${messagePath}?mode=latest&limit=2`);
    expect((await persisted.json()).messages.map((message: Message) => message.id)).toEqual([
      remote.id,
      own.id,
    ]);
    if (scenario === "older page") {
      await expect(page.getByRole("status")).toHaveCount(0);
      const repeatedResponse = page.waitForResponse(
        (response) =>
          new URL(response.url()).pathname === messagePath &&
          response.request().method() === "POST",
      );
      await composer.fill("Confirmed own message");
      await page.getByRole("button", { name: "Send", exact: true }).click();
      const repeated = await repeatedResponse;
      expect(repeated.status()).toBe(201);
      expect(repeated.request().postDataJSON().nonce).not.toBe(
        receipt.request().postDataJSON().nonce,
      );
    }
  });
}
