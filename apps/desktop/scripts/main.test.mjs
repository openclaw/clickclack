import assert from "node:assert/strict";
import { mkdir, readFile, rm, writeFile } from "node:fs/promises";
import test from "node:test";
import { A, B, C, deferred, desktop, settle, until } from "./main-harness.mjs";

for (const failure of ["write", "rename"]) {
  test(`failed settings ${failure} preserves server, route, window and sender authority`, async (t) => {
    const d = await desktop(t);
    const original = d.main;
    d.send(original, "desktop:set-active-route", "/app/team/general");
    const blockedPath = failure === "write" ? `${d.destination}.tmp` : d.destination;
    if (failure === "rename") await rm(blockedPath);
    await mkdir(blockedPath);
    await assert.rejects(d.save(B));
    assert.equal(d.getSettings().serverUrl, A);
    assert.equal(d.main, original);
    d.send(original, "desktop:set-unread", 7);
    assert.equal(d.badges.at(-1), 7);
    d.app.emit("second-instance", {}, []);
    assert.deepEqual(original.messages.at(-1), ["desktop:navigate", "/app/team/general"]);
    await rm(blockedPath, { recursive: true });
    original.bounds.width = 1440;
    original.emit("resize");
    await d.flushTimers();
    await d.idle();
    assert.equal((await d.disk()).serverUrl, A);
    assert.equal((await d.disk()).window.width, 1440);
    assert.equal((await d.save(B)).serverUrl, B);
    assert.equal(d.main.url, `${B}/app`);
  });
}

test("settings saves and window writes stay ordered while persistence and capability probing wait", async (t) => {
  const d = await desktop(t);
  const original = d.main;
  const probe = deferred();
  const write = deferred();
  const reachedWrite = deferred();
  d.controls.probe = (url) =>
    url.startsWith(B) ? probe.promise : Promise.resolve(new Response(""));
  let held = false;
  d.controls.writeFile = async (...args) => {
    if (!held) {
      held = true;
      reachedWrite.resolve();
      await write.promise;
    }
    return writeFile(...args);
  };
  const saveB = d.save(B);
  const saveC = d.save(C);
  await settle();
  const duringSave = { server: d.getSettings().serverUrl, window: d.main };
  probe.resolve(new Response('<head><meta name="clickclack-desktop-titlebar" content="1"></head>'));
  await reachedWrite.promise;
  original.bounds.width = 1500;
  original.emit("resize");
  await d.flushTimers();
  write.resolve();
  const results = await Promise.all([saveB, saveC]);
  await d.idle();
  assert.equal(duringSave.server, A);
  assert.equal(duringSave.window, original);
  assert.deepEqual(
    results.map((result) => result.serverUrl),
    [B, C],
  );
  assert.equal(d.getSettings().serverUrl, C);
  assert.equal((await d.disk()).serverUrl, C);
  assert.equal((await d.disk()).window.width, 1500);
  assert.equal(d.main.options.width, 1500);
  assert.equal(d.main.options.titleBarStyle, undefined);
  assert.equal(d.main.url, `${C}/app`);
});

test("a failed queued server change cannot roll back a later successful save", async (t) => {
  const d = await desktop(t);
  const failedWrite = deferred();
  const reachedWrite = deferred();
  const oauth = deferred();
  d.controls.fetch = (url) =>
    url.endsWith("/consume") ? oauth.promise : Promise.resolve(new Response("{}"));
  await d.signIn();
  d.callback();
  let first = true;
  d.controls.writeFile = async (...args) => {
    if (first) {
      first = false;
      reachedWrite.resolve();
      await failedWrite.promise;
    }
    return writeFile(...args);
  };
  const rejected = assert.rejects(d.save(B), /disk unavailable/);
  await reachedWrite.promise;
  const saveC = d.save(C);
  failedWrite.reject(new Error("disk unavailable"));
  await rejected;
  assert.equal((await saveC).serverUrl, C);
  oauth.resolve(new Response("{}"));
  await d.idle();
  assert.equal(d.getSettings().serverUrl, C);
  assert.equal((await d.disk()).serverUrl, C);
  assert.deepEqual(d.main.loads, [`${C}/app`]);
  assert.deepEqual(d.errors, []);
});

