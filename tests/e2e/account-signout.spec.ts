import { expect, test } from "@playwright/test";
import { waitForAppReady } from "./app-ready";

test("failed sign-out preserves the session and offers a retry", async ({ page }) => {
  await page.goto("/app");
  await waitForAppReady(page);
  await page.getByRole("button", { name: /Account settings for/ }).click({ button: "right" });
  const settings = page.getByRole("dialog", { name: "Account settings" });
  await page.route("**/api/auth/logout", (route) =>
    route.fulfill({
      status: 503,
      contentType: "application/json",
      body: JSON.stringify({ error: "Sign-out unavailable. Try again." }),
    }),
  );
  await settings.getByRole("button", { name: "Sign out", exact: true }).click();
  await expect(settings.getByRole("status")).toHaveText("Sign-out unavailable. Try again.");
  await expect(settings.getByRole("button", { name: "Sign out", exact: true })).toBeEnabled();
  await page.unroute("**/api/auth/logout");
  await settings.getByRole("button", { name: "Sign out", exact: true }).click();
  await expect(settings).not.toBeVisible();
});
