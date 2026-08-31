import { expect, test, type Page } from "@playwright/test";
import { randomUUID } from "node:crypto";
import type { Channel, Workspace } from "../../apps/web/src/lib/types";
import { waitForAppReady } from "./app-ready";
import { deferred } from "./thread-fixture";

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