test("failed persistence preserves the active OAuth attempt", async (t) => {
  const d = await desktop(t);
  await d.signIn();
  await mkdir(`${d.destination}.tmp`);
  await assert.rejects(d.save(B));
  d.callback();
  await settle();
  assert.deepEqual(d.main.loads, [`${A}/app`, `${A}/app`]);
  assert.deepEqual(d.errors, []);
});

for (const stage of ["consume", "me"]) {
  for (const outcome of ["success", "http-error", "network-error"]) {
    test(`server switch during ${stage}: stale ${outcome} cannot navigate or report an error`, async (t) => {
      const d = await desktop(t);
      const held = deferred();
      d.controls.fetch = (url) =>
        url.endsWith(stage === "consume" ? "/consume" : "/me")
          ? held.promise
          : Promise.resolve(new Response("{}"));
      await d.signIn();
      d.callback();
      await until(() =>
        d.requests.some(({ url }) => url.endsWith(stage === "consume" ? "/consume" : "/me")),
      );
      await d.save(B);
      const replacement = d.main;
      d.send(replacement, "desktop:set-active-route", "/app/team/new");
      if (outcome === "network-error") held.reject(new Error("old request failed"));
      else held.resolve(new Response("{}", { status: outcome === "success" ? 200 : 500 }));
      await settle();
      assert.deepEqual(replacement.loads, [`${B}/app`]);
      assert.deepEqual(d.errors, []);
      d.app.emit("second-instance", {}, []);
      assert.deepEqual(replacement.messages.at(-1), ["desktop:navigate", "/app/team/new"]);
      d.send(replacement, "desktop:set-unread", 9);
      assert.equal(d.badges.at(-1), 9);
      assert.equal(d.getSettings().serverUrl, B);
    });
  }
}

for (const stage of ["consume", "me"]) {
  test(`server switch while reading the ${stage} response body stays on the selected server`, async (t) => {
    const d = await desktop(t);
    let body;
    const response = new Response(
      new ReadableStream({
        start(controller) {
          body = controller;
        },
      }),
    );
    d.controls.fetch = (url) =>
      Promise.resolve(
        url.endsWith(stage === "consume" ? "/consume" : "/me") ? response : new Response("{}"),
      );
    await d.signIn();
    d.callback();
    await until(() => response.bodyUsed);
    await d.save(B);
    body.close();
    await settle();
    assert.deepEqual(d.main.loads, [`${B}/app`]);
    assert.deepEqual(d.errors, []);
  });
}

for (const outcome of ["success", "http-error", "network-error"]) {
  test(`a replaced OAuth attempt cannot finish or erase the next attempt after ${outcome}`, async (t) => {
    const d = await desktop(t);
    const held = deferred();
    d.controls.fetch = (_url, options) =>
      options.body?.includes("a".repeat(43)) ? held.promise : Promise.resolve(new Response("{}"));
    await d.signIn();
    d.callback();
    await until(() => d.requests.length === 1);
    await d.signIn();
    if (outcome === "network-error") held.reject(new Error("old request failed"));
    else held.resolve(new Response("{}", { status: outcome === "success" ? 200 : 500 }));
    await settle();
    assert.deepEqual(d.main.loads, [`${A}/app`]);
    d.callback("b".repeat(43));
    await settle();
    assert.deepEqual(d.main.loads, [`${A}/app`, `${A}/app`]);
    assert.deepEqual(d.errors, []);
    const consumed = d.requests.filter(({ url }) => url.endsWith("/consume"));
    assert.equal(consumed.length, 2);
    assert.notEqual(
      JSON.parse(consumed[0].options.body).code_verifier,
      JSON.parse(consumed[1].options.body).code_verifier,
    );
  });
}

