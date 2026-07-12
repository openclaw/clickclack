import { expect, test, type Page } from "@playwright/test";
import { execFile, execFileSync } from "node:child_process";
import { randomUUID } from "node:crypto";
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { promisify } from "node:util";
import {
  buildOpenClawConfigSnippet,
  buildOpenClawShellSnippet,
  openClawWorkspaceIdentifier,
} from "../../apps/web/src/lib/bots";
import { productAppURLForHost } from "../../apps/web/src/productLinks";

const serverURL = "http://127.0.0.1:18082";
const execFileAsync = promisify(execFile);
const goCacheEnv = {
  GOCACHE: execFileSync("go", ["env", "GOCACHE"], { cwd: process.cwd(), encoding: "utf8" }).trim(),
  GOMODCACHE: execFileSync("go", ["env", "GOMODCACHE"], {
    cwd: process.cwd(),
    encoding: "utf8",
  }).trim(),
};

function clickclack(args: string[], input?: string, env: NodeJS.ProcessEnv = {}): string {
  return execFileSync("go", ["run", "./apps/api/cmd/clickclack", ...args], {
    cwd: process.cwd(),
    encoding: "utf8",
    env: { ...process.env, ...env },
    input,
  }).trim();
}

async function clickclackAsync(args: string[], env: NodeJS.ProcessEnv = {}): Promise<string> {
  const { stdout } = await execFileAsync("go", ["run", "./apps/api/cmd/clickclack", ...args], {
    cwd: process.cwd(),
    encoding: "utf8",
    env: { ...process.env, ...env },
  });
  return stdout.trim();
}

function isolatedHome(): NodeJS.ProcessEnv {
  const root = mkdtempSync(join(tmpdir(), "clickclack-e2e-"));
  return {
    ...goCacheEnv,
    HOME: root,
    XDG_CONFIG_HOME: join(root, ".config"),
  };
}

async function settleScrollFrames(page: Page) {
  await page.evaluate(
    () =>
      new Promise<void>((resolve) => {
        let frames = 12;
        const step = () => {
          frames--;
          if (frames <= 0) {
            resolve();
            return;
          }
          requestAnimationFrame(step);
        };
        requestAnimationFrame(step);
      }),
  );
}

async function expectMessageNearScrollBottom(page: Page, text: string) {
  await expect
    .poll(() =>
      page.locator(".messages-scroll").evaluate((el, messageText) => {
        const row = [...el.querySelectorAll<HTMLElement>("[data-message-id]")].find((item) =>
          item.textContent?.includes(messageText),
        );
        if (!row) return Number.POSITIVE_INFINITY;
        const group = row.closest<HTMLElement>(".message-group");
        const viewport = el.getBoundingClientRect();
        const rect = (group || row).getBoundingClientRect();
        return Math.abs(viewport.bottom - rect.bottom);
      }, text),
    )
    .toBeLessThanOrEqual(20);
}

async function expectMessageNearComposer(page: Page, text: string) {
  await expect
    .poll(() =>
      page.locator(".timeline").evaluate((el, messageText) => {
        const row = [...el.querySelectorAll<HTMLElement>("[data-message-id]")].find((item) =>
          item.textContent?.includes(messageText),
        );
        // Measure proximity to the composer dock, not the inner input card. The
        // dock reserves a fixed agent-responding band above the card. That
        // chrome is intentional dead space, not distance
        // between the newest message and the composer area, so anchoring to the
        // dock keeps this assertion about "is the message pinned to the
        // composer?" instead of tracking composer-internal chrome height.
        const composer =
          el.querySelector<HTMLElement>(".composer-dock") ??
          el.querySelector<HTMLElement>(".composer-card");
        if (!row || !composer) return Number.POSITIVE_INFINITY;
        const group = row.closest<HTMLElement>(".message-group");
        const messageRect = (group || row).getBoundingClientRect();
        const composerRect = composer.getBoundingClientRect();
        return composerRect.top - messageRect.bottom;
      }, text),
    )
    .toBeLessThanOrEqual(24);
}

async function expectScrollAtMessageEnd(page: Page) {
  await expect
    .poll(() =>
      page.locator(".messages-scroll").evaluate((el) => {
        const distance = el.scrollHeight - el.scrollTop - el.clientHeight;
        return distance <= 36;
      }),
    )
    .toBe(true);
}

type GeometryBox = {
  left: number;
  right: number;
  top: number;
  bottom: number;
  width: number;
  height: number;
};

type MobileGeometry = {
  viewportWidth: number;
  viewportHeight: number;
  scrollWidth: number;
  rail: GeometryBox;
  sidebar: GeometryBox;
  timeline: GeometryBox;
  toolbar: GeometryBox;
  composer: GeometryBox;
  toggle: GeometryBox;
  firstGuild: GeometryBox;
  textareaFontSize: number;
  toolbarOverflowX: string;
};

async function mobileGeometry(page: Page): Promise<MobileGeometry> {
  return page.evaluate(() => {
    const box = (selector: string): GeometryBox => {
      const element = document.querySelector<HTMLElement>(selector);
      if (!element) throw new Error(`missing element ${selector}`);
      const rect = element.getBoundingClientRect();
      return {
        left: rect.left,
        right: rect.right,
        top: rect.top,
        bottom: rect.bottom,
        width: rect.width,
        height: rect.height,
      };
    };
    const textarea = document.querySelector<HTMLTextAreaElement>(
      'textarea[aria-label="Message body"]',
    );
    const toolbar = document.querySelector<HTMLElement>(".composer-toolbar");
    if (!textarea || !toolbar) throw new Error("missing composer controls");
    return {
      viewportWidth: window.innerWidth,
      viewportHeight: window.innerHeight,
      scrollWidth: document.documentElement.scrollWidth,
      rail: box(".guild-rail"),
      sidebar: box(".sidebar"),
      timeline: box(".timeline"),
      toolbar: box(".composer-toolbar"),
      composer: box(".composer"),
      toggle: box(".mobile-nav-toggle"),
      firstGuild: box(".guild-rail .guild.home"),
      textareaFontSize: Number.parseFloat(getComputedStyle(textarea).fontSize),
      toolbarOverflowX: getComputedStyle(toolbar).overflowX,
    };
  });
}

test("product website links to app and docs", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: /Team chat for humans/ })).toBeVisible();
  await expect(page.locator(".product-site")).toHaveCSS("display", "block");
  for (const openApp of await page.getByRole("link", { name: "Open app" }).all()) {
    await expect(openApp).toHaveAttribute("href", "/app");
  }
  await expect(page.getByRole("link", { name: "Read the docs" })).toHaveAttribute(
    "href",
    "https://docs.clickclack.chat",
  );
  await expect(page.getByText("Open source · MIT · Single Go binary")).toBeVisible();
});

test("self-hosted product website links stay on the local app route", async ({ page }) => {
  await page.goto("http://selfhost.localhost:18082/");
  await expect(page.getByRole("heading", { name: /Team chat for humans/ })).toBeVisible();
  for (const openApp of await page.getByRole("link", { name: "Open app" }).all()) {
    await expect(openApp).toHaveAttribute("href", "/app");
  }
});

test("product website app URL host routing", () => {
  expect(productAppURLForHost("clickclack.chat")).toBe("https://app.clickclack.chat");
  expect(productAppURLForHost("www.clickclack.chat")).toBe("https://app.clickclack.chat");
  expect(productAppURLForHost("CLICKCLACK.CHAT")).toBe("https://app.clickclack.chat");
  expect(productAppURLForHost("localhost")).toBe("/app");
  expect(productAppURLForHost("127.0.0.1")).toBe("/app");
  expect(productAppURLForHost("::1")).toBe("/app");
  expect(productAppURLForHost("selfhost.localhost")).toBe("/app");
  expect(productAppURLForHost("ixandru.tail75b497.ts.net")).toBe("/app");
  expect(productAppURLForHost("clickclack.lan")).toBe("/app");
  expect(productAppURLForHost("chat.example.com")).toBe("/app");
});

test("OpenClaw install snippets use supported workspace identifiers", () => {
  const workspace = openClawWorkspaceIdentifier({
    id: "wsp_01test",
    slug: "team-chat",
  });
  const config = buildOpenClawConfigSnippet({
    workspace,
    botHandle: "release-bot",
    botUserID: "usr_01bot",
    mode: "single",
    baseURL: "https://chat.example.com",
  });
  expect(config).toContain('workspace: "team-chat"');
  expect(config).not.toContain("route_id");

  const shell = buildOpenClawShellSnippet({
    botHandle: "release-bot",
    token: "ccb_test'value",
    mode: "single",
  });
  expect(shell).toContain(`export CLICKCLACK_BOT_TOKEN='ccb_test'"'"'value'`);
});

