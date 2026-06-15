import { expect, test, type Page } from "@playwright/test";
import { execFile, execFileSync } from "node:child_process";
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { promisify } from "node:util";

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
        const composer = el.querySelector<HTMLElement>(".composer-card");
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
  await expect(page.getByRole("heading", { name: "ClickClack" })).toBeVisible();
  await expect(page.locator(".product-site")).toHaveCSS("display", "block");
  await expect(page.getByRole("link", { name: "Open app" })).toHaveAttribute("href", "/app");
  await expect(page.getByRole("link", { name: "Read docs" })).toHaveAttribute(
    "href",
    "https://docs.clickclack.chat",
  );
  await expect(page.getByText("Self-hostable chat. Serious tool. Mild brine.")).toBeVisible();
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
  await page
    .getByRole("button", { name: /Account settings for Local Captain/ })
    .click({ button: "right" });
  await expect(page.getByRole("heading", { name: "Profile settings" })).toBeVisible();

  await page.getByLabel("Browser notifications").check();

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

  const composer = page.locator('textarea[aria-label="Message body"]');
  const toggle = page.getByRole("button", { name: "Toggle navigation" });
  await expect(toggle).toHaveAttribute("aria-expanded", "false");

  await toggle.click();
  await expect(toggle).toHaveAttribute("aria-expanded", "true");
  await expect(page.getByRole("button", { name: "Close navigation" })).toBeVisible();

  await page.getByRole("button", { name: "Collapse sidebar" }).click();
  await expect(toggle).toHaveAttribute("aria-expanded", "false");

  await toggle.click();
  await expect(toggle).toHaveAttribute("aria-expanded", "true");
  await page.setViewportSize({ width: 1024, height: 844 });
  await expect(page.getByRole("button", { name: "Collapse sidebar" })).toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(toggle).toHaveAttribute("aria-expanded", "false");

  await toggle.click();
  await page.keyboard.type("hidden draft");
  await expect(composer).toHaveValue("");

  await page.keyboard.press("Escape");
  await expect(toggle).toHaveAttribute("aria-expanded", "false");

  await toggle.click();
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

  await page.getByRole("button", { name: "Open thread" }).first().click();
  await expect(page.getByText("Thread", { exact: true })).toBeVisible();

  await page.getByLabel("Reply body").fill("thread _reply_");
  await page.locator(".reply-composer").getByRole("button", { name: "Reply" }).click();
  await expect(page.locator(".reply .markdown").filter({ hasText: "thread reply" })).toBeVisible();

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
  const channelName = `notify-${Date.now()}`;
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
  const remoteResponse = await page.request.post(`/api/channels/${channel.channel.id}/messages`, {
    headers: { "X-ClickClack-User": senderID },
    data: { body: "ping from another channel" },
  });
  expect(remoteResponse.ok()).toBe(true);

  await expect
    .poll(() =>
      page.evaluate(
        () =>
          (
            window as unknown as {
              __clickclackNotifications: { title: string; options?: NotificationOptions }[];
            }
          ).__clickclackNotifications,
      ),
    )
    .toEqual([
      expect.objectContaining({
        title: expect.stringContaining("ClickClack in #notify-"),
        options: expect.objectContaining({
          body: "New message",
          icon: "/favicon.svg",
        }),
      }),
    ]);

  await page.evaluate(() => {
    const notification = (
      window as unknown as {
        __clickclackNotifications: { onclick?: (() => void) | null }[];
      }
    ).__clickclackNotifications[0];
    notification.onclick?.();
  });

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
  expect(newerResponses).toHaveLength(1);
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
