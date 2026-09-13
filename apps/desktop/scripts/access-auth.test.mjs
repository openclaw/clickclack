import assert from "node:assert/strict";
import test from "node:test";
import { A, B, deferred, desktop, settle } from "./main-harness.mjs";

const ACCESS = "https://example.cloudflareaccess.com";
const login = `${ACCESS}/cdn-cgi/access/login/127.0.0.1?redirect_url=%2Fapp`;

function navigate(window, url, name = "will-redirect", mainFrame = true) {
  let prevented = false;
  window.webContents.emit(
    name,
    {
      url,
      isMainFrame: mainFrame,
      preventDefault() {
        prevented = true;
      },
    },
    url,
    false,
    mainFrame,
  );
  return prevented;
}

function authWindow(d) {
  return d.windows.findLast(
    (window) =>
      !window.destroyed &&
      window.options.parent === d.main &&
      !window.options.webPreferences.preload,
  );
}

test("Access redirects use the app session in an isolated window, preserving the original route", async (t) => {
  const d = await desktop(t);
  const main = d.main;
  d.send(main, "desktop:set-active-route", "/app/team/general");
  main.url = `${A}/app/team/general`;
  assert.equal(navigate(main, login), true);
  await settle();
  const auth = authWindow(d);
  assert.ok(auth);
  assert.equal(auth.options.webPreferences.session, main.webContents.session);
  assert.equal(auth.options.webPreferences.preload, undefined);
  assert.equal(auth.options.webPreferences.nodeIntegration, false);
  assert.equal(auth.options.webPreferences.contextIsolation, true);
  assert.equal(auth.options.webPreferences.sandbox, true);
  assert.equal(auth.options.webPreferences.webSecurity, true);
  assert.equal(d.session.permissionCheck(), false);
  let permission;
  d.session.permissionRequest(auth.webContents, "notifications", (allowed) => {
    permission = allowed;
  });
  assert.equal(permission, false);
  assert.deepEqual(d.browserURLs, []);
  assert.equal(auth.openHandler({ url: "https://external.example/" }).action, "deny");

  // Redirecting to the callback is not completion: its response must set the cookies first.
  navigate(auth, `${A}/cdn-cgi/access/authorized?nonce=redacted`);
  assert.equal(auth.destroyed, false);
  navigate(auth, `${A}/app`);
  assert.equal(auth.destroyed, false);
  auth.webContents.emit("did-navigate", {}, `${A}/app`);
  await settle();
  assert.equal(auth.destroyed, true);
  assert.equal(main.loads.at(-1), `${A}/app/team/general`);
  assert.deepEqual(d.errors, []);
});

test("ordinary external links and non-Access redirects retain browser behavior", async (t) => {
  const d = await desktop(t);
  assert.equal(navigate(d.main, "https://external.example/"), true);
  assert.equal(authWindow(d), undefined);
  assert.equal(navigate(d.main, login, "will-navigate"), true);
  assert.equal(authWindow(d), undefined);
  assert.deepEqual(d.browserURLs, ["https://external.example/", login]);
});

test("subframe redirects cannot start Access and repeated redirects cannot spawn extra windows", async (t) => {
  const d = await desktop(t);
  assert.equal(navigate(d.main, login, "will-redirect", false), false);
  assert.equal(authWindow(d), undefined);
  navigate(d.main, login);
  await settle();
  const auth = authWindow(d);
  const count = d.windows.length;
  navigate(d.main, login);
  await settle();
  assert.equal(authWindow(d), auth);
  assert.equal(d.windows.length, count);
});

test("Access rejects unrelated origins and custom protocols without opening a browser", async (t) => {
  for (const url of [
    "https://idp.example/",
    "http://example.cloudflareaccess.com/",
    "file:///tmp/a",
    "javascript:alert(1)",
  ]) {
    const d = await desktop(t);
    navigate(d.main, login);
    await settle();
    const auth = authWindow(d);
    assert.equal(navigate(auth, url, "will-frame-navigate"), true);
    await settle();
    assert.deepEqual(d.browserURLs, []);
    assert.ok(d.errors.length > 0);
    assert.match(d.errors[0], /turn off Eager redirect cookie/);
  }
});

test("Access guards subframe navigation and permits only the Turnstile frame exception", async (t) => {
  const d = await desktop(t);
  navigate(d.main, login);
  await settle();
  const auth = authWindow(d);
  assert.equal(
    navigate(auth, "https://challenges.cloudflare.com/turnstile", "will-frame-navigate", false),
    false,
  );
  assert.equal(navigate(auth, "https://external.example/", "will-frame-navigate", false), true);
  assert.equal(auth.destroyed, true);
  assert.deepEqual(d.browserURLs, []);

  navigate(d.main, login);
  await settle();
  assert.equal(
    navigate(authWindow(d), "https://challenges.cloudflare.com/", "will-frame-navigate"),
    true,
  );
});

