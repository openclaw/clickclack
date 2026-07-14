import { expect, test, type Page } from "@playwright/test";
import http from "node:http";
import type { AddressInfo } from "node:net";
import { waitForAppReady } from "./app-ready";

async function createWorkspace(page: Page, suffix: string, stamp: number) {
  const response = await page.request.post("/api/workspaces", {
    data: {
      name: `Commands ${suffix} ${stamp}`,
      slug: `commands-${suffix.toLowerCase()}-${stamp}`,
    },
  });
  expect(response.ok()).toBe(true);
  const body = (await response.json()) as {
    workspace: { id: string; route_id: string; slug: string };
  };
  return body.workspace;
}

async function createChannel(page: Page, workspaceID: string, name = "general") {
  const response = await page.request.post(`/api/workspaces/${workspaceID}/channels`, {
    data: { name, kind: "public" },
  });
  expect(response.ok()).toBe(true);
  return ((await response.json()) as { channel: { id: string; name: string } }).channel;
}

async function createBot(page: Page, workspaceID: string, stamp: number, suffix: string) {
  const response = await page.request.post(`/api/workspaces/${workspaceID}/bots`, {
    data: {
      display_name: `Cmd Bot ${suffix} ${stamp}`,
      handle: `cmd-bot-${suffix}-${stamp}`,
      token_name: "e2e",
    },
  });
  expect(response.ok()).toBe(true);
  return (await response.json()) as {
    bot: { id: string; handle: string; display_name: string };
    bot_token: { token: string };
  };
}

async function registerSlashCommand(
  page: Page,
  workspaceID: string,
  botUserID: string,
  command: string,
  callbackURL: string,
  description = "Registered command",
) {
  const response = await page.request.post(`/api/workspaces/${workspaceID}/slash-commands`, {
    data: {
      command,
      description,
      callback_url: callbackURL,
      bot_user_id: botUserID,
    },
  });
  expect(response.ok()).toBe(true);
  return ((await response.json()) as { slash_command: { id: string } }).slash_command;
}

async function setBotCommands(
  page: Page,
  botToken: string,
  commands: { command: string; description: string; args_hint?: string }[],
) {
  const response = await page.request.put("/api/bots/self/commands", {
    headers: { Authorization: `Bearer ${botToken}` },
    data: { commands },
  });
  expect(response.ok()).toBe(true);
}

type SlashProbe = {
  url: string;
  payloads: Record<string, unknown>[];
  received: Promise<void>;
  release: () => void;
  close: () => Promise<void>;
};

