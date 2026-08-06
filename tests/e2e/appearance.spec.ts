import { expect, test } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { randomUUID } from "node:crypto";
import { waitForAppReady } from "./app-ready";

// Appearance prefs apply locally first, persist to the account, and use
// localStorage as the pre-paint cache. These tests drive the real UI and assert
// local rendering, migration, reload, and cross-context roaming.

type ServerAppearancePreferences = {
  color_mode?: "" | "light" | "dark";
  board_theme?: "" | "ember" | "moss" | "iris";
  message_layout?: "" | "outlined";
  density?: "" | "compact";
};

const appearanceStorageKeys = [
  "clickclack:color-mode:v1",
  "clickclack:board-theme:v1",
  "clickclack:message-layout:v1",
  "clickclack:density:v1",
  "clickclack:appearance-user:v1",
];

let appearanceUserID = "";

test.beforeEach(async ({ page }) => {
  const workspacesResponse = await page.request.get("/api/workspaces");
  expect(workspacesResponse.ok()).toBe(true);
  const { workspaces } = (await workspacesResponse.json()) as {
    workspaces: { id: string }[];
  };
  const suffix = randomUUID();
  appearanceUserID = execFileSync(
    "go",
    [
      "run",
      "./apps/api/cmd/clickclack",
      "admin",
      "user",
      "create",
      "--data",
      "./data/e2e",
      "--workspace",
      workspaces[0].id,
      "--name",
      `Appearance Tester ${suffix}`,
      "--email",
      `appearance-${suffix}@example.com`,
    ],
    { cwd: process.cwd(), encoding: "utf8" },
  ).trim();
  await page.setExtraHTTPHeaders({ "X-ClickClack-User": appearanceUserID });
});

async function serverAppearance(page: import("@playwright/test").Page) {
  return page.evaluate(async () => {
    const response = await fetch("/api/me");
    const body = (await response.json()) as {
      user: { appearance_preferences?: ServerAppearancePreferences };
    };
    return body.user.appearance_preferences ?? null;
  });
}

async function resetAppearance(page: import("@playwright/test").Page) {
  if (page.isClosed()) return;
  await page
    .evaluate(async (keys) => {
      for (const key of keys) localStorage.removeItem(key);
      await fetch("/api/me", {
        method: "PATCH",
        headers: {
          "Content-Type": "application/json",
          "X-ClickClack-CSRF": "1",
        },
        body: JSON.stringify({
          appearance_preferences: {
            color_mode: "",
            board_theme: "",
            message_layout: "",
            density: "",
          },
        }),
      });
    }, appearanceStorageKeys)
    .catch(() => {});
}

test.afterEach(async ({ page }) => {
  await resetAppearance(page);
});

async function openAppearanceSettings(page: import("@playwright/test").Page) {
  await page.goto("/app");
  await waitForAppReady(page);
  const accountSettings = page.getByRole("button", { name: /account settings/i });
  const modal = page.getByRole("dialog", { name: "Account settings" });
  const appearanceHeading = modal.getByRole("heading", { name: "Appearance" });
  const mobileNavigation = page.getByRole("button", { name: "Toggle navigation" });
  if (
    (await mobileNavigation.isVisible()) &&
    (await mobileNavigation.getAttribute("aria-expanded")) !== "true"
  ) {
    await mobileNavigation.click();
    await expect(mobileNavigation).toHaveAttribute("aria-expanded", "true");
  }
  await expect(accountSettings).toBeVisible();
  await accountSettings.click();
  await expect(modal.getByRole("heading", { name: "Profile settings" })).toBeVisible();
  await modal.getByRole("button", { name: "Appearance", exact: true }).click();
  await expect(appearanceHeading).toBeVisible();
}

test("migrates an existing local appearance cache once", async ({ page }) => {
  let appearancePatchCount = 0;
  page.on("request", (request) => {
    if (
      request.method() === "PATCH" &&
      new URL(request.url()).pathname === "/api/me" &&
      request.postData()?.includes("appearance_preferences")
    ) {
      appearancePatchCount += 1;
    }
  });
  await page.addInitScript(() => {
    if (sessionStorage.getItem("appearance-migration-seeded")) return;
    sessionStorage.setItem("appearance-migration-seeded", "1");
    localStorage.setItem("clickclack:board-theme:v1", "moss");
    localStorage.setItem("clickclack:density:v1", "compact");
  });

  await page.goto("/app");
  await waitForAppReady(page);
  await expect
    .poll(() => serverAppearance(page))
    .toMatchObject({ board_theme: "moss", density: "compact" });
  await expect.poll(() => Promise.resolve(appearancePatchCount)).toBe(1);

  await page.evaluate(() => localStorage.clear());
  await page.reload();
  await waitForAppReady(page);
  await expect(page.locator("html")).toHaveAttribute("data-board", "moss");
  await expect(page.locator("html")).toHaveAttribute("data-density", "compact");
  await expect.poll(() => Promise.resolve(appearancePatchCount)).toBe(1);
});