for (const replacement of ["attempt", "server"]) {
  test(`late browser-open rejection neither rejects nor clears a replacement ${replacement}`, async (t) => {
    const d = await desktop(t);
    const held = deferred();
    d.controls.openExternal = () => (d.browserURLs.length === 1 ? held.promise : Promise.resolve());
    const first = d.signIn();
    const firstResult = first.then(
      () => "resolved",
      () => "rejected",
    );
    if (replacement === "server") await d.save(B);
    await d.signIn(d.main);
    held.reject(new Error("old browser launch failed"));
    assert.equal(await firstResult, "resolved");
    d.callback();
    await settle();
    const server = replacement === "server" ? B : A;
    assert.deepEqual(d.main.loads, [`${server}/app`, `${server}/app`]);
    assert.deepEqual(d.errors, []);
  });
}

test("current browser-open failure is still returned to the renderer", async (t) => {
  const d = await desktop(t);
  d.controls.openExternal = async () => {
    throw new Error("browser unavailable");
  };
  await assert.rejects(d.signIn(), /browser unavailable/);
});

test("OAuth cannot use a recreated window even at the same origin", async (t) => {
  const d = await desktop(t);
  const held = deferred();
  d.controls.fetch = (url) =>
    url.endsWith("/consume") ? held.promise : Promise.resolve(new Response("{}"));
  await d.signIn();
  d.callback();
  await until(() => d.requests.length === 1);
  d.main.destroy();
  d.app.emit("activate");
  const replacement = d.main;
  held.resolve(new Response("{}"));
  await settle();
  assert.deepEqual(replacement.loads, [`${A}/app`]);
  assert.deepEqual(d.errors, []);
});

for (const navigationFails of [false, true]) {
  test(`navigation ${navigationFails ? "failure" : "completion"} from an old attempt cannot affect its replacement`, async (t) => {
    const d = await desktop(t);
    const original = d.main;
    const navigation = deferred();
    original.navigation = navigation;
    await d.signIn();
    d.callback();
    await until(() => original.loads.length === 2);
    await d.save(B);
    const replacement = d.main;
    const shows = replacement.shows;
    if (navigationFails) navigation.reject(new Error("navigation cancelled"));
    else navigation.resolve();
    await settle();
    assert.equal(replacement.shows, shows);
    assert.deepEqual(d.errors, []);
  });
}

for (const stage of ["consume", "me", "navigation"]) {
  test(`current ${stage} error stays visible`, async (t) => {
    const d = await desktop(t);
    d.controls.fetch = (url) =>
      Promise.resolve(
        new Response("{}", {
          status:
            url.endsWith(stage === "consume" ? "/consume" : "/me") && stage !== "navigation"
              ? 503
              : 200,
        }),
      );
    if (stage === "navigation") {
      d.main.navigation = deferred();
      d.main.navigation.promise.catch(() => {});
      d.main.navigation.reject(new Error("navigation unavailable"));
    }
    await d.signIn();
    d.callback();
    await until(() => d.errors.length > 0);
    assert.match(d.errors[0], stage === "navigation" ? /navigation unavailable/ : /503/);
  });
}

test("current OAuth verifies the session before navigating and consumes the attempt once", async (t) => {
  const d = await desktop(t);
  const me = deferred();
  d.controls.fetch = (url) =>
    url.endsWith("/me") ? me.promise : Promise.resolve(new Response("{}"));
  await d.signIn();
  d.callback();
  await until(() => d.requests.length === 2);
  assert.deepEqual(d.main.loads, [`${A}/app`]);
  assert.equal(d.requests[0].options.credentials, "include");
  assert.equal(d.requests[1].url, `${A}/api/me`);
  me.resolve(new Response("{}"));
  await settle();
  assert.deepEqual(d.main.loads, [`${A}/app`, `${A}/app`]);
  assert.deepEqual(d.errors, []);
  d.callback();
  await until(() => d.errors.length > 0);
  assert.match(d.errors[0], /expired/);
  assert.equal(d.requests.length, 2);
  assert.equal(JSON.parse(await readFile(d.destination, "utf8")).serverUrl, A);
});
