import { expect, test } from "@playwright/test";
import { waitForAppReady } from "./app-ready";

test("realtime session expiry returns to sign in", async ({ page }) => {
  await page.route("**/api/realtime/events?*", (route) =>
    route.fulfill({
      status: 401,
      contentType: "application/json",
      body: '{"error":"session revoked"}',
    }),
  );
  await page.goto("/app");
  await expect(page.getByRole("region", { name: "Sign in" })).toBeVisible();
  await expect(page.getByLabel("Message body")).not.toBeVisible();
});

test("signing in again cannot display the previous account's private content", async ({ page }) => {
  const suffix = Date.now();
  const workspace = (
    await (
      await page.request.post("/api/workspaces", { data: { name: `Reauth ${suffix}` } })
    ).json()
  ).workspace;
  const createChannel = async (name: string, kind: string) =>
    (
      await (
        await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
          data: { name, kind },
        })
      ).json()
    ).channel;
  const first = await createChannel(`private-${suffix}`, "private");
  const next = await createChannel(`next-${suffix}`, "public");
  const privateText = `Previous account private message ${suffix}`;
  expect(
    (
      await page.request.post(`/api/channels/${first.id}/messages`, { data: { body: privateText } })
    ).ok(),
  ).toBe(true);
  const replacement = (
    await (
      await page.request.post("/api/auth/magic/request", {
        data: { email: `replacement-${suffix}@example.com`, display_name: `Replacement ${suffix}` },
      })
    ).json()
  ).token;
  await page.goto(`/app/${workspace.route_id}/${first.route_id}`);
  await waitForAppReady(page);
  await expect(page.getByText(privateText, { exact: true })).toBeVisible();
  await page.route("**/api/routes/**", (route) =>
    route.fulfill({
      status: 401,
      contentType: "application/json",
      body: '{"error":"session expired"}',
    }),
  );
  await page.getByText(next.name, { exact: true }).click();
  await expect(page.getByRole("region", { name: "Sign in" })).toBeVisible();
  await page.unroute("**/api/routes/**");
  let notifyEntered!: () => void;
  const entered = new Promise<void>((resolve) => {
    notifyEntered = resolve;
  });
  let unblock!: () => void;
  const release = new Promise<void>((resolve) => {
    unblock = resolve;
  });
  await page.route("**/api/me", async (route) => {
    notifyEntered();
    await release;
    await route.continue();
  });
  try {
    if (!(await page.getByLabel("Sign-in token").isVisible()))
      await page.getByText("Have a sign-in token?").click();
    await page.getByLabel("Sign-in token").fill(replacement);
    await page.getByRole("button", { name: "Use token", exact: true }).click();
    await entered;
    await expect(page.getByText(privateText, { exact: true })).not.toBeVisible();
  } finally {
    unblock();
  }
  await expect(
    page.getByRole("button", { name: new RegExp(`Account settings for Replacement ${suffix}`) }),
  ).toBeVisible();
  await expect(page.getByText(first.name, { exact: true })).not.toBeVisible();
});
