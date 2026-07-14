import { expect, test, type Page } from "@playwright/test";

async function createWorkspace(page: Page, suffix: string, stamp: number) {
  const response = await page.request.post("/api/workspaces", {
    data: {
      name: `Integrations ${suffix} ${stamp}`,
      slug: `integrations-${suffix.toLowerCase()}-${stamp}`,
    },
  });
  expect(response.ok()).toBe(true);
  const body = (await response.json()) as {
    workspace: { id: string; route_id: string; slug: string };
  };
  return body.workspace;
}

test("installs an OpenClaw app through the wizard and uninstalls with cascade", async ({
  page,
}) => {
  const stamp = Date.now();
  const workspace = await createWorkspace(page, "Wizard", stamp);

  await page.goto(`/app/${workspace.route_id}/settings/integrations`);
  await expect(page.getByRole("heading", { name: "Integrations" })).toBeVisible();
  await expect(page.getByText("No apps installed", { exact: false })).toBeVisible();

  await page.getByRole("button", { name: "Add app" }).click();
  await page.locator(".ws-intg__catalog-card", { hasText: "OpenClaw" }).click();

  // Bot step: manifest prefills the identity; keep the defaults.
  const displayName = page.getByRole("textbox", { name: "Display name" });
  await expect(displayName).toHaveValue("OpenClaw");
  await page.getByRole("button", { name: "Continue" }).click();

  // Config step: default channel + agent activity opt-in.
  await expect(page.getByText("Default channel")).toBeVisible();
  await page.getByText("Stream agent activity").click();
  await page.getByRole("button", { name: "Install", exact: true }).click();

  // Reveal: one-time token plus an OpenClaw config with the chosen options.
  await expect(page.getByText("Your new token is ready")).toBeVisible();
  const snippet = page.locator(".ws-bots__reveal-snippet").first();
  await expect(snippet).toContainText(`workspace: "${workspace.slug}"`);
  await expect(snippet).toContainText("agentActivity: true");
  await expect(page.getByText("agent_activity:write")).toBeVisible();
  await page.getByText("I've copied this token somewhere safe.").click();
  await page.getByRole("button", { name: "Done" }).click();

  // Installed row is live.
  const row = page.locator(".ws-bots__row", { hasText: "OpenClaw" });
  await expect(row).toBeVisible();
  await expect(page.getByText("1 app installed")).toBeVisible();

  // Uninstall with the cascade confirm. Installing auto-expanded the row.
  await page.getByRole("button", { name: "Uninstall", exact: true }).click();
  await expect(
    page.getByText("Uninstalling revokes 0 slash commands", { exact: false }),
  ).toBeVisible();
  await page.getByText("Also revoke the bot's", { exact: false }).click();
  await page.getByRole("button", { name: "Uninstall app" }).click();
  await expect(page.getByText("No apps installed", { exact: false })).toBeVisible();
});

test("manages slash commands, event subscriptions, and deliveries on an installation", async ({
  page,
}) => {
  const stamp = Date.now();
  const workspace = await createWorkspace(page, "Hooks", stamp);

  const botResponse = await page.request.post(`/api/workspaces/${workspace.id}/bots`, {
    data: {
      display_name: `Hooks Bot ${stamp}`,
      handle: `hooks-bot-${stamp}`,
      token_name: "e2e",
    },
  });
  expect(botResponse.ok()).toBe(true);
  const { bot } = (await botResponse.json()) as { bot: { id: string } };

  const installResponse = await page.request.post(
    `/api/workspaces/${workspace.id}/app-installations`,
    {
      data: {
        app_slug: "custom",
        display_name: `Hooks App ${stamp}`,
        bot_user_id: bot.id,
        config: {},
      },
    },
  );
  expect(installResponse.ok()).toBe(true);

  await page.goto(`/app/${workspace.route_id}/settings/integrations`);
  const row = page.locator(".ws-bots__row", { hasText: `Hooks App ${stamp}` });
  await row.locator(".ws-bots__row-main").click();

  // Slash command: create, secret reveals once, rotate mints a fresh one.
  await page.getByRole("button", { name: "Add command" }).click();
  await page.getByRole("textbox", { name: "Command", exact: true }).fill("deploy");
  await page.getByRole("textbox", { name: "Description" }).fill("Deploy from e2e");
  await page.getByRole("textbox", { name: "Callback URL" }).fill("https://example.com/hooks/e2e");
  await page.getByRole("button", { name: "Create command" }).click();
  await expect(page.getByText("/deploy")).toBeVisible();
  const commandSecret = page.locator(".ws-intg__secret").first();
  await expect(commandSecret).toContainText("visible once");
  const firstSecret = await commandSecret.locator("input").inputValue();
  expect(firstSecret.length).toBeGreaterThan(10);
  await commandSecret.getByRole("button", { name: "Done" }).click();
  await expect(page.locator(".ws-intg__secret")).toHaveCount(0);

  page.once("dialog", (dialog) => void dialog.accept());
  await page.getByRole("button", { name: "Rotate secret" }).first().click();
  const rotatedSecret = page.locator(".ws-intg__secret").first();
  await expect(rotatedSecret).toContainText("visible once");
  const secondSecret = await rotatedSecret.locator("input").inputValue();
  expect(secondSecret.length).toBeGreaterThan(10);
  expect(secondSecret).not.toBe(firstSecret);
  await rotatedSecret.getByRole("button", { name: "Done" }).click();

  // Event subscription on all events, then the (empty) delivery log.
  await page.getByRole("button", { name: "Add subscription" }).click();
  await page.getByRole("textbox", { name: "Callback URL" }).fill("https://example.com/events/e2e");
  await page.getByText("All events", { exact: false }).click();
  await page.getByRole("button", { name: "Create subscription" }).click();
  await expect(page.getByText("https://example.com/events/e2e")).toBeVisible();
  const subscriptionSecret = page.locator(".ws-intg__secret").first();
  await expect(subscriptionSecret).toContainText("visible once");
  await subscriptionSecret.getByRole("button", { name: "Done" }).click();

  await page.getByRole("button", { name: "Deliveries", exact: true }).click();
  await expect(page.getByText("No deliveries yet", { exact: false })).toBeVisible();

  // Revoke the command; it leaves the list.
  page.once("dialog", (dialog) => void dialog.accept());
  await page.getByRole("button", { name: "Revoke", exact: true }).first().click();
  await expect(page.getByText("/deploy")).toHaveCount(0);
});
