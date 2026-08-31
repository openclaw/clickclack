import { expect, test, type Page } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { waitForAppReady } from "./app-ready";

test.beforeEach(async ({ page }) => {
  const response = await page.request.get("/api/workspaces");
  expect(response.ok()).toBe(true);
  const { workspaces } = await response.json();
  const created = await page.request.post(`/api/workspaces/${workspaces[0].id}/bots`, {
    data: { display_name: `Settings ${randomUUID()}` },
  });
  expect(created.ok()).toBe(true);
  const { bot } = await created.json();
  await page.context().setExtraHTTPHeaders({ "X-ClickClack-User": bot.id });
});

async function openSettings(page: Page, section: "profile" | "notifications") {
  await page.goto("/app");
  await waitForAppReady(page);
  await page.getByRole("button", { name: /Account settings for/ }).click();
  const modal = page.getByRole("dialog", { name: "Account settings" });
  await expect(modal.getByRole("heading", { name: "Profile settings" })).toBeVisible();
  if (section === "notifications") {
    await modal.getByRole("button", { name: "Notifications", exact: true }).click();
    await expect(modal.getByRole("heading", { name: "Notifications" })).toBeVisible();
  }
  return modal;
}

test("profile saves preserve notifications changed in another tab", async ({ page, context }) => {
  const profile = await openSettings(page, "profile");
  await profile.getByLabel("Display name").fill("Updated profile");

  const otherPage = await context.newPage();
  const notifications = await openSettings(otherPage, "notifications");
  const key = "x".repeat(30);
  await notifications.getByLabel("Pushover user key").fill(key);
  await notifications.getByLabel("Pushover notifications", { exact: true }).check();
  await notifications.getByRole("button", { name: "Save notifications" }).click();
  await expect(notifications.getByRole("status")).toHaveText("Saved");

  await profile.getByRole("button", { name: "Save profile" }).click();
  await expect(profile).toBeHidden();
  const response = await page.request.get("/api/me");
  expect(response.ok()).toBe(true);
  expect((await response.json()).user).toMatchObject({
    display_name: "Updated profile",
    notification_settings: { pushover_enabled: true, pushover_user_key: key },
  });
});

test("notification saves preserve profile changes from another tab", async ({ page, context }) => {
  const notifications = await openSettings(page, "notifications");
  const key = "y".repeat(30);
  await notifications.getByLabel("Pushover user key").fill(key);

  const otherPage = await context.newPage();
  const profile = await openSettings(otherPage, "profile");
  const handle = `updated-${randomUUID().slice(0, 8)}`;
  const avatar = new URL("/favicon.svg", page.url()).href;
  await profile.getByLabel("Display name").fill("Newer profile");
  await profile.getByLabel("Handle", { exact: true }).fill(handle);
  await profile.getByLabel("Avatar URL").fill(avatar);
  await profile.getByRole("button", { name: "Save profile" }).click();
  await expect(profile).toBeHidden();

  await notifications.getByRole("button", { name: "Save notifications" }).click();
  await expect(notifications.getByRole("status")).toHaveText("Saved");
  const response = await page.request.get("/api/me");
  expect(response.ok()).toBe(true);
  expect((await response.json()).user).toMatchObject({
    display_name: "Newer profile",
    handle,
    avatar_url: avatar,
    notification_settings: { pushover_enabled: false, pushover_user_key: key },
  });
});
