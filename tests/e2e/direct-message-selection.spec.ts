import { expect, test, type Page } from "@playwright/test";
import { randomUUID } from "node:crypto";
import type { Channel, DirectConversation, User, Workspace } from "../../apps/web/src/lib/types";
import { waitForAppReady } from "./app-ready";
import { deferred, openThread } from "./thread-fixture";

async function workspaceFixture(page: Page, name: string) {
  const response = await page.request.post("/api/workspaces", { data: { name } });
  expect(response.ok()).toBe(true);
  const { workspace }: { workspace: Workspace } = await response.json();
  const channelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: "dm-proof" },
  });
  expect(channelResponse.ok()).toBe(true);
  const { channel }: { channel: Channel } = await channelResponse.json();
  return { workspace, channel };
}

async function fixture(
  page: Page,
  existing: "none" | "open" | "closed" = "none",
  withGroup = true,
) {
  const suffix = randomUUID().slice(0, 8);
  const { workspace, channel } = await workspaceFixture(page, `DM selection ${suffix}`);
  const me = await page.request.get("/api/me");
  const { user }: { user: User } = await me.json();
  const people: User[] = [];
  for (const name of ["Alice", "Bob"]) {
    const response = await page.request.post(`/api/workspaces/${workspace.id}/bots`, {
      data: { display_name: `${name} ${suffix}`, handle: `${name.toLowerCase()}-${suffix}` },
    });
    expect(response.ok()).toBe(true);
    people.push((await response.json()).bot);
  }
  const [alice, bob] = people;
  const create = async (ids: string[]) => {
    const response = await page.request.post("/api/dms", {
      data: { workspace_id: workspace.id, member_ids: ids },
    });
    expect(response.ok()).toBe(true);
    return (await response.json()).conversation as DirectConversation;
  };
  const group = withGroup ? await create([alice.id, bob.id]) : undefined;
  const direct = existing === "none" ? undefined : await create([alice.id]);
  if (existing === "closed") {
    expect((await page.request.delete(`/api/dms/${direct!.id}`)).ok()).toBe(true);
  }
  await page.goto(`/app/${workspace.route_id}/${channel.route_id}`);
  await waitForAppReady(page);
  return { workspace, channel, user, alice, bob, direct, group };
}

function dialog(page: Page) {
  return page.locator(".profile-modal", { has: page.getByRole("heading", { name: "Start a DM" }) });
}

async function startFromDialog(page: Page, memberID: string) {
  await page.getByRole("button", { name: "Start direct message" }).click();
  await dialog(page).getByLabel("Find a person").fill(memberID);
  await dialog(page).getByRole("button", { name: "Start DM", exact: true }).click();
}

for (const existing of [false, true]) {
  test(`People ${existing ? "links to the exact one-to-one" : "opens a profile"} when a group contains the person`, async ({
    page,
  }) => {
    const data = await fixture(page, existing ? "open" : "none");
    const person = page
      .locator("#sidebar-people-list")
      .getByRole("link")
      .filter({ hasText: data.alice.display_name });
    await expect(person).toHaveAttribute(
      "href",
      existing ? `/app/${data.workspace.route_id}/${data.direct!.route_id}` : "#",
    );
    if (existing) {
      const opened = page.context().waitForEvent("page");
      await person.click({ modifiers: ["ControlOrMeta"] });
      const otherTab = await opened;
      await expect(otherTab).toHaveURL(new RegExp(`/${data.direct!.route_id}$`));
      await otherTab.close();
    }
    await person.click();
    if (!existing) {
      await expect(
        page
          .locator(".profile-pane")
          .getByRole("heading", { name: data.alice.display_name, exact: true }),
      ).toBeVisible();
      await page
        .locator(".profile-pane")
        .getByRole("button", { name: "Message", exact: true })
        .click();
    }
    await expect(
      page
        .locator(".timeline")
        .getByRole("heading", { name: `@${data.alice.display_name}`, exact: true }),
    ).toBeVisible();
    await expect(person).toHaveClass(/active/);
    // Explicit group selection still opens the group, without marking a person active.
    await page.locator(`#sidebar-direct-messages-list a[href$="/${data.group!.route_id}"]`).click();
    await expect(page).toHaveURL(new RegExp(`/${data.group!.route_id}$`));
    await expect(person).not.toHaveClass(/active/);
  });
}

for (const existing of ["none", "open", "closed"] as const) {
  test(`Start DM resolves the exact recipient with a ${existing} one-to-one and a visible group`, async ({
    page,
  }) => {
    const data = await fixture(page, existing);
    await startFromDialog(page, data.alice.id);
    await expect(
      page
        .locator(".timeline")
        .getByRole("heading", { name: `@${data.alice.display_name}`, exact: true }),
    ).toBeVisible();
    const response = await page.request.get(`/api/dms?workspace_id=${data.workspace.id}`);
    const { conversations }: { conversations: DirectConversation[] } = await response.json();
    const exact = conversations.filter(
      (dm) => dm.members.length === 2 && dm.members.some((member) => member.id === data.alice.id),
    );
    expect(exact).toHaveLength(1);
    expect(exact[0].members.map((member) => member.id).sort()).toEqual(
      [data.user.id, data.alice.id].sort(),
    );
    if (data.direct) expect(exact[0].id).toBe(data.direct.id);
    await expect(page).toHaveURL(new RegExp(`/${exact[0].route_id}$`));
  });
}