test("channels can be reordered accessibly and persist locally", async ({ page, browser }) => {
  const suffix = randomUUID().replaceAll("-", "").slice(0, 12);
  const workspaceResponse = await page.request.post("/api/workspaces", {
    data: { name: `Channel order ${suffix}` },
  });
  expect(workspaceResponse.ok()).toBe(true);
  const { workspace } = (await workspaceResponse.json()) as {
    workspace: { id: string; route_id: string };
  };

  const names = [`aa-order-${suffix}`, `mm-order-${suffix}`, `zz-order-${suffix}`];
  for (const name of names) {
    const response = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
      data: { name, kind: "public" },
    });
    expect(response.ok()).toBe(true);
  }

  await page.goto(`/app/${workspace.route_id}`);
  const channelNames = (targetPage: Page) =>
    targetPage
      .locator("#sidebar-channels-list")
      .locator("a.channel .nav-label")
      .evaluateAll((labels) => labels.map((label) => label.textContent?.trim()));

  await expect.poll(() => channelNames(page)).toEqual(names);

  const peer = await page.context().newPage();
  await peer.goto(`/app/${workspace.route_id}`);
  await expect.poll(() => channelNames(peer)).toEqual(names);

  const source = page.getByRole("button", { name: `Move #${names[2]}` });
  await expect(source).toHaveAttribute("aria-describedby", "channel-order-instructions");
  const target = page.getByRole("link", { name: `# ${names[0]}` }).locator("..");
  await source.dragTo(target, { targetPosition: { x: 40, y: 1 } });
  await expect.poll(() => channelNames(page)).toEqual([names[2], names[0], names[1]]);
  await expect.poll(() => channelNames(peer)).toEqual([names[2], names[0], names[1]]);

  await page.reload();
  await expect.poll(() => channelNames(page)).toEqual([names[2], names[0], names[1]]);

  await page.getByRole("button", { name: "Channels", exact: true }).click();
  await expect(page.getByRole("button", { name: "Channels", exact: true })).toHaveAttribute(
    "aria-expanded",
    "false",
  );
  await expect.poll(() => channelNames(page)).toEqual([names[0]]);
  await page.reload();
  await expect(page.getByRole("button", { name: "Channels", exact: true })).toHaveAttribute(
    "aria-expanded",
    "false",
  );
  await expect.poll(() => channelNames(page)).toEqual([names[0]]);
  await page.getByRole("button", { name: "Channels", exact: true }).click();
  await expect.poll(() => channelNames(page)).toEqual([names[2], names[0], names[1]]);

  await page.getByRole("button", { name: `Move #${names[2]}` }).focus();
  await page.keyboard.press("ArrowDown");
  await expect.poll(() => channelNames(page)).toEqual([names[0], names[2], names[1]]);
  await expect.poll(() => channelNames(peer)).toEqual([names[0], names[2], names[1]]);
  await expect(page.getByText(`Moved #${names[2]} to position 2 of 3`)).toBeAttached();

  const addedName = `bb-order-${suffix}`;
  const addedResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: addedName, kind: "public" },
  });
  expect(addedResponse.ok()).toBe(true);
  await page.reload();
  await expect.poll(() => channelNames(page)).toEqual([names[0], names[2], names[1], addedName]);

  const secondWorkspaceResponse = await page.request.post("/api/workspaces", {
    data: { name: `Other channel order ${suffix}` },
  });
  expect(secondWorkspaceResponse.ok()).toBe(true);
  const { workspace: secondWorkspace } = (await secondWorkspaceResponse.json()) as {
    workspace: { id: string; route_id: string };
  };
  for (const name of [`aa-other-${suffix}`, `zz-other-${suffix}`]) {
    const response = await page.request.post(`/api/workspaces/${secondWorkspace.id}/channels`, {
      data: { name, kind: "public" },
    });
    expect(response.ok()).toBe(true);
  }
  await page.goto(`/app/${secondWorkspace.route_id}`);
  await expect.poll(() => channelNames(page)).toEqual([`aa-other-${suffix}`, `zz-other-${suffix}`]);
  await page.goto(`/app/${workspace.route_id}`);
  await expect.poll(() => channelNames(page)).toEqual([names[0], names[2], names[1], addedName]);

  const storageKey = await page.evaluate(() =>
    Object.keys(localStorage).find((key) => key.startsWith("clickclack:sidebar-channel-order:v1:")),
  );
  expect(storageKey).toBeTruthy();
  await page.evaluate((key) => localStorage.setItem(key, "not-json"), storageKey!);
  await page.reload();
  await expect.poll(() => channelNames(page)).toEqual([names[0], addedName, names[1], names[2]]);

  await page.evaluate(({ key, value }) => localStorage.setItem(key, value), {
    key: storageKey!,
    value: "x".repeat(1_000_001),
  });
  await page.reload();
  await expect.poll(() => channelNames(page)).toEqual([names[0], addedName, names[1], names[2]]);

  await page.addInitScript(() => {
    const prefix = "clickclack:sidebar-channel-order:v1:";
    const getItem = Storage.prototype.getItem;
    const setItem = Storage.prototype.setItem;
    Storage.prototype.getItem = function (key: string) {
      if (key.startsWith(prefix)) throw new Error("blocked storage");
      return getItem.call(this, key);
    };
    Storage.prototype.setItem = function (key: string, value: string) {
      if (key.startsWith(prefix)) throw new Error("blocked storage");
      return setItem.call(this, key, value);
    };
  });
  await page.reload();
  await page.getByRole("button", { name: `Move #${names[2]}` }).focus();
  await page.keyboard.press("ArrowUp");
  await expect.poll(() => channelNames(page)).toEqual([names[0], addedName, names[2], names[1]]);

  await peer.close();

  const mobileContext = await browser.newContext({
    baseURL: serverURL,
    hasTouch: true,
    isMobile: true,
    viewport: { width: 390, height: 844 },
  });
  const mobilePage = await mobileContext.newPage();
  await mobilePage.goto(`/app/${workspace.route_id}`);
  await mobilePage.getByRole("button", { name: "Toggle navigation" }).click();
  await mobilePage.getByRole("button", { name: `Move #${names[1]} up` }).click();
  await expect
    .poll(() => channelNames(mobilePage))
    .toEqual([names[0], names[1], addedName, names[2]]);
  await mobilePage.reload();
  await mobilePage.getByRole("button", { name: "Toggle navigation" }).click();
  await expect
    .poll(() => channelNames(mobilePage))
    .toEqual([names[0], names[1], addedName, names[2]]);
  await mobileContext.close();
});

test("app subdomain root opens the chat app", async ({ page }) => {
  await page.goto("http://app.localhost:18082/");
  await expect(page.getByText("Connected")).toBeVisible();
  await expect(page.getByRole("heading", { name: "#general" })).toBeVisible();
});

test("shows realtime connection state in the shell", async ({ page }) => {
  await page.goto("/app");
  await expect(page.getByText("Connected")).toBeVisible();
  await expect(
    page.getByRole("button", { name: /Account settings for Local Captain/ }),
  ).toContainText("Active");
});

test("coalesces durable agent activity and applies activity preferences", async ({ page }) => {
  const meResponse = await page.request.get("/api/me");
  const me = (await meResponse.json()) as { user: { id: string } };
  const notificationStorageKey = `clickclack:browser-notifications-enabled:v1:${me.user.id}`;
  await page.addInitScript((storageKey) => {
    type CapturedNotification = { title: string; close: () => void };
    const target = window as unknown as {
      __clickclackNotifications: CapturedNotification[];
      Notification: typeof Notification;
    };
    class FakeNotification implements CapturedNotification {
      static permission: NotificationPermission = "granted";
      static requestPermission = () => Promise.resolve("granted" as NotificationPermission);
      title: string;
      onclick: (() => void) | null = null;

      constructor(title: string) {
        this.title = title;
        target.__clickclackNotifications.push(this);
      }

      close() {}
    }
    localStorage.setItem(storageKey, "enabled");
    target.__clickclackNotifications = [];
    target.Notification = FakeNotification as unknown as typeof Notification;
  }, notificationStorageKey);

  const workspacesResponse = await page.request.get("/api/workspaces");
  const workspaces = (await workspacesResponse.json()) as { workspaces: { id: string }[] };
  const workspaceId = workspaces.workspaces[0].id;
  // Keep the bootstrap channel first in the shared suite's sorted channel list.
  const channelResponse = await page.request.post(`/api/workspaces/${workspaceId}/channels`, {
    data: { name: `zz-agent-activity-${Date.now()}`, kind: "public" },
  });
  expect(channelResponse.ok()).toBe(true);
  const { channel } = (await channelResponse.json()) as {
    channel: { id: string; name: string };
  };
  const backgroundChannelResponse = await page.request.post(
    `/api/workspaces/${workspaceId}/channels`,
    { data: { name: `zz-agent-background-${Date.now()}`, kind: "public" } },
  );
  expect(backgroundChannelResponse.ok()).toBe(true);
  const backgroundChannel = (await backgroundChannelResponse.json()) as {
    channel: { id: string };
  };

  const botResponse = await page.request.post(`/api/workspaces/${workspaceId}/bots`, {
    data: {
      display_name: "Activity Bot",
      handle: `activity-bot-${Date.now()}`,
      token_name: "e2e",
      scopes: ["bot:write", "agent_activity:write"],
    },
  });
  expect(botResponse.ok()).toBe(true);
  const createdBot = (await botResponse.json()) as { bot_token: { token: string } };
  const botHeaders = { Authorization: `Bearer ${createdBot.bot_token.token}` };
  const turnId = `turn-${Date.now()}`;
  for (const data of [
    { body: "Checking the deployment boundary.", kind: "agent_commentary", turn_id: turnId },
    { body: "**bash inspect**\n\nvalidated local target", kind: "agent_tool", turn_id: turnId },
    { body: "Deployment boundary is healthy." },
  ]) {
    const response = await page.request.post(`/api/channels/${channel.id}/messages`, {
      headers: botHeaders,
      data,
    });
    expect(response.ok()).toBe(true);
  }
  const secondBotResponse = await page.request.post(`/api/workspaces/${workspaceId}/bots`, {
    data: {
      display_name: "Second Activity Bot",
      handle: `activity2-${Date.now()}`,
      token_name: "e2e",
      scopes: ["bot:write", "agent_activity:write"],
    },
  });
  expect(secondBotResponse.ok()).toBe(true);
  const secondBot = (await secondBotResponse.json()) as { bot_token: { token: string } };
  for (const data of [
    { body: "A separate bot reused this turn ID.", kind: "agent_commentary", turn_id: turnId },
    { body: "Second bot finished independently." },
  ]) {
    const response = await page.request.post(`/api/channels/${channel.id}/messages`, {
      headers: { Authorization: `Bearer ${secondBot.bot_token.token}` },
      data,
    });
    expect(response.ok()).toBe(true);
  }

  await page.goto("/app");
  await page.getByRole("link", { name: `# ${channel.name}` }).click();
  await expect(page.getByRole("heading", { name: `#${channel.name}` })).toBeVisible();
  const preambles = page.getByLabel("Agent preamble");
  await expect(preambles).toHaveCount(2);
  const preamble = preambles.nth(0);
  await expect(preamble.getByRole("button", { name: "Show preamble" })).toHaveAttribute(
    "aria-expanded",
    "false",
  );
  await expect(page.getByText("Deployment boundary is healthy.")).toBeVisible();

  await preamble.getByRole("button", { name: "Show preamble" }).click();
  await expect(preamble.getByText("Checking the deployment boundary.")).toBeVisible();
  await expect(preamble.getByText("bash")).toBeVisible();
  await preamble.getByRole("button", { name: /bash/ }).click();
  await expect(preamble.getByText("validated local target")).toBeVisible();

  // Ignore any replayed events from the initial realtime subscription; this
  // assertion begins with the background activity posted below.
  await page.evaluate(() => {
    (
      window as unknown as {
        __clickclackNotifications: { title: string }[];
      }
    ).__clickclackNotifications = [];
  });
  const backgroundActivityResponse = await page.request.post(
    `/api/channels/${backgroundChannel.channel.id}/messages`,
    {
      headers: botHeaders,
      data: {
        body: "Background commentary must stay quiet.",
        kind: "agent_commentary",
        turn_id: `background-${Date.now()}`,
      },
    },
  );
  expect(backgroundActivityResponse.ok()).toBe(true);
  await expect
    .poll(() =>
      page.evaluate(
        () =>
          (
            window as unknown as {
              __clickclackNotifications: { title: string }[];
            }
          ).__clickclackNotifications.length,
      ),
    )
    .toBe(0);
  const backgroundMessageResponse = await page.request.post(
    `/api/channels/${backgroundChannel.channel.id}/messages`,
    { headers: botHeaders, data: { body: "Ordinary background message." } },
  );
  expect(backgroundMessageResponse.ok()).toBe(true);
  await expect
    .poll(() =>
      page.evaluate(
        () =>
          (
            window as unknown as {
              __clickclackNotifications: { title: string }[];
            }
          ).__clickclackNotifications.length,
      ),
    )
    .toBe(1);

  await page.getByRole("button", { name: /Account settings for/ }).click({ button: "right" });
  const settings = page.getByLabel("Account settings");
  await settings.getByLabel("Hide agent commentary").check();
  await expect(preamble.getByText("Checking the deployment boundary.")).toHaveCount(0);
  await settings.getByLabel("Hide tool calls").check();
  await expect(preambles).toHaveCount(0);
  await settings.getByLabel("Your message alignment").selectOption("right");
  await expect(page.locator("html")).toHaveAttribute("data-user-align", "right");
});

