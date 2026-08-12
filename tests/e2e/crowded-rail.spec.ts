import { expect, test, type Locator } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { waitForAppReady } from "./app-ready";

// A clipped or off-viewport element keeps its layout box, so toBeVisible()
// still passes for it. Hit-test the centre instead (see workspace-create.spec).
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

// Regression: with enough workspaces the rail outgrew the shell (the shell's
// implicit grid row expanded to fit it), and focusing the + button
// programmatically scrolled the overflow:hidden shell — pushing the
// guild-create popover above the viewport. An explicit constrained shell row
// keeps the rail inside the shell so .guild-list scrolls internally instead.
test("create form stays usable with a crowded rail", async ({ page }) => {
  await page.goto("/app");
  await waitForAppReady(page);

  for (let i = 0; i < 14; i++) {
    await page.getByRole("button", { name: "Create workspace" }).click();
    const input = page.getByLabel("Workspace name");
    await input.fill(`Crowd ${i} ${randomUUID().slice(0, 6)}`);
    await input.press("Enter");
    await expect(page.getByLabel("Workspace name")).toBeHidden();
  }

  await page.getByRole("button", { name: "Create workspace" }).click();
  await expectHittable(page.getByLabel("Workspace name"));
});
