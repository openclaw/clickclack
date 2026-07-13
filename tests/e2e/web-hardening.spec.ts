import { expect, test, type Page, type Route } from "@playwright/test";
import { connectRealtime } from "../../apps/web/src/lib/realtime.svelte";
import type { RealtimeEvent, User } from "../../apps/web/src/lib/types";

type SocketListener = (event: { data?: unknown }) => void;

class FakeWebSocket {
  static instances: FakeWebSocket[] = [];

  readonly url: string;
  readonly listeners = new Map<string, SocketListener[]>();
  closeCalls = 0;

  constructor(url: string | URL) {
    this.url = String(url);
    FakeWebSocket.instances.push(this);
  }

  addEventListener(type: string, listener: SocketListener) {
    this.listeners.set(type, [...(this.listeners.get(type) ?? []), listener]);
  }

  emit(type: string, data?: unknown) {
    for (const listener of this.listeners.get(type) ?? []) listener({ data });
  }

  close() {
    this.closeCalls++;
    this.emit("close");
  }
}

function realtimeEvent(cursor: string): RealtimeEvent {
  return {
    id: `event-${cursor}`,
    cursor,
    type: "message.created",
    workspace_id: "workspace-1",
    created_at: "2026-07-13T00:00:00Z",
    payload: {},
  };
}

function installRealtimeGlobals(storage: {
  getItem: (key: string) => string | null;
  setItem: (key: string, value: string) => void;
}) {
  const windowDescriptor = Object.getOwnPropertyDescriptor(globalThis, "window");
  const webSocketDescriptor = Object.getOwnPropertyDescriptor(globalThis, "WebSocket");
  FakeWebSocket.instances = [];
  Object.defineProperty(globalThis, "window", {
    configurable: true,
    value: {
      location: {
        href: "http://clickclack.test/app",
        protocol: "http:",
      },
      localStorage: storage,
      setTimeout: (handler: TimerHandler, timeout?: number) =>
        globalThis.setTimeout(handler as () => void, timeout) as unknown as number,
      clearTimeout: (timer: number) => globalThis.clearTimeout(timer),
    },
  });
  Object.defineProperty(globalThis, "WebSocket", {
    configurable: true,
    value: FakeWebSocket,
  });
  return () => {
    if (windowDescriptor) Object.defineProperty(globalThis, "window", windowDescriptor);
    else Reflect.deleteProperty(globalThis, "window");
    if (webSocketDescriptor) Object.defineProperty(globalThis, "WebSocket", webSocketDescriptor);
    else Reflect.deleteProperty(globalThis, "WebSocket");
  };
}

test("realtime serializes async delivery and persists each cursor afterward", async () => {
  const writes: string[] = [];
  const delivered: string[] = [];
  let releaseFirst: (() => void) | undefined;
  const firstDelivery = new Promise<void>((resolve) => {
    releaseFirst = resolve;
  });
  const restore = installRealtimeGlobals({
    getItem: () => "cursor-0",
    setItem: (_key, value) => writes.push(value),
  });

  try {
    const connection = connectRealtime({
      workspaceID: "workspace-1",
      onEvent: async (event) => {
        delivered.push(event.cursor);
        if (event.cursor === "cursor-1") await firstDelivery;
      },
    });
    const socket = FakeWebSocket.instances[0];
    expect(socket.url).toContain("after_cursor=cursor-0");

    socket.emit("message", JSON.stringify(realtimeEvent("cursor-1")));
    socket.emit("message", JSON.stringify(realtimeEvent("cursor-2")));

    await expect.poll(() => delivered).toEqual(["cursor-1"]);
    expect(writes).toEqual([]);

    releaseFirst?.();
    await expect.poll(() => delivered).toEqual(["cursor-1", "cursor-2"]);
    await expect.poll(() => writes).toEqual(["cursor-1", "cursor-2"]);
    connection.close();
  } finally {
    restore();
  }
});

test("realtime storage failures do not block startup or delivery", async () => {
  let delivered = 0;
  const restore = installRealtimeGlobals({
    getItem: () => {
      throw new Error("storage read blocked");
    },
    setItem: () => {
      throw new Error("storage write blocked");
    },
  });

  try {
    const connection = connectRealtime({
      workspaceID: "workspace-1",
      onEvent: () => {
        delivered++;
      },
    });
    const socket = FakeWebSocket.instances[0];
    expect(socket.url).not.toContain("after_cursor");

    socket.emit("message", JSON.stringify(realtimeEvent("cursor-1")));
    await expect.poll(() => delivered).toBe(1);
    expect(socket.closeCalls).toBe(0);
    connection.close();
  } finally {
    restore();
  }
});

