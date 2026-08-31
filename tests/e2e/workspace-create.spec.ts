import { expect, test, type Locator } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { waitForAppReady } from "./app-ready";

// A clipped element keeps its layout box, so toBeVisible() and boundingBox()
// both still pass for it. Hit-test the centre instead: an element clipped by an
// ancestor scroll container is not painted, so it never wins that point.
async function expectHittable(locator: Locator) {
  await expect(locator).toBeVisible();
  await expect
    .poll(() =>
      locator.evaluate((element) => {
        const rect = element.getBoundingClientRect();
        const hit = document.elementFromPoint(
          rect.left + rect.width / 2,
          rect.top + rect.height / 2,
        );
        return hit === element || element.contains(hit);
      }),
    )
    .toBe(true);
}

test("the workspace create form opens and is not clipped by the rail", async ({ page }) => {
  await page.goto("/app");
  await waitForAppReady(page);

  await page.getByRole("button", { name: "Create workspace" }).click();

  // Regression: the rail scrolled itself, and a scroll container clips both
  // axes, so this popover — anchored outside the rail at left: 100% — rendered
  // but was painted away. Clicking + looked like it did nothing at all.
  const nameInput = page.getByLabel("Workspace name");
  await expectHittable(nameInput);

  const name = `Rail Workspace ${randomUUID().replaceAll("-", "").slice(0, 8)}`;
  await nameInput.fill(name);
  await nameInput.press("Enter");

  await expect(page.getByRole("link", { name })).toBeVisible();
});

test("workspace creation shows failures and retains the name for retry", async ({ page }) => {
  await page.goto("/app");
  await waitForAppReady(page);
  let attempts = 0;
  await page.route("**/api/workspaces", async (route) => {
    if (route.request().method() !== "POST") return route.continue();
    attempts += 1;
    if (attempts === 1) {
      await route.fulfill({ status: 503, json: { error: "Workspace creation unavailable" } });
    } else {
      await route.continue();
    }
  });
  const name = `Retry workspace ${randomUUID().slice(0, 8)}`;
  await page.getByRole("button", { name: "Create workspace" }).click();
  const input = page.getByLabel("Workspace name");
  await input.fill(name);
  await input.press("Enter");
  await expect(page.getByRole("alert")).toHaveText("Workspace creation unavailable");
  await expect(input).toHaveValue(name);
  await input.press("Enter");
  await expect(input).toBeHidden();
  await expect(page.getByRole("link", { name, exact: true })).toBeVisible();
  expect(attempts).toBe(2);
});
