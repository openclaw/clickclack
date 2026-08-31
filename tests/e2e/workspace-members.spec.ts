import { expect, test, type Page } from "@playwright/test";

async function fixture(page: Page) {
  const workspaceResponse = await page.request.post("/api/workspaces", {
    data: { name: `Member proof ${Date.now()}` },
  });
  expect(workspaceResponse.ok()).toBe(true);
  const { workspace } = await workspaceResponse.json();
  const channelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: "member-proof", kind: "public" },
  });
  expect(channelResponse.ok()).toBe(true);
  const { channel } = await channelResponse.json();
  const rootResponse = await page.request.post(`/api/channels/${channel.id}/messages`, {
    data: { body: "Synthetic member-directory proof" },
  });
  expect(rootResponse.ok()).toBe(true);
  const { message } = await rootResponse.json();
  const threadResponse = await page.request.get(`/api/messages/${message.id}/thread`);
  const { root } = await threadResponse.json();
  return { workspace, channel, root };
}

const surfaces = ["chat", "channel", "thread", "overview"] as const;
type Surface = (typeof surfaces)[number];

async function withoutAbortSignalAny(page: Page) {
  await page.addInitScript(() => {
    delete (AbortSignal as Partial<typeof AbortSignal>).any;
  });
}

function destination(surface: Surface, data: Awaited<ReturnType<typeof fixture>>) {
  const { workspace, channel, root } = data;
  if (surface === "overview") return `/app/${workspace.route_id}/settings/overview`;
  if (surface === "chat") return `/app/${workspace.route_id}/${channel.route_id}`;
  return `/embed/${surface}/${workspace.route_id}/${surface === "channel" ? channel.route_id : root.route_id}`;
}

function member(workspaceID: string, role = "member") {
  return {
    workspace_id: workspaceID,
    user: {
      id: `usr_proof_${role}`,
      kind: role === "bot" ? "bot" : "human",
      display_name: role === "member" ? "Second Page Person" : `Excluded ${role}`,
      handle: role === "member" ? "second-page" : `excluded-${role}`,
      avatar_url: "",
      created_at: "2026-08-01T00:00:00Z",
    },
    role,
    joined_at: "2026-08-01T00:00:00Z",
  };
}

