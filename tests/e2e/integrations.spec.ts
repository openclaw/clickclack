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
  const channelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: "general", kind: "public" },
  });
  expect(channelResponse.ok()).toBe(true);

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
  const row = page.locator(".ws-bots__row", {
    has: page.locator(".ws-bots__row-name", { hasText: /^OpenClaw$/ }),
  });
  await expect(row).toBeVisible();
  await expect(page.getByText("1 app installed")).toBeVisible();

  // The manifest remains reusable: install a second independent OpenClaw agent.
  await page.getByRole("button", { name: "Add app" }).click();
  await page.locator(".ws-intg__catalog-card", { hasText: "OpenClaw" }).click();
  await page.getByRole("textbox", { name: "Display name" }).fill("OpenClaw Secondary");
  await page.getByRole("textbox", { name: "Handle" }).fill(`openclaw-secondary-${stamp}`);
  await page.getByRole("button", { name: "Continue" }).click();
  await page.getByRole("button", { name: "Install", exact: true }).click();
  await expect(page.getByText("Your new token is ready")).toBeVisible();
  await page.getByText("I've copied this token somewhere safe.").click();
  await page.getByRole("button", { name: "Done" }).click();
  await expect(page.getByText("2 apps installed")).toBeVisible();
  await expect(
    page.locator(".ws-bots__row", {
      has: page.locator(".ws-bots__row-name", { hasText: /^OpenClaw Secondary$/ }),
    }),
  ).toBeVisible();

  // Uninstall the second install with the cascade confirm. Installing auto-expanded the row.
  await page.getByRole("button", { name: "Uninstall", exact: true }).click();
  await expect(
    page.getByText("Uninstalling revokes 0 slash commands", { exact: false }),
  ).toBeVisible();
  await page.getByText("Also revoke the bot's", { exact: false }).click();
  await page.getByRole("button", { name: "Uninstall app" }).click();
  await expect(page.getByText("1 app installed")).toBeVisible();

  // The first OpenClaw installation is independent and can be removed separately.
  await row.locator(".ws-bots__row-main").click();
  await page.getByRole("button", { name: "Uninstall", exact: true }).click();
  await page.getByText("Also revoke the bot's", { exact: false }).click();
  await page.getByRole("button", { name: "Uninstall app" }).click();
  await expect(page.getByText("No apps installed", { exact: false })).toBeVisible();
});

test("keeps successful integration data when one initial request fails", async ({ page }) => {
  const stamp = Date.now();
  const workspace = await createWorkspace(page, "Partial", stamp);
  const botResponse = await page.request.post(`/api/workspaces/${workspace.id}/bots`, {
    data: {
      display_name: `Partial Bot ${stamp}`,
      handle: `partial-bot-${stamp}`,
    },
  });
  expect(botResponse.ok()).toBe(true);
  const { bot } = (await botResponse.json()) as { bot: { id: string } };
  const installResponse = await page.request.post(
    `/api/workspaces/${workspace.id}/app-installations`,
    {
      data: {
        app_slug: "custom",
        display_name: `Partial App ${stamp}`,
        bot_user_id: bot.id,
        config: {},
      },
    },
  );
  expect(installResponse.ok()).toBe(true);

  await page.route(`**/api/workspaces/${workspace.id}/connected-accounts`, async (route) => {
    await route.fulfill({ status: 500, json: { error: "forced account failure" } });
  });
  await page.route(`**/api/workspaces/${workspace.id}/slash-commands`, async (route) => {
    await route.fulfill({ status: 500, json: { error: "forced command failure" } });
  });
  await page.goto(`/app/${workspace.route_id}/settings/integrations`);

  await expect(
    page.getByText("Some integration data could not be loaded", { exact: false }),
  ).toBeVisible();
  await expect(page.getByText(`Partial App ${stamp}`, { exact: true })).toBeVisible();
  await expect(page.getByText("1 app installed")).toBeVisible();
  await expect(
    page.getByText("Connected accounts are unavailable", { exact: false }),
  ).toBeVisible();
  await page
    .locator(".ws-bots__row", { hasText: `Partial App ${stamp}` })
    .locator(".ws-bots__row-main")
    .click();
  await expect(page.getByRole("button", { name: "Uninstall", exact: true })).toBeDisabled();
});