async function startSlashProbe(
  reply: {
    response_type: string;
    text: string;
  },
  options: { deferred?: boolean; status?: number } = {},
): Promise<SlashProbe> {
  const payloads: Record<string, unknown>[] = [];
  let resolveReceived: () => void = () => {};
  let resolveRelease: () => void = () => {};
  const received = new Promise<void>((resolve) => {
    resolveReceived = resolve;
  });
  const responseGate = new Promise<void>((resolve) => {
    resolveRelease = resolve;
  });
  const server = http.createServer((request, response) => {
    let raw = "";
    request.on("data", (chunk) => (raw += chunk));
    request.on("end", async () => {
      try {
        payloads.push(JSON.parse(raw) as Record<string, unknown>);
      } catch {
        payloads.push({ raw });
      }
      resolveReceived();
      if (options.deferred) await responseGate;
      response.writeHead(options.status ?? 200, { "content-type": "application/json" });
      response.end(JSON.stringify(reply));
    });
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address() as AddressInfo;
  return {
    url: `http://127.0.0.1:${address.port}/slash`,
    payloads,
    received,
    release: resolveRelease,
    close: () => new Promise<void>((resolve) => server.close(() => resolve())),
  };
}

async function openChannel(page: Page, routeID: string, channelName: string) {
  await page.goto(`/app/${routeID}`);
  await waitForAppReady(page);
  await page.getByRole("link", { name: `# ${channelName}` }).click();
  await expect(page.getByRole("heading", { name: `#${channelName}` })).toBeVisible();
}

test("dispatches a registered slash command through the hook without posting the invocation", async ({
  page,
}) => {
  const stamp = Date.now();
  const workspace = await createWorkspace(page, "Dispatch", stamp);
  const channel = await createChannel(page, workspace.id);
  const { bot } = await createBot(page, workspace.id, stamp, "dispatch");
  const probe = await startSlashProbe({ response_type: "in_channel", text: "deploy started" });
  try {
    await registerSlashCommand(page, workspace.id, bot.id, "/deploy", probe.url);

    await openChannel(page, workspace.route_id, channel.name);
    await page.getByLabel("Message body").fill("/deploy prod");
    await page.getByRole("button", { name: "Send" }).click();

    // The bot's in_channel response lands as a bot message over realtime.
    await expect(page.locator(".markdown").filter({ hasText: "deploy started" })).toBeVisible();
    // Slack semantics: the invocation itself is never posted.
    await expect(page.locator(".markdown").filter({ hasText: "/deploy prod" })).toHaveCount(0);
    // Composer is cleared and ready for the next message.
    await expect(page.getByLabel("Message body")).toHaveValue("");

    expect(probe.payloads).toHaveLength(1);
    expect(probe.payloads[0].command).toBe("/deploy");
    expect(probe.payloads[0].text).toBe("prod");
    expect(probe.payloads[0].channel_id).toBe(channel.id);
  } finally {
    await probe.close();
  }
});

test("shows ephemeral slash responses as a local-only composer notice", async ({ page }) => {
  const stamp = Date.now();
  const workspace = await createWorkspace(page, "Ephemeral", stamp);
  const channel = await createChannel(page, workspace.id);
  const { bot } = await createBot(page, workspace.id, stamp, "ephemeral");
  const probe = await startSlashProbe({ response_type: "ephemeral", text: "you are the captain" });
  try {
    await registerSlashCommand(page, workspace.id, bot.id, "/whoami", probe.url);

    await openChannel(page, workspace.route_id, channel.name);
    await page.getByLabel("Message body").fill("/whoami");
    await page.getByRole("button", { name: "Send" }).click();

    const notice = page.locator(".composer-notice");
    await expect(notice).toBeVisible();
    await expect(notice).toContainText("Only visible to you");
    await expect(notice).toContainText("you are the captain");
    // Ephemeral responses never enter the message stream.
    await expect(page.locator(".markdown").filter({ hasText: "you are the captain" })).toHaveCount(
      0,
    );

    await notice.getByRole("button", { name: "Dismiss notice" }).click();
    await expect(notice).toHaveCount(0);
  } finally {
    await probe.close();
  }
});

test("unregistered slash text falls through to a plain message", async ({ page }) => {
  const stamp = Date.now();
  const workspace = await createWorkspace(page, "Fallback", stamp);
  const channel = await createChannel(page, workspace.id);

  await openChannel(page, workspace.route_id, channel.name);
  await page.getByLabel("Message body").fill("/shrug oh well");
  await page.getByRole("button", { name: "Send" }).click();

  await expect(page.locator(".markdown").filter({ hasText: "/shrug oh well" })).toBeVisible();
  await expect(page.locator(".composer-notice")).toHaveCount(0);
});

test("quoted registered slash text falls through to a quoted plain message", async ({ page }) => {
  const stamp = Date.now();
  const workspace = await createWorkspace(page, "Quoted", stamp);
  const channel = await createChannel(page, workspace.id);
  const { bot } = await createBot(page, workspace.id, stamp, "quoted");
  const probe = await startSlashProbe({ response_type: "in_channel", text: "should not run" });
  try {
    await registerSlashCommand(page, workspace.id, bot.id, "/deploy", probe.url);

    await openChannel(page, workspace.route_id, channel.name);
    await page.getByLabel("Message body").fill("quoted source");
    await page.getByRole("button", { name: "Send" }).click();
    const sourceRow = page.locator(".message-row", {
      has: page.locator(".markdown").filter({ hasText: "quoted source" }),
    });
    await expect(sourceRow).toBeVisible();
    await sourceRow.hover();
    await sourceRow.getByRole("button", { name: "Reply" }).click();

    await page.getByLabel("Message body").fill("/deploy quoted");
    await page.getByRole("button", { name: "Send" }).click();

    const replyRow = page.locator(".message-row", {
      has: page.locator(".markdown").filter({ hasText: "/deploy quoted" }),
    });
    await expect(replyRow).toBeVisible();
    await expect(replyRow.locator(".quote-block")).toContainText("quoted source");
    expect(probe.payloads).toHaveLength(0);
  } finally {
    probe.release();
    await probe.close();
  }
});

test("restores a registered slash draft when callback dispatch fails", async ({ page }) => {
  const stamp = Date.now();
  const workspace = await createWorkspace(page, "Failure", stamp);
  const channel = await createChannel(page, workspace.id);
  const { bot } = await createBot(page, workspace.id, stamp, "failure");
  const probe = await startSlashProbe(
    { response_type: "ephemeral", text: "unavailable" },
    { status: 500 },
  );
  try {
    await registerSlashCommand(page, workspace.id, bot.id, "/broken", probe.url);

    await openChannel(page, workspace.route_id, channel.name);
    await page.getByLabel("Message body").fill("/broken retry-me");
    await page.getByRole("button", { name: "Send" }).click();

    await expect(page.locator(".composer-notice--error")).toBeVisible();
    await expect(page.getByLabel("Message body")).toHaveValue("/broken retry-me");
    await expect(page.locator(".markdown").filter({ hasText: "/broken retry-me" })).toHaveCount(0);
  } finally {
    probe.release();
    await probe.close();
  }
});

test("does not apply delayed slash callback state after a conversation change", async ({
  page,
}) => {
  const stamp = Date.now();
  const workspace = await createWorkspace(page, "Delayed", stamp);
  const firstChannel = await createChannel(page, workspace.id);
  const secondChannel = await createChannel(page, workspace.id, "other");
  const { bot } = await createBot(page, workspace.id, stamp, "delayed");
  const probe = await startSlashProbe(
    { response_type: "ephemeral", text: "late response" },
    { deferred: true },
  );
  try {
    await registerSlashCommand(page, workspace.id, bot.id, "/slow", probe.url);

    await openChannel(page, workspace.route_id, firstChannel.name);
    await page.getByLabel("Message body").fill("/slow");
    await page.getByRole("button", { name: "Send" }).click();
    await probe.received;

    await page.getByRole("link", { name: `# ${secondChannel.name}` }).click();
    await expect(page.getByRole("heading", { name: `#${secondChannel.name}` })).toBeVisible();
    probe.release();
    await expect(page.locator(".composer-notice")).toHaveCount(0);
    await expect(page.getByLabel("Message body")).toHaveValue("");

    await page.getByRole("link", { name: `# ${firstChannel.name}` }).click();
    await expect(page.getByRole("heading", { name: `#${firstChannel.name}` })).toBeVisible();
    await expect(page.locator(".composer-notice")).toHaveCount(0);
    await expect(page.getByLabel("Message body")).toHaveValue("");
  } finally {
    probe.release();
    await probe.close();
  }
});

test("limits direct message command menus to participating bots", async ({ page }) => {
  const stamp = Date.now();
  const workspace = await createWorkspace(page, "Direct", stamp);
  await createChannel(page, workspace.id);
  const participant = await createBot(page, workspace.id, stamp, "party");
  const outsider = await createBot(page, workspace.id, stamp, "other");
  await setBotCommands(page, participant.bot_token.token, [
    { command: "status", description: "Show participant status" },
  ]);
  await setBotCommands(page, outsider.bot_token.token, [
    { command: "private", description: "Outsider-only command" },
  ]);
  await registerSlashCommand(
    page,
    workspace.id,
    participant.bot.id,
    "/deploy",
    "https://example.com/slash",
  );
  const directResponse = await page.request.post("/api/dms", {
    data: { workspace_id: workspace.id, member_ids: [participant.bot.id] },
  });
  expect(directResponse.ok()).toBe(true);
  const { conversation } = (await directResponse.json()) as {
    conversation: { route_id: string };
  };

  await page.goto(`/app/${workspace.route_id}/${conversation.route_id}`);
  await waitForAppReady(page);
  await expect(
    page.getByRole("heading", { name: new RegExp(participant.bot.display_name) }),
  ).toBeVisible();
  await page.getByLabel("Message body").fill("/");

  const suggestions = page.locator(".composer-suggestions button");
  await expect(suggestions).toHaveCount(1);
  await expect(suggestions.filter({ hasText: "/status" })).toContainText(
    `@${participant.bot.handle}`,
  );
  await expect(suggestions.filter({ hasText: "/private" })).toHaveCount(0);
  await expect(suggestions.filter({ hasText: "/deploy" })).toHaveCount(0);
});

test("merges bot-declared command menus into composer autocomplete", async ({ page }) => {
  const stamp = Date.now();
  const workspace = await createWorkspace(page, "Menu", stamp);
  const channel = await createChannel(page, workspace.id);
  const { bot, bot_token: token } = await createBot(page, workspace.id, stamp, "menu");
  const probe = await startSlashProbe({ response_type: "in_channel", text: "ok" });
  let holdNextBotCommandResponse = false;
  let resolveStaleResponse: (fulfill: () => Promise<void>) => void = () => {};
  const staleResponse = new Promise<() => Promise<void>>((resolve) => {
    resolveStaleResponse = resolve;
  });
  try {
    await page.route(`**/api/workspaces/${workspace.id}/bot-commands`, async (route) => {
      if (!holdNextBotCommandResponse) {
        await route.continue();
        return;
      }
      holdNextBotCommandResponse = false;
      const response = await route.fetch();
      resolveStaleResponse(() => route.fulfill({ response }));
    });
    await registerSlashCommand(
      page,
      workspace.id,
      bot.id,
      "/deploy",
      probe.url,
      "Deploy something",
    );
    await setBotCommands(page, token.token, [
      // Collides with the registered command: the registered one must win.
      { command: "deploy", description: "Bot-declared deploy (should lose)" },
      { command: "status", description: "Show agent status", args_hint: "[session]" },
    ]);

    await openChannel(page, workspace.route_id, channel.name);
    const composer = page.getByLabel("Message body");
    await composer.fill("/");

    const suggestions = page.locator(".composer-suggestions button");
    // Registered /deploy wins the collision; bot-declared /status merges in.
    await expect(suggestions).toHaveCount(2);
    const deploy = suggestions.filter({ hasText: "/deploy" });
    await expect(deploy).toContainText("Deploy something");
    await expect(deploy).toContainText("command");
    const statusEntry = suggestions.filter({ hasText: "/status" });
    await expect(statusEntry).toContainText("Show agent status");
    await expect(statusEntry).toContainText("[session]");
    await expect(statusEntry).toContainText(`@${bot.handle}`);

    // Capture an older realtime refresh after the server has returned it, then
    // let a newer refresh complete before releasing the stale response.
    holdNextBotCommandResponse = true;
    await setBotCommands(page, token.token, [{ command: "stale", description: "Stale command" }]);
    const fulfillStaleResponse = await staleResponse;
    await setBotCommands(page, token.token, [
      { command: "restart", description: "Restart the agent" },
    ]);
    await composer.fill("/re");
    await expect(
      page.locator(".composer-suggestions button", { hasText: "/restart" }),
    ).toBeVisible();

    await fulfillStaleResponse();
    await page.evaluate(
      () =>
        new Promise<void>((resolve) =>
          requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
        ),
    );
    await composer.fill("/");
    await expect(
      page.locator(".composer-suggestions button", { hasText: "/restart" }),
    ).toBeVisible();
    await expect(page.locator(".composer-suggestions button", { hasText: "/stale" })).toHaveCount(
      0,
    );
    await expect(page.locator(".composer-suggestions button", { hasText: "/status" })).toHaveCount(
      0,
    );
  } finally {
    await probe.close();
  }
});