for (const surface of surfaces) {
  for (const scenario of [
    { name: "missing cursor", cursors: [undefined], requests: [""], error: "incomplete page" },
    { name: "empty cursor", cursors: [""], requests: [""], error: "incomplete page" },
    {
      name: "repeated cursor",
      cursors: ["stuck", "stuck"],
      requests: ["", "stuck"],
      error: "repeated a pagination cursor",
    },
    {
      name: "cursor cycle",
      cursors: ["a", "b", "a"],
      requests: ["", "a", "b"],
      error: "repeated a pagination cursor",
    },
  ]) {
    test(`${surface} stops member traversal on ${scenario.name}`, async ({ page }, testInfo) => {
      const data = await fixture(page);
      const requests: string[] = [];
      await page.route(`**/api/workspaces/${data.workspace.id}/members?**`, async (route) => {
        const query = new URL(route.request().url()).searchParams;
        requests.push(query.get("cursor") ?? "");
        expect(query.get("limit")).toBe(surface === "overview" ? "200" : "100");
        // Stop a broken client after the first excess request; the sequence assertion still fails.
        if (requests.length > scenario.requests.length) return route.abort();
        await route.fulfill({
          json: {
            members: [member(data.workspace.id)],
            has_more: true,
            next_cursor: scenario.cursors[requests.length - 1],
          },
        });
      });
      await page.goto(destination(surface, data));
      await expect.poll(() => requests.length).toBeGreaterThanOrEqual(scenario.requests.length);
      // Observe a bounded quiet window after the final response, including any queued follow-up.
      await page.waitForTimeout(250);
      await page.screenshot({ path: testInfo.outputPath("member-directory.png") });
      expect(requests).toEqual(scenario.requests);
      await expect(page.getByText(new RegExp(scenario.error))).toBeVisible();
      if (surface !== "overview") {
        const composer = page.getByLabel(surface === "thread" ? "Reply body" : "Message body");
        await expect(composer).toBeEnabled();
        await composer.fill("Chat remains usable");
        await expect(composer).toHaveValue("Chat remains usable");
      }
    });
  }

  test(`${surface} discovers members on a valid second page without AbortSignal.any`, async ({
    page,
  }) => {
    await withoutAbortSignalAny(page);
    const data = await fixture(page);
    const requests: string[] = [];
    await page.route(`**/api/workspaces/${data.workspace.id}/members?**`, async (route) => {
      const cursor = new URL(route.request().url()).searchParams.get("cursor") ?? "";
      requests.push(cursor);
      await route.fulfill({
        json: cursor
          ? {
              members: [member(data.workspace.id)],
              has_more: false,
            }
          : {
              members: [
                member(data.workspace.id, "bot"),
                member(data.workspace.id, "guest"),
                member(data.workspace.id, "owner"),
              ],
              has_more: true,
              next_cursor: "second",
            },
      });
    });
    await page.goto(destination(surface, data));
    if (surface === "overview") {
      const selector = page.getByRole("combobox", { name: "New workspace owner" });
      await expect(selector.getByRole("option", { name: "Second Page Person" })).toHaveCount(1);
      await expect(selector.getByRole("option", { name: /Excluded/ })).toHaveCount(0);
    } else {
      const composer = page.getByLabel(surface === "thread" ? "Reply body" : "Message body");
      await composer.fill("@second");
      await expect(page.getByRole("option", { name: /Second Page Person/ })).toBeVisible();
    }
    expect(requests).toEqual(["", "second"]);
  });

  test(`${surface} cancels member traversal when navigating away without AbortSignal.any`, async ({
    page,
  }) => {
    await withoutAbortSignalAny(page);
    const data = await fixture(page);
    const next = await fixture(page);
    let started = false;
    let aborted = false;
    let release!: () => void;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    const path = `/api/workspaces/${data.workspace.id}/members`;
    page.on("requestfailed", (request) => {
      if (request.url().includes(path)) aborted = true;
    });
    await page.route(`**${path}?**`, async (route) => {
      started = true;
      await gate;
      await route.fulfill({ json: { members: [], has_more: false } });
    });
    try {
      await page.goto(destination(surface, data));
      await expect.poll(() => started).toBe(true);
      const href = destination("chat", next);
      await page.evaluate((href) => {
        const link = document.createElement("a");
        link.href = href;
        link.textContent = "Change proof workspace";
        link.style.cssText =
          "position:fixed;top:0;right:0;z-index:2147483647;background:white;padding:12px";
        document.body.append(link);
      }, href);
      await page.getByRole("link", { name: "Change proof workspace" }).click();
      await expect(page).toHaveURL(new RegExp(`${href}$`));
      await expect.poll(() => aborted).toBe(true);
    } finally {
      release();
    }
  });
}

test("cancellable member requests retain the default page timeout without AbortSignal.any", async ({
  page,
}) => {
  await withoutAbortSignalAny(page);
  const data = await fixture(page);
  await page.addInitScript(() => {
    const timers: { delay: number; controller: AbortController }[] = [];
    AbortSignal.timeout = (delay) => {
      const controller = new AbortController();
      timers.push({ delay, controller });
      return controller.signal;
    };
    Object.assign(window, {
      expireMemberProofTimers() {
        for (const timer of timers) {
          if (timer.delay === 30_000) {
            timer.controller.abort(new DOMException("Member request timed out", "TimeoutError"));
          }
        }
      },
    });
  });
  let started = false;
  let release!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route(`**/api/workspaces/${data.workspace.id}/members?**`, async (route) => {
    started = true;
    await gate;
    await route.fulfill({ json: { members: [], has_more: false } });
  });
  try {
    await page.goto(destination("chat", data));
    await expect(page.getByLabel("Message body")).toBeVisible();
    await expect.poll(() => started).toBe(true);
    await page.evaluate(() => {
      (window as typeof window & { expireMemberProofTimers(): void }).expireMemberProofTimers();
    });
    await expect(page.getByText("Mentions unavailable: Member request timed out")).toBeVisible();
    await expect(page.getByLabel("Message body")).toBeEnabled();
  } finally {
    release();
  }
});