test("requires installation data before adding an app", async ({ page }) => {
  const stamp = Date.now();
  const workspace = await createWorkspace(page, "InstallData", stamp);
  await page.route(`**/api/workspaces/${workspace.id}/app-installations`, async (route) => {
    await route.fulfill({ status: 500, json: { error: "forced installation failure" } });
  });

  await page.goto(`/app/${workspace.route_id}/settings/integrations`);

  await expect(
    page.getByText("Some integration data could not be loaded", { exact: false }),
  ).toBeVisible();
  const addApp = page.getByRole("button", { name: "Add app" });
  await expect(addApp).toBeDisabled();
  await expect(addApp).toHaveAttribute(
    "title",
    "Refresh before adding an app so installation, bot, and channel data is current.",
  );
});

test("retries lost setup responses without duplicate resources", async ({ page }) => {
  const stamp = Date.now();
  const workspace = await createWorkspace(page, "Retry", stamp);
  const channelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: "general", kind: "public" },
  });
  expect(channelResponse.ok()).toBe(true);

  let botAttempts = 0;
  await page.route(`**/api/workspaces/${workspace.id}/bots`, async (route) => {
    if (route.request().method() !== "POST" || botAttempts++ > 0) {
      await route.continue();
      return;
    }
    const response = await route.fetch();
    expect(response.ok()).toBe(true);
    await route.fulfill({ status: 502, json: { error: "lost bot response" } });
  });

  let installationAttempts = 0;
  await page.route(`**/api/workspaces/${workspace.id}/app-installations`, async (route) => {
    if (route.request().method() !== "POST" || installationAttempts++ > 0) {
      await route.continue();
      return;
    }
    const response = await route.fetch();
    expect(response.ok()).toBe(true);
    await route.fulfill({ status: 502, json: { error: "lost installation response" } });
  });

  await page.goto(`/app/${workspace.route_id}/settings/integrations`);
  await page.getByRole("button", { name: "Add app" }).click();
  await page.locator(".ws-intg__catalog-card", { hasText: "OpenClaw" }).click();
  await page.getByRole("textbox", { name: "Display name" }).fill(`Retry Agent ${stamp}`);
  await page.getByRole("textbox", { name: "Handle" }).fill(`retry-agent-${stamp}`);
  await page.getByRole("button", { name: "Continue" }).click();

  await page.getByRole("button", { name: "Install", exact: true }).click();
  await expect(page.getByText("Retrying reuses the same bot and token row.")).toBeVisible();
  await expect(page.getByRole("button", { name: "Retry install" })).toBeVisible();
  await expect(page.getByRole("combobox", { name: "Default channel" })).toBeDisabled();

  await page.getByRole("button", { name: "Retry install" }).click();
  await expect(
    page.getByText("retrying reuses them and the same installation request", { exact: false }),
  ).toBeVisible();

  await page.getByRole("button", { name: "Retry install" }).click();
  await expect(page.getByText("Your new token is ready")).toBeVisible();

  const botsResponse = await page.request.get(`/api/workspaces/${workspace.id}/bots`);
  expect(botsResponse.ok()).toBe(true);
  const botsBody = (await botsResponse.json()) as {
    bots: Array<{ bot: { handle: string }; tokens: Array<{ revoked_at?: string }> }>;
  };
  const retryBots = botsBody.bots.filter((entry) => entry.bot.handle === `retry-agent-${stamp}`);
  expect(retryBots).toHaveLength(1);
  expect(retryBots[0]?.tokens).toHaveLength(1);
  expect(retryBots[0]?.tokens[0]?.revoked_at).toBeUndefined();

  const installationsResponse = await page.request.get(
    `/api/workspaces/${workspace.id}/app-installations`,
  );
  expect(installationsResponse.ok()).toBe(true);
  const installationsBody = (await installationsResponse.json()) as {
    app_installations: Array<{ bot_user_id: string }>;
  };
  expect(installationsBody.app_installations).toHaveLength(1);
});

