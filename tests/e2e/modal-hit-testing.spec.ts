import { expect, test, type Locator } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { waitForAppReady } from "./app-ready";

async function expectTopmost(locator: Locator) {
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

test("shared create dialogs stay above their close backdrops", async ({ page }) => {
  await page.goto("/app");
  await waitForAppReady(page);

  const channelName = `modal-stack-${randomUUID().replaceAll("-", "").slice(0, 12)}`;
  await page.getByRole("button", { name: "Create channel" }).click();
  const channelDialog = page.locator(".profile-modal", {
    has: page.getByRole("heading", { name: "Create channel" }),
  });
  const channelInput = channelDialog.getByLabel("Channel name");
  await expectTopmost(channelInput);
  await channelInput.fill(channelName);
  await expect(channelDialog).toBeVisible();

  const createChannel = channelDialog.getByRole("button", { name: "Create channel" });
  await expectTopmost(createChannel);
  await createChannel.click();
  await expect(channelDialog).toBeHidden();
  await expect(page.getByRole("heading", { name: `#${channelName}` })).toBeVisible();

  await page.getByRole("button", { name: "Start direct message" }).click();
  const directDialog = page.locator(".profile-modal", {
    has: page.getByRole("heading", { name: "Start a DM" }),
  });
  const personInput = directDialog.getByLabel("Find a person");
  await expectTopmost(personInput);
  await personInput.fill(`no-match-${channelName}`);
  await expect(directDialog).toBeVisible();
  await expectTopmost(directDialog.getByRole("button", { name: "Start DM" }));
  await directDialog.getByRole("button", { name: "Cancel" }).click();
  await expect(directDialog).toBeHidden();
});
