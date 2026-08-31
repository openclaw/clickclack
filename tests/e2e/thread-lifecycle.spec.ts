import { expect, test } from "@playwright/test";
import { deferred, threadFixture, openThread } from "./thread-fixture";

for (const action of ["close", "switch"] as const) {
  test(`a late thread load cannot undo ${action}`, async ({ page }) => {
    const { roots, workspace, channel } = await threadFixture(page);
    const held = deferred();
    const requested = deferred();
    const delivered = deferred();
    await page.route(`**/api/messages/${roots[0].id}/thread?*`, async (route) => {
      const response = await route.fetch();
      requested.resolve();
      await held.promise;
      await route.fulfill({ response });
      delivered.resolve();
    });
    try {
      await openThread(page, roots[0].id);
      await requested.promise;
      if (action === "close") {
        await page
          .getByLabel("Thread pane", { exact: true })
          .getByRole("button", { name: "Close thread" })
          .click();
      } else {
        await openThread(page, roots[1].id);
        await expect(page.locator(".thread-root")).toContainText(roots[1].body);
      }
      const expectedPath = `/app/${workspace.route_id}/${action === "close" ? channel.route_id : roots[1].route_id}`;
      await expect(page).toHaveURL(new URL(expectedPath, page.url()).toString());
      const expectedURL = page.url();
      held.resolve();
      await delivered.promise;
      // Wait for the released fetch and its Svelte update, including accidental navigation.
      await page.waitForTimeout(300);
      if (action === "close") {
        await expect(
          page
            .getByLabel("Thread pane", { exact: true })
            .getByRole("button", { name: "Close thread" }),
        ).toBeHidden();
      } else {
        await expect(page.locator(".thread-root")).toContainText(roots[1].body);
        await expect(page.locator(".reply-list")).toContainText("Second existing reply");
        await expect(page.locator(".reply-list")).not.toContainText("First existing reply");
      }
      await expect(page).toHaveURL(expectedURL);
    } finally {
      held.resolve();
    }
  });
}

test("failed thread replies retain their text and quote and retry without duplicating a committed reply", async ({
  page,
}) => {
  const { roots } = await threadFixture(page);
  await openThread(page, roots[0].id);
  await expect(page.locator(".reply-list")).toContainText("First existing reply");
  await page.locator(".thread-root").getByRole("button", { name: "Reply", exact: true }).click();
  const body = "Keep this quoted reply";
  await page.getByLabel("Reply body").fill(body);
  const submissions: { nonce: string; quoted_message_id: string }[] = [];
  await page.route(`**/api/messages/${roots[0].id}/thread/replies`, async (route) => {
    submissions.push(route.request().postDataJSON());
    const response = await route.fetch();
    if (submissions.length === 1) {
      await route.fulfill({ status: 503, json: { error: "Reply response interrupted" } });
    } else {
      await route.fulfill({ response });
    }
  });
  await page.getByRole("button", { name: "Reply", exact: true }).last().click();
  await expect(page.getByLabel("Reply body")).toHaveValue(body);
  await expect(page.getByRole("alert")).toContainText("Reply response interrupted");
  await expect(page.getByLabel("Replying to message")).toContainText(roots[0].body);
  await page.getByRole("button", { name: "Reply", exact: true }).last().click();
  await expect(page.getByLabel("Reply body")).toHaveValue("");
  expect(submissions).toHaveLength(2);
  expect(submissions[1].nonce).toBe(submissions[0].nonce);
  expect(submissions[1].quoted_message_id).toBe(roots[0].id);
  const response = await page.request.get(`/api/messages/${roots[0].id}/thread`);
  const { replies } = await response.json();
  expect(replies.filter((reply: { body: string }) => reply.body === body)).toHaveLength(1);
});

test("a reply response stays with its thread after switching panes", async ({ page }) => {
  const { roots } = await threadFixture(page);
  await openThread(page, roots[0].id);
  await expect(page.locator(".reply-list")).toContainText("First existing reply");
  const held = deferred();
  const requested = deferred();
  const delivered = deferred();
  await page.route(`**/api/messages/${roots[0].id}/thread/replies`, async (route) => {
    requested.resolve();
    await held.promise;
    const response = await route.fetch();
    await route.fulfill({ response });
    delivered.resolve();
  });
  try {
    await page.getByLabel("Reply body").fill("Reply belongs to first thread");
    await page.getByRole("button", { name: "Reply", exact: true }).last().click();
    await requested.promise;
    await openThread(page, roots[1].id);
    await expect(page.locator(".thread-root")).toContainText(roots[1].body);
    await page.getByLabel("Reply body").fill("Second thread draft");
    held.resolve();
    await delivered.promise;
    await page.waitForTimeout(300);
    await expect(page.locator(".reply-list")).not.toContainText("Reply belongs to first thread");
    await expect(page.getByLabel("Reply body")).toHaveValue("Second thread draft");
    await openThread(page, roots[0].id);
    await expect(page.locator(".reply-list")).toContainText("Reply belongs to first thread");
  } finally {
    held.resolve();
  }
});

for (const surface of ["main", "embed"] as const) {
  test(`a delayed reply receipt cannot lower a newer ${surface} thread summary`, async ({
    page,
  }) => {
    const { roots, workspace } = await threadFixture(page);
    if (surface === "embed") {
      await page.goto(`/embed/thread/${workspace.route_id}/${roots[0].route_id}`);
    } else {
      await openThread(page, roots[0].id);
    }
    await expect(page.locator(".reply-list")).toContainText("First existing reply");
    const held = deferred();
    const committed = deferred();
    const delivered = deferred();
    await page.route(`**/api/messages/${roots[0].id}/thread/replies`, async (route) => {
      const response = await route.fetch();
      committed.resolve();
      await held.promise;
      await route.fulfill({ response });
      delivered.resolve();
    });
    try {
      await page.getByLabel("Reply body").fill("Our held reply receipt");
      await page.locator(".reply-composer").getByRole("button", { name: "Reply" }).click();
      await committed.promise;
      const later = await page.request.post(`/api/messages/${roots[0].id}/thread/replies`, {
        data: { body: "A later reply from another session" },
      });
      expect(later.ok()).toBe(true);
      await expect(page.locator(".reply-list")).toContainText("A later reply from another session");
      const summary = page
        .getByLabel(surface === "main" ? "Thread pane" : "Embedded thread", { exact: true })
        .locator("header > div > strong")
        .first();
      await expect(summary).toContainText("3 replies");
      held.resolve();
      await delivered.promise;
      await page.waitForTimeout(300);
      await expect(summary).toContainText("3 replies");
    } finally {
      held.resolve();
    }
  });
}