test("does not apply refresh snapshots older than a local install", async ({ page }) => {
  const stamp = Date.now();
  const workspace = await createWorkspace(page, "RefreshRace", stamp);

  let captureNextInstallations = false;
  let releaseSnapshot: (() => void) | undefined;
  const snapshotReleased = new Promise<void>((resolve) => {
    releaseSnapshot = resolve;
  });
  let snapshotCaptured: (() => void) | undefined;
  const captured = new Promise<void>((resolve) => {
    snapshotCaptured = resolve;
  });

  await page.goto(`/app/${workspace.route_id}/settings/integrations`);
  await expect(page.getByRole("heading", { name: "Integrations" })).toBeVisible();
  await page.route(`**/api/workspaces/${workspace.id}/app-installations`, async (route) => {
    if (route.request().method() !== "GET" || !captureNextInstallations) {
      await route.continue();
      return;
    }
    captureNextInstallations = false;
    const response = await route.fetch();
    snapshotCaptured?.();
    await snapshotReleased;
    await route.fulfill({ response });
  });

  captureNextInstallations = true;
  await page.getByRole("button", { name: "Refresh" }).click();
  await captured;

  await page.getByRole("button", { name: "Add app" }).click();
  await page.locator(".ws-intg__catalog-card", { hasText: "Custom app" }).click();
  await page.getByRole("textbox", { name: "Display name" }).fill(`Refresh Race ${stamp}`);
  await page.getByRole("textbox", { name: "Handle" }).fill(`refresh-race-${stamp}`);
  await page.getByRole("button", { name: "Install", exact: true }).click();
  await expect(page.getByText("Your new token is ready")).toBeVisible();
  await expect(page.getByText("1 app installed")).toBeVisible();

  releaseSnapshot?.();
  await expect(page.locator('button[title="Refresh"]')).toBeEnabled();
  await expect(page.getByText("1 app installed")).toBeVisible();
  await expect(page.getByText(`Refresh Race ${stamp}`, { exact: true })).toBeVisible();
});

test("keeps each registration busy until its own secret rotation finishes", async ({ page }) => {
  const stamp = Date.now();
  const workspace = await createWorkspace(page, "SecretRace", stamp);
  const botResponse = await page.request.post(`/api/workspaces/${workspace.id}/bots`, {
    data: {
      display_name: `Secret Race Bot ${stamp}`,
      handle: `secret-race-bot-${stamp}`,
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
        display_name: `Secret Race App ${stamp}`,
        bot_user_id: bot.id,
        config: {},
      },
    },
  );
  expect(installResponse.ok()).toBe(true);
  const { app_installation: installation } = (await installResponse.json()) as {
    app_installation: { id: string };
  };

  async function createCommand(command: string) {
    const response = await page.request.post(`/api/workspaces/${workspace.id}/slash-commands`, {
      data: {
        app_installation_id: installation.id,
        command,
        callback_url: `https://example.com/${command}`,
        bot_user_id: bot.id,
      },
    });
    expect(response.ok()).toBe(true);
    return ((await response.json()) as { slash_command: { id: string } }).slash_command;
  }

  async function createSubscription(suffix: string) {
    const response = await page.request.post(
      `/api/workspaces/${workspace.id}/event-subscriptions`,
      {
        data: {
          app_installation_id: installation.id,
          event_types: ["message.created"],
          callback_url: `https://example.com/events/${suffix}`,
        },
      },
    );
    expect(response.ok()).toBe(true);
    return ((await response.json()) as { event_subscription: { id: string } }).event_subscription;
  }

  const firstCommand = await createCommand(`first-${stamp}`);
  await createCommand(`second-${stamp}`);
  const firstSubscription = await createSubscription(`first-${stamp}`);
  await createSubscription(`second-${stamp}`);

  await page.goto(`/app/${workspace.route_id}/settings/integrations`);
  await page
    .locator(".ws-bots__row", { hasText: `Secret Race App ${stamp}` })
    .locator(".ws-bots__row-main")
    .click();
  page.on("dialog", (dialog) => void dialog.accept());

  const slashPanel = page.locator(".ws-intg__panel", { hasText: "Slash commands" });
  const firstCommandRow = slashPanel.locator(".ws-intg__item-row", {
    hasText: `/first-${stamp}`,
  });
  const secondCommandRow = slashPanel.locator(".ws-intg__item-row", {
    hasText: `/second-${stamp}`,
  });
  let releaseCommand: (() => void) | undefined;
  const commandReleased = new Promise<void>((resolve) => {
    releaseCommand = resolve;
  });
  let captureCommand: (() => void) | undefined;
  const commandCaptured = new Promise<void>((resolve) => {
    captureCommand = resolve;
  });
  await page.route(`**/api/slash-commands/${firstCommand.id}/rotate-secret`, async (route) => {
    const response = await route.fetch();
    captureCommand?.();
    await commandReleased;
    await route.fulfill({ response });
  });

  await firstCommandRow.getByRole("button", { name: "Rotate secret" }).click();
  await commandCaptured;
  await secondCommandRow.getByRole("button", { name: "Rotate secret" }).click();
  await expect(secondCommandRow.locator(".ws-intg__secret")).toBeVisible();
  await expect(firstCommandRow.getByRole("button", { name: "Rotate secret" })).toBeDisabled();
  await expect(firstCommandRow.getByRole("button", { name: "Revoke" })).toBeDisabled();
  releaseCommand?.();
  await expect(firstCommandRow.locator(".ws-intg__secret")).toBeVisible();
  await expect(firstCommandRow.getByRole("button", { name: "Rotate secret" })).toBeEnabled();

  const subscriptionsPanel = page.locator(".ws-intg__panel", {
    hasText: "Event subscriptions",
  });
  const firstSubscriptionRow = subscriptionsPanel.locator(".ws-intg__item-row", {
    hasText: `https://example.com/events/first-${stamp}`,
  });
  const secondSubscriptionRow = subscriptionsPanel.locator(".ws-intg__item-row", {
    hasText: `https://example.com/events/second-${stamp}`,
  });
  let releaseSubscription: (() => void) | undefined;
  const subscriptionReleased = new Promise<void>((resolve) => {
    releaseSubscription = resolve;
  });
  let captureSubscription: (() => void) | undefined;
  const subscriptionCaptured = new Promise<void>((resolve) => {
    captureSubscription = resolve;
  });
  await page.route(
    `**/api/event-subscriptions/${firstSubscription.id}/rotate-secret`,
    async (route) => {
      const response = await route.fetch();
      captureSubscription?.();
      await subscriptionReleased;
      await route.fulfill({ response });
    },
  );

  await firstSubscriptionRow.getByRole("button", { name: "Rotate secret" }).click();
  await subscriptionCaptured;
  await secondSubscriptionRow.getByRole("button", { name: "Rotate secret" }).click();
  await expect(secondSubscriptionRow.locator(".ws-intg__secret")).toBeVisible();
  await expect(firstSubscriptionRow.getByRole("button", { name: "Rotate secret" })).toBeDisabled();
  await expect(firstSubscriptionRow.getByRole("button", { name: "Revoke" })).toBeDisabled();
  releaseSubscription?.();
  await expect(firstSubscriptionRow.locator(".ws-intg__secret")).toBeVisible();
  await expect(firstSubscriptionRow.getByRole("button", { name: "Rotate secret" })).toBeEnabled();
});