test("browser notifications require explicit profile opt-in", async ({ page }) => {
  const meResponse = await page.request.get("/api/me");
  const me = (await meResponse.json()) as { user: { id: string } };
  const storageKey = `clickclack:browser-notifications-enabled:v1:${me.user.id}`;
  await page.addInitScript(() => {
    const target = window as unknown as {
      __clickclackPermissionRequests: number;
      Notification: typeof Notification;
    };
    class FakeNotification {
      static permission: NotificationPermission = "default";
      static requestPermission = () => {
        target.__clickclackPermissionRequests += 1;
        FakeNotification.permission = "granted";
        return Promise.resolve("granted" as NotificationPermission);
      };
    }
    target.__clickclackPermissionRequests = 0;
    target.Notification = FakeNotification as unknown as typeof Notification;
  });

  await page.goto("/app");
  await expect(page.getByText("Connected", { exact: true }).first()).toBeVisible();
  await page
    .getByRole("button", { name: /Account settings for Local Captain/ })
    .click({ button: "right" });
  await expect(page.getByRole("heading", { name: "Profile settings" })).toBeVisible();

  const browserNotifications = page.getByLabel("Browser notifications");
  await expect(browserNotifications).toBeEnabled();
  await browserNotifications.check();

  await expect
    .poll(() => page.evaluate((key) => localStorage.getItem(key), storageKey))
    .toBe("enabled");
  await expect
    .poll(() =>
      page.evaluate(
        () =>
          (
            window as unknown as {
              __clickclackPermissionRequests: number;
            }
          ).__clickclackPermissionRequests,
      ),
    )
    .toBe(1);
});

test("browser notification storage failures do not block app startup", async ({ page }) => {
  await page.addInitScript(() => {
    const blockedKeyPrefix = "clickclack:browser-notifications-enabled:v1:";
    const getItem = Storage.prototype.getItem;
    const setItem = Storage.prototype.setItem;
    const removeItem = Storage.prototype.removeItem;
    Storage.prototype.getItem = function (key: string) {
      if (key.startsWith(blockedKeyPrefix)) throw new Error("blocked storage");
      return getItem.call(this, key);
    };
    Storage.prototype.setItem = function (key: string, value: string) {
      if (key.startsWith(blockedKeyPrefix)) throw new Error("blocked storage");
      return setItem.call(this, key, value);
    };
    Storage.prototype.removeItem = function (key: string) {
      if (key.startsWith(blockedKeyPrefix)) throw new Error("blocked storage");
      return removeItem.call(this, key);
    };
  });

  await page.goto("/app");
  await expect(page.getByRole("heading", { name: "#general" })).toBeVisible();
});

test("mobile navigation behaves like a drawer", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/app");
  await expect(page.getByRole("heading", { name: "#general" })).toBeVisible();

  const composer = page.locator('textarea[aria-label="Message body"]');
  const toggle = page.getByRole("button", { name: "Toggle navigation" });
  const openMobileNavigation = async () => {
    await expect(toggle).toBeVisible();
    await toggle.click();
    await expect(toggle).toHaveAttribute("aria-expanded", "true");
  };

  await expect(page.getByRole("heading", { name: "#general" })).toBeVisible();
  await expect(composer).toBeVisible();
  await expect(toggle).toHaveAttribute("aria-expanded", "false");

  await openMobileNavigation();
  await expect(page.getByRole("button", { name: "Close navigation" })).toBeVisible();

  await page.getByRole("button", { name: "Close navigation" }).click();
  await expect(toggle).toHaveAttribute("aria-expanded", "false");

  await openMobileNavigation();
  await page.setViewportSize({ width: 1024, height: 844 });
  await expect(page.getByRole("button", { name: "Collapse sidebar" })).toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(toggle).toHaveAttribute("aria-expanded", "false");

  await openMobileNavigation();
  await page.keyboard.type("hidden draft");
  await expect(composer).toHaveValue("");

  await page.keyboard.press("Escape");
  await expect(toggle).toHaveAttribute("aria-expanded", "false");

  await openMobileNavigation();
  await page.getByRole("button", { name: "Close navigation" }).click();
  await expect(toggle).toHaveAttribute("aria-expanded", "false");
});

test("desktop sidebar collapse preference still toggles", async ({ page }) => {
  await page.setViewportSize({ width: 1024, height: 844 });
  await page.goto("/app");
  await expect(page.getByText("Connected")).toBeVisible();

  const shell = page.locator(".shell");
  await page.getByRole("button", { name: "Collapse sidebar" }).click();
  await expect(shell).toHaveClass(/sidebar-collapsed/);
  await page
    .getByRole("button", { name: "Expand sidebar" })
    .evaluate((button: HTMLButtonElement) => button.click());
  await expect(shell).not.toHaveClass(/sidebar-collapsed/);
});

test("desktop shell moves sidebar and search controls into the title bar", async ({ page }) => {
  await page.addInitScript(() => {
    Object.assign(window, {
      clickclackDesktop: {
        integratedTitleBar: true,
        notify: async () => true,
        onNavigate: () => () => {},
        onQuickCompose: () => () => {},
        openSettings: () => {},
        platform: "darwin",
        setActiveRoute: () => {},
        setUnreadCount: () => {},
        signInWithGitHub: async () => true,
      },
    });
  });
  await page.setViewportSize({ width: 1280, height: 860 });
  await page.goto("/app");
  await expect(page.getByRole("heading", { name: "#general" })).toBeVisible();

  const shell = page.locator(".shell");
  const titlebar = page.locator(".desktop-titlebar");
  const titlebarSearch = titlebar.getByLabel("Search messages");
  await expect(titlebar).toBeVisible();
  await expect(page.getByTitle("ClickClack home")).toHaveAttribute("href", "/app");
  await expect(page.locator(".topbar .search")).toHaveCount(0);
  await expect(page.locator(".sidebar .sidebar-collapse")).toHaveCount(0);
  expect(
    await titlebarSearch.evaluate((input) => {
      const box = input.closest("form")?.getBoundingClientRect();
      return box ? Math.abs(box.left + box.width / 2 - window.innerWidth / 2) : Infinity;
    }),
  ).toBeLessThan(1);

  await titlebar.getByRole("button", { name: "Collapse sidebar" }).click();
  await expect(shell).toHaveClass(/sidebar-collapsed/);
  await titlebar.getByRole("button", { name: "Expand sidebar" }).click();
  await expect(shell).not.toHaveClass(/sidebar-collapsed/);

  await page.getByLabel("Message body").fill("desktop titlebar search probe");
  await page.getByRole("button", { name: "Send" }).click();
  await titlebarSearch.fill("titlebar search probe");
  await titlebar.getByRole("button", { name: "Search" }).click();
  await expect(
    page.getByLabel("Search results").getByText("desktop titlebar search probe"),
  ).toBeVisible();
  await titlebar.getByRole("button", { name: "Reset" }).click();
  await expect(titlebarSearch).toHaveValue("");
  await expect(page.getByLabel("Search results")).toHaveCount(0);

  await page.setViewportSize({ width: 1024, height: 700 });
  await page.getByRole("button", { name: "View profile for Local Captain" }).first().click();
  await expect(page.getByRole("button", { name: "Close profile" })).toBeVisible();
  expect(
    await page.locator(".thread").evaluate((pane) => {
      const titlebar = document.querySelector(".desktop-titlebar")?.getBoundingClientRect();
      return titlebar ? pane.getBoundingClientRect().top - titlebar.bottom : -Infinity;
    }),
  ).toBeGreaterThanOrEqual(0);
  await page.getByRole("button", { name: "Close profile" }).click();

  await page.setViewportSize({ width: 760, height: 700 });
  await expect(page.getByRole("button", { name: "Toggle navigation" })).toHaveCount(0);
  const titlebarNavigation = titlebar.getByRole("button", { name: "Open navigation" });
  await expect(titlebarNavigation).toBeVisible();
  await titlebarNavigation.click();
  await expect(shell).toHaveClass(/nav-open/);
  await titlebar.getByRole("button", { name: "Close navigation" }).click();
  await expect(shell).not.toHaveClass(/nav-open/);
});

test("desktop title bar preserves Windows caption-control space", async ({ page }) => {
  await page.addInitScript(() => {
    Object.assign(window, {
      clickclackDesktop: {
        integratedTitleBar: true,
        notify: async () => true,
        onNavigate: () => () => {},
        onQuickCompose: () => () => {},
        openSettings: () => {},
        platform: "win32",
        setActiveRoute: () => {},
        setUnreadCount: () => {},
        signInWithGitHub: async () => true,
      },
    });
  });
  await page.setViewportSize({ width: 760, height: 700 });
  await page.goto("/app");

  const titlebar = page.locator(".desktop-titlebar");
  await expect(titlebar).toBeVisible();
  await expect(titlebar.getByRole("button", { name: "Open settings" })).not.toBeVisible();
  expect(
    await titlebar.getByLabel("Search messages").evaluate((input) => {
      const box = input.closest("form")?.getBoundingClientRect();
      return box ? window.innerWidth - box.right : -Infinity;
    }),
  ).toBeGreaterThanOrEqual(148);
});

test("desktop bridge keeps native frame layout when renderer chrome is disabled", async ({
  page,
}) => {
  await page.addInitScript(() => {
    Object.assign(window, {
      clickclackDesktop: {
        integratedTitleBar: false,
        notify: async () => true,
        onNavigate: () => () => {},
        onQuickCompose: () => () => {},
        openSettings: () => {},
        platform: "darwin",
        setActiveRoute: () => {},
        setUnreadCount: () => {},
        signInWithGitHub: async () => true,
      },
    });
  });
  await page.setViewportSize({ width: 1024, height: 844 });
  await page.goto("/app");

  await expect(page.locator(".desktop-titlebar")).toHaveCount(0);
  await expect(page.locator(".topbar .search")).toBeVisible();
  await expect(page.locator(".sidebar .sidebar-collapse")).toBeVisible();
});

