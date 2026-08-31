import { expect, test, type Page, type Route } from "@playwright/test";
import { threadFixture, openThread, deferred, longThread, scrollThreadTo } from "./thread-fixture";

async function holdResponse(
  route: Route,
  entered: ReturnType<typeof deferred>,
  release: ReturnType<typeof deferred>,
) {
  const response = await route.fetch();
  entered.resolve();
  await release.promise;
  await route.fulfill({ response });
}

async function edit(page: Page, id: string, body: string) {
  const row = page.locator(`.reply[data-message-id="${id}"]`);
  await row.getByRole("button", { name: "Edit message", exact: true }).click();
  await row.getByLabel("Edit message", { exact: true }).fill(body);
  await row.getByRole("button", { name: "Save", exact: true }).click();
  await expect(row.locator(".markdown")).toHaveText(body);
}

test("older retry retains its window, hydrates partial reactions and survives a concurrent newer load", async ({
  page,
}) => {
  test.setTimeout(90_000);
  const { root, replies } = await longThread(page);
  const before = deferred(),
    release = deferred();
  let requests = 0;
  await page.request.post(`/api/messages/${replies[0].id}/reactions`, { data: { emoji: "👍" } });
  // A fresh page starts at the tail with no cached first-reply reactions.
  await page.reload();
  await expect(page.locator(".reply-list .reply")).toHaveCount(100);
  await page.request.post(`/api/messages/${replies[0].id}/reactions`, { data: { emoji: "🔥" } });
  await page.route(`**/api/messages/${root.id}/thread?*`, async (route) => {
    if (!new URL(route.request().url()).searchParams.has("before_seq")) return route.continue();
    requests++;
    if (requests === 1)
      return route.fulfill({ status: 503, json: { error: "Older page unavailable" } });
    return holdResponse(route, before, release);
  });
  try {
    await page.getByRole("button", { name: "Load older replies", exact: true }).click();
    await expect(page.getByRole("alert")).toContainText("Older page unavailable");
    await expect(page.locator(".reply-list .reply")).toHaveCount(100);
    await page.getByRole("button", { name: "Retry older replies", exact: true }).click();
    await before.promise;
    const anchor = page.locator(`.reply[data-message-id="${replies[28].id}"]`);
    const viewport = page.locator(".thread-scroll");
    await viewport.hover();
    await page.mouse.wheel(0, 1);
    await anchor.evaluate((node) => node.scrollIntoView({ block: "start" }));
    await expect
      .poll(async () =>
        Math.abs((await anchor.boundingBox())!.y - (await viewport.boundingBox())!.y),
      )
      .toBeLessThan(3);
    const y = (await anchor.boundingBox())!.y;
    const response = await page.request.post(`/api/messages/${root.id}/thread/replies`, {
      data: { body: "Newer while older is held" },
    });
    const remote = (await response.json()).message;
    await expect(page.locator(`.reply[data-message-id="${remote.id}"]`)).toBeAttached();
    release.resolve();
    await expect(page.locator(".reply-list .reply")).toHaveCount(129);
    await expect.poll(async () => Math.abs((await anchor.boundingBox())!.y - y)).toBeLessThan(3);
    const first = page.locator(`.reply[data-message-id="${replies[0].id}"]`);
    await expect(
      first.getByRole("button", { name: "👍 — 1 reaction", exact: true }),
    ).toBeAttached();
    await expect(
      first.getByRole("button", { name: "🔥 — 1 reaction", exact: true }),
    ).toBeAttached();
  } finally {
    release.resolve();
  }
});

test("a delayed latest snapshot preserves a live reply and an acknowledged same-row edit", async ({
  page,
}) => {
  test.setTimeout(90_000);
  const { root, replies } = await longThread(page, 102);
  await scrollThreadTo(page.locator(`.reply[data-message-id="${replies[10].id}"]`), page);
  const entered = deferred(),
    release = deferred(),
    delivered = deferred();
  let hold = true;
  await page.route(`**/api/messages/${root.id}/thread?*`, async (route) => {
    const query = new URL(route.request().url()).searchParams;
    if (!hold || query.get("latest") !== "true" || query.get("limit") !== "100")
      return route.continue();
    hold = false;
    await holdResponse(route, entered, release);
    delivered.resolve();
  });
  try {
    await page.getByRole("button", { name: "Jump to latest", exact: true }).click();
    await entered.promise;
    await edit(page, replies[20].id, "Acknowledged edit during latest fetch");
    const response = await page.request.post(`/api/messages/${root.id}/thread/replies`, {
      data: { body: "Live after captured latest" },
    });
    const remote = (await response.json()).message;
    await expect(page.locator(`.reply[data-message-id="${remote.id}"]`)).toBeAttached();
    release.resolve();
    await delivered.promise;
    await expect(page.locator(`.reply[data-message-id="${replies[20].id}"] .markdown`)).toHaveText(
      "Acknowledged edit during latest fetch",
    );
    await expect(page.locator(`.reply[data-message-id="${remote.id}"]`)).toHaveCount(1);
    await expect(page.locator(".thread > header strong").first()).toContainText("103 replies");
  } finally {
    release.resolve();
  }
});

