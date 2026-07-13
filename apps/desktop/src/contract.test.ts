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
    `https://chat.example.com/api/auth/github/desktop/start?code_challenge=${challenge}&desktop_protocol=2`,
  );
  assert.throws(() => desktopOAuthStartURL("https://chat.example.com", "short"), /challenge/);
  assert.equal(
    desktopOAuthCallbackCode(`clickclack://auth/callback?code=${"a1".repeat(16)}`),
    "a1".repeat(16),
  );
  assert.equal(
    desktopOAuthCallbackCode(`chat.clickclack.desktop:/auth/callback?code=${"a1".repeat(16)}`),
    "a1".repeat(16),
  );
  assert.equal(
    desktopOAuthCallbackCode(`chat.clickclack.desktop:/auth/callback?code=${"A".repeat(43)}`),
    "A".repeat(43),
  );
  assert.equal(desktopOAuthCallbackCode("clickclack://auth/callback?code=bad"), null);
  assert.equal(desktopOAuthCallbackCode(`clickclack://app/callback?code=${"a1".repeat(16)}`), null);
  assert.equal(
    desktopOAuthCallbackCode(`chat.clickclack.desktop:/wrong?code=${"a1".repeat(16)}`),
    null,
  );
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