for (const surface of ["channel", "thread"] as const) {
  test(`${surface} keeps sent messages and simultaneous notices visibly separate`, async ({
    page,
  }, testInfo) => {
    const data = await fixture(page);
    const postPath =
      surface === "channel"
        ? `/api/channels/${data.channel.id}/messages`
        : `/api/messages/${data.root.id}/thread/replies`;
    // Fill the scroll area so the newest message exercises the composer boundary.
    for (let index = 0; index < 16; index += 1) {
      const response = await page.request.post(postPath, {
        data: {
          body: `Synthetic layout history ${index}\n\nA second line for the message window.`,
        },
      });
      expect(response.ok()).toBe(true);
    }
    await page.route(`**/api/workspaces/${data.workspace.id}/members?**`, (route) =>
      route.fulfill({ json: { members: [], has_more: true } }),
    );
    await page.goto(destination(surface, data));
    const memberNotice = page.getByText(/Mentions unavailable:.*incomplete page/);
    await expect(memberNotice).toBeVisible();
    const composer = page.getByLabel(surface === "thread" ? "Reply body" : "Message body");
    const submit = page
      .locator(surface === "thread" ? ".reply-composer" : ".embed-channel-composer")
      .getByRole("button", { name: surface === "thread" ? "Reply" : "Send", exact: true });
    await composer.fill("Newest synthetic message stays visible");
    await submit.click();
    const newest = page.getByText("Newest synthetic message stays visible", { exact: true });
    await expect(newest).toBeVisible();
    await newest.scrollIntoViewIfNeeded();
    const assertClearGeometry = async () => {
      const message = await newest.boundingBox();
      const input = await composer.boundingBox();
      const notices = await page.locator(".embed-notice").all();
      expect(message).not.toBeNull();
      expect(input).not.toBeNull();
      expect(message!.y).toBeGreaterThanOrEqual(0);
      expect(message!.y + message!.height).toBeLessThanOrEqual(input!.y);
      let previousBottom = 0;
      for (const notice of notices) {
        const box = (await notice.boundingBox())!;
        expect(message!.y + message!.height).toBeLessThanOrEqual(box.y);
        expect(box.y).toBeGreaterThanOrEqual(previousBottom);
        expect(box.y + box.height).toBeLessThanOrEqual(page.viewportSize()!.height);
        previousBottom = box.y + box.height;
      }
    };
    await assertClearGeometry();
    await page.route(`**${postPath}`, (route) =>
      route.request().method() === "POST"
        ? route.fulfill({ status: 503, json: { error: "Synthetic send failure" } })
        : route.continue(),
    );
    await composer.fill("Synthetic failed submission");
    await submit.click();
    await expect(page.getByText("Synthetic send failure", { exact: true })).toBeVisible();
    await expect(page.locator(".embed-notice")).toHaveCount(2);
    await newest.scrollIntoViewIfNeeded();
    await assertClearGeometry();
    await page.screenshot({ path: testInfo.outputPath("visible-notices.png") });
  });
}

