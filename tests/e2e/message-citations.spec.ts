import { expect, test } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { waitForAppReady } from "./app-ready";

test("channel root citations copy, reopen, highlight, and keep their URL after the first reply", async ({
  browser,
  page,
}) => {
  test.setTimeout(45_000);
  await page.addInitScript(() => {
    window.addEventListener("DOMContentLoaded", () => {
      const highlightedMessageIDs: string[] = [];
      Object.assign(window, { highlightedMessageIDs });
      new MutationObserver(() => {
        for (const row of document.querySelectorAll<HTMLElement>("[data-message-id].highlight")) {
          const id = row.dataset.messageId;
          if (id && !highlightedMessageIDs.includes(id)) highlightedMessageIDs.push(id);
        }
      }).observe(document.body, { attributes: true, attributeFilter: ["class"], subtree: true });
    });
  });
  const suffix = randomUUID().replaceAll("-", "").slice(0, 12);
  const workspaceResponse = await page.request.post("/api/workspaces", {
    data: { name: `Citation proof ${suffix}` },
  });
  expect(workspaceResponse.ok()).toBe(true);
  const { workspace } = (await workspaceResponse.json()) as {
    workspace: { id: string; route_id: string };
  };
  const channelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: `citation-proof-${suffix}`, kind: "public" },
  });
  expect(channelResponse.ok()).toBe(true);
  const { channel } = (await channelResponse.json()) as {
    channel: { id: string; route_id: string };
  };
  const messageResponse = await page.request.post(`/api/channels/${channel.id}/messages`, {
    data: { body: `Stable citation ${suffix}` },
  });
  expect(messageResponse.ok()).toBe(true);
  const { message } = (await messageResponse.json()) as {
    message: { id: string; route_id: string; body: string };
  };
  expect(message.route_id).toMatch(/^M[A-Z0-9]{16}$/);

  await page.goto(`/app/${workspace.route_id}/${channel.route_id}`);
  await waitForAppReady(page);
  const row = page.locator(`[data-message-id="${message.id}"]`);
  await expect(row).toBeVisible();
  await page.evaluate(() => {
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: {
        writeText: async (value: string) => Object.assign(window, { copiedCitation: value }),
      },
    });
  });
  await row.hover();
  await row.getByRole("button", { name: "More actions" }).click();
  await row.getByRole("menuitem", { name: "Copy link" }).click();
  const expectedURL = new URL(
    `/app/${workspace.route_id}/${message.route_id}`,
    page.url(),
  ).toString();
  await expect
    .poll(() => page.evaluate(() => Reflect.get(window, "copiedCitation")))
    .toBe(expectedURL);

  const mobileContext = await browser.newContext({
    baseURL: new URL(page.url()).origin,
    hasTouch: true,
    isMobile: true,
    viewport: { width: 390, height: 720 },
  });
  const mobilePage = await mobileContext.newPage();
  await mobilePage.goto(`/app/${workspace.route_id}/${channel.route_id}`);
  await waitForAppReady(mobilePage);
  await mobilePage.evaluate(() => {
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: {
        writeText: async (value: string) => Object.assign(window, { copiedCitation: value }),
      },
    });
  });
  const mobileRow = mobilePage.locator(`[data-message-id="${message.id}"]`);
  const mobileTrigger = mobileRow.getByRole("button", { name: "More actions" });
  await mobileTrigger.focus();
  await mobilePage.keyboard.press("Enter");
  const actionSheet = mobilePage.getByRole("dialog", { name: "Message actions" });
  await actionSheet.getByRole("button", { name: "Copy link" }).click();
  await expect(actionSheet).toBeHidden();
  await expect
    .poll(() => mobilePage.evaluate(() => Reflect.get(window, "copiedCitation")))
    .toBe(expectedURL);
  await mobileContext.close();

  await page.goto(expectedURL);
  await waitForAppReady(page);
  await expect(page).toHaveURL(expectedURL);
  await expect
    .poll(() => page.evaluate(() => Reflect.get(window, "highlightedMessageIDs")))
    .toContain(message.id);
  await expect(page.locator(".thread.open")).toHaveCount(0);

  const replyResponse = await page.request.post(`/api/messages/${message.id}/thread/replies`, {
    data: { body: `First reply ${suffix}` },
  });
  expect(replyResponse.ok()).toBe(true);
  await page.reload();
  await waitForAppReady(page);
  await expect(page).toHaveURL(expectedURL);
  await expect(page.locator(".thread.open")).toBeVisible();
  await expect(page.locator(".thread-root", { hasText: message.body })).toBeVisible();
  await expect(page.locator(".reply", { hasText: `First reply ${suffix}` })).toBeVisible();

  const root = page.locator(".thread-root");
  await page.evaluate(() => {
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText: async () => Promise.reject(new Error("clipboard denied")) },
    });
  });
  await root.getByRole("button", { name: "Copy link" }).click();
  const fallback = page.getByRole("dialog", { name: "Copy message link" });
  await expect(fallback).toBeVisible();
  const input = fallback.getByLabel("Message link");
  await expect(input).toHaveValue(expectedURL);
  await expect(input).toBeFocused();
  await expect(input).toHaveJSProperty("selectionStart", 0);
  await expect(input).toHaveJSProperty("selectionEnd", expectedURL.length);
});