test("mobile navigation geometry clears the timeline at narrow widths", async ({ page }) => {
  for (const width of [390, 320]) {
    await page.setViewportSize({ width, height: 844 });
    await page.goto("/app");
    await expect(page.getByRole("heading", { name: "#general" })).toBeVisible();
    await expect
      .poll(() =>
        page.evaluate(
          () => document.querySelector<HTMLMetaElement>('meta[name="viewport"]')?.content || "",
        ),
      )
      .toContain("viewport-fit=cover");

    await expect
      .poll(async () => (await mobileGeometry(page)).sidebar.right)
      .toBeLessThanOrEqual(0.5);
    const closed = await mobileGeometry(page);
    expect(closed.rail.right).toBeLessThanOrEqual(0.5);
    expect(closed.timeline.left).toBe(0);
    expect(closed.timeline.width).toBe(width);
    expect(closed.scrollWidth).toBeLessThanOrEqual(width);
    expect(closed.toolbar.right).toBeLessThanOrEqual(closed.viewportWidth);
    expect(closed.toolbar.bottom).toBeLessThanOrEqual(closed.composer.bottom);
    expect(closed.textareaFontSize).toBeGreaterThanOrEqual(16);
    expect(closed.toolbarOverflowX).toBe("auto");

    await page.getByRole("button", { name: "Toggle navigation" }).click();
    await expect(page.getByRole("button", { name: "Toggle navigation" })).toHaveAttribute(
      "aria-expanded",
      "true",
    );
    await expect
      .poll(async () => (await mobileGeometry(page)).sidebar.left)
      .toBeGreaterThanOrEqual(71.5);
    const open = await mobileGeometry(page);
    expect(open.rail.left).toBeGreaterThanOrEqual(0);
    expect(open.sidebar.left).toBeGreaterThanOrEqual(open.rail.right - 0.5);
    expect(open.sidebar.right).toBeLessThanOrEqual(open.viewportWidth + 0.5);
    expect(open.firstGuild.top).toBeGreaterThanOrEqual(open.toggle.bottom);
    expect(open.scrollWidth).toBeLessThanOrEqual(width);
  }
});

