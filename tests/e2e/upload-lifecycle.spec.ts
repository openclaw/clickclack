import { expect, test, type Page } from "@playwright/test";
import { randomUUID } from "node:crypto";
import type { Channel, Workspace } from "../../apps/web/src/lib/types";
import { waitForAppReady } from "./app-ready";
import { deferred, openThread } from "./thread-fixture";

async function fixture(page: Page) {
  const response = await page.request.post("/api/workspaces", {
    data: { name: `Upload lifecycle ${randomUUID()}` },
  });
  expect(response.ok()).toBe(true);
  const { workspace }: { workspace: Workspace } = await response.json();
  const created = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: "uploads" },
  });
  expect(created.ok()).toBe(true);
  const { channel }: { channel: Channel } = await created.json();
  return { workspace, channel, path: `/app/${workspace.route_id}/${channel.route_id}` };
}

function file(name: string) {
  return { name, mimeType: "text/plain", buffer: Buffer.from(`Synthetic ${name}`) };
}

async function holdNextUpload(page: Page) {
  const requested = deferred(),
    release = deferred(),
    delivered = deferred();
  let first = true;
  await page.route("**/api/uploads", async (route) => {
    const held = first;
    first = false;
    const response = await route.fetch();
    expect(response.status()).toBe(201);
    if (held) {
      requested.resolve();
      await release.promise;
    }
    await route.fulfill({ response });
    if (held) delivered.resolve();
  });
  return { requested, release, delivered };
}

async function settleReceipt(page: Page) {
  await page.evaluate(
    () =>
      new Promise<void>((resolve) =>
        requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
      ),
  );
}

test("the latest selected file wins and stays with same-workspace drafts", async ({
  page,
}, testInfo) => {
  const data = await fixture(page);
  const response = await page.request.post(`/api/workspaces/${data.workspace.id}/channels`, {
    data: { name: "other-upload-channel" },
  });
  const { channel: other }: { channel: Channel } = await response.json();
  await page.goto(data.path);
  await waitForAppReady(page);
  const gate = await holdNextUpload(page);
  try {
    await page.getByLabel("Upload file", { exact: true }).setInputFiles(file("first.txt"));
    await gate.requested.promise;
    await page.getByLabel("Upload file", { exact: true }).setInputFiles(file("second.txt"));
    await expect(page.locator(".attachment-name")).toContainText("second.txt");
    gate.release.resolve();
    await gate.delivered.promise;
    await settleReceipt(page);
    await page.screenshot({ path: testInfo.outputPath("upload-selection.png") });
    await expect(page.locator(".attachment-name")).toContainText("second.txt");
    await page.locator(`#sidebar-channels-list a[href$="/${other.route_id}"]`).click();
    await expect(page).toHaveURL(new RegExp(`/${other.route_id}$`));
    await expect(page.locator(".attachment-name")).toContainText("second.txt");
  } finally {
    gate.release.resolve();
  }
});