test("realtime does not advance past a failed event", async () => {
  const writes: string[] = [];
  const restore = installRealtimeGlobals({
    getItem: () => null,
    setItem: (_key, value) => writes.push(value),
  });
  const originalConsoleError = console.error;
  console.error = () => {};

  try {
    const connection = connectRealtime({
      workspaceID: "workspace-1",
      reconnectDelayMs: 60_000,
      onEvent: async () => {
        throw new Error("delivery failed");
      },
    });
    const socket = FakeWebSocket.instances[0];
    socket.emit("message", JSON.stringify(realtimeEvent("cursor-1")));

    await expect.poll(() => socket.closeCalls).toBe(1);
    expect(writes).toEqual([]);
    connection.close();
  } finally {
    console.error = originalConsoleError;
    restore();
  }
});

test("realtime never persists a cursor from an obsolete reconnect generation", async () => {
  const writes: string[] = [];
  const delivered: string[] = [];
  let releaseFirst: (() => void) | undefined;
  const firstDelivery = new Promise<void>((resolve) => {
    releaseFirst = resolve;
  });
  const restore = installRealtimeGlobals({
    getItem: () => null,
    setItem: (_key, value) => writes.push(value),
  });

  try {
    const connection = connectRealtime({
      workspaceID: "workspace-1",
      reconnectDelayMs: 0,
      onEvent: async (event) => {
        delivered.push(event.cursor);
        if (event.cursor === "cursor-1") await firstDelivery;
      },
    });
    const firstSocket = FakeWebSocket.instances[0];
    firstSocket.emit("message", JSON.stringify(realtimeEvent("cursor-1")));
    await expect.poll(() => delivered).toEqual(["cursor-1"]);

    firstSocket.emit("close");
    await expect.poll(() => FakeWebSocket.instances).toHaveLength(2);
    FakeWebSocket.instances[1].emit("message", JSON.stringify(realtimeEvent("cursor-2")));

    releaseFirst?.();
    await expect.poll(() => delivered).toEqual(["cursor-1", "cursor-2"]);
    await expect.poll(() => writes).toEqual(["cursor-2"]);
    connection.close();
  } finally {
    restore();
  }
});

async function currentUser(page: Page): Promise<User> {
  const response = await page.request.get("/api/me");
  expect(response.ok()).toBe(true);
  return ((await response.json()) as { user: User }).user;
}

test("account settings trap focus, inert the app, restore focus, and PATCH owned fields", async ({
  page,
}) => {
  const user = await currentUser(page);
  const patches: Record<string, unknown>[] = [];
  await page.route("**/api/me", async (route) => {
    if (route.request().method() !== "PATCH") {
      await route.fallback();
      return;
    }
    const patch = route.request().postDataJSON() as Record<string, unknown>;
    patches.push(patch);
    await route.fulfill({
      json: {
        user: {
          ...user,
          ...patch,
          notification_settings:
            (patch.notification_settings as User["notification_settings"]) ??
            user.notification_settings,
        },
      },
    });
  });

  await page.goto("/app");
  const trigger = page.getByRole("button", { name: /Account settings for/ });
  await trigger.click();

  const dialog = page.getByRole("dialog", { name: "Account settings" });
  const close = dialog.getByRole("button", { name: "Close", exact: true });
  await expect(dialog).toBeVisible();
  await expect(page.locator(".shell")).toHaveAttribute("inert", "");
  await expect(page.locator(".shell")).toHaveAttribute("aria-hidden", "true");
  await expect(close).toBeFocused();

  await expect(dialog.getByRole("button", { name: "Save profile" })).toBeVisible();
  await page.keyboard.press("Shift+Tab");
  await expect(dialog.getByRole("button", { name: "Save profile" })).toBeFocused();
  await page.keyboard.press("Tab");
  await expect(close).toBeFocused();

  await dialog.getByLabel("Display name").fill("Focused User");
  await dialog.getByRole("button", { name: "Save profile" }).click();
  await expect(dialog).toHaveCount(0);
  await expect(page.locator(".shell")).not.toHaveAttribute("inert", "");
  await expect(page.locator(".shell")).not.toHaveAttribute("aria-hidden", "true");
  await expect(page.locator(".user-card")).toBeFocused();
  expect(Object.keys(patches[0]).sort()).toEqual(["avatar_url", "display_name", "handle"]);

  await page.locator(".user-card").click();
  await page.getByRole("button", { name: "Notifications" }).click();
  await page.getByLabel("Pushover notifications").check();
  await page.getByLabel("Pushover user key").fill("u-focused");
  await page.getByRole("button", { name: "Save notifications" }).click();
  await expect.poll(() => patches.length).toBe(2);
  expect(Object.keys(patches[1])).toEqual(["notification_settings"]);

  await page.getByLabel("Pushover user key").focus();
  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);
  await expect(page.locator(".user-card")).toBeFocused();
});