test("keeps each connected account busy until its own disconnect finishes", async ({ page }) => {
  const stamp = Date.now();
  const workspace = await createWorkspace(page, "AccountRace", stamp);
  const meResponse = await page.request.get("/api/me");
  expect(meResponse.ok()).toBe(true);
  const { user } = (await meResponse.json()) as { user: { id: string } };

  async function createAccount(suffix: string) {
    const response = await page.request.post(`/api/workspaces/${workspace.id}/connected-accounts`, {
      data: {
        user_id: user.id,
        provider: "github",
        provider_account_id: `${suffix}-${stamp}`,
        display_name: `${suffix} Account ${stamp}`,
      },
    });
    expect(response.ok()).toBe(true);
    return ((await response.json()) as { connected_account: { id: string } }).connected_account;
  }

  const firstAccount = await createAccount("First");
  await createAccount("Second");
  await page.goto(`/app/${workspace.route_id}/settings/integrations`);
  page.on("dialog", (dialog) => void dialog.accept());

  const firstRow = page.locator(".ws-intg__item-row", {
    hasText: `First Account ${stamp}`,
  });
  const secondRow = page.locator(".ws-intg__item-row", {
    hasText: `Second Account ${stamp}`,
  });
  let releaseFirst: (() => void) | undefined;
  const firstReleased = new Promise<void>((resolve) => {
    releaseFirst = resolve;
  });
  let captureFirst: (() => void) | undefined;
  const firstCaptured = new Promise<void>((resolve) => {
    captureFirst = resolve;
  });
  await page.route(`**/api/connected-accounts/${firstAccount.id}/revoke`, async (route) => {
    const response = await route.fetch();
    captureFirst?.();
    await firstReleased;
    await route.fulfill({ response });
  });

  await firstRow.getByRole("button", { name: "Disconnect" }).click();
  await firstCaptured;
  await secondRow.getByRole("button", { name: "Disconnect" }).click();
  await expect(secondRow).toHaveCount(0);
  await expect(firstRow.getByRole("button", { name: "Disconnect" })).toBeDisabled();

  releaseFirst?.();
  await expect(firstRow).toHaveCount(0);
});

