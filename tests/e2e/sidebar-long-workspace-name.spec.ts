import { expect, test, type Locator, type Page } from "@playwright/test";
import { randomUUID } from "node:crypto";
import type { Workspace } from "../../apps/web/src/lib/types";
import { waitForAppReady } from "./app-ready";
import { createGeneralChannel } from "./channel-fixture";

// Too wide for the 260px sidebar on one line.
const longName = "Northwind Field Operations Coordination Workspace";

async function openWorkspace(page: Page, name: string) {
  const response = await page.request.post("/api/workspaces", {
    data: { name, slug: `sidebar-name-${randomUUID().slice(0, 8)}` },
  });
  expect(response.ok()).toBe(true);
  const { workspace } = (await response.json()) as { workspace: Workspace };
  await page.goto(`/app/${workspace.route_id}`);
  await waitForAppReady(page);
}

function sidebarOf(page: Page) {
  return page.getByRole("complementary", { name: "Channels and DMs" });
}

// toBeVisible() passes for a clipped control, so check its box and its hit test.
function placement(control: Locator) {
  return control.evaluate((element) => {
    const rect = element.getBoundingClientRect();
    const sidebar = element.closest(".sidebar")?.getBoundingClientRect();
    const hit = document.elementFromPoint(rect.left + rect.width / 2, rect.top + rect.height / 2);
    return {
      inSidebar:
        sidebar !== undefined &&
        rect.left >= sidebar.left &&
        rect.right <= sidebar.right &&
        rect.top >= sidebar.top &&
        rect.bottom <= sidebar.bottom,
      inViewport:
        rect.left >= 0 &&
        rect.right <= window.innerWidth &&
        rect.top >= 0 &&
        rect.bottom <= window.innerHeight,
      topmost: hit === element || element.contains(hit),
    };
  });
}

const usable = { inSidebar: true, inViewport: true, topmost: true };

async function expectCreateButtons(page: Page) {
  const sidebar = sidebarOf(page);
  const controls = [
    { button: "Create channel", heading: "Create channel" },
    { button: "Start direct message", heading: "Start a DM" },
  ];
  for (const { button, heading } of controls) {
    const control = sidebar.getByRole("button", { name: button });
    await expect.poll(() => placement(control), { message: button }).toEqual(usable);
    await control.click();
    const dialog = page.locator(".profile-modal").getByRole("heading", { name: heading });
    await expect(dialog).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(dialog).toBeHidden();
  }
}

function overflows(locator: Locator) {
  return locator.evaluate((element) => element.scrollWidth > element.clientWidth);
}

test("a long workspace name truncates and keeps the sidebar create buttons usable", async ({
  page,
}) => {
  await openWorkspace(page, longName);
  const sidebar = sidebarOf(page);

  await expectCreateButtons(page);
  await expect
    .poll(() => placement(sidebar.getByRole("button", { name: "Collapse sidebar" })))
    .toEqual(usable);
  await expect.poll(() => overflows(sidebar.getByText(longName, { exact: true }))).toBe(true);
  await expect.poll(() => overflows(sidebar)).toBe(false);
});

test("a short workspace name keeps the same sidebar layout", async ({ page }) => {
  const shortName = "Northwind";
  await openWorkspace(page, shortName);
  const sidebar = sidebarOf(page);

  await expectCreateButtons(page);
  await expect.poll(() => overflows(sidebar.getByText(shortName, { exact: true }))).toBe(false);
  await expect.poll(() => overflows(sidebar)).toBe(false);
});

test("the mobile drawer keeps its create buttons with a long workspace name", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await openWorkspace(page, longName);
  await page.getByRole("button", { name: "Toggle navigation" }).click();
  const sidebar = sidebarOf(page);

  await expectCreateButtons(page);
  await expect.poll(() => overflows(sidebar.getByText(longName, { exact: true }))).toBe(true);
  await expect.poll(() => overflows(sidebar)).toBe(false);
});

test("a long account name keeps the desktop sidebar create buttons usable", async ({ page }) => {
  const label = "Northwind Operations Coordination";
  const { route } = await createGeneralChannel(page, label, true);
  await page.addInitScript(() => {
    Object.assign(window, {
      clickclackDesktop: {
        integratedTitleBar: true,
        notify: async () => true,
        onNavigate: () => () => {},
        onQuickCompose: () => () => {},
        openSettings: () => {},
        platform: "darwin",
        setActiveRoute: () => {},
        setUnreadCount: () => {},
        signInWithGitHub: async () => true,
      },
    });
  });
  await page.goto(route);
  await waitForAppReady(page);

  await expectCreateButtons(page);
  const account = page.getByRole("button", { name: /Account settings for/ });
  await expect
    .poll(() => overflows(account.getByText(`${label} Tester`, { exact: true })))
    .toBe(true);
  await expect.poll(() => overflows(sidebarOf(page))).toBe(false);
});
