import { expect, test } from "@playwright/test";
import { randomUUID } from "node:crypto";
import type { Message, RealtimeEvent } from "../../apps/web/src/lib/types";
import { waitForAppReady } from "./app-ready";
import { deferred } from "./thread-fixture";

for (const surface of ["timeline", "channel embed", "thread embed"] as const) {
  test(`${surface}: an older edit acknowledgement preserves a newer realtime edit`, async ({
    page,
    playwright,
    baseURL,
  }, testInfo) => {
    const createdWorkspace = await page.request.post("/api/workspaces", {
      data: { name: `Edit acknowledgement ${randomUUID().slice(0, 8)}` },
    });
    expect(createdWorkspace.ok()).toBe(true);
    const { workspace } = await createdWorkspace.json();
    const createdChannel = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
      data: { name: "edit-acknowledgement" },
    });
    expect(createdChannel.ok()).toBe(true);
    const { channel } = await createdChannel.json();
    const createdMessage = await page.request.post(`/api/channels/${channel.id}/messages`, {
      data: { body: "Message before either edit" },
    });
    expect(createdMessage.ok()).toBe(true);
    const { message }: { message: Message } = await createdMessage.json();
    let url = `/app/${workspace.route_id}/${channel.route_id}`;
    if (surface === "channel embed") {
      url = `/embed/channel/${workspace.route_id}/${channel.route_id}`;
    } else if (surface === "thread embed") {
      const routed = await page.request.post(`/api/messages/${message.id}/route`);
      expect(routed.ok()).toBe(true);
      url = `/embed/thread/${workspace.route_id}/${(await routed.json()).message.route_id}`;
    }
    await page.goto(url);
    if (surface === "timeline") await waitForAppReady(page);
    const row = page.locator(
      `${surface === "thread embed" ? ".thread-root" : ".message-row"}[data-message-id="${message.id}"]`,
    );
    await expect(row.locator(".markdown")).toHaveText(message.body);
    const cursor = () =>
      page.evaluate((id) => localStorage.getItem(`clickclack:${id}:cursor`) || "", workspace.id);
    await expect.poll(cursor).not.toBe("");
    const otherSession = await playwright.request.newContext({
      baseURL,
      storageState: await page.context().storageState(),
    });
    const localBody = "Earlier edit saved in this view";
    const remoteBody = "Newer edit saved in another session";
    const ackEntered = deferred(),
      releaseAck = deferred(),
      refreshEntered = deferred(),
      releaseRefresh = deferred();
    let localCursor = "",
      localEditedAt = "",
      holdRefresh = false;
    await page.route(`**/api/messages/${message.id}`, async (route) => {
      if (route.request().method() === "PATCH") {
        const response = await route.fetch();
        expect(response.ok()).toBe(true);
        const saved: { message: Message; event: RealtimeEvent } = await response.json();
        expect(saved.message.body).toBe(localBody);
        localCursor = saved.event.cursor || "";
        localEditedAt = saved.message.edited_at || "";
        expect(localCursor).not.toBe("");
        ackEntered.resolve();
        await releaseAck.promise;
        await route.fulfill({ response });
      } else if (route.request().method() === "GET" && holdRefresh) {
        const response = await route.fetch();
        expect(response.ok()).toBe(true);
        expect((await response.json()).message.body).toBe(remoteBody);
        refreshEntered.resolve();
        await releaseRefresh.promise;
        await route.fulfill({ response });
      } else {
        await route.continue();
      }
    });
    try {
      if (surface === "thread embed") {
        await row.getByRole("button", { name: "Edit message", exact: true }).click();
      } else {
        await row.hover();
        await row.getByRole("button", { name: "More actions" }).click();
        await row.getByRole("menuitem", { name: "Edit message" }).click();
      }
      const editor = row.getByRole("textbox", { name: "Edit message", exact: true });
      await editor.fill(localBody);
      await row.getByRole("button", { name: "Save", exact: true }).click();
      await ackEntered.promise;
      // Checkpoint A's event first, leaving only its HTTP acknowledgement outstanding.
      await expect.poll(async () => (await cursor()) >= localCursor).toBe(true);
      holdRefresh = true;
      const edited = await otherSession.patch(`/api/messages/${message.id}`, {
        data: { body: remoteBody },
      });
      expect(edited.ok()).toBe(true);
      const newer: { message: Message; event: RealtimeEvent } = await edited.json();
      expect(newer.event.cursor).toBeTruthy();
      await testInfo.attach("server-edit-revisions", {
        body: JSON.stringify({ earlier: localEditedAt, newer: newer.message.edited_at }, null, 2),
        contentType: "application/json",
      });
      await refreshEntered.promise;
      releaseAck.resolve();
      await expect(editor).toHaveCount(0);
      releaseRefresh.resolve();
      await expect.poll(async () => (await cursor()) >= newer.event.cursor!).toBe(true);
      await page.screenshot({ path: testInfo.outputPath("after-ack-and-refresh.png") });
      await expect(row.locator(".markdown")).toHaveText(remoteBody);
    } finally {
      releaseAck.resolve();
      releaseRefresh.resolve();
      await otherSession.dispose();
    }
  });
}