test("appearance choices survive cache loss and roam to a second browser context", async ({
  browser,
  page,
}) => {
  await openAppearanceSettings(page);
  const html = page.locator("html");

  await page.getByRole("radio", { name: /^Ember/ }).click();
  await page.getByRole("radio", { name: /^Moss/ }).click();
  await page.getByRole("radio", { name: /^Iris/ }).click();
  await page.getByRole("radio", { name: /^Compact/ }).click();
  await expect
    .poll(() => serverAppearance(page))
    .toMatchObject({ board_theme: "iris", density: "compact" });

  await page.evaluate(() => localStorage.clear());
  await page.reload();
  await waitForAppReady(page);
  await expect(html).toHaveAttribute("data-board", "iris");
  await expect(html).toHaveAttribute("data-density", "compact");

  const secondContext = await browser.newContext({
    extraHTTPHeaders: { "X-ClickClack-User": appearanceUserID },
  });
  try {
    await secondContext.addCookies(await page.context().cookies());
    const secondPage = await secondContext.newPage();
    await secondPage.goto("/app");
    await waitForAppReady(secondPage);
    await expect(secondPage.locator("html")).toHaveAttribute("data-board", "iris");
    await expect(secondPage.locator("html")).toHaveAttribute("data-density", "compact");
  } finally {
    await secondContext.close();
  }
});

test("stale account responses cannot overwrite a newer appearance choice", async ({ page }) => {
  let releaseProfileResponse!: () => void;
  let markProfileResponseCaptured!: () => void;
  const profileResponseGate = new Promise<void>((resolve) => {
    releaseProfileResponse = resolve;
  });
  const profileResponseCaptured = new Promise<void>((resolve) => {
    markProfileResponseCaptured = resolve;
  });
  await page.route("**/api/me", async (route) => {
    const request = route.request();
    const body = request.postData() ?? "";
    if (
      request.method() === "PATCH" &&
      body.includes('"display_name"') &&
      !body.includes('"appearance_preferences"')
    ) {
      const response = await route.fetch();
      markProfileResponseCaptured();
      await profileResponseGate;
      await route.fulfill({ response });
      return;
    }
    await route.continue();
  });

  await openAppearanceSettings(page);
  const modal = page.getByRole("dialog", { name: "Account settings" });
  await modal.locator(".settings-modal__rail-item").first().click();
  await expect(modal.getByRole("heading", { name: "Profile settings" })).toBeVisible();
  await modal.getByRole("button", { name: "Save profile" }).click();
  await profileResponseCaptured;

  await modal.getByRole("button", { name: "Appearance", exact: true }).click();
  await modal.getByRole("radio", { name: /^Iris/ }).click();
  await expect.poll(() => serverAppearance(page)).toMatchObject({ board_theme: "iris" });
  releaseProfileResponse();

  await expect(modal).toBeHidden();
  await expect(page.locator("html")).toHaveAttribute("data-board", "iris");
  await expect
    .poll(() => page.evaluate(() => localStorage.getItem("clickclack:board-theme:v1")))
    .toBe("iris");
});

test("forced color mode applies instantly and survives reload", async ({ page }) => {
  await openAppearanceSettings(page);

  const html = page.locator("html");
  await expect(html).not.toHaveAttribute("data-color-mode");

  await page.getByRole("radio", { name: "Dark" }).click();
  await expect(html).toHaveAttribute("data-color-mode", "dark");
  // color-scheme pins to dark, so light-dark() resolves the dark background.
  await expect
    .poll(() => page.evaluate(() => getComputedStyle(document.documentElement).colorScheme))
    .toBe("dark");

  await page.reload();
  // The app.html boot script applies the stored mode before hydration.
  await expect(html).toHaveAttribute("data-color-mode", "dark");

  await openAppearanceSettings(page);
  await page.getByRole("radio", { name: "System" }).click();
  await expect(html).not.toHaveAttribute("data-color-mode");
});

test("board theme retunes the app palette and survives reload", async ({ page }) => {
  await openAppearanceSettings(page);

  const html = page.locator("html");
  const accentOf = () =>
    page.evaluate(() =>
      getComputedStyle(document.documentElement).getPropertyValue("--accent").trim(),
    );
  const signalAccent = await accentOf();

  await page.getByRole("radio", { name: /^Ember/ }).click();
  await expect(html).toHaveAttribute("data-board", "ember");
  await expect.poll(accentOf).not.toBe(signalAccent);

  await page.reload();
  await expect(html).toHaveAttribute("data-board", "ember");

  await openAppearanceSettings(page);
  await page.getByRole("radio", { name: /^Galactic/ }).click();
  await expect(html).not.toHaveAttribute("data-board");
  await expect.poll(accentOf).toBe(signalAccent);
});