test("requires a real active channel for OpenClaw installs", async ({ page }) => {
  const stamp = Date.now();
  const workspace = await createWorkspace(page, "NoChannels", stamp);
  await page.route(`**/api/workspaces/${workspace.id}/channels`, async (route) => {
    await route.fulfill({ json: { channels: [] } });
  });

  await page.goto(`/app/${workspace.route_id}/settings/integrations`);
  await page.getByRole("button", { name: "Add app" }).click();
  await page.locator(".ws-intg__catalog-card", { hasText: "OpenClaw" }).click();
  await page.getByRole("button", { name: "Continue" }).click();

  await expect(
    page.getByText("Create or restore a channel before installing this app."),
  ).toBeVisible();
  await expect(page.getByRole("button", { name: "Install", exact: true })).toBeDisabled();
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

  // Later list failures must not hide successful mutation responses or one-time secrets.
  await page.route(`**/api/workspaces/${workspace.id}/slash-commands`, async (route) => {
    if (route.request().method() === "GET") {
      await route.fulfill({ status: 500, json: { error: "forced command list failure" } });
      return;
    }
    await route.continue();
  });
  await page.route(`**/api/workspaces/${workspace.id}/event-subscriptions`, async (route) => {
    if (route.request().method() === "GET") {
      await route.fulfill({ status: 500, json: { error: "forced subscription list failure" } });
      return;
    }
    await route.continue();
  });

  // Slash command: create, secret reveals once, rotate mints a fresh one.
  await page.getByRole("button", { name: "Add command" }).click();
  await page.getByRole("textbox", { name: "Command", exact: true }).fill("//deploy");
  await page.getByRole("textbox", { name: "Description" }).fill("Deploy from e2e");
  await page.getByRole("textbox", { name: "Callback URL" }).fill("https://example.com/hooks/e2e");
  await page.getByRole("button", { name: "Create command" }).click();
  await expect(page.getByText("/deploy", { exact: true })).toBeVisible();
  await expect(page.getByText("//deploy", { exact: true })).toHaveCount(0);
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
  const subscriptionResponsePromise = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      response.url().endsWith(`/api/workspaces/${workspace.id}/event-subscriptions`),
  );
  await page.getByRole("button", { name: "Create subscription" }).click();
  const subscriptionResponse = await subscriptionResponsePromise;
  const { event_subscription: subscription } = (await subscriptionResponse.json()) as {
    event_subscription: { id: string };
  };
  await expect(page.getByText("https://example.com/events/e2e")).toBeVisible();
  const subscriptionSecret = page.locator(".ws-intg__secret").first();
  await expect(subscriptionSecret).toContainText("visible once");
  await subscriptionSecret.getByRole("button", { name: "Done" }).click();

  let deliveryRequestCount = 0;
  await page.route(`**/api/event-subscriptions/${subscription.id}/deliveries**`, async (route) => {
    deliveryRequestCount += 1;
    if (deliveryRequestCount === 1) {
      await route.fulfill({
        json: {
          deliveries: [
            {
              id: "eda_e2e_first",
              subscription_id: subscription.id,
              event_id: "evt_e2e_first",
              workspace_id: workspace.id,
              event_type: "message.created",
              attempt: 1,
              response_status: 204,
              created_at: "2026-07-14T10:00:00Z",
              completed_at: "2026-07-14T10:00:01Z",
            },
          ],
          next_cursor: "eda_e2e_first",
        },
      });
      return;
    }
    await route.fulfill({ status: 500, json: { error: "forced delivery page failure" } });
  });

  await page.getByRole("button", { name: "Deliveries", exact: true }).click();
  await expect(page.getByText("message.created", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Load older deliveries" }).click();
  await expect(page.getByText("forced delivery page failure", { exact: false })).toBeVisible();
  await expect(page.getByText("message.created", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Retry" })).toBeVisible();

  // Revoke the command; it leaves the list.
  page.once("dialog", (dialog) => void dialog.accept());
  await page.getByRole("button", { name: "Revoke", exact: true }).first().click();
  await expect(page.getByText("/deploy", { exact: true })).toHaveCount(0);
});
