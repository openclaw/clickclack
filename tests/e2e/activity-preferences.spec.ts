import { expect, test } from "@playwright/test";
import { createGeneralChannel } from "./channel-fixture";
import { waitForAppReady } from "./app-ready";

test("revealing activity overrides an older hide-all preference after reload", async ({ page }) => {
  const { route } = await createGeneralChannel(page, "Activity preferences");
  await page.goto(route);
  await waitForAppReady(page);
  await page.evaluate(() => {
    localStorage.setItem("clickclack:show-agent-activity:v1", "0");
    localStorage.removeItem("clickclack:hide-commentary:v1");
    localStorage.removeItem("clickclack:hide-tool-calls:v1");
  });
  await page.reload();
  await waitForAppReady(page);
  await page.getByRole("button", { name: /Account settings for/ }).click();
  const settings = page.getByRole("dialog", { name: "Account settings" });
  const commentary = settings.getByLabel("Hide agent commentary");
  const tools = settings.getByLabel("Hide tool calls");
  await expect(commentary).toBeChecked();
  await expect(tools).toBeChecked();
  await commentary.uncheck();
  await page.reload();
  await waitForAppReady(page);
  await page.getByRole("button", { name: /Account settings for/ }).click();
  await expect(commentary).not.toBeChecked();
  await expect(tools).toBeChecked();
  await tools.uncheck();
  await page.reload();
  await waitForAppReady(page);
  await page.getByRole("button", { name: /Account settings for/ }).click();
  await expect(commentary).not.toBeChecked();
  await expect(tools).not.toBeChecked();
});