test("sends messages, searches, uploads, opens a thread, and creates a DM", async ({ page }) => {
  const consoleMessages: string[] = [];
  page.on("console", (message) => consoleMessages.push(`${message.type()}: ${message.text()}`));
  page.on("pageerror", (error) => consoleMessages.push(`pageerror: ${error.message}`));
  const workspacesResponse = await page.request.get("/api/workspaces");
  const workspaces = (await workspacesResponse.json()) as { workspaces: { id: string }[] };
  const workspaceId = workspaces.workspaces[0].id;
  const channelResponse = await page.request.post(`/api/workspaces/${workspaceId}/channels`, {
    data: { name: `main-${Date.now()}`, kind: "public" },
  });
  const { channel } = (await channelResponse.json()) as { channel: { id: string; name: string } };
  const secondUserId = execFileSync(
    "go",
    [
      "run",
      "./apps/api/cmd/clickclack",
      "admin",
      "user",
      "create",
      "--data",
      "./data/e2e",
      "--workspace",
      workspaceId,
      "--name",
      "Second User",
      "--email",
      "second@example.com",
    ],
    { cwd: process.cwd(), encoding: "utf8" },
  ).trim();

  await page.goto("/app");
  await expect(page).toHaveURL(/\/app\/[^/]+\/[^/]+$/);

  await page
    .getByRole("button", { name: /Account settings for Local Captain/ })
    .click({ button: "right" });
  await expect(page.getByRole("heading", { name: "Profile settings" })).toBeVisible();
  await page.getByLabel("Display name").fill("Peter Steinberger");
  await page.getByLabel("Handle").fill("@steipete");
  await page.getByLabel("Avatar URL").fill("https://avatars.githubusercontent.com/u/280?v=4");
  await page.getByRole("button", { name: "Save profile" }).click();
  await expect(page.getByRole("button", { name: /@steipete/ })).toBeVisible();

  await page.getByRole("link", { name: `# ${channel.name}` }).click();
  await expect(page.getByRole("heading", { name: `#${channel.name}` })).toBeVisible();

  await page.getByLabel("Message body").fill("hello **playwright**");
  await page.getByRole("button", { name: "Send" }).click();
  await expect(
    page.locator(".markdown").filter({ hasText: "hello playwright" }),
    consoleMessages.join("\n"),
  ).toBeVisible({
    timeout: 5_000,
  });
  await page.getByRole("button", { name: "View profile for Peter Steinberger" }).first().click();
  await expect(
    page.getByLabel("Profile pane").getByRole("heading", { name: "Peter Steinberger" }),
  ).toBeVisible();
  await expect(page.getByLabel("Profile pane").getByText("@steipete").first()).toBeVisible();
  const infoIconOffset = await page
    .getByLabel("Profile pane")
    .locator(".info-icon")
    .first()
    .evaluate((node) => {
      const box = node.getBoundingClientRect();
      const svg = node.querySelector("svg")?.getBoundingClientRect();
      if (!svg) return Number.POSITIVE_INFINITY;
      return Math.max(
        Math.abs(box.left + box.width / 2 - (svg.left + svg.width / 2)),
        Math.abs(box.top + box.height / 2 - (svg.top + svg.height / 2)),
      );
    });
  expect(infoIconOffset).toBeLessThan(1);
  await page.getByRole("button", { name: "Close profile" }).click();

  await page.getByLabel("Search messages").fill("playwright");
  await page.getByRole("button", { name: "Search" }).click();
  await expect(page.getByLabel("Search results").getByText("hello **playwright**")).toBeVisible();

  await page.getByLabel("Upload file").setInputFiles({
    name: "note.txt",
    mimeType: "text/plain",
    buffer: Buffer.from("uploaded from playwright"),
  });
  await expect(page.getByText("note.txt")).toBeVisible();
  await page.getByLabel("Message body").fill("message with upload");
  await page.getByRole("button", { name: "Send" }).click();
  await expect(page.locator(".markdown").filter({ hasText: "message with upload" })).toBeVisible();

  await page.getByLabel("Upload file").setInputFiles({
    name: "pixel.png",
    mimeType: "image/png",
    buffer: Buffer.from(
      "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII=",
      "base64",
    ),
  });
  await expect(page.getByText("pixel.png")).toBeVisible();
  await page.getByLabel("Message body").fill("inline image upload");
  await page.getByRole("button", { name: "Send" }).click();
  const imageAttachment = page.locator(".media-tile--image").filter({ hasText: "pixel.png" });
  await expect(imageAttachment).toBeVisible();
  await expect(imageAttachment.getByRole("link", { name: "Download pixel.png" })).toBeAttached();
  await page.getByRole("button", { name: "Open image pixel.png" }).click();
  await expect(
    page.getByLabel("Image viewer").getByRole("img", { name: "pixel.png" }),
  ).toBeVisible();
  await expect(
    page.getByLabel("Image viewer").getByRole("link", { name: "Open original" }),
  ).toBeVisible();
  await page.getByLabel("Image viewer").getByRole("button", { name: "Close image viewer" }).click();

  await page.getByLabel("Upload file").setInputFiles({
    name: "clip.mp4",
    mimeType: "video/mp4",
    buffer: Buffer.from("not a real mp4, but enough to assert inline video rendering"),
  });
  await expect(page.getByText("clip.mp4")).toBeVisible();
  await page.getByLabel("Message body").fill("inline video upload");
  await page.getByRole("button", { name: "Send" }).click();
  const videoAttachment = page.locator(".media-tile--video").filter({ hasText: "clip.mp4" });
  const inlineVideo = videoAttachment.locator('video[aria-label="clip.mp4"]');
  const videoDownload = videoAttachment.getByRole("link", { name: "Download clip.mp4" });
  await expect(inlineVideo).toBeVisible();
  await expect(videoDownload).toHaveAttribute("download", "clip.mp4");
  await expect(videoDownload).toHaveAttribute("href", /\/api\/uploads\//);
  await expect(inlineVideo).not.toHaveAttribute("controls", "");

  await page.route("https://media.giphy.com/**", async (route) => {
    await route.fulfill({
      contentType: "image/gif",
      body: Buffer.from("R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw==", "base64"),
    });
  });
  await page.getByRole("button", { name: "GIF picker" }).click();
  await page.getByLabel("Search GIFs").fill("ship");
  await page.getByRole("button", { name: /Ship it/ }).click();
  await expect(page.getByLabel("Message body")).toHaveValue(/!\[Ship it\]/);
  await page.getByRole("button", { name: "Send" }).click();
  const replayGif = page.getByRole("button", { name: "Replay GIF Ship it" });
  await expect(replayGif).toBeVisible({ timeout: 7_000 });
  await replayGif.click();
  await expect(replayGif).toBeVisible({ timeout: 7_000 });

  const threadedRow = page
    .locator(".message-row")
    .filter({ has: page.locator(".markdown").filter({ hasText: "hello playwright" }) });
  await threadedRow.getByRole("button", { name: "Open thread" }).click();
  await expect(page.getByLabel("Thread pane")).toBeVisible();

  await page.getByLabel("Reply body").fill("thread _reply_");
  await page.locator(".reply-composer").getByRole("button", { name: "Reply" }).click();
  await expect(page.locator(".reply .markdown").filter({ hasText: "thread reply" })).toBeVisible();
  await expect(threadedRow.locator(".thread-hint")).toContainText("1 reply");

  const threadPane = page.getByLabel("Thread pane");
  await threadPane.getByRole("button", { name: "Close thread" }).click();
  await expect(threadPane.getByRole("button", { name: "Close thread" })).toBeHidden();
  await expect(threadPane.getByText("No thread open")).toBeVisible();
  await threadedRow.locator(".markdown").click();
  await expect(page.getByLabel("Thread pane")).toBeVisible();

  await page.reload();
  await page.getByRole("link", { name: `# ${channel.name}` }).click();
  await expect(page).toHaveURL(/\/app\/T[A-Z0-9]{16}\/C[A-Z0-9]{16}$/);
  await expect(
    page.locator(".messages-scroll .markdown").filter({ hasText: "hello playwright" }),
  ).toBeVisible();

  await page.getByRole("button", { name: "Start direct message" }).click();
  await page.getByLabel("Find a person").fill(secondUserId);
  await page.getByLabel("Find a person").press("Enter");
  await expect(page.getByRole("heading", { name: /Second User/ })).toBeVisible();
  await expect(
    page.locator(".nav-section", { hasText: "People" }).getByText("Second User"),
  ).toBeVisible();
  await page.getByLabel("Message body").fill("private playwright");
  await page.getByRole("button", { name: "Send" }).click();
  await expect(page.locator(".markdown").filter({ hasText: "private playwright" })).toBeVisible();
});

test("closes direct messages without deleting history", async ({ page }) => {
  const workspacesResponse = await page.request.get("/api/workspaces");
  const workspaces = (await workspacesResponse.json()) as {
    workspaces: { id: string; route_id: string }[];
  };
  const workspace = workspaces.workspaces[0];
  const name = `Close User ${Date.now()}`;
  const otherUserId = clickclack([
    "admin",
    "user",
    "create",
    "--data",
    "./data/e2e",
    "--workspace",
    workspace.id,
    "--name",
    name,
    "--email",
    `${name.toLowerCase().replaceAll(" ", ".")}@example.com`,
  ]);
  const dmResponse = await page.request.post("/api/dms", {
    data: { workspace_id: workspace.id, member_ids: [otherUserId] },
  });
  expect(dmResponse.ok()).toBe(true);
  const { conversation } = (await dmResponse.json()) as {
    conversation: { id: string; route_id: string };
  };

  await page.goto("/app");
  const dmSection = page.locator(".nav-section", { hasText: "Direct messages" });
  const dmLink = dmSection.getByRole("link", { name: new RegExp(name) });
  const closeDirectMessage = async () => {
    await dmSection
      .getByRole("button", { name: `Direct message actions for ${name}` })
      .click({ force: true });
    await dmSection.getByRole("menuitem", { name: "Close direct message" }).click();
  };
  await expect(dmLink).toBeVisible();
  await closeDirectMessage();
  await expect(dmLink).toBeHidden();
  await expect(dmSection.getByText(`Closed ${name}`)).toBeVisible();
  await dmSection.getByRole("button", { name: "Undo" }).click();
  await expect(dmLink).toBeVisible();

  await closeDirectMessage();
  await expect(dmLink).toBeHidden();
  const hiddenGet = await page.request.get(`/api/dms/${conversation.id}`);
  expect(hiddenGet.ok()).toBe(true);
  await page.goto(`/app/${workspace.route_id}/${conversation.route_id}`);
  await expect(page.getByRole("heading", { name: new RegExp(name) })).toBeVisible();

  await closeDirectMessage();
  await expect(dmLink).toBeHidden();
  const reopened = await page.request.post("/api/dms", {
    data: { workspace_id: workspace.id, member_ids: [otherUserId] },
  });
  expect(reopened.ok()).toBe(true);
  const reopenedBody = (await reopened.json()) as { conversation: { id: string } };
  expect(reopenedBody.conversation.id).toBe(conversation.id);
  await page.reload();
  await expect(dmLink).toBeVisible();

  await closeDirectMessage();
  await expect(dmLink).toBeHidden();
  const messageResponse = await page.request.post(`/api/dms/${conversation.id}/messages`, {
    headers: { "X-ClickClack-User": otherUserId },
    data: { body: "resurface this dm" },
  });
  expect(messageResponse.ok()).toBe(true);
  await expect(dmLink).toBeVisible();
  await dmLink.click();
  await expect(page.locator(".markdown").filter({ hasText: "resurface this dm" })).toBeVisible();
});

test("unread bar jumps to the new-message divider across repeated unread cycles", async ({
  page,
}) => {
  const workspacesResponse = await page.request.get("/api/workspaces");
  const workspaces = (await workspacesResponse.json()) as { workspaces: { id: string }[] };
  const workspaceId = workspaces.workspaces[0].id;
  const channelName = `unread-jump-${Date.now()}`;
  const channelResponse = await page.request.post(`/api/workspaces/${workspaceId}/channels`, {
    data: { name: channelName, kind: "public" },
  });
  const channel = (await channelResponse.json()) as { channel: { id: string; name: string } };

  for (let i = 0; i < 36; i++) {
    const response = await page.request.post(`/api/channels/${channel.channel.id}/messages`, {
      data: {
        body: `read history ${i} ${"with enough text to create scrollable history ".repeat(3)}`,
      },
    });
    expect(response.ok()).toBe(true);
  }
  const historyReadResponse = await page.request.post(`/api/channels/${channel.channel.id}/read`, {
    data: { seq: 36 },
  });
  expect(historyReadResponse.ok()).toBe(true);

  const email = `${channelName}@example.com`;
  const senderID = clickclack([
    "admin",
    "user",
    "create",
    "--data",
    "./data/e2e",
    "--workspace",
    workspaceId,
    "--name",
    "Unread Sender",
    "--email",
    email,
  ]);

  async function currentChannelState(): Promise<{ last_read_seq?: number; unread_count?: number }> {
    const response = await page.request.get(`/api/workspaces/${workspaceId}/channels`);
    const data = (await response.json()) as {
      channels: { id: string; last_read_seq?: number; unread_count?: number }[];
    };
    const current = data.channels.find((item) => item.id === channel.channel.id);
    if (!current) throw new Error("channel missing from list");
    return current;
  }

  await page.goto("/app");
  await page.getByRole("link", { name: `# ${channel.channel.name}` }).click();
  await expect(page.getByRole("heading", { name: `#${channel.channel.name}` })).toBeVisible();
  await expect(page.locator(".markdown").filter({ hasText: "read history 35" })).toBeVisible();
  await settleScrollFrames(page);

  const scrollport = page.locator(".messages-scroll");
  await expect
    .poll(() => scrollport.evaluate((el) => el.scrollHeight > el.clientHeight + 120))
    .toBe(true);
  await scrollport.evaluate((el) => {
    el.scrollTop = 0;
    el.dispatchEvent(new Event("scroll", { bubbles: true }));
  });
  await expect(page.getByRole("button", { name: /^Jump to latest$/ })).toHaveCount(0);
  await expect(page.locator(".markdown").filter({ hasText: "read history 0" })).toBeVisible();

  const unreadResponse = await page.request.post(`/api/channels/${channel.channel.id}/messages`, {
    headers: { "X-ClickClack-User": senderID },
    data: { body: "unread after scroll" },
  });
  expect(unreadResponse.ok()).toBe(true);

  const jump = page.getByRole("button", { name: "Jump to 1 new message" });
  await expect(jump).toBeVisible();
  await jump.click();
  await expect(page.getByRole("separator", { name: "New messages" })).toBeVisible();
  await expect(page.locator(".markdown").filter({ hasText: "unread after scroll" })).toBeVisible();
  await page.waitForTimeout(1400);
  await expect(page.getByRole("separator", { name: "New messages" })).toBeVisible();
  await expect.poll(async () => (await currentChannelState()).last_read_seq || 0).toBe(36);
  await expect.poll(async () => (await currentChannelState()).unread_count || 0).toBe(1);

  await page.getByLabel("Message body").fill("local reply while unread");
  await page.getByRole("button", { name: "Send", exact: true }).click();
  await expect(
    page.locator(".markdown").filter({ hasText: "local reply while unread" }),
  ).toBeVisible();
  await expectMessageNearComposer(page, "local reply while unread");
  await expect
    .poll(async () => (await currentChannelState()).last_read_seq || 0)
    .toBeGreaterThan(36);
  await expect.poll(async () => (await currentChannelState()).unread_count || 0).toBe(0);
  await expect(page.getByRole("separator", { name: "New messages" })).toHaveCount(0);

  await scrollport.evaluate((el) => {
    el.scrollTop = 0;
    el.dispatchEvent(new Event("scroll", { bubbles: true }));
  });

  const secondUnreadResponse = await page.request.post(
    `/api/channels/${channel.channel.id}/messages`,
    {
      headers: { "X-ClickClack-User": senderID },
      data: { body: "second unread after clear" },
    },
  );
  expect(secondUnreadResponse.ok()).toBe(true);

  const secondJump = page.getByRole("button", { name: /Jump to \d+ new messages?/ });
  await expect(secondJump).toBeVisible();
  await secondJump.click();

  await expect(page.getByRole("separator", { name: "New messages" })).toBeVisible();
  await expect(
    page.locator(".markdown").filter({ hasText: "second unread after clear" }),
  ).toBeVisible();
});

test("remote messages keep a live channel pinned without unread UI", async ({ page }) => {
  const workspacesResponse = await page.request.get("/api/workspaces");
  const workspaces = (await workspacesResponse.json()) as { workspaces: { id: string }[] };
  const workspaceId = workspaces.workspaces[0].id;
  const channelName = `live-pinned-${Date.now()}`;
  const channelResponse = await page.request.post(`/api/workspaces/${workspaceId}/channels`, {
    data: { name: channelName, kind: "public" },
  });
  const channel = (await channelResponse.json()) as { channel: { id: string; name: string } };

  for (let i = 0; i < 32; i++) {
    const response = await page.request.post(`/api/channels/${channel.channel.id}/messages`, {
      data: {
        body: `live history ${i} ${"enough text to make the channel scroll ".repeat(3)}`,
      },
    });
    expect(response.ok()).toBe(true);
  }
  const readResponse = await page.request.post(`/api/channels/${channel.channel.id}/read`, {
    data: { seq: 32 },
  });
  expect(readResponse.ok()).toBe(true);

  const senderID = clickclack([
    "admin",
    "user",
    "create",
    "--data",
    "./data/e2e",
    "--workspace",
    workspaceId,
    "--name",
    "Live Sender",
    "--email",
    `${channelName}@example.com`,
  ]);

  async function currentChannelState(): Promise<{ last_read_seq?: number; unread_count?: number }> {
    const response = await page.request.get(`/api/workspaces/${workspaceId}/channels`);
    const data = (await response.json()) as {
      channels: { id: string; last_read_seq?: number; unread_count?: number }[];
    };
    const current = data.channels.find((item) => item.id === channel.channel.id);
    if (!current) throw new Error("channel missing from list");
    return current;
  }

  await page.goto("/app");
  await page.getByRole("link", { name: `# ${channel.channel.name}` }).click();
  await expect(page.getByRole("heading", { name: `#${channel.channel.name}` })).toBeVisible();
  await expect(page.locator(".markdown").filter({ hasText: "live history 31" })).toBeVisible();
  await settleScrollFrames(page);
  await expectScrollAtMessageEnd(page);

  const remoteResponse = await page.request.post(`/api/channels/${channel.channel.id}/messages`, {
    headers: { "X-ClickClack-User": senderID },
    data: { body: "live remote while bottom" },
  });
  expect(remoteResponse.ok()).toBe(true);

  await expect(
    page.locator(".markdown").filter({ hasText: "live remote while bottom" }),
  ).toBeVisible();
  await expectMessageNearComposer(page, "live remote while bottom");
  await expectScrollAtMessageEnd(page);
  await expect(page.getByRole("separator", { name: "New messages" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: /new messages?/ })).toHaveCount(0);
  await expect
    .poll(async () => (await currentChannelState()).last_read_seq || 0)
    .toBeGreaterThanOrEqual(33);
  await expect.poll(async () => (await currentChannelState()).unread_count || 0).toBe(0);
});

test("browser notifications announce incoming messages outside the active conversation", async ({
  page,
}) => {
  const meResponse = await page.request.get("/api/me");
  const me = (await meResponse.json()) as { user: { id: string } };
  const storageKey = `clickclack:browser-notifications-enabled:v1:${me.user.id}`;
  await page.addInitScript((key) => {
    type CapturedNotification = {
      title: string;
      options?: NotificationOptions;
      closed?: boolean;
      onclick?: (() => void) | null;
      close: () => void;
    };
    const target = window as unknown as {
      __clickclackNotifications: CapturedNotification[];
      Notification: typeof Notification;
    };
    class FakeNotification implements CapturedNotification {
      static permission: NotificationPermission = "granted";
      static requestPermission = () => Promise.resolve("granted" as NotificationPermission);
      title: string;
      options?: NotificationOptions;
      closed = false;
      onclick: (() => void) | null = null;

      constructor(title: string, options?: NotificationOptions) {
        this.title = title;
        this.options = options;
        target.__clickclackNotifications.push(this);
      }

      close() {
        this.closed = true;
      }
    }
    target.__clickclackNotifications = [];
    target.Notification = FakeNotification as unknown as typeof Notification;
    localStorage.setItem(key, "enabled");
  }, storageKey);

  const workspacesResponse = await page.request.get("/api/workspaces");
  const workspaces = (await workspacesResponse.json()) as { workspaces: { id: string }[] };
  const workspaceId = workspaces.workspaces[0].id;
  const channelName = `notify-${randomUUID()}`;
  const channelResponse = await page.request.post(`/api/workspaces/${workspaceId}/channels`, {
    data: { name: channelName, kind: "public" },
  });
  const channel = (await channelResponse.json()) as { channel: { id: string; name: string } };
  const senderID = clickclack([
    "admin",
    "user",
    "create",
    "--data",
    "./data/e2e",
    "--workspace",
    workspaceId,
    "--name",
    "Notification Sender",
    "--email",
    `${channelName}@example.com`,
  ]);

  await page.goto("/app");
  await expect(page.getByRole("heading", { name: "#general" })).toBeVisible();
  await expect(page.getByText("Connected", { exact: true }).first()).toBeVisible();
  const remoteResponse = await page.request.post(`/api/channels/${channel.channel.id}/messages`, {
    headers: { "X-ClickClack-User": senderID },
    data: { body: "ping from another channel" },
  });
  expect(remoteResponse.ok()).toBe(true);

  await expect
    .poll(() =>
      page.evaluate(
        (name) =>
          (
            window as unknown as {
              __clickclackNotifications: { title: string; options?: NotificationOptions }[];
            }
          ).__clickclackNotifications.find(
            (notification) => notification.title === `ClickClack in #${name}`,
          ),
        channel.channel.name,
      ),
    )
    .toEqual(
      expect.objectContaining({
        options: expect.objectContaining({
          body: "New message",
          icon: "/favicon.svg",
        }),
      }),
    );

  await page.evaluate((name) => {
    const notification = (
      window as unknown as {
        __clickclackNotifications: { title: string; onclick?: (() => void) | null }[];
      }
    ).__clickclackNotifications.find((candidate) => candidate.title === `ClickClack in #${name}`);
    if (!notification) {
      throw new Error("Expected a channel notification");
    }
    notification.onclick?.();
  }, channel.channel.name);

  await expect(page.getByRole("heading", { name: `#${channel.channel.name}` })).toBeVisible();
});

test("clicking the active conversation does not refetch its messages", async ({ page }) => {
  const workspacesResponse = await page.request.get("/api/workspaces");
  const workspaces = (await workspacesResponse.json()) as {
    workspaces: { id: string; route_id: string }[];
  };
  const workspace = workspaces.workspaces[0];
  const workspaceId = workspace.id;
  const channelName = `active-nav-${Date.now()}`;
  const channelResponse = await page.request.post(`/api/workspaces/${workspaceId}/channels`, {
    data: { name: channelName, kind: "public" },
  });
  const channel = (await channelResponse.json()) as {
    channel: { id: string; route_id: string; name: string };
  };
  const messageResponse = await page.request.post(`/api/channels/${channel.channel.id}/messages`, {
    data: { body: "active nav baseline" },
  });
  expect(messageResponse.ok()).toBe(true);

  await page.goto(`/app/${workspace.route_id}/${channel.channel.route_id}`);
  await expect(page.getByRole("heading", { name: `#${channel.channel.name}` })).toBeVisible();
  await expect(page.locator(".markdown").filter({ hasText: "active nav baseline" })).toBeVisible();
  await settleScrollFrames(page);

  const messageRequests: string[] = [];
  page.on("request", (request) => {
    const url = request.url();
    if (
      request.method() === "GET" &&
      url.includes(`/api/channels/${channel.channel.id}/messages`)
    ) {
      messageRequests.push(url);
    }
  });

  await page.getByRole("link", { name: `# ${channel.channel.name}` }).click();
  await page.waitForTimeout(250);
  expect(messageRequests).toEqual([]);
});

test("read history returns to latest with Escape when scrolled up", async ({ page }) => {
  const workspacesResponse = await page.request.get("/api/workspaces");
  const workspaces = (await workspacesResponse.json()) as { workspaces: { id: string }[] };
  const workspaceId = workspaces.workspaces[0].id;
  const channelName = `latest-jump-${Date.now()}`;
  const channelResponse = await page.request.post(`/api/workspaces/${workspaceId}/channels`, {
    data: { name: channelName, kind: "public" },
  });
  const channel = (await channelResponse.json()) as { channel: { id: string; name: string } };

  for (let i = 0; i < 36; i++) {
    const response = await page.request.post(`/api/channels/${channel.channel.id}/messages`, {
      data: {
        body: `read latest ${i} ${"enough text to create a scrollable read history ".repeat(3)}`,
      },
    });
    expect(response.ok()).toBe(true);
  }
  const readResponse = await page.request.post(`/api/channels/${channel.channel.id}/read`, {
    data: { seq: 36 },
  });
  expect(readResponse.ok()).toBe(true);

  await page.goto("/app");
  await page.getByRole("link", { name: `# ${channel.channel.name}` }).click();
  await expect(page.locator(".markdown").filter({ hasText: "read latest 35" })).toBeVisible();
  await settleScrollFrames(page);
  await expectMessageNearScrollBottom(page, "read latest 35");
  await expectMessageNearComposer(page, "read latest 35");
  await page.reload();
  await expect(page.locator(".markdown").filter({ hasText: "read latest 35" })).toBeVisible();
  await settleScrollFrames(page);
  await expectMessageNearScrollBottom(page, "read latest 35");
  await expectMessageNearComposer(page, "read latest 35");
  await page.getByLabel("Message body").fill("bottom after send");
  await page.getByRole("button", { name: "Send", exact: true }).click();
  await expect(page.locator(".markdown").filter({ hasText: "bottom after send" })).toBeVisible();
  await settleScrollFrames(page);
  await expectMessageNearScrollBottom(page, "bottom after send");
  await expectMessageNearComposer(page, "bottom after send");

  const scrollport = page.locator(".messages-scroll");
  await expect
    .poll(() => scrollport.evaluate((el) => el.scrollHeight > el.clientHeight + 120))
    .toBe(true);
  await scrollport.evaluate((el) => {
    el.scrollTop = 0;
    el.dispatchEvent(new Event("scroll", { bubbles: true }));
  });

  await expect(page.getByText("You're Viewing Older Messages")).toHaveCount(0);
  await expect(page.getByRole("button", { name: /^Jump to latest$/ })).toHaveCount(0);
  await page.getByLabel("Message body").fill("sent while reading older history");
  await page.getByRole("button", { name: "Send", exact: true }).click();
  await expect(
    page.locator(".markdown").filter({ hasText: "sent while reading older history" }),
  ).toBeVisible();
  await expectMessageNearComposer(page, "sent while reading older history");
  await expectScrollAtMessageEnd(page);

  await scrollport.evaluate((el) => {
    el.scrollTop = 0;
    el.dispatchEvent(new Event("scroll", { bubbles: true }));
  });
  await page.keyboard.press("Escape");
  await expectScrollAtMessageEnd(page);
  await expect(page.getByRole("separator", { name: "New messages" })).toHaveCount(0);
});

test("refresh with unread messages opens at the divider without marking read", async ({ page }) => {
  const workspacesResponse = await page.request.get("/api/workspaces");
  const workspaces = (await workspacesResponse.json()) as {
    workspaces: { id: string; route_id: string }[];
  };
  const workspace = workspaces.workspaces[0];
  const workspaceId = workspace.id;
  const channelName = `refresh-read-${Date.now()}`;
  const channelResponse = await page.request.post(`/api/workspaces/${workspaceId}/channels`, {
    data: { name: channelName, kind: "public" },
  });
  const channel = (await channelResponse.json()) as {
    channel: { id: string; route_id: string; name: string };
  };
  const senderID = clickclack([
    "admin",
    "user",
    "create",
    "--data",
    "./data/e2e",
    "--workspace",
    workspaceId,
    "--name",
    "Refresh Sender",
    "--email",
    `${channelName}@example.com`,
  ]);

  for (let i = 0; i < 6; i++) {
    const response = await page.request.post(`/api/channels/${channel.channel.id}/messages`, {
      headers: { "X-ClickClack-User": senderID },
      data: { body: `refresh unread ${i}` },
    });
    expect(response.ok()).toBe(true);
  }

  async function currentChannelState(): Promise<{ last_read_seq?: number; unread_count?: number }> {
    const response = await page.request.get(`/api/workspaces/${workspaceId}/channels`);
    const data = (await response.json()) as {
      channels: { id: string; last_read_seq?: number; unread_count?: number }[];
    };
    const current = data.channels.find((item) => item.id === channel.channel.id);
    if (!current) throw new Error("channel missing from list");
    return current;
  }

  await page.goto(`/app/${workspaceId}/${channel.channel.id}`);
  await expect(page).toHaveURL(
    new RegExp(`/app/${workspace.route_id}/${channel.channel.route_id}$`),
  );
  await expect(page.getByRole("heading", { name: `#${channel.channel.name}` })).toBeVisible();
  await expect(page.locator(".markdown").filter({ hasText: "refresh unread 5" })).toBeVisible();
  await expect(page.getByRole("separator", { name: "New messages" })).toBeVisible();
  const unreadJump = page.getByRole("button", { name: "Jump to 6 new messages" });
  await expect(unreadJump).toBeVisible();
  await expect.poll(async () => (await currentChannelState()).last_read_seq || 0).toBe(0);
  await expect.poll(async () => (await currentChannelState()).unread_count || 0).toBe(6);

  await page.reload();
  await expect(page.getByRole("heading", { name: `#${channel.channel.name}` })).toBeVisible();
  await expect(page.locator(".markdown").filter({ hasText: "refresh unread 5" })).toBeVisible();
  await expect(page.getByRole("separator", { name: "New messages" })).toBeVisible();
  await unreadJump.click();
  await expect.poll(async () => (await currentChannelState()).last_read_seq || 0).toBe(0);
  await expect.poll(async () => (await currentChannelState()).unread_count || 0).toBe(6);

  await page.getByRole("button", { name: "Mark as read" }).click();
  await expect(page.getByRole("separator", { name: "New messages" })).toHaveCount(0);
  await expect.poll(async () => (await currentChannelState()).last_read_seq || 0).toBe(6);
  await expect.poll(async () => (await currentChannelState()).unread_count || 0).toBe(0);
});

test("automatic read receipts do not clear unseen paged history", async ({ page }) => {
  const workspacesResponse = await page.request.get("/api/workspaces");
  const workspaces = (await workspacesResponse.json()) as { workspaces: { id: string }[] };
  const workspaceId = workspaces.workspaces[0].id;
  const channelName = `unread-window-${Date.now()}`;
  const channelResponse = await page.request.post(`/api/workspaces/${workspaceId}/channels`, {
    data: { name: channelName, kind: "public" },
  });
  const channel = (await channelResponse.json()) as { channel: { id: string; name: string } };
  const senderID = clickclack([
    "admin",
    "user",
    "create",
    "--data",
    "./data/e2e",
    "--workspace",
    workspaceId,
    "--name",
    "Unread Window Sender",
    "--email",
    `${channelName}@example.com`,
  ]);

  for (let i = 0; i < 180; i++) {
    const response = await page.request.post(`/api/channels/${channel.channel.id}/messages`, {
      headers: { "X-ClickClack-User": senderID },
      data: {
        body: `auto-read-msg-${String(i).padStart(3, "0")} ${i === 179 ? "latesttargetword " : ""}${"unread paging row ".repeat(4)}`,
      },
    });
    expect(response.ok()).toBe(true);
  }

  async function currentChannelState(): Promise<{ last_read_seq?: number; unread_count?: number }> {
    const response = await page.request.get(`/api/workspaces/${workspaceId}/channels`);
    const data = (await response.json()) as {
      channels: { id: string; last_read_seq?: number; unread_count?: number }[];
    };
    const current = data.channels.find((item) => item.id === channel.channel.id);
    if (!current) throw new Error("channel missing from list");
    return current;
  }

  await page.goto("/app");
  await page.getByRole("link", { name: `# ${channel.channel.name}` }).click();
  await settleScrollFrames(page);
  const unreadJump = page.getByRole("button", { name: /Jump to \d+ new messages/ });
  await expect(unreadJump).toBeVisible();
  await page.getByLabel("Search messages").fill("latesttargetword");
  await page.getByRole("button", { name: "Search" }).click();
  await expect(page.getByLabel("Search results").getByText("latesttargetword")).toBeVisible();
  const latestUnreadPage = page.waitForResponse(
    (response) =>
      response.url().includes(`/api/channels/${channel.channel.id}/messages`) &&
      response.url().includes("around_seq=180") &&
      response.ok(),
  );
  await page.getByLabel("Search results").getByText("latesttargetword").click();
  await latestUnreadPage;
  await expect(page.locator(".markdown").filter({ hasText: "auto-read-msg-179" })).toBeVisible();
  await expect(page.getByRole("separator", { name: "New messages" })).toHaveCount(0);
  await unreadJump.click();
  await expect(page.getByRole("separator", { name: "New messages" })).toBeVisible();
  await expect(page.locator(".markdown").filter({ hasText: "auto-read-msg-000" })).toBeVisible();
  await expect.poll(async () => (await currentChannelState()).last_read_seq || 0).toBe(0);

  await page.keyboard.press("Escape");
  await expect(page.locator(".markdown").filter({ hasText: "auto-read-msg-179" })).toBeVisible();
  await expectScrollAtMessageEnd(page);
  await expect.poll(async () => (await currentChannelState()).last_read_seq || 0).toBe(180);
  await expect.poll(async () => (await currentChannelState()).unread_count || 0).toBe(0);
});

test("message history pages older, newer, and search target windows", async ({ page }) => {
  const workspacesResponse = await page.request.get("/api/workspaces");
  const workspaces = (await workspacesResponse.json()) as { workspaces: { id: string }[] };
  const workspaceId = workspaces.workspaces[0].id;
  const channelName = `history-paging-${Date.now()}`;
  const channelResponse = await page.request.post(`/api/workspaces/${workspaceId}/channels`, {
    data: { name: channelName, kind: "public" },
  });
  const channel = (await channelResponse.json()) as { channel: { id: string; name: string } };
  const senderID = clickclack([
    "admin",
    "user",
    "create",
    "--data",
    "./data/e2e",
    "--workspace",
    workspaceId,
    "--name",
    "History Sender",
    "--email",
    `${channelName}@example.com`,
  ]);

  for (let i = 0; i < 260; i++) {
    const response = await page.request.post(`/api/channels/${channel.channel.id}/messages`, {
      data: {
        body: `history-msg-${String(i).padStart(3, "0")} ${i === 10 ? "targetten " : ""}${"paged history row ".repeat(4)}`,
      },
    });
    expect(response.ok()).toBe(true);
  }
  const readResponse = await page.request.post(`/api/channels/${channel.channel.id}/read`, {
    data: { seq: 260 },
  });
  expect(readResponse.ok()).toBe(true);

  await page.goto("/app");
  await page.getByRole("link", { name: `# ${channel.channel.name}` }).click();
  await expect(page.getByRole("heading", { name: `#${channel.channel.name}` })).toBeVisible();
  await expect(page.locator(".markdown").filter({ hasText: "history-msg-259" })).toBeVisible();
  await expect(page.locator(".markdown").filter({ hasText: "history-msg-000" })).toHaveCount(0);

  const scrollport = page.locator(".messages-scroll");
  let releaseOlderPage: (() => void) | undefined;
  const firstOlderPageGate = new Promise<void>((resolve) => {
    releaseOlderPage = resolve;
  });
  let olderPageRequests = 0;
  await page.route("**/api/channels/**/messages**", async (route) => {
    const url = route.request().url();
    if (
      !url.includes(`/api/channels/${channel.channel.id}/messages`) ||
      !url.includes("before_seq=")
    ) {
      await route.continue();
      return;
    }
    olderPageRequests++;
    if (olderPageRequests === 1) await firstOlderPageGate;
    await route.continue();
  });
  const olderPage = page.waitForResponse(
    (response) =>
      response.url().includes(`/api/channels/${channel.channel.id}/messages`) &&
      response.url().includes("before_seq=") &&
      response.ok(),
  );
  await settleScrollFrames(page);
  await scrollport.evaluate(
    (el) =>
      new Promise<void>((resolve) => {
        el.scrollTop = 1;
        el.dispatchEvent(new Event("scroll", { bubbles: true }));
        requestAnimationFrame(() => {
          el.scrollTop = 0;
          el.dispatchEvent(new Event("scroll", { bubbles: true }));
          resolve();
        });
      }),
  );
  await scrollport.evaluate((el) => {
    for (let i = 0; i < 8; i++) {
      el.scrollTop = 0;
      el.dispatchEvent(new Event("scroll", { bubbles: true }));
    }
  });
  await scrollport.hover();
  await page.mouse.wheel(0, -1200);
  await expect.poll(() => olderPageRequests).toBe(1);
  await expect(page.getByRole("status", { name: "Loading older messages" })).toBeVisible();
  await page.waitForTimeout(150);
  expect(olderPageRequests).toBe(1);
  releaseOlderPage?.();
  await olderPage;
  await expect.poll(() => scrollport.evaluate((el) => el.scrollTop)).toBeGreaterThan(20);
  await expect(page.locator(".markdown").filter({ hasText: "history-msg-160" })).toBeVisible();
  await expect(page.locator(".markdown").filter({ hasText: "history-msg-000" })).toHaveCount(0);

  await page.getByLabel("Search messages").fill("targetten");
  await page.getByRole("button", { name: "Search" }).click();
  await expect(page.getByLabel("Search results").getByText("targetten")).toBeVisible();
  const aroundPage = page.waitForResponse(
    (response) =>
      response.url().includes(`/api/channels/${channel.channel.id}/messages`) &&
      response.url().includes("around_seq=") &&
      response.ok(),
  );
  await page.getByLabel("Search results").getByText("targetten").click();
  await aroundPage;
  await expect(page.locator(".markdown").filter({ hasText: "history-msg-010" })).toBeVisible();
  await settleScrollFrames(page);

  const newerResponses: string[] = [];
  page.on("response", (response) => {
    if (
      response.url().includes(`/api/channels/${channel.channel.id}/messages`) &&
      response.url().includes("after_seq=") &&
      response.ok()
    ) {
      newerResponses.push(response.url());
    }
  });

  const liveMessageResponse = await page.request.post(
    `/api/channels/${channel.channel.id}/messages`,
    {
      headers: { "X-ClickClack-User": senderID },
      data: { body: "live while reading old history" },
    },
  );
  expect(liveMessageResponse.ok()).toBe(true);
  await page.waitForTimeout(300);
  expect(newerResponses).toHaveLength(0);
  await expect(page.locator(".markdown").filter({ hasText: "history-msg-010" })).toBeVisible();
  await expect(
    page.locator(".markdown").filter({ hasText: "live while reading old history" }),
  ).toHaveCount(0);

  const newerPage = page.waitForResponse(
    (response) =>
      response.url().includes(`/api/channels/${channel.channel.id}/messages`) &&
      response.url().includes("after_seq=") &&
      response.ok(),
  );
  await scrollport.evaluate((el) => {
    el.scrollTop = el.scrollHeight;
    el.dispatchEvent(new Event("scroll", { bubbles: true }));
  });
  await newerPage;
  await page.waitForTimeout(300);
  // Scrolling to the bottom of the loaded window engages newer-paging and
  // loads incrementally rather than jumping to the live tail. The exact number
  // of pages depends on how many newer rows fit above the prefetch sentinel
  // before it leaves the trigger margin: a single 50-row page sits right at one
  // viewport, so depending on row height and composer height the fill can take
  // a second sequential page (after_seq=100 then after_seq=150). That cascade
  // is benign forward fill, not a runaway to the live tail, which the
  // history-msg-259 absence below guards. Assert the invariant (paging engaged,
  // stayed incremental) instead of an exact request count that tracks
  // pixel-level viewport fill.
  expect(newerResponses.length).toBeGreaterThanOrEqual(1);
  await expect(page.locator(".markdown").filter({ hasText: "history-msg-149" })).toBeVisible();
  await expect(page.locator(".markdown").filter({ hasText: "history-msg-259" })).toHaveCount(0);
});

test("CLI supports multiple accounts chatting in one channel", async ({ page }) => {
  const workspacesResponse = await page.request.get("/api/workspaces");
  const workspaces = (await workspacesResponse.json()) as { workspaces: { id: string }[] };
  const workspaceId = workspaces.workspaces[0].id;

  const channelsResponse = await page.request.get(`/api/workspaces/${workspaceId}/channels`);
  const channels = (await channelsResponse.json()) as { channels: { id: string; name: string }[] };
  const general =
    channels.channels.find((channel) => channel.name === "general") ?? channels.channels[0];

  const ownerMagicToken = clickclack([
    "admin",
    "magic-link",
    "create",
    "--data",
    "./data/e2e",
    "--email",
    "local@clickclack.chat",
    "--name",
    "Local Captain",
  ]);
  const ownerSessionToken = clickclack([
    "--server",
    serverURL,
    "login",
    "--magic-token",
    ownerMagicToken,
    "--plain",
    "--no-store",
  ]);

  clickclack([
    "admin",
    "user",
    "create",
    "--data",
    "./data/e2e",
    "--workspace",
    workspaceId,
    "--name",
    "CLI Second",
    "--email",
    "cli-second@example.com",
  ]);
  const secondMagicToken = clickclack([
    "admin",
    "magic-link",
    "create",
    "--data",
    "./data/e2e",
    "--email",
    "cli-second@example.com",
    "--name",
    "CLI Second",
  ]);
  const secondSessionToken = clickclack([
    "--server",
    serverURL,
    "login",
    "--magic-token",
    secondMagicToken,
    "--plain",
    "--no-store",
  ]);

  const ownerMessageId = clickclack([
    "--server",
    serverURL,
    "--token",
    ownerSessionToken,
    "send",
    "--workspace",
    workspaceId,
    "--channel",
    general.id,
    "--plain",
    "owner from cli",
  ]);
  const secondMessageId = clickclack(
    [
      "--server",
      serverURL,
      "--token",
      secondSessionToken,
      "send",
      "--workspace",
      workspaceId,
      "--channel",
      general.id,
      "--stdin",
      "--plain",
    ],
    "second from cli",
  );

  const listJSON = clickclack([
    "--server",
    serverURL,
    "--token",
    ownerSessionToken,
    "messages",
    "list",
    "--workspace",
    workspaceId,
    "--channel",
    general.id,
    "--limit",
    "20",
    "--json",
  ]);
  const listed = JSON.parse(listJSON) as {
    messages: { id: string; body: string; author?: { display_name: string } }[];
  };
  expect(listed.messages).toEqual(
    expect.arrayContaining([
      expect.objectContaining({ id: ownerMessageId, body: "owner from cli" }),
      expect.objectContaining({ id: secondMessageId, body: "second from cli" }),
    ]),
  );
  expect(
    listed.messages.find((message) => message.id === ownerMessageId)?.author?.display_name,
  ).toMatch(/^(Local Captain|Peter Steinberger)$/);
  expect(
    listed.messages.find((message) => message.id === secondMessageId)?.author?.display_name,
  ).toBe("CLI Second");

  const replyId = clickclack(
    [
      "--server",
      serverURL,
      "--token",
      secondSessionToken,
      "threads",
      "reply",
      ownerMessageId,
      "--stdin",
      "--plain",
    ],
    "thread reply from second cli",
  );
  const threadJSON = clickclack([
    "--server",
    serverURL,
    "--token",
    ownerSessionToken,
    "threads",
    "open",
    ownerMessageId,
    "--json",
  ]);
  const thread = JSON.parse(threadJSON) as {
    replies: { id: string; body: string; author?: { display_name: string } }[];
  };
  expect(thread.replies).toEqual(
    expect.arrayContaining([
      expect.objectContaining({
        id: replyId,
        body: "thread reply from second cli",
        author: expect.objectContaining({ display_name: "CLI Second" }),
      }),
    ]),
  );
});

test("CLI does not reuse stored tokens for another server", async ({ page }) => {
  const workspacesResponse = await page.request.get("/api/workspaces");
  const workspaces = (await workspacesResponse.json()) as { workspaces: { id: string }[] };
  const workspaceId = workspaces.workspaces[0].id;
  const channelsResponse = await page.request.get(`/api/workspaces/${workspaceId}/channels`);
  const channels = (await channelsResponse.json()) as { channels: { id: string }[] };
  const channelId = channels.channels[0].id;

  const ownerMagicToken = clickclack([
    "admin",
    "magic-link",
    "create",
    "--data",
    "./data/e2e",
    "--email",
    "local@clickclack.chat",
    "--name",
    "Local Captain",
  ]);
  const env = isolatedHome();
  clickclack(["--server", serverURL, "login", "--magic-token", ownerMagicToken], undefined, env);

  let leakedAuth = "";
  const http = await import("node:http");
  const probeServer = http.createServer((request, response) => {
    leakedAuth = request.headers.authorization ?? "";
    response.writeHead(401, { "content-type": "application/json" });
    response.end(JSON.stringify({ error: "unauthorized" }));
  });
  await new Promise<void>((resolve) => probeServer.listen(0, "127.0.0.1", resolve));
  try {
    const address = probeServer.address();
    if (typeof address !== "object" || address === null) throw new Error("missing probe address");
    await expect(
      clickclackAsync(
        [
          "--server",
          `http://127.0.0.1:${address.port}`,
          "send",
          "--workspace",
          workspaceId,
          "--channel",
          channelId,
          "should not leak",
        ],
        env,
      ),
    ).rejects.toThrow();
    expect(leakedAuth).toBe("");
  } finally {
    await new Promise<void>((resolve) => probeServer.close(() => resolve()));
  }
});

test("CLI does not reuse saved workspace defaults for another server", async ({ page }) => {
  const workspacesResponse = await page.request.get("/api/workspaces");
  const workspaces = (await workspacesResponse.json()) as { workspaces: { id: string }[] };
  const oldWorkspaceId = workspaces.workspaces[0].id;
  const channelsResponse = await page.request.get(`/api/workspaces/${oldWorkspaceId}/channels`);
  const channels = (await channelsResponse.json()) as { channels: { id: string }[] };
  const oldChannelId = channels.channels[0].id;

  const ownerMagicToken = clickclack([
    "admin",
    "magic-link",
    "create",
    "--data",
    "./data/e2e",
    "--email",
    "local@clickclack.chat",
    "--name",
    "Local Captain",
  ]);
  const env = isolatedHome();
  clickclack(
    [
      "--server",
      serverURL,
      "--workspace",
      oldWorkspaceId,
      "--channel",
      oldChannelId,
      "login",
      "--magic-token",
      ownerMagicToken,
    ],
    undefined,
    env,
  );

  const requestedPaths: string[] = [];
  const http = await import("node:http");
  const probeServer = http.createServer((request, response) => {
    requestedPaths.push(request.url ?? "");
    response.setHeader("content-type", "application/json");
    if (request.url === "/api/workspaces") {
      response.end(
        JSON.stringify({
          workspaces: [{ id: "wsp_probe", slug: "probe", name: "Probe Workspace" }],
        }),
      );
      return;
    }
    if (request.url === "/api/workspaces/wsp_probe/channels") {
      response.end(
        JSON.stringify({
          workspace: { id: "wsp_probe", slug: "probe", name: "Probe Workspace" },
          channels: [{ id: "chn_probe", name: "general", kind: "public" }],
        }),
      );
      return;
    }
    response.writeHead(404);
    response.end(JSON.stringify({ error: "not found" }));
  });
  await new Promise<void>((resolve) => probeServer.listen(0, "127.0.0.1", resolve));
  try {
    const address = probeServer.address();
    if (typeof address !== "object" || address === null) throw new Error("missing probe address");
    const output = await clickclackAsync(
      ["--server", `http://127.0.0.1:${address.port}`, "channels", "list"],
      env,
    );
    expect(output).toContain("chn_probe");
    expect(requestedPaths).toContain("/api/workspaces/wsp_probe/channels");
    expect(requestedPaths).not.toContain(`/api/workspaces/${oldWorkspaceId}/channels`);
  } finally {
    await new Promise<void>((resolve) => probeServer.close(() => resolve()));
  }
});

test("CLI honors user override when stored credentials exist", async ({ page }) => {
  const workspacesResponse = await page.request.get("/api/workspaces");
  const workspaces = (await workspacesResponse.json()) as { workspaces: { id: string }[] };
  const workspaceId = workspaces.workspaces[0].id;
  const channelsResponse = await page.request.get(`/api/workspaces/${workspaceId}/channels`);
  const channels = (await channelsResponse.json()) as { channels: { id: string }[] };
  const channelId = channels.channels[0].id;

  const ownerMagicToken = clickclack([
    "admin",
    "magic-link",
    "create",
    "--data",
    "./data/e2e",
    "--email",
    "local@clickclack.chat",
    "--name",
    "Local Captain",
  ]);
  const env = isolatedHome();
  clickclack(["--server", serverURL, "login", "--magic-token", ownerMagicToken], undefined, env);

  const secondUserId = clickclack([
    "admin",
    "user",
    "create",
    "--data",
    "./data/e2e",
    "--workspace",
    workspaceId,
    "--name",
    "Override User",
    "--email",
    "override@example.com",
  ]);

  const messageId = clickclack(
    [
      "--server",
      serverURL,
      "--user",
      secondUserId,
      "send",
      "--workspace",
      workspaceId,
      "--channel",
      channelId,
      "--plain",
      "sent with user override",
    ],
    undefined,
    env,
  );

  const messagesJSON = clickclack(
    [
      "--server",
      serverURL,
      "--user",
      secondUserId,
      "messages",
      "list",
      "--workspace",
      workspaceId,
      "--channel",
      channelId,
      "--json",
    ],
    undefined,
    env,
  );
  const messages = JSON.parse(messagesJSON) as {
    messages: { id: string; body: string; author?: { display_name: string } }[];
  };
  expect(messages.messages).toEqual(
    expect.arrayContaining([
      expect.objectContaining({
        id: messageId,
        body: "sent with user override",
        author: expect.objectContaining({ display_name: "Override User" }),
      }),
    ]),
  );
});

test("CLI resolves channel IDs across visible workspaces", async ({ page }) => {
  const firstWorkspacesResponse = await page.request.get("/api/workspaces");
  const firstWorkspaces = (await firstWorkspacesResponse.json()) as {
    workspaces: { id: string }[];
  };
  const defaultWorkspaceId = firstWorkspaces.workspaces[0].id;

  const ownerMagicToken = clickclack([
    "admin",
    "magic-link",
    "create",
    "--data",
    "./data/e2e",
    "--email",
    "local@clickclack.chat",
    "--name",
    "Local Captain",
  ]);
  const ownerToken = clickclack([
    "--server",
    serverURL,
    "login",
    "--magic-token",
    ownerMagicToken,
    "--plain",
    "--no-store",
  ]);

  const workspaceResponse = await page.request.post("/api/workspaces", {
    data: { name: "Other CLI Workspace", slug: "other-cli" },
  });
  const workspace = (await workspaceResponse.json()) as { workspace: { id: string } };
  expect(workspace.workspace.id).not.toBe(defaultWorkspaceId);
  const channelResponse = await page.request.post(
    `/api/workspaces/${workspace.workspace.id}/channels`,
    {
      data: { name: "remote-room", kind: "public" },
    },
  );
  const channel = (await channelResponse.json()) as { channel: { id: string; name: string } };

  const messageId = clickclack([
    "--server",
    serverURL,
    "--token",
    ownerToken,
    "send",
    "--channel",
    channel.channel.id,
    "--plain",
    "cross workspace channel id",
  ]);
  const messagesJSON = clickclack([
    "--server",
    serverURL,
    "--token",
    ownerToken,
    "messages",
    "list",
    "--channel",
    channel.channel.id,
    "--json",
  ]);
  const messages = JSON.parse(messagesJSON) as { messages: { id: string; body: string }[] };
  expect(messages.messages).toEqual(
    expect.arrayContaining([
      expect.objectContaining({ id: messageId, body: "cross workspace channel id" }),
    ]),
  );
});