test("message layout switches to outlined chains and survives reload", async ({ page }) => {
  await openAppearanceSettings(page);

  const html = page.locator("html");
  await expect(html).not.toHaveAttribute("data-message-layout");
  await expect(page.getByRole("radio", { name: /^Standard/ })).toHaveAttribute(
    "aria-checked",
    "true",
  );

  await page.getByRole("radio", { name: /^Outlined chains/ }).click();
  await expect(html).toHaveAttribute("data-message-layout", "outlined");
  await expect
    .poll(() => page.evaluate(() => localStorage.getItem("clickclack:message-layout:v1")))
    .toBe("outlined");

  await page.reload();
  await expect(html).toHaveAttribute("data-message-layout", "outlined");

  await openAppearanceSettings(page);
  await page.getByRole("radio", { name: /^Standard/ }).click();
  await expect(html).not.toHaveAttribute("data-message-layout");
  await expect
    .poll(() => page.evaluate(() => localStorage.getItem("clickclack:message-layout:v1")))
    .toBeNull();
});

test("density switches to compact and survives reload", async ({ page }) => {
  await openAppearanceSettings(page);

  const html = page.locator("html");
  await expect(html).not.toHaveAttribute("data-density");
  await expect(page.getByRole("radio", { name: /^Comfortable/ })).toHaveAttribute(
    "aria-checked",
    "true",
  );

  await page.getByRole("radio", { name: /^Compact/ }).click();
  await expect(html).toHaveAttribute("data-density", "compact");
  await expect
    .poll(() => page.evaluate(() => localStorage.getItem("clickclack:density:v1")))
    .toBe("compact");

  await page.evaluate(() => {
    const fixture = document.createElement("article");
    fixture.id = "appearance-density-fixture";
    fixture.className = "message-group";
    fixture.style.cssText = "position:fixed;left:-10000px;top:0;width:600px;visibility:hidden";
    fixture.innerHTML = `
      <span class="avatar"></span>
      <div class="group-body">
        <header><strong>Fixture</strong><time>2:14 PM</time></header>
        <div class="message-row">
          <span class="row-stamp"></span>
          <div class="message-content"><div class="markdown">Fixture message</div></div>
        </div>
      </div>
    `;
    document.body.append(fixture);
  });

  await page.getByRole("radio", { name: /^Outlined chains/ }).click();
  const messageGroup = page.locator("#appearance-density-fixture");
  const messageRow = messageGroup.locator(".message-row");
  await expect
    .poll(() =>
      messageRow.evaluate((element) => {
        const style = getComputedStyle(element);
        return { marginLeft: style.marginLeft, paddingLeft: style.paddingLeft };
      }),
    )
    .toEqual({ marginLeft: "0px", paddingLeft: "0px" });

  await page.setViewportSize({ width: 390, height: 844 });
  await expect
    .poll(() => messageGroup.evaluate((element) => getComputedStyle(element).paddingLeft))
    .toBe("12px");

  // The pre-paint boot script applies the stored density before hydration.
  await page.reload();
  await expect(html).toHaveAttribute("data-density", "compact");

  await openAppearanceSettings(page);
  await page.getByRole("radio", { name: /^Comfortable/ }).click();
  await expect(html).not.toHaveAttribute("data-density");
  await expect
    .poll(() => page.evaluate(() => localStorage.getItem("clickclack:density:v1")))
    .toBeNull();
});

test("appearance choices support radio keyboard navigation", async ({ page }) => {
  await openAppearanceSettings(page);

  const colorModes = page.getByRole("radiogroup", { name: "Color mode" });
  const system = colorModes.getByRole("radio", { name: "System" });
  const light = colorModes.getByRole("radio", { name: "Light" });
  await expect(system).toHaveAttribute("tabindex", "0");
  await expect(light).toHaveAttribute("tabindex", "-1");
  await system.focus();
  await page.keyboard.press("ArrowRight");
  await expect(light).toBeFocused();
  await expect(light).toHaveAttribute("aria-checked", "true");
  await expect(system).toHaveAttribute("tabindex", "-1");

  const boards = page.getByRole("radiogroup", { name: "Board theme" });
  const signal = boards.getByRole("radio", { name: /^Galactic/ });
  const iris = boards.getByRole("radio", { name: /^Iris/ });
  await signal.focus();
  await page.keyboard.press("ArrowLeft");
  await expect(iris).toBeFocused();
  await expect(iris).toHaveAttribute("aria-checked", "true");

  const messageLayouts = page.getByRole("radiogroup", { name: "Message layout" });
  const standard = messageLayouts.getByRole("radio", { name: /^Standard/ });
  const outlined = messageLayouts.getByRole("radio", { name: /^Outlined chains/ });
  await standard.focus();
  await page.keyboard.press("End");
  await expect(outlined).toBeFocused();
  await expect(outlined).toHaveAttribute("aria-checked", "true");
  await page.keyboard.press("Home");
  await expect(standard).toBeFocused();
  await expect(standard).toHaveAttribute("aria-checked", "true");
  await page.keyboard.press("ArrowUp");
  await expect(outlined).toBeFocused();
  await expect(outlined).toHaveAttribute("tabindex", "0");

  const densities = page.getByRole("radiogroup", { name: "Density" });
  const comfortable = densities.getByRole("radio", { name: /^Comfortable/ });
  const compact = densities.getByRole("radio", { name: /^Compact/ });
  await comfortable.focus();
  await page.keyboard.press("ArrowDown");
  await expect(compact).toBeFocused();
  await expect(compact).toHaveAttribute("aria-checked", "true");
});