test("embedded channel recovers newer messages from its message window", async ({
  page,
}, testInfo) => {
  const data = await fixture(page);
  const nextResponse = await page.request.post(`/api/channels/${data.channel.id}/messages`, {
    data: { body: "Newer synthetic message" },
  });
  const { message: next } = await nextResponse.json();
  const errors: string[] = [];
  const newer: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.route(`**/api/channels/${data.channel.id}/messages?**`, async (route) => {
    const query = new URL(route.request().url()).searchParams;
    if (query.has("after_seq")) {
      newer.push(query.get("after_seq")!);
      return route.continue();
    }
    await route.fulfill({
      json: {
        messages: [data.root],
        oldest_seq: data.root.channel_seq,
        newest_seq: data.root.channel_seq,
        has_older: false,
        has_newer: true,
      },
    });
  });
  await page.goto(destination("channel", data));
  await expect(page.getByLabel("Message body")).toBeVisible();
  // Bootstrap reconciles the mocked snapshot before persisting its cursor.
  // Let that replacement finish before exercising manual window recovery.
  await expect
    .poll(() =>
      page.evaluate((id) => localStorage.getItem(`clickclack:${id}:cursor`), data.workspace.id),
    )
    .toBeTruthy();
  await page.locator(".messages-scroll").dispatchEvent("wheel", { deltaY: 100 });
  await page.screenshot({ path: testInfo.outputPath("newer-messages.png") });
  // Initial scroll and the bootstrap replacement can recover the same cursor.
  await expect
    .poll(() => ({ newer: [...new Set(newer)], errors }))
    .toEqual({ newer: [String(data.root.channel_seq)], errors: [] });
  await expect(page.locator(`[data-message-id="${next.id}"]`)).toContainText(
    "Newer synthetic message",
  );
});

test("embedded newer-message loads are scoped to the active recovery generation", async ({
  page,
}) => {
  const data = await fixture(page);
  const nextResponse = await page.request.post(`/api/channels/${data.channel.id}/messages`, {
    data: { body: "Recovered after authentication" },
  });
  const { message: next } = await nextResponse.json();
  const releases: (() => void)[] = [];
  let newerRequests = 0;
  await page.route(`**/api/channels/${data.channel.id}/messages**`, async (route) => {
    if (route.request().method() === "POST") {
      return route.fulfill({ status: 401, json: { error: "Synthetic expired session" } });
    }
    const query = new URL(route.request().url()).searchParams;
    if (query.has("after_seq")) {
      newerRequests += 1;
      await new Promise<void>((resolve) => releases.push(resolve));
      return route.continue();
    }
    return route.fulfill({
      json: {
        messages: [data.root],
        oldest_seq: data.root.channel_seq,
        newest_seq: data.root.channel_seq,
        has_older: false,
        has_newer: true,
      },
    });
  });
  try {
    await page.goto(destination("channel", data));
    await expect(page.getByLabel("Message body")).toBeVisible();
    await page.locator(".messages-scroll").dispatchEvent("wheel", { deltaY: 100 });
    await expect.poll(() => newerRequests).toBe(1);
    await page.getByLabel("Message body").fill("Trigger synthetic authentication expiry");
    await page.getByRole("button", { name: "Send", exact: true }).click();
    await expect(page.getByRole("heading", { name: "Sign in to ClickClack" })).toBeVisible();
    // Returning to the tab retries authentication without remounting the embed.
    await page.evaluate(() => window.dispatchEvent(new Event("focus")));
    await expect(page.getByLabel("Message body")).toBeVisible();
    await page.locator(".messages-scroll").dispatchEvent("wheel", { deltaY: 100 });
    await expect.poll(() => newerRequests).toBe(2);
    const oldResponse = page.waitForResponse((response) => response.url().includes("after_seq="));
    releases[0]();
    await oldResponse;
    await page.locator(".messages-scroll").dispatchEvent("wheel", { deltaY: 100 });
    await page.waitForTimeout(250);
    expect(newerRequests).toBe(2);
    releases[1]();
    await expect(page.locator(`[data-message-id="${next.id}"]`)).toContainText(
      "Recovered after authentication",
    );
  } finally {
    for (const release of releases) release();
  }
});