for (const queryKind of ["name", "handle", "ambiguous", "late join"] as const) {
  test(`Start DM resolves ${queryKind} search to a selected recipient`, async ({
    page,
  }, testInfo) => {
    const data = await fixture(page, "none", false);
    if (queryKind === "late join") {
      const composer = page.getByLabel("Message body");
      await composer.fill(`@${data.alice.handle}`);
      await expect(
        page.getByRole("option", { name: new RegExp(data.alice.display_name) }),
      ).toBeVisible();
      await composer.fill("");
      const suffix = randomUUID().slice(0, 8);
      const joined = await page.request.post(`/api/workspaces/${data.workspace.id}/bots`, {
        data: { display_name: `Latecomer ${suffix}`, handle: `latecomer-${suffix}` },
      });
      expect(joined.ok()).toBe(true);
      data.alice = (await joined.json()).bot;
    }
    await expect(
      page.locator("#sidebar-people-list").getByText(data.alice.display_name),
    ).toHaveCount(0);
    await page.getByRole("button", { name: "Start direct message" }).click();
    const scope = dialog(page);
    const input = scope.getByLabel("Find a person");
    const submit = scope.getByRole("button", { name: "Start DM", exact: true });
    await input.fill(
      queryKind === "late join"
        ? "Latecomer"
        : queryKind === "name"
          ? "Alice"
          : queryKind === "handle"
            ? `@${data.alice.handle}`
            : data.alice.display_name.split(" ")[1],
    );
    await page.screenshot({ path: testInfo.outputPath("newcomer-dm-search.png") });
    const recipient = scope.getByRole("button").filter({ hasText: data.alice.display_name });
    await expect(recipient).toBeVisible();
    await page.screenshot({ path: testInfo.outputPath("newcomer-dm-choice.png") });
    const created = page.waitForResponse(
      (response) =>
        response.request().method() === "POST" && new URL(response.url()).pathname === "/api/dms",
    );
    if (queryKind === "ambiguous") {
      await expect(submit).toBeDisabled();
      await expect(scope.getByText("Choose a person from the results.")).toBeVisible();
      await recipient.click();
    } else {
      await input.press("Enter");
    }
    const response = await created;
    expect(response.ok()).toBe(true);
    const { conversation }: { conversation: DirectConversation } = await response.json();
    expect(conversation.members.map((member) => member.id).sort()).toEqual(
      [data.user.id, data.alice.id].sort(),
    );
    await expect(
      page
        .locator(".timeline")
        .getByRole("heading", { name: `@${data.alice.display_name}`, exact: true }),
    ).toBeVisible();
    const body = "Hello to a new workspace member";
    const sent = page.waitForResponse(
      (response) =>
        response.request().method() === "POST" &&
        new URL(response.url()).pathname === `/api/dms/${conversation.id}/messages`,
    );
    await page.getByLabel("Message body").fill(body);
    await page.getByRole("button", { name: "Send", exact: true }).click();
    expect((await sent).ok()).toBe(true);
    await expect(page.locator(".message-row .markdown")).toHaveText(body);
    await page.screenshot({ path: testInfo.outputPath("newcomer-dm-message.png") });
  });
}

for (const surface of ["dialog", "profile"] as const) {
  test(`failed ${surface} DM creation retains the recipient and retries a committed request`, async ({
    page,
  }) => {
    const data = await fixture(page, surface === "profile" ? "open" : "none", false);
    const source = data.direct ? `/api/dms/${data.direct.id}` : `/api/channels/${data.channel.id}`;
    const message = await page.request.post(`${source}/messages`, {
      headers: { "X-ClickClack-User": data.alice.id },
      data: { body: "A person available from conversation history" },
    });
    expect(message.ok()).toBe(true);
    if (data.direct) {
      const { message: root } = await message.json();
      await page.goto(`/app/${data.workspace.route_id}/${data.direct.route_id}`);
      await waitForAppReady(page);
      await openThread(page, root.id);
      await expect(page.getByLabel("Thread pane", { exact: true })).toBeVisible();
      await page
        .locator("main.timeline")
        .getByRole("button", { name: `View profile for ${data.alice.display_name}`, exact: true })
        .click();
      await expect(page).toHaveURL(new RegExp(`/${data.direct.route_id}$`));
    } else {
      await page.reload();
      await waitForAppReady(page);
    }
    const created: string[] = [];
    await page.route("**/api/dms", async (route) => {
      if (route.request().method() !== "POST") return route.continue();
      const response = await route.fetch();
      created.push((await response.json()).conversation.id);
      if (created.length === 1) {
        await route.fulfill({ status: 503, json: { error: "DM response interrupted" } });
      } else {
        await route.fulfill({ response });
      }
    });
    const scope = surface === "dialog" ? dialog(page) : page.locator(".profile-pane");
    const retry = scope.getByRole("button", {
      name: surface === "dialog" ? "Start DM" : "Message",
      exact: true,
    });
    if (surface === "dialog") {
      await page.getByRole("button", { name: "Start direct message" }).click();
      await scope.getByLabel("Find a person").fill("Alice");
      await scope.getByRole("button").filter({ hasText: data.alice.display_name }).click();
    } else await retry.click();
    await expect(scope.getByRole("alert")).toHaveText("DM response interrupted");
    if (surface === "dialog")
      await expect(scope.getByLabel("Find a person")).toHaveValue(data.alice.id);
    else
      await expect(
        scope.getByRole("heading", { name: data.alice.display_name, exact: true }),
      ).toBeVisible();
    await retry.click();
    await expect(scope).toBeHidden();
    await expect(
      page
        .locator(".timeline")
        .getByRole("heading", { name: `@${data.alice.display_name}`, exact: true }),
    ).toBeVisible();
    expect(created).toHaveLength(2);
    expect(new Set(created).size).toBe(1);
  });
}