test("an unrelated local edit does not discard a held remote row update", async ({ page }) => {
  const { roots } = await threadFixture(page);
  const root = roots[0];
  const first = (await (await page.request.get(`/api/messages/${root.id}/thread`)).json())
    .replies[0];
  const second = (
    await (
      await page.request.post(`/api/messages/${root.id}/thread/replies`, {
        data: { body: "Second row" },
      })
    ).json()
  ).message;
  await openThread(page, root.id);
  await expect(page.locator(".reply-list .reply")).toHaveCount(2);
  const entered = deferred(),
    release = deferred();
  let held = true;
  await page.route(`**/api/messages/${second.id}`, async (route) => {
    if (route.request().method() !== "GET" || !held) return route.continue();
    held = false;
    await holdResponse(route, entered, release);
  });
  try {
    await page.request.patch(`/api/messages/${second.id}`, {
      data: { body: "Remote second row updated" },
    });
    await entered.promise;
    await edit(page, first.id, "Local first row updated");
    release.resolve();
    await expect(page.locator(`.reply[data-message-id="${second.id}"] .markdown`)).toHaveText(
      "Remote second row updated",
    );
    await expect(page.locator(`.reply[data-message-id="${first.id}"] .markdown`)).toHaveText(
      "Local first row updated",
    );
  } finally {
    release.resolve();
  }
});

for (const leave of ["root", "workspace"] as const) {
  test(`a late history edge cannot replace the selected ${leave}`, async ({ page }) => {
    test.setTimeout(90_000);
    const { root, roots, workspace, replies } = await longThread(page, 102);
    const other = (await (await page.request.get("/api/workspaces")).json()).workspaces.find(
      (item: { id: string }) => item.id !== workspace.id,
    );
    const entered = deferred(),
      release = deferred(),
      delivered = deferred();
    await page.route(`**/api/messages/${root.id}/thread?*`, async (route) => {
      if (!new URL(route.request().url()).searchParams.has("before_seq")) return route.continue();
      await holdResponse(route, entered, release);
      delivered.resolve();
    });
    try {
      await page.getByRole("button", { name: "Load older replies", exact: true }).click();
      await entered.promise;
      if (leave === "root") {
        await openThread(page, roots[1].id);
        await expect(page.locator(".thread-root")).toContainText(roots[1].body);
      } else {
        await page.getByRole("link", { name: other.name, exact: true }).click();
        await expect(page).not.toHaveURL(new RegExp(workspace.route_id));
      }
      const destination =
        leave === "root"
          ? new RegExp(`/app/${workspace.route_id}/${roots[1].route_id}$`)
          : new RegExp(`/app/${other.route_id}(?:/|$)`);
      await expect(page).toHaveURL(destination);
      release.resolve();
      await delivered.promise;
      await expect(page.locator(`.reply[data-message-id="${replies[0].id}"]`)).toHaveCount(0);
      await expect(page).toHaveURL(destination);
    } finally {
      release.resolve();
    }
  });
}

test("held older page cannot undo acknowledged edit while row was outside the window", async ({
  page,
}) => {
  test.setTimeout(90_000);
  const { root, replies } = await longThread(page, 128);
  await page.getByRole("button", { name: "Load older replies", exact: true }).click();
  const first = page.locator(`.reply[data-message-id="${replies[0].id}"]`);
  const patchEntered = deferred(),
    releasePatch = deferred(),
    patchDelivered = deferred(),
    olderEntered = deferred(),
    releaseOlder = deferred();
  await page.route(`**/api/messages/${replies[0].id}`, async (route) => {
    if (route.request().method() !== "PATCH") return route.continue();
    patchEntered.resolve();
    await releasePatch.promise;
    const response = await route.fetch();
    await route.fulfill({ response });
    patchDelivered.resolve();
  });
  await first.getByRole("button", { name: "Edit message", exact: true }).click();
  await first.getByLabel("Edit message", { exact: true }).fill("Acknowledged outside window");
  await first.getByRole("button", { name: "Save", exact: true }).click();
  await patchEntered.promise;
  await page.getByRole("button", { name: "Jump to latest", exact: true }).click();
  await expect(first).toHaveCount(0);
  await page.route(`**/api/messages/${root.id}/thread?*`, async (route) => {
    if (!new URL(route.request().url()).searchParams.has("before_seq")) return route.continue();
    const response = await route.fetch();
    olderEntered.resolve();
    await releaseOlder.promise;
    await route.fulfill({ response });
  });
  try {
    await page.getByRole("button", { name: "Load older replies", exact: true }).click();
    await olderEntered.promise;
    releasePatch.resolve();
    await patchDelivered.promise;
    await page.evaluate(
      () => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))),
    );
    releaseOlder.resolve();
    await expect(first.locator(".markdown")).toHaveText("Acknowledged outside window");
  } finally {
    releasePatch.resolve();
    releaseOlder.resolve();
  }
});
