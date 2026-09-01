import { expect, test } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { waitForAppReady } from "./app-ready";
import { settleScrollFrames } from "./message-frames";
import { deferred, openThread } from "./thread-fixture";

for (const surface of ["timeline", "thread"] as const) {
  test(`Copy link from the ${surface} preserves a pending message edit`, async ({ page }) => {
    await page.addInitScript(() => {
      Object.defineProperty(navigator, "clipboard", {
        configurable: true,
        value: {
          writeText: async () => {
            throw new Error("Clipboard denied");
          },
        },
      });
    });
    const workspaceResponse = await page.request.post("/api/workspaces", {
      data: { name: `Route ownership ${randomUUID().slice(0, 8)}` },
    });
    expect(workspaceResponse.ok()).toBe(true);
    const { workspace } = await workspaceResponse.json();
    const channelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
      data: { name: "route-ownership" },
    });
    expect(channelResponse.ok()).toBe(true);
    const { channel } = await channelResponse.json();
    const originalBody = "Message before the route request";
    const editedBody = "Newer edit survives copying its link";
    const created = await page.request.post(`/api/channels/${channel.id}/messages`, {
      data: { body: originalBody },
    });
    expect(created.ok()).toBe(true);
    const { message } = await created.json();
    if (surface === "thread") {
      const reply = await page.request.post(`/api/messages/${message.id}/thread/replies`, {
        data: { body: "One existing reply" },
      });
      expect(reply.ok()).toBe(true);
    }
    await page.goto(`/app/${workspace.route_id}/${channel.route_id}`);
    await waitForAppReady(page);
    const row = page.locator(`.message-row[data-message-id="${message.id}"]`);
    await expect(row.locator(".markdown")).toHaveText(originalBody);
    if (surface === "thread") {
      await openThread(page, message.id);
      await expect(page.locator(".thread-root .markdown")).toHaveText(originalBody);
      await expect(page.locator(".messages")).not.toHaveClass(/is-revealing/);
      await settleScrollFrames(page);
    }
    const routeEntered = deferred(),
      routeRelease = deferred(),
      refreshEntered = deferred(),
      refreshRelease = deferred();
    let routeID = "";
    await page.route(`**/api/messages/${message.id}/route`, async (route) => {
      const response = await route.fetch();
      expect(response.ok()).toBe(true);
      const snapshot = (await response.json()).message;
      expect(snapshot.body).toBe(originalBody);
      routeID = snapshot.route_id;
      expect(routeID).toBeTruthy();
      routeEntered.resolve();
      await routeRelease.promise;
      await route.fulfill({ response });
    });
    await page.route(`**/api/messages/${message.id}`, async (route) => {
      if (route.request().method() !== "GET") return route.continue();
      const response = await route.fetch();
      expect(response.ok()).toBe(true);
      expect((await response.json()).message.body).toBe(editedBody);
      refreshEntered.resolve();
      await refreshRelease.promise;
      await route.fulfill({ response });
    });
    try {
      if (surface === "timeline") {
        await row.hover();
        await row.getByRole("button", { name: "More actions" }).click();
        await row.getByRole("menuitem", { name: "Copy link" }).click();
      } else {
        await page.locator(".thread-root").getByRole("button", { name: "Copy link" }).click();
      }
      await routeEntered.promise;
      const edited = await page.request.patch(`/api/messages/${message.id}`, {
        data: { body: editedBody },
      });
      expect(edited.ok()).toBe(true);
      const { event } = await edited.json();
      expect(event.cursor).toBeTruthy();
      await refreshEntered.promise;
      routeRelease.resolve();
      const fallback = page.getByRole("dialog", { name: "Copy message link" });
      await expect(fallback.getByLabel("Message link")).toHaveValue(
        new RegExp(`/app/${workspace.route_id}/${routeID}$`),
      );
      refreshRelease.resolve();
      await expect
        .poll(() =>
          page.evaluate(({ key, cursor }) => (localStorage.getItem(key) || "") >= cursor, {
            key: `clickclack:${workspace.id}:cursor`,
            cursor: event.cursor,
          }),
        )
        .toBe(true);
      await expect(row.locator(".markdown")).toHaveText(editedBody);
      if (surface === "thread") {
        await expect(page.locator(".thread-root .markdown")).toHaveText(editedBody);
      }
    } finally {
      routeRelease.resolve();
      refreshRelease.resolve();
    }
  });
}