test("a self DM request reports the server rejection without selecting a group", async ({
  page,
}) => {
  const data = await fixture(page);
  const before = page.url();
  await startFromDialog(page, data.user.id);
  await expect(dialog(page).getByRole("alert")).toContainText("at least two members");
  await expect(dialog(page).getByLabel("Find a person")).toHaveValue(data.user.id);
  await expect(page).toHaveURL(before);
});

for (const switchWorkspace of [false, true]) {
  test(`a delayed DM response preserves the dialog ${switchWorkspace ? "in another workspace" : "in the same workspace"}`, async ({
    page,
  }) => {
    const data = await fixture(page, "none", false);
    const other = await workspaceFixture(page, `Alternate ${randomUUID().slice(0, 8)}`);
    await page.reload();
    await waitForAppReady(page);
    const held = deferred();
    const requested = deferred();
    const delivered = deferred();
    let created!: DirectConversation;
    await page.route("**/api/dms", async (route) => {
      if (route.request().method() !== "POST") return route.continue();
      const response = await route.fetch();
      created = (await response.json()).conversation;
      requested.resolve();
      await held.promise;
      await route.fulfill({ response });
      delivered.resolve();
    });
    try {
      await startFromDialog(page, data.alice.id);
      await requested.promise;
      await dialog(page).getByRole("button", { name: "Cancel", exact: true }).click();
      if (switchWorkspace) {
        await page.getByRole("link", { name: other.workspace.name, exact: true }).click();
        await expect(page).toHaveURL(new RegExp(`/app/${other.workspace.route_id}/[^/]+$`));
      }
      await page.getByRole("button", { name: "Start direct message" }).click();
      await dialog(page).getByLabel("Find a person").fill("keep this new recipient");
      const destination = page.url();
      held.resolve();
      await delivered.promise;
      // Allow the released response and any erroneous navigation to settle.
      await page.waitForTimeout(300);
      await expect(page).toHaveURL(destination);
      await expect(dialog(page).getByLabel("Find a person")).toHaveValue("keep this new recipient");
      await expect(
        page.locator(`#sidebar-direct-messages-list a[href$="/${created.route_id}"]`),
      ).toHaveCount(switchWorkspace ? 0 : 1);
    } finally {
      held.resolve();
    }
  });
}

test("a profile DM start releases a superseded workspace create form", async ({ page }) => {
  const data = await fixture(page);
  await expect
    .poll(() =>
      page.evaluate((id) => localStorage.getItem(`clickclack:${id}:cursor`), data.workspace.id),
    )
    .toBeTruthy();
  await page
    .locator("#sidebar-people-list")
    .getByRole("link")
    .filter({ hasText: data.alice.display_name })
    .click();
  const held = deferred();
  const requested = deferred();
  const delivered = deferred();
  await page.route("**/api/workspaces", async (route) => {
    if (route.request().method() !== "POST") return route.continue();
    const response = await route.fetch();
    requested.resolve();
    await held.promise;
    await route.fulfill({ response });
    delivered.resolve();
  });
  try {
    await page.getByRole("button", { name: "Create workspace" }).click();
    const input = page.getByLabel("Workspace name");
    const name = `Superseded ${randomUUID().slice(0, 8)}`;
    await input.fill(name);
    await input.press("Enter");
    await requested.promise;
    await page
      .locator(".profile-pane")
      .getByRole("button", { name: "Message", exact: true })
      .click();
    await expect(
      page
        .locator(".timeline")
        .getByRole("heading", { name: `@${data.alice.display_name}`, exact: true }),
    ).toBeVisible();
    const destination = page.url();
    held.resolve();
    await delivered.promise;
    await expect(input).toBeEnabled();
    await expect(page).toHaveURL(destination);
    await expect(page.getByRole("link", { name, exact: true })).toBeVisible();
  } finally {
    held.resolve();
  }
});
