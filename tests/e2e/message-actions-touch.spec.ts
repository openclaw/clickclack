import { expect, test, devices } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { waitForAppReady } from "./app-ready";
import { settleScrollFrames } from "./message-frames";

// A phone with no pointer keeps the touch behavior: the toolbar stays a 1px
// anchor and a finger's long press opens the action sheet.
test.use({ ...devices["iPhone 14"] });

test("a finger on a phone still gets the long-press sheet, not the toolbar", async ({ page }) => {
  const suffix = randomUUID().slice(0, 8);
  const workspaceResponse = await page.request.post("/api/workspaces", {
    data: { name: `Touch ${suffix}` },
  });
  const { workspace } = (await workspaceResponse.json()) as {
    workspace: { id: string; route_id: string };
  };
  const channelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: `touch-${suffix}`, kind: "public" },
  });
  const { channel } = (await channelResponse.json()) as {
    channel: { id: string; route_id: string };
  };
  const posted = await page.request.post(`/api/channels/${channel.id}/messages`, {
    data: { body: `Long press me ${suffix}` },
  });
  const { message } = (await posted.json()) as { message: { id: string } };
  await page.goto(`/app/${workspace.route_id}/${channel.route_id}`);
  await waitForAppReady(page);
  await expect(page.locator(".messages.is-revealing")).toHaveCount(0);
  await settleScrollFrames(page);

  const row = page.locator(`[data-message-id="${message.id}"]`).first();
  const actionsWidth = await row
    .locator(".message-actions")
    .evaluate((el) => getComputedStyle(el).width);
  expect(actionsWidth).toBe("1px");

  const box = (await row.boundingBox())!;
  const point = { clientX: box.x + 120, clientY: box.y + box.height / 2 };
  await row.dispatchEvent("pointerdown", {
    pointerType: "touch",
    isPrimary: true,
    button: 0,
    pointerId: 3,
    bubbles: true,
    ...point,
  });
  await page.waitForTimeout(700);
  await page.dispatchEvent("body", "pointerup", {
    pointerType: "touch",
    isPrimary: true,
    button: 0,
    pointerId: 3,
    bubbles: true,
    ...point,
  });
  const sheet = page.locator(`#message-action-sheet-${message.id}`);
  await expect(sheet).toBeVisible();
  // The finger is still down when the sheet appears under it. iOS would carry
  // the press into a text selection on the sheet's first row unless the sheet
  // is unselectable.
  const selectable = await sheet.evaluate((el) => {
    const button = el.querySelector(".sheet-actions button") as HTMLElement | null;
    return [el, button].filter(Boolean).map((node) => getComputedStyle(node!).webkitUserSelect);
  });
  expect(selectable).toEqual(["none", "none"]);
});