test("Access windows cannot download or send desktop bridge messages", async (t) => {
  const d = await desktop(t);
  navigate(d.main, login);
  await settle();
  const auth = authWindow(d);
  let prevented = false;
  d.session.emit(
    "will-download",
    {
      preventDefault() {
        prevented = true;
      },
    },
    { once() {} },
    auth.webContents,
  );
  assert.equal(prevented, true);
  d.send(auth, "desktop:set-unread", 99);
  assert.equal(d.badges.includes(99), false);
});

test("server changes invalidate Access before a late callback can navigate the replacement", async (t) => {
  const d = await desktop(t);
  navigate(d.main, login);
  await settle();
  const auth = authWindow(d);
  await d.save(B);
  const replacement = d.main;
  assert.equal(auth.destroyed, true);
  auth.webContents.emit("did-navigate", {}, `${A}/app`);
  await settle();
  assert.deepEqual(replacement.loads, [`${B}/app`]);
  assert.deepEqual(d.errors, []);
});

test("closing the main window invalidates an Access callback", async (t) => {
  const d = await desktop(t);
  const main = d.main;
  navigate(main, login);
  await settle();
  const auth = authWindow(d);
  const windowsBeforeClose = d.windows.length;
  main.destroy();
  assert.equal(auth.destroyed, true);
  assert.equal(d.windows.length, windowsBeforeClose);
  const count = main.loads.length;
  auth.webContents.emit("did-navigate", {}, `${A}/app`);
  await settle();
  assert.equal(main.loads.length, count);
  assert.deepEqual(d.errors, []);
});

test("cancelled Access reports recovery instructions without reloading or retaining the attempt", async (t) => {
  const d = await desktop(t);
  navigate(d.main, login);
  await settle();
  const auth = authWindow(d);
  const count = d.main.loads.length;
  auth.destroy();
  await settle();
  assert.equal(d.main.loads.length, count);
  assert.ok(d.errors.length > 0);
  navigate(d.main, login);
  await settle();
  assert.notEqual(authWindow(d), auth);
});

test("Reload recovers the server route after cancelled Access", async (t) => {
  const d = await desktop(t);
  navigate(d.main, login);
  await settle();
  authWindow(d).destroy();
  await settle();
  const view = d.applicationMenu.find((entry) => entry.label === "View");
  view.submenu.find((entry) => entry.label === "Reload").click();
  await settle();
  assert.equal(d.main.loads.at(-1), `${A}/app`);
});

test("quitting clears Access without showing errors or recreating a window", async (t) => {
  const d = await desktop(t);
  navigate(d.main, login);
  await settle();
  const auth = authWindow(d);
  const count = d.windows.length;
  const shows = d.main.shows;
  d.app.emit("before-quit");
  await settle();
  assert.equal(auth.destroyed, true);
  assert.equal(d.windows.length, count);
  assert.equal(d.main.shows, shows);
  assert.deepEqual(d.errors, []);
});

test("later OTP navigation failures offer recovery without exposing the authentication URL", async (t) => {
  const d = await desktop(t);
  navigate(d.main, login);
  await settle();
  const auth = authWindow(d);
  auth.webContents.emit("did-fail-load", {}, -3, "ERR_ABORTED", login, true);
  assert.equal(auth.destroyed, false);
  auth.webContents.emit(
    "did-fail-load",
    {},
    -105,
    "ERR_NAME_NOT_RESOLVED",
    `${login}&token=secret`,
    true,
  );
  await settle();
  assert.equal(auth.destroyed, true);
  assert.equal(d.errors.length, 1);
  assert.equal(d.errors[0].includes("secret"), false);
});

for (const outcome of ["success", "error"]) {
  test(`server changes during Access completion suppress stale ${outcome}`, async (t) => {
    const d = await desktop(t);
    const original = d.main;
    navigate(original, login);
    await settle();
    const auth = authWindow(d);
    const held = deferred();
    d.controls.loadURL = (window) => (window === original ? held.promise : Promise.resolve());
    auth.webContents.emit("did-navigate", {}, `${A}/app`);
    await settle();
    await d.save(B);
    if (outcome === "error") held.reject(new Error("stale navigation failed"));
    else held.resolve();
    await settle();
    assert.deepEqual(d.main.loads, [`${B}/app`]);
    assert.deepEqual(d.errors, []);
  });
}
