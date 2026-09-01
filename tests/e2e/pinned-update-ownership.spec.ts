import { expect, test } from "@playwright/test";
import { randomUUID } from "node:crypto";
import type { Message, RealtimeEvent } from "../../apps/web/src/lib/types";
import { waitForAppReady } from "./app-ready";
import { deferred } from "./thread-fixture";

for (const mutation of ["edit", "deletion"] as const) {
  test(`a delayed pinned list preserves an acknowledged message ${mutation}`, async ({
    page,
  }, testInfo) => {
    const createdWorkspace = await page.request.post("/api/workspaces", {
      data: { name: `Pinned update ${randomUUID().slice(0, 8)}` },
    });
    expect(createdWorkspace.ok()).toBe(true);
    const { workspace } = await createdWorkspace.json();
    const createdChannel = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
      data: { name: "pinned-update" },
    });
    expect(createdChannel.ok()).toBe(true);
    const { channel } = await createdChannel.json();
    const createdMessage = await page.request.post(`/api/channels/${channel.id}/messages`, {
      data: { body: "Pinned text before the edit" },
    });
    expect(createdMessage.ok()).toBe(true);
    const { message }: { message: Message } = await createdMessage.json();
    const pinned = await page.request.post(`/api/channels/${channel.id}/pins`, {
      data: { message_id: message.id },
    });
    expect(pinned.ok()).toBe(true);
    const heldUpdates: (() => void)[] = [];
    let releaseUpdates = false;
    await page.routeWebSocket("**/api/realtime/ws?*", (socket) => {
      socket.connectToServer().onMessage((raw) => {
        const event = JSON.parse(String(raw)) as RealtimeEvent;
        // The HTTP acknowledgement can arrive before its realtime notification.
        if (
          !releaseUpdates &&
          (mutation === "edit"
            ? event.type === "message.updated"
            : event.type === "message.deleted" || event.type === "pin.removed") &&
          event.payload.message_id === message.id
        ) {
          heldUpdates.push(() => socket.send(raw));
          return;
        }
        socket.send(raw);
      });
    });
    await page.goto(`/app/${workspace.route_id}/${channel.route_id}`);
    await waitForAppReady(page);
    const row = page.locator(`.message-row[data-message-id="${message.id}"]`);
    await expect(row.locator(".markdown")).toHaveText(message.body);
    const pinsEntered = deferred(),
      releasePins = deferred();
    let holdPins = true;
    await page.route(`**/api/channels/${channel.id}/pins?*`, async (route) => {
      if (!holdPins) return route.continue();
      const response = await route.fetch();
      expect(response.ok()).toBe(true);
      const snapshot: { messages: Message[] } = await response.json();
      expect(snapshot.messages.find((pin) => pin.id === message.id)?.body).toBe(message.body);
      pinsEntered.resolve();
      await releasePins.promise;
      await route.fulfill({ response });
    });
    try {
      await page.getByRole("button", { name: "Pinned items" }).click();
      const panel = page.getByRole("complementary", { name: "Pinned messages pane" });
      await pinsEntered.promise;
      const editedBody = "Newer edit stays in the pinned card";
      await row.hover();
      await row.getByRole("button", { name: "More actions" }).click();
      if (mutation === "edit") {
        await row.getByRole("menuitem", { name: "Edit message" }).click();
        await row.getByRole("textbox", { name: "Edit message", exact: true }).fill(editedBody);
        await row.getByRole("button", { name: "Save", exact: true }).click();
        await expect(row.locator(".markdown")).toHaveText(editedBody);
      } else {
        await row.getByRole("menuitem", { name: "Delete message" }).click();
        const dialog = page.getByRole("dialog", { name: "Delete message" });
        await dialog.getByRole("button", { name: "Delete", exact: true }).click();
        await expect(dialog).toBeHidden();
        await expect(row.locator(".message-deleted")).toHaveText("This message was deleted.");
      }
      await expect.poll(() => heldUpdates.length).toBeGreaterThan(0);
      holdPins = false;
      releasePins.resolve();
      await expect(panel.getByText("Loading...", { exact: true })).toHaveCount(0);
      await page.screenshot({ path: testInfo.outputPath("after-pinned-snapshot.png") });
      if (mutation === "edit") {
        await expect(panel.locator(`[data-message-id="${message.id}"] .markdown`)).toHaveText(
          editedBody,
        );
      } else {
        await expect(panel.locator(`[data-message-id="${message.id}"]`)).toHaveCount(0);
        await expect(panel.getByText("No pinned messages", { exact: true })).toBeVisible();
      }
    } finally {
      holdPins = false;
      releasePins.resolve();
      releaseUpdates = true;
      for (const release of heldUpdates) release();
    }
  });
}
