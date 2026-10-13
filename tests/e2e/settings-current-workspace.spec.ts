import { expect, test, type Page } from "@playwright/test";
import { randomUUID } from "node:crypto";
import type { Workspace } from "../../apps/web/src/lib/types";
import { waitForAppReady } from "./app-ready";

// Every spec shares one server and one bootstrap account, so the live workspace
// list grows as other files create workspaces. Create the ones this proof needs
// for real, then pin the listing to exactly those so the selector is the thing
// under test rather than whatever the account happens to own.
async function pinnedWorkspaces(page: Page, count: number): Promise<Workspace[]> {
  const created: Workspace[] = [];
  for (let index = 0; index < count; index += 1) {
    const response = await page.request.post("/api/workspaces", {
      data: { name: `Rail pick ${index} ${randomUUID().slice(0, 8)}` },
    });
    expect(response.ok()).toBe(true);
    created.push((await response.json()).workspace);
  }
  await page.route("**/api/workspaces", async (route) => {
    if (route.request().method() !== "GET") return route.continue();
    const response = await route.fetch();
    const body = await response.json();
    const listed = new Map<string, Workspace>(
      (body.workspaces as Workspace[]).map((workspace) => [workspace.id, workspace]),
    );
    const workspaces = created.map((workspace) => listed.get(workspace.id) ?? workspace);
    await route.fulfill({ response, json: { ...body, workspaces } });
  });
  return created;
}

async function openAccountSettings(page: Page) {
  await page.getByRole("button", { name: /Account settings for/ }).click();
  const modal = page.getByRole("dialog", { name: "Account settings" });
  await expect(modal.getByRole("heading", { name: "Profile settings" })).toBeVisible();
  return modal;
}

function workspacePicker(modal: ReturnType<Page["getByRole"]>) {
  return modal.getByRole("combobox", { name: "Workspace" });
}

function currentPathRoot(page: Page) {
  return new URL(page.url()).pathname.split("/").slice(0, 3).join("/");
}

// Regression: the rail rendered a group per workspace in listing order with no
// idea which one the user was standing in, so the first Integrations belonged
// to a different workspace and closing its settings page stranded the user
// there.
test("the settings rail opens on the workspace the user is standing in", async ({
  page,
}, testInfo) => {
  const workspaces = await pinnedWorkspaces(page, 3);
  const current = workspaces[1];
  await page.goto(`/app/${current.route_id}`);
  await waitForAppReady(page);

  let modal = await openAccountSettings(page);
  await page.screenshot({
    path: testInfo.outputPath("workspace-settings-rail.png"),
    animations: "disabled",
  });
  await modal.getByRole("button", { name: "Integrations" }).first().click();
  await expect(page).toHaveURL(`/app/${current.route_id}/settings/integrations`);
  await page.getByRole("button", { name: "Close workspace settings" }).click();
  await expect.poll(() => currentPathRoot(page)).toBe(`/app/${current.route_id}`);

  modal = await openAccountSettings(page);
  const picker = workspacePicker(modal);
  await expect(picker).toHaveValue(current.id);
  // Preselected, and the current workspace is marked in the list itself.
  await expect(picker.locator("option")).toHaveCount(3);
  await expect(picker.locator(`option[value="${current.id}"]`)).toHaveText(
    `${current.name} (current)`,
  );
  await expect(picker.locator("option", { hasText: "(current)" })).toHaveCount(1);
  // One workspace group, not one per workspace, and no "other workspaces" rail.
  await expect(
    modal.locator(".settings-modal__rail-heading", { hasText: "Workspace" }),
  ).toHaveCount(1);
  await expect(modal.getByText("Other workspaces", { exact: true })).toHaveCount(0);
  await expect(modal.getByRole("button", { name: "Integrations" })).toHaveCount(1);
});

test("picking another workspace points the sections at it", async ({ page }) => {
  const workspaces = await pinnedWorkspaces(page, 3);
  const current = workspaces[1];
  const other = workspaces[2];
  await page.goto(`/app/${current.route_id}`);
  await waitForAppReady(page);

  const modal = await openAccountSettings(page);
  const picker = workspacePicker(modal);
  await picker.selectOption(other.id);
  await expect(picker).toHaveValue(other.id);
  // The rows still belong to exactly one workspace, now the picked one.
  await expect(modal.getByRole("button", { name: "Integrations" })).toHaveCount(1);

  await modal.getByRole("button", { name: "Integrations" }).click();
  await expect(page).toHaveURL(`/app/${other.route_id}/settings/integrations`);
});

test("a lone workspace is named in the heading and needs no selector", async ({ page }) => {
  const [only] = await pinnedWorkspaces(page, 1);
  await page.goto(`/app/${only.route_id}`);
  await waitForAppReady(page);

  const modal = await openAccountSettings(page);
  await expect(workspacePicker(modal)).toHaveCount(0);
  await expect(
    modal.locator(".settings-modal__rail-heading", { hasText: "Workspace ·" }),
  ).toContainText(only.name);
  await expect(modal.getByRole("button", { name: "Integrations" })).toHaveCount(1);
});

test.describe("phone width", () => {
  test.use({ viewport: { width: 390, height: 844 } });

  // Regression: the profile card lives inside the navigation drawer at this
  // width, and opening the modal left the drawer open underneath it, so the
  // modal rail and the channel list collided on screen.
  test("opening account settings closes the navigation drawer", async ({ page }, testInfo) => {
    const workspaces = await pinnedWorkspaces(page, 3);
    const current = workspaces[1];
    await page.goto(`/app/${current.route_id}`);
    await waitForAppReady(page);

    const navToggle = page.getByRole("button", { name: "Toggle navigation" });
    await navToggle.click();
    await expect(page.locator(".shell")).toHaveClass(/nav-open/);

    const modal = await openAccountSettings(page);
    await page.screenshot({
      path: testInfo.outputPath("workspace-settings-phone.png"),
      animations: "disabled",
    });
    await expect(page.locator(".shell")).not.toHaveClass(/nav-open/);
    await expect(navToggle).toHaveAttribute("aria-expanded", "false");

    await expect(workspacePicker(modal)).toHaveValue(current.id);
    await modal.getByRole("button", { name: "Integrations" }).click();
    await expect(page).toHaveURL(`/app/${current.route_id}/settings/integrations`);
  });
});