async function createWorkspaceWithChannel(page: Page, label: string) {
  const stamp = `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
  const workspaceResponse = await page.request.post("/api/workspaces", {
    data: { name: `${label} ${stamp}` },
  });
  expect(workspaceResponse.ok()).toBe(true);
  const { workspace } = (await workspaceResponse.json()) as {
    workspace: { id: string; route_id: string; name: string };
  };
  const channelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: `channel-${stamp}`, kind: "public" },
  });
  expect(channelResponse.ok()).toBe(true);
  const { channel } = (await channelResponse.json()) as {
    channel: { id: string; route_id: string; name: string };
  };
  return { workspace, channel };
}

function delayedUploadRoute() {
  let release: (() => void) | undefined;
  const wait = new Promise<void>((resolve) => {
    release = resolve;
  });
  let started: (() => void) | undefined;
  const requestStarted = new Promise<void>((resolve) => {
    started = resolve;
  });
  return {
    requestStarted,
    release: () => release?.(),
    handler: async (route: Route) => {
      started?.();
      await wait;
      await route.fallback();
    },
  };
}

test("message send stays blocked while its upload is pending", async ({ page }) => {
  const { workspace, channel } = await createWorkspaceWithChannel(page, "Upload wait");
  const upload = delayedUploadRoute();
  let messagePosts = 0;
  page.on("request", (request) => {
    if (
      request.method() === "POST" &&
      request.url().endsWith(`/api/channels/${channel.id}/messages`)
    ) {
      messagePosts++;
    }
  });
  await page.route("**/api/uploads", upload.handler);
  await page.goto(`/app/${workspace.route_id}/${channel.route_id}`);
  await expect(page.getByLabel("Message body")).toHaveAttribute(
    "placeholder",
    `Message #${channel.name}`,
  );

  const selectFile = page.getByLabel("Upload file").setInputFiles({
    name: "blocked-send.txt",
    mimeType: "text/plain",
    buffer: Buffer.from("wait for me"),
  });
  await upload.requestStarted;
  await page.getByLabel("Message body").fill("send with completed upload");
  await page.getByRole("button", { name: "Send" }).click();
  await expect.poll(() => messagePosts).toBe(0);
  await expect(page.getByLabel("Message body")).toHaveValue("send with completed upload");

  upload.release();
  await selectFile;
  await expect(page.getByText("blocked-send.txt")).toBeVisible();
  await page.getByRole("button", { name: "Send" }).click();
  await expect.poll(() => messagePosts).toBe(1);
  await expect(
    page.locator(".markdown").filter({ hasText: "send with completed upload" }),
  ).toBeVisible();
});

test("workspace changes discard uploads that finish for the previous workspace", async ({
  page,
}) => {
  const first = await createWorkspaceWithChannel(page, "Upload source");
  const second = await createWorkspaceWithChannel(page, "Upload destination");
  const upload = delayedUploadRoute();
  await page.route("**/api/uploads", upload.handler);
  await page.goto(`/app/${first.workspace.route_id}/${first.channel.route_id}`);
  await expect(page.getByLabel("Message body")).toHaveAttribute(
    "placeholder",
    `Message #${first.channel.name}`,
  );

  const selectFile = page.getByLabel("Upload file").setInputFiles({
    name: "stale-workspace.txt",
    mimeType: "text/plain",
    buffer: Buffer.from("stale"),
  });
  await upload.requestStarted;
  await page.getByRole("link", { name: second.workspace.name }).click();
  await expect(page).toHaveURL(new RegExp(`/app/${second.workspace.route_id}/`));

  upload.release();
  await selectFile;
  await expect(page.getByText("stale-workspace.txt")).toHaveCount(0);
  await page.getByLabel("Message body").fill("new workspace text");
  await page.getByRole("button", { name: "Send" }).click();
  await expect(page.locator(".markdown").filter({ hasText: "new workspace text" })).toBeVisible();
  await expect(page.getByText("stale-workspace.txt")).toHaveCount(0);
});
