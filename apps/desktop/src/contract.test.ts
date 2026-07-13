import assert from "node:assert/strict";
import test from "node:test";
import {
  appURL,
  clampUnreadCount,
  desktopBridgeAllowed,
  desktopMainWindowNavigationAllowed,
  desktopOAuthCallbackCode,
  desktopOAuthStartURL,
  desktopTitleBarOptions,
  deepLinkToRoute,
  hasIntegratedTitleBarCapability,
  mergeSettings,
  normalizeServerURL,
  safeAppRoute,
  sanitizeNotification,
} from "./contract";
import {
  activeDesktopAuthAttempt,
  applyWindowsUnreadOverlay,
  isValidClickClackProbeResponse,
  nextDesktopAuthAttempt,
  RendererSignalQueue,
} from "./runtime";

test("normalizes hosted and loopback servers", () => {
  assert.equal(normalizeServerURL("https://chat.example.com/app/"), "https://chat.example.com");
  assert.equal(normalizeServerURL("http://127.0.0.1:8080"), "http://127.0.0.1:8080");
  assert.throws(() => normalizeServerURL("http://chat.example.com"), /HTTPS/);
  assert.throws(() => normalizeServerURL("https://user:secret@chat.example.com"), /credentials/);
  assert.throws(() => normalizeServerURL("https://chat.example.com/tenant"), /extra path/);
});

test("keeps navigation inside ClickClack app routes", () => {
  assert.equal(
    safeAppRoute("/app/team/general?from=notification"),
    "/app/team/general?from=notification",
  );
  assert.equal(
    appURL("https://chat.example.com", "/app/team"),
    "https://chat.example.com/app/team",
  );
  assert.equal(safeAppRoute("https://evil.example/app"), null);
  assert.equal(safeAppRoute("//evil.example/app"), null);
  assert.equal(safeAppRoute("/docs"), null);
});

test("maps explicit deep-link forms to app routes", () => {
  assert.equal(deepLinkToRoute("clickclack://app/team/general"), "/app/team/general");
  assert.equal(
    deepLinkToRoute("clickclack://open?path=%2Fapp%2Fteam%2Fgeneral"),
    "/app/team/general",
  );
  assert.equal(deepLinkToRoute("clickclack://evil/app/team"), null);
  assert.equal(deepLinkToRoute("https://chat.example.com/app/team"), null);
});

test("builds and validates the desktop OAuth handoff", () => {
  const challenge = "a".repeat(43);
  assert.equal(
    desktopOAuthStartURL("https://chat.example.com", challenge),
    `https://chat.example.com/api/auth/github/desktop/start?code_challenge=${challenge}`,
  );
  assert.throws(() => desktopOAuthStartURL("https://chat.example.com", "short"), /challenge/);
  assert.equal(
    desktopOAuthCallbackCode(`clickclack://auth/callback?code=${"a1".repeat(16)}`),
    "a1".repeat(16),
  );
  assert.equal(desktopOAuthCallbackCode("clickclack://auth/callback?code=bad"), null);
  assert.equal(desktopOAuthCallbackCode(`clickclack://app/callback?code=${"a1".repeat(16)}`), null);
});

test("exposes the desktop bridge only to the configured server origin", () => {
  assert.equal(
    desktopBridgeAllowed("https://app.clickclack.chat", "https://app.clickclack.chat"),
    true,
  );
  assert.equal(desktopBridgeAllowed("https://github.com", "https://app.clickclack.chat"), false);
  assert.equal(desktopBridgeAllowed("https://app.clickclack.chat", undefined), false);
});

test("keeps integrated desktop chrome on app routes", () => {
  assert.equal(
    desktopMainWindowNavigationAllowed(
      "https://chat.example.com/app/team/general",
      "https://chat.example.com",
      true,
    ),
    true,
  );
  assert.equal(
    desktopMainWindowNavigationAllowed(
      "https://chat.example.com/",
      "https://chat.example.com",
      true,
    ),
    false,
  );
  assert.equal(
    desktopMainWindowNavigationAllowed(
      "https://chat.example.com/",
      "https://chat.example.com",
      false,
    ),
    true,
  );
  assert.equal(
    desktopMainWindowNavigationAllowed(
      "https://other.example/app",
      "https://chat.example.com",
      false,
    ),
    false,
  );
});

test("uses integrated native title bars on each desktop platform", () => {
  assert.deepEqual(desktopTitleBarOptions("darwin", true), {
    titleBarStyle: "hiddenInset",
    trafficLightPosition: { x: 16, y: 18 },
  });
  assert.deepEqual(desktopTitleBarOptions("win32", true), {
    titleBarOverlay: {
      color: "#17181e",
      height: 52,
      symbolColor: "#e7e9ee",
    },
    titleBarStyle: "hidden",
  });
  assert.deepEqual(desktopTitleBarOptions("linux", false), {
    titleBarOverlay: {
      color: "#fbf6ee",
      height: 52,
      symbolColor: "#22201d",
    },
    titleBarStyle: "hidden",
  });
});

