import { expect, type Page } from "@playwright/test";

export async function waitForAppReady(page: Page) {
  // SSR exposes controls before Svelte binds their handlers. Realtime only
  // connects after the client has mounted and started the selected workspace.
  await expect(page.locator('.shell[data-connected="true"]')).toBeVisible();
}