test("browser Back abandons an upload from another workspace", async ({ page }) => {
  const origin = await fixture(page),
    next = await fixture(page);
  await page.goto(origin.path);
  await waitForAppReady(page);
  await page.getByRole("link", { name: next.workspace.name, exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/app/${next.workspace.route_id}/[^/]+$`));
  await waitForAppReady(page);
  const gate = await holdNextUpload(page);
  try {
    await page
      .getByLabel("Upload file", { exact: true })
      .setInputFiles(file("other-workspace.txt"));
    await gate.requested.promise;
    await page.goBack();
    await expect(page).toHaveURL(new RegExp(origin.path + "$"));
    await waitForAppReady(page);
    gate.release.resolve();
    await gate.delivered.promise;
    await settleReceipt(page);
    await expect(page.locator(".attachment-name")).toHaveCount(0);
    const sent = page.waitForResponse(
      (response) =>
        response.request().method() === "POST" &&
        response.url().endsWith(`/api/channels/${origin.channel.id}/messages`),
    );
    await page
      .getByLabel("Message body", { exact: true })
      .fill("The returned draft has no foreign attachment");
    await page.getByRole("button", { name: "Send", exact: true }).click();
    const { message } = await (await sent).json();
    await expect(page.locator(`.message-row[data-message-id="${message.id}"]`)).toBeVisible();
    const saved = await page.request.get(`/api/messages/${message.id}`);
    expect((await saved.json()).message.attachments ?? []).toEqual([]);
  } finally {
    gate.release.resolve();
  }
});

for (const action of ["send", "remove"] as const) {
  test(`${action} abandons a pending replacement upload`, async ({ page }) => {
    const data = await fixture(page);
    await page.goto(data.path);
    await waitForAppReady(page);
    await page.getByLabel("Upload file", { exact: true }).setInputFiles(file("ready.txt"));
    await expect(page.locator(".attachment-name")).toContainText("ready.txt");
    const gate = await holdNextUpload(page);
    try {
      await page.getByLabel("Upload file", { exact: true }).setInputFiles(file("replacement.txt"));
      await gate.requested.promise;
      if (action === "send") {
        await page.getByLabel("Message body", { exact: true }).fill("Send the visible attachment");
        await page.getByRole("button", { name: "Send", exact: true }).click();
        await expect(
          page.locator(".message-row").getByText("ready.txt", { exact: true }),
        ).toBeVisible();
      } else {
        await page.getByRole("button", { name: "Remove attachment" }).click();
      }
      gate.release.resolve();
      await gate.delivered.promise;
      await settleReceipt(page);
      await expect(page.locator(".attachment-name")).toHaveCount(0);
    } finally {
      gate.release.resolve();
    }
  });
}

test("sending a registered command abandons its pending upload", async ({ page }) => {
  const data = await fixture(page);
  await page.route(`**/api/workspaces/${data.workspace.id}/slash-commands`, (route) =>
    route.fulfill({
      json: {
        slash_commands: [
          { id: "cmd_upload_proof", command: "/upload-proof", description: "Synthetic command" },
        ],
      },
    }),
  );
  await page.route(`**/api/hooks/slash/${data.channel.id}`, (route) =>
    route.fulfill({ json: { response_type: "ephemeral", text: "Synthetic command complete" } }),
  );
  await page.goto(data.path);
  await waitForAppReady(page);
  const gate = await holdNextUpload(page);
  try {
    await page.getByLabel("Upload file", { exact: true }).setInputFiles(file("command-upload.txt"));
    await gate.requested.promise;
    await page.getByLabel("Message body", { exact: true }).fill("/upload-proof");
    await page.getByRole("button", { name: "Send", exact: true }).click();
    await expect(page.getByText("Synthetic command complete", { exact: true })).toBeVisible();
    gate.release.resolve();
    await gate.delivered.promise;
    await settleReceipt(page);
    await expect(page.locator(".attachment-name")).toHaveCount(0);
  } finally {
    gate.release.resolve();
  }
});

test("a failed upload is visible and the same file can be retried", async ({ page }) => {
  const data = await fixture(page);
  await page.goto(data.path);
  await waitForAppReady(page);
  await page.route("**/api/uploads", (route) =>
    route.fulfill({ status: 503, json: { error: "Synthetic upload unavailable" } }),
  );
  const input = page.getByLabel("Upload file", { exact: true });
  await input.setInputFiles(file("retry.txt"));
  await expect(page.getByText("Synthetic upload unavailable", { exact: true })).toBeVisible();
  await page.unroute("**/api/uploads");
  await input.setInputFiles(file("retry.txt"));
  await expect(page.locator(".attachment-name")).toContainText("retry.txt");
  await expect(page.getByText("Synthetic upload unavailable", { exact: true })).toHaveCount(0);
});

for (const [action, refresh] of [
  ["retry", true],
  ["discard", true],
  ["discard", false],
] as const) {
  test(`a failed attachment keeps its ${action} action ${refresh ? "through thread updates" : "without a refresh"}`, async ({
    page,
  }) => {
    const data = await fixture(page);
    const existing = await page.request.post(`/api/channels/${data.channel.id}/messages`, {
      data: { body: "An unrelated thread" },
    });
    const { message: root } = await existing.json();
    const created = await page.request.post(`/api/workspaces/${data.workspace.id}/channels`, {
      data: { name: "attachment-neighbor" },
    });
    const { channel: other } = await created.json();
    await page.goto(data.path);
    await waitForAppReady(page);
    await page.getByLabel("Upload file", { exact: true }).setInputFiles(file("attachment.txt"));
    await expect(page.locator(".attachment-name")).toContainText("attachment.txt");
    await page.route("**/api/messages/*/attachments", (route) =>
      route.fulfill({ status: 503, json: { error: "Attachment unavailable" } }),
    );
    const nonces: string[] = [];
    page.on("request", (request) => {
      if (
        request.method() === "POST" &&
        request.url().endsWith(`/api/channels/${data.channel.id}/messages`)
      ) {
        nonces.push(request.postDataJSON().nonce);
      }
    });
    const sent = page.waitForResponse(
      (response) =>
        response.request().method() === "POST" &&
        response.url().endsWith(`/api/channels/${data.channel.id}/messages`),
    );
    await page
      .getByLabel("Message body", { exact: true })
      .fill("The message was saved before its attachment failed");
    await page.getByRole("button", { name: "Send", exact: true }).click();
    const { message } = await (await sent).json();
    const row = page.locator(`.message-row[data-message-id="${message.id}"]`);
    await expect(row).toHaveClass(/is-failed/);
    if (refresh) {
      // Reopening fetches the saved body while the failed attachment stays local.
      await page.locator(`#sidebar-channels-list a[href$="/${other.route_id}"]`).click();
      await expect(
        page.getByRole("heading", { name: "#attachment-neighbor", exact: true }),
      ).toBeVisible();
      const loaded = page.waitForResponse(
        (response) =>
          response.request().method() === "GET" &&
          response.url().includes(`/api/channels/${data.channel.id}/messages?`),
      );
      await page.locator(`#sidebar-channels-list a[href$="/${data.channel.route_id}"]`).click();
      await loaded;
      await expect(row).toHaveClass(/is-failed/);
    }
    if (action === "retry") {
      const summary = page.waitForResponse((response) =>
        response.url().includes(`/api/messages/${message.id}/thread?`),
      );
      expect(
        (
          await page.request.post(`/api/messages/${message.id}/thread/replies`, {
            data: { body: "Another session replies to the saved message" },
          })
        ).ok(),
      ).toBe(true);
      await summary;
      await expect(row.getByRole("button", { name: "Retry", exact: true })).toBeVisible();
      await page.unroute("**/api/messages/*/attachments");
      await row.getByRole("button", { name: "Retry", exact: true }).click();
      await expect(row).not.toHaveClass(/is-pending|is-failed/);
      await expect(row.getByText("attachment.txt", { exact: true })).toBeVisible();
      expect(nonces).toHaveLength(1);
    } else {
      if (refresh) {
        await openThread(page, root.id);
        await expect(page.locator(".thread-root")).toContainText(root.body);
      }
      await row.getByRole("button", { name: "Discard", exact: true }).click();
      await expect(row).not.toHaveClass(/is-failed/);
      await expect(row.getByText("attachment.txt", { exact: true })).toHaveCount(0);
    }
    await expect(row).toContainText(message.body);
    const saved = await page.request.get(`/api/messages/${message.id}`);
    expect((await saved.json()).message.attachments ?? []).toHaveLength(action === "retry" ? 1 : 0);
    const latest = await page.request.get(`/api/channels/${data.channel.id}/messages`);
    expect(
      (await latest.json()).messages.filter((current: { id: string }) => current.id === message.id),
    ).toHaveLength(1);
  });
}
