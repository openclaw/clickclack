import { expect, test, type Page } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { waitForAppReady } from "./app-ready";

// Isolated workspace + #general so realtime traffic from parallel workers
// cannot reflow the message list around the lightbox triggers.
async function createIsolatedGeneralRoute(page: Page, label: string): Promise<string> {
  const wsSuffix = randomUUID().replaceAll("-", "").slice(0, 12);
  const workspaceResponse = await page.request.post("/api/workspaces", {
    data: { name: `${label} ${wsSuffix}` },
  });
  expect(workspaceResponse.ok()).toBe(true);
  const { workspace } = (await workspaceResponse.json()) as {
    workspace: { id: string; route_id: string };
  };
  const channelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: "general", kind: "public" },
  });
  expect(channelResponse.ok()).toBe(true);
  const { channel } = (await channelResponse.json()) as { channel: { route_id: string } };
  return `/app/${workspace.route_id}/${channel.route_id}`;
}

test("opens conversation and thread images in an accessible lightbox", async ({ page }) => {
  const suffix = Date.now();
  const filename = `lightbox-${suffix}.png`;
  const messageText = `image lightbox ${suffix}`;

  await page.goto(await createIsolatedGeneralRoute(page, "Image Viewer"));
  await waitForAppReady(page);
  await page.getByLabel("Upload file").setInputFiles({
    name: filename,
    mimeType: "image/png",
    buffer: Buffer.from(
      "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII=",
      "base64",
    ),
  });
  await expect(page.getByText(filename)).toBeVisible();
  await page.getByLabel("Message body").fill(messageText);
  await page.getByRole("button", { name: "Send" }).click();

  const imageRow = page.locator(".message-row").filter({ hasText: messageText });
  const conversationTrigger = imageRow.getByRole("button", { name: `Open image ${filename}` });
  await expect(conversationTrigger).toBeVisible();

  await page.getByRole("button", { name: /Account settings for/ }).click({ button: "right" });
  const settingsDialog = page.getByRole("dialog", { name: "Account settings" });
  await expect(settingsDialog).toBeVisible();
  await conversationTrigger.focus();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("dialog", { name: `Image viewer: ${filename}` })).toHaveCount(0);
  await settingsDialog.getByRole("button", { name: "Close" }).click();

  await conversationTrigger.click();

  const dialog = page.getByRole("dialog", { name: `Image viewer: ${filename}` });
  const closeButton = dialog.getByRole("button", { name: "Close image viewer" });
  const openOriginal = dialog.getByRole("link", { name: "Open original" });
  await expect(dialog).toBeVisible();
  await expect(dialog).toHaveAttribute("aria-modal", "true");
  const displayedImage = dialog.getByRole("img", { name: filename });
  await expect(displayedImage).toBeVisible();
  await expect
    .poll(() => displayedImage.evaluate((image: HTMLImageElement) => image.naturalWidth))
    .toBeGreaterThan(0);
  const uploadedImageURL = await displayedImage.getAttribute("src");
  expect(uploadedImageURL).toMatch(/\/api\/uploads\//);
  await expect(openOriginal).toHaveAttribute("href", uploadedImageURL!);
  await expect(page.locator(".shell")).toHaveAttribute("inert", "");
  await expect(closeButton).toBeFocused();

  await page.keyboard.press("Tab");
  await expect(openOriginal).toBeFocused();
  await page.keyboard.press("Shift+Tab");
  await expect(closeButton).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);
  await expect(conversationTrigger).toBeFocused();
  await expect(page.locator(".shell")).not.toHaveAttribute("inert", "");

  await imageRow.getByRole("button", { name: "Open thread" }).click();
  const threadPane = page.getByLabel("Thread pane", { exact: true });
  const threadTrigger = threadPane.getByRole("button", { name: `Open image ${filename}` });
  await expect(threadTrigger).toBeVisible();
  await threadTrigger.click();
  await expect(dialog).toBeVisible();
  await expect(displayedImage).toHaveAttribute("src", uploadedImageURL!);
  await expect
    .poll(() => displayedImage.evaluate((image: HTMLImageElement) => image.naturalWidth))
    .toBeGreaterThan(0);
  await expect(openOriginal).toHaveAttribute("href", uploadedImageURL!);

  await page.locator(".image-viewer-scrim > .modal-backdrop").click({ position: { x: 4, y: 4 } });
  await expect(dialog).toHaveCount(0);
  await expect(threadTrigger).toBeFocused();
});
