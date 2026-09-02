import { expect, test, type Page, type Request } from "@playwright/test";
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

async function holdUserResponse(page: Page, method: "GET" | "PATCH") {
  let release!: () => void;
  let enter!: () => void;
  let complete!: () => void;
  let captured = false;
  const entered = new Promise<void>((resolve) => (enter = resolve));
  const blocked = new Promise<void>((resolve) => (release = resolve));
  const completed = new Promise<void>((resolve) => (complete = resolve));
  await page.route("**/api/me", async (route) => {
    if (captured || route.request().method() !== method) return route.continue();
    captured = true;
    const response = await route.fetch();
    expect(response.ok()).toBe(true);
    const settled = (request: Request) => {
      if (request !== route.request()) return;
      page.off("requestfinished", settled);
      page.off("requestfailed", settled);
      complete();
    };
    page.on("requestfinished", settled);
    page.on("requestfailed", settled);
    enter();
    await blocked;
    await route.fulfill({ response });
  });
  return {
    entered,
    release,
    async finish() {
      release();
      await completed;
      await page.evaluate(
        () =>
          new Promise<void>((resolve) =>
            requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
          ),
      );
    },
  };
}

for (const [section, destination] of [
  ["profile", "notifications"],
  ["notifications", "profile"],
  ["profile", "reopened profile"],
] as const) {
  test(`a delayed ${section} save preserves the ${destination} draft`, async ({
    page,
  }, testInfo) => {
    const modal = await openSettings(page, section);
    const savedField = section === "profile" ? "Display name" : "Pushover user key";
    await modal
      .getByLabel(savedField)
      .fill(section === "profile" ? "Saved profile" : "s".repeat(30));
    const held = await holdUserResponse(page, "PATCH");
    try {
      await modal.getByRole("button", { name: `Save ${section}` }).click();
      await held.entered;
      if (destination === "reopened profile") {
        await modal.getByRole("button", { name: "Close", exact: true }).click();
        await expect(modal).toBeHidden();
        await page.getByRole("button", { name: /Account settings for/ }).click();
      } else {
        await modal
          .getByRole("button", {
            name: destination === "profile" ? "Settings" : "Notifications",
            exact: destination !== "profile",
          })
          .click();
      }
      const targetSection = destination === "notifications" ? "Notifications" : "Profile settings";
      await expect(modal.getByRole("heading", { name: targetSection })).toBeVisible();
      const draftField = destination === "notifications" ? "Pushover user key" : "Display name";
      const draft = destination === "notifications" ? "d".repeat(30) : "Unsaved profile draft";
      await modal.getByLabel(draftField).fill(draft);
      await held.finish();
      await page.screenshot({ path: testInfo.outputPath("late-save-result.png") });
      await expect(modal.getByRole("heading", { name: targetSection })).toBeVisible();
      await expect(modal.getByLabel(draftField)).toHaveValue(draft);
    } finally {
      held.release();
    }
  });
}

test("a closed account refresh cannot restore an older profile", async ({ page }, testInfo) => {
  await page.goto("/app");
  await waitForAppReady(page);
  const held = await holdUserResponse(page, "GET");
  const modal = page.getByRole("dialog", { name: "Account settings" });
  try {
    await page.getByRole("button", { name: /Account settings for/ }).click();
    await held.entered;
    await modal.getByRole("button", { name: "Close", exact: true }).click();
    await expect(modal).toBeHidden();
    await page.getByRole("button", { name: /Account settings for/ }).click();
    await expect(modal.getByRole("heading", { name: "Profile settings" })).toBeVisible();
    await modal.getByLabel("Display name").fill("Newer saved profile");
    await modal.getByRole("button", { name: "Save profile" }).click();
    await expect(modal).toBeHidden();
    await expect(
      page.getByRole("button", { name: "Account settings for Newer saved profile" }),
    ).toBeVisible();
    await held.finish();
    await page.screenshot({ path: testInfo.outputPath("late-refresh-result.png") });
    await expect(
      page.getByRole("button", { name: "Account settings for Newer saved profile" }),
    ).toBeVisible();
  } finally {
    held.release();
  }
});

test("a closed account refresh cannot revert refreshed appearance", async ({ page }, testInfo) => {
  const initialized = await page.request.patch("/api/me", {
    data: { appearance_preferences: { board_theme: "" } },
  });
  expect(initialized.ok()).toBe(true);
  await page.goto("/app");
  await waitForAppReady(page);
  const held = await holdUserResponse(page, "GET");
  const modal = page.getByRole("dialog", { name: "Account settings" });
  try {
    await page.getByRole("button", { name: /Account settings for/ }).click();
    await held.entered;
    await modal.getByRole("button", { name: "Close", exact: true }).click();
    await expect(modal).toBeHidden();
    const changedElsewhere = await page.request.patch("/api/me", {
      data: { appearance_preferences: { board_theme: "iris" } },
    });
    expect(changedElsewhere.ok()).toBe(true);
    await page.getByRole("button", { name: /Account settings for/ }).click();
    await expect(modal.getByRole("heading", { name: "Profile settings" })).toBeVisible();
    await expect(page.locator("html")).toHaveAttribute("data-board", "iris");
    await held.finish();
    await page.screenshot({ path: testInfo.outputPath("late-appearance-result.png") });
    await expect(page.locator("html")).toHaveAttribute("data-board", "iris");
  } finally {
    held.release();
  }
});

for (const section of ["profile", "notifications"] as const) {
  test(`a pending ${section} save locks its submitted fields`, async ({ page }) => {
    const modal = await openSettings(page, section);
    const field = modal.getByLabel(section === "profile" ? "Display name" : "Pushover user key");
    await field.fill(section === "profile" ? "Submitted profile" : "s".repeat(30));
    const held = await holdUserResponse(page, "PATCH");
    try {
      await modal.getByRole("button", { name: `Save ${section}` }).click();
      await held.entered;
      await expect(field).toBeDisabled();
      await held.finish();
      if (section === "profile") await expect(modal).toBeHidden();
      else {
        await expect(field).toBeEnabled();
        await expect(modal.getByRole("status")).toHaveText("Saved");
      }
    } finally {
      held.release();
    }
  });
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