test("detects renderer support before replacing native window chrome", () => {
  assert.equal(
    hasIntegratedTitleBarCapability(
      '<html><head><meta name="clickclack-desktop-titlebar" content="1" /></head></html>',
    ),
    true,
  );
  assert.equal(
    hasIntegratedTitleBarCapability(
      '<html><head><meta name="clickclack-desktop-titlebar" content="0" /></head></html>',
    ),
    false,
  );
  assert.equal(hasIntegratedTitleBarCapability("<html><head></head></html>"), false);
});

test("bounds badge and notification data from the renderer", () => {
  assert.equal(clampUnreadCount(-4), 0);
  assert.equal(clampUnreadCount(20_000), 9999);
  assert.deepEqual(
    sanitizeNotification({
      title: " Agent reply ",
      body: " Finished the task ",
      route: "/app/team/agents",
      tag: "msg_1",
    }),
    {
      title: "Agent reply",
      body: "Finished the task",
      route: "/app/team/agents",
      tag: "msg_1",
    },
  );
  assert.equal(sanitizeNotification({ title: "", body: "nope" }), null);
});

test("recovers safely from malformed persisted settings", () => {
  const settings = mergeSettings({
    closeToTray: false,
    serverUrl: "javascript:alert(1)",
    startAtLogin: true,
    window: { width: 120, height: 900, x: 42, maximized: true },
  });
  assert.equal(settings.serverUrl, "https://app.clickclack.chat");
  assert.equal(settings.closeToTray, false);
  assert.equal(settings.startAtLogin, true);
  assert.deepEqual(settings.window, {
    width: undefined,
    height: 900,
    x: 42,
    y: undefined,
    maximized: true,
  });
});

test("accepts only a genuine same-origin ClickClack readiness response", async () => {
  const response = (
    url: string,
    status: number,
    body: unknown,
    redirected = false,
  ): Pick<Response, "json" | "redirected" | "status" | "url"> => ({
    json: async () => body,
    redirected,
    status,
    url,
  });
  const serverUrl = "https://chat.example.com";
  const readyUrl = `${serverUrl}/readyz`;

  assert.equal(
    await isValidClickClackProbeResponse(response(readyUrl, 200, { status: "ready" }), serverUrl),
    true,
  );
  assert.equal(
    await isValidClickClackProbeResponse(
      response(readyUrl, 200, { status: "ready" }, true),
      serverUrl,
    ),
    false,
  );
  assert.equal(
    await isValidClickClackProbeResponse(
      response("https://other.example/readyz", 200, { status: "ready" }),
      serverUrl,
    ),
    false,
  );
  assert.equal(
    await isValidClickClackProbeResponse(response(readyUrl, 404, { status: "ready" }), serverUrl),
    false,
  );
  assert.equal(
    await isValidClickClackProbeResponse(response(readyUrl, 200, { status: "ok" }), serverUrl),
    false,
  );
});

test("reopens active desktop OAuth attempts and expires stale state", () => {
  const startedAt = 10_000;
  const first = nextDesktopAuthAttempt(
    null,
    "https://chat.example.com",
    "first-verifier",
    startedAt,
  );
  const duplicate = nextDesktopAuthAttempt(
    first.attempt,
    "https://chat.example.com",
    "second-verifier",
    startedAt + 1,
  );
  assert.equal(first.shouldOpen, true);
  assert.equal(duplicate.shouldOpen, true);
  assert.equal(duplicate.attempt, first.attempt);
  assert.equal(duplicate.attempt.verifier, "first-verifier");
  assert.equal(
    activeDesktopAuthAttempt(first.attempt, "https://chat.example.com", startedAt + 299_999),
    first.attempt,
  );
  assert.equal(
    activeDesktopAuthAttempt(first.attempt, "https://chat.example.com", startedAt + 300_000),
    null,
  );
  const replacement = nextDesktopAuthAttempt(
    first.attempt,
    "https://chat.example.com",
    "replacement-verifier",
    startedAt + 300_000,
  );
  assert.equal(replacement.shouldOpen, true);
  assert.equal(replacement.attempt.verifier, "replacement-verifier");
});

test("reapplies Windows unread overlays to replacement windows", () => {
  const calls: Array<{ description: string; overlay: string | null; window: string }> = [];
  const createWindow = (name: string) => ({
    setOverlayIcon(overlay: string | null, description: string) {
      calls.push({ description, overlay, window: name });
    },
  });
  applyWindowsUnreadOverlay("win32", createWindow("initial"), 7, () => "badge");
  applyWindowsUnreadOverlay("win32", createWindow("replacement"), 7, () => "badge");
  applyWindowsUnreadOverlay("darwin", createWindow("mac"), 7, () => "unused");
  assert.deepEqual(calls, [
    { description: "7 unread messages", overlay: "badge", window: "initial" },
    { description: "7 unread messages", overlay: "badge", window: "replacement" },
  ]);
});

test("queues Quick Compose until the renderer finishes loading", () => {
  const queue = new RendererSignalQueue();
  assert.equal(queue.request(), false);
  assert.equal(queue.finishLoading(), true);
  assert.equal(queue.finishLoading(), false);
  assert.equal(queue.request(), true);
  queue.beginLoading();
  assert.equal(queue.request(), false);
  assert.equal(queue.finishLoading(), true);
});
