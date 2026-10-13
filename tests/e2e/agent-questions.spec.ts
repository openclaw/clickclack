import { expect, test, type APIRequestContext, type Page } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { randomUUID } from "node:crypto";
import { waitForAppReady } from "./app-ready";
import { createGeneralChannel } from "./channel-fixture";

type AskedMessage = {
  id: string;
  question: {
    status: string;
    version: number;
    note?: string;
    response?: { answers?: Record<string, string[]>; responder?: { id: string } };
  };
};

function admin(args: string[]) {
  return execFileSync(
    "go",
    ["run", "./apps/api/cmd/clickclack", "admin", ...args, "--data", "./data/e2e"],
    { cwd: process.cwd(), encoding: "utf8" },
  ).trim();
}

async function createBot(page: Page, workspaceID: string, displayName: string) {
  const suffix = randomUUID().replaceAll("-", "").slice(0, 8);
  const response = await page.request.post(`/api/workspaces/${workspaceID}/bots`, {
    data: {
      display_name: displayName,
      handle: `${displayName.toLowerCase()}-${suffix}`,
      token_name: "e2e",
      scopes: ["bot:write"],
    },
  });
  expect(response.status()).toBe(201);
  const { bot_token } = (await response.json()) as { bot_token: { token: string } };
  return bot_token.token;
}

function minutesFromNow(minutes: number) {
  return new Date(Date.now() + minutes * 60_000).toISOString();
}

async function ask(
  request: APIRequestContext,
  token: string,
  endpoint: string,
  body: string,
  question: Record<string, unknown>,
) {
  const response = await request.post(endpoint, {
    headers: { Authorization: `Bearer ${token}` },
    data: { body, question },
  });
  expect(response.status()).toBe(201);
  expect(response.headers()["x-clickclack-questions"]).toBe("supported");
  return ((await response.json()) as { message: AskedMessage }).message;
}

async function readAsBot(request: APIRequestContext, token: string, messageID: string) {
  const response = await request.get(`/api/messages/${messageID}`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  expect(response.ok()).toBe(true);
  return ((await response.json()) as { message: AskedMessage }).message;
}

async function resolve(
  request: APIRequestContext,
  token: string,
  messageID: string,
  data: Record<string, unknown>,
) {
  const response = await request.post(`/api/messages/${messageID}/question/resolution`, {
    headers: { Authorization: `Bearer ${token}` },
    data,
  });
  expect(response.status()).toBe(200);
}

test("a quick question answers on the first tap and shows the bot's outcome", async ({ page }) => {
  const { workspace, channel, route } = await createGeneralChannel(page, "Quick question proof");
  const token = await createBot(page, workspace.id, "Courier");
  const message = await ask(
    page.request,
    token,
    `/api/channels/${channel.id}/messages`,
    "Courier needs input: which day do we ship?",
    {
      external_id: "e2e-quick",
      expires_at: minutesFromNow(10),
      items: [
        {
          id: "ship_date",
          header: "Date",
          prompt: "Which day do we ship?",
          options: [{ label: "Mon 15", description: "AA 2231" }, { label: "Tue 16" }],
        },
      ],
    },
  );

  await page.goto(route);
  await waitForAppReady(page);
  const card = page.locator(`[data-message-id="${message.id}"] .question-card`);
  await expect(card).toHaveAttribute("data-question-status", "open");
  await expect(card.getByText("Needs your answer")).toBeVisible();

  await card.getByRole("radio", { name: /Tue 16/ }).click();
  await expect(card).toHaveAttribute("data-question-status", "submitted");
  await expect(card.getByText("Sent · waiting for Courier")).toBeVisible();
  const submitted = await readAsBot(page.request, token, message.id);
  expect(submitted.question.response?.answers).toEqual({ ship_date: ["Tue 16"] });

  await resolve(page.request, token, message.id, {
    status: "answered",
    expected_version: submitted.question.version,
  });
  await expect(card).toHaveAttribute("data-question-status", "answered");
  await expect(card.locator(".question-summary")).toContainText("Date");
  await expect(card.locator(".question-summary")).toContainText("Tue 16");
});

test("only listed responders answer a form question, and a reopened card explains why", async ({
  page,
  browser,
}) => {
  const { workspace, channel, route } = await createGeneralChannel(page, "Form question proof");
  const meResponse = await page.request.get("/api/me");
  expect(meResponse.ok()).toBe(true);
  const { user: owner } = (await meResponse.json()) as { user: { id: string } };
  const suffix = randomUUID().replaceAll("-", "").slice(0, 12);
  const responderID = admin([
    "user",
    "create",
    "--name",
    "Question Responder",
    "--email",
    `question-responder-${suffix}@example.com`,
  ]);
  admin([
    "member",
    "add",
    "--workspace",
    workspace.id,
    "--created-by",
    owner.id,
    "--user",
    responderID,
    "--role",
    "member",
  ]);
  const token = await createBot(page, workspace.id, "Planner");
  const message = await ask(
    page.request,
    token,
    `/api/channels/${channel.id}/messages`,
    "Planner needs three details before the draft",
    {
      title: "Three details before the draft",
      expires_at: minutesFromNow(30),
      responder_user_ids: [responderID],
      allow_skip: false,
      items: [
        {
          id: "ship_date",
          header: "Date",
          prompt: "Which day do we ship?",
          options: [{ label: "Mon 15" }, { label: "Tue 16" }],
          allow_other: true,
        },
        { id: "boxes", header: "Boxes", prompt: "How many boxes?" },
        {
          id: "extras",
          header: "Extras",
          prompt: "Anything else?",
          multi_select: true,
          options: [{ label: "Sleeve" }, { label: "UPC label" }],
        },
      ],
    },
  );

  await page.goto(route);
  await waitForAppReady(page);
  const ownerCard = page.locator(`[data-message-id="${message.id}"] .question-card`);
  await expect(ownerCard.getByText("Three details before the draft")).toBeVisible();
  await expect(ownerCard.getByRole("radio", { name: /Mon 15/ })).toBeDisabled();
  await expect(ownerCard.locator(".question-lock")).toContainText("can answer this question");
  await expect(ownerCard.getByRole("button", { name: "Skip" })).toHaveCount(0);

  const context = await browser.newContext({
    extraHTTPHeaders: { "X-ClickClack-User": responderID },
  });
  try {
    const responderPage = await context.newPage();
    await responderPage.goto(route);
    await waitForAppReady(responderPage);
    let card = responderPage.locator(`[data-message-id="${message.id}"] .question-card`);
    const date = card.getByRole("radiogroup", { name: "Date" });
    const send = card.getByRole("button", { name: "Send answers" });
    await expect(send).toBeDisabled();

    await card.getByRole("textbox", { name: "Boxes" }).fill("120");
    await card.getByRole("checkbox", { name: /Sleeve/ }).click();
    await card.getByRole("checkbox", { name: /UPC label/ }).click();
    await date.getByRole("radio", { name: /Mon 15/ }).focus();
    await responderPage.keyboard.press("2");
    await expect(date.getByRole("radio", { name: /Tue 16/ })).toHaveAttribute(
      "aria-checked",
      "true",
    );
    await expect(card.getByLabel("3 of 3 answered")).toBeVisible();
    await expect(date.getByRole("radio", { name: /Mon 15/ })).toBeFocused();
    await responderPage.keyboard.press("3");
    const other = card.getByRole("textbox", { name: "Other answer for Date" });
    await expect(other).toBeFocused();
    await other.press("Escape");
    await expect(other).toHaveCount(0);
    await date.getByRole("radio", { name: /Tue 16/ }).focus();
    await responderPage.keyboard.press("3");
    await other.fill("Wed 17");
    await other.press("Enter");

    await expect(card).toHaveAttribute("data-question-status", "submitted");
    await expect(card.getByText("Sent · waiting for Planner")).toBeVisible();
    await expect(ownerCard).toHaveAttribute("data-question-status", "submitted");
    await expect(ownerCard.getByText("by Question Responder")).toBeVisible();
    const submitted = await readAsBot(page.request, token, message.id);
    expect(submitted.question.response?.answers).toEqual({
      ship_date: ["Wed 17"],
      boxes: ["120"],
      extras: ["Sleeve", "UPC label"],
    });
    expect(submitted.question.response?.responder?.id).toBe(responderID);

    await resolve(page.request, token, message.id, {
      status: "open",
      note: "Pick a listed date and at most 100 boxes",
      expected_version: submitted.question.version,
    });
    card = responderPage.locator(`[data-message-id="${message.id}"] .question-card`);
    await expect(card).toHaveAttribute("data-question-status", "open");
    await expect(card.getByText("Pick a listed date and at most 100 boxes")).toBeVisible();
    // The reopened card grows at the bottom of the channel; the list keeps following it.
    await expect(card.getByRole("button", { name: "Send answers" })).toBeInViewport();
    await card.getByRole("radio", { name: /Mon 15/ }).click();
    await card.getByRole("textbox", { name: "Boxes" }).fill("80");
    await card.getByRole("checkbox", { name: /Sleeve/ }).click();
    await card.getByRole("button", { name: "Send answers" }).click();
    await expect(card).toHaveAttribute("data-question-status", "submitted");
    await expect(card.getByText("Pick a listed date and at most 100 boxes")).toHaveCount(0);

    const corrected = await readAsBot(page.request, token, message.id);
    await resolve(page.request, token, message.id, {
      status: "answered",
      expected_version: corrected.question.version,
    });
    await expect(ownerCard).toHaveAttribute("data-question-status", "answered");
    const summary = ownerCard.locator(".question-summary");
    await expect(summary).toContainText("Mon 15");
    await expect(summary).toContainText("80");
    await expect(summary).toContainText("Sleeve");
  } finally {
    await context.close();
  }
});

test("a question in a thread closes at its deadline", async ({ page }) => {
  const { workspace, channel, route } = await createGeneralChannel(page, "Thread question proof");
  const token = await createBot(page, workspace.id, "Scout");
  const rootResponse = await page.request.post(`/api/channels/${channel.id}/messages`, {
    data: { body: "Scout, can you confirm the pickup?" },
  });
  expect(rootResponse.ok()).toBe(true);
  const { message: root } = (await rootResponse.json()) as { message: { id: string } };
  const reply = await ask(
    page.request,
    token,
    `/api/messages/${root.id}/thread/replies`,
    "Scout needs input: which truck?",
    {
      expires_at: minutesFromNow(1),
      items: [
        {
          id: "truck",
          header: "Truck",
          prompt: "Which truck picks up?",
          options: [{ label: "Morning" }, { label: "Afternoon" }],
        },
      ],
    },
  );

  await page.clock.install();
  await page.goto(route);
  await waitForAppReady(page);
  const rootRow = page.locator(`[data-message-id="${root.id}"]`);
  await rootRow.hover();
  await rootRow.getByRole("button", { name: "Open thread" }).click();
  const threadPane = page.getByRole("complementary", { name: "Thread pane" });
  const card = threadPane.locator(".question-card");
  await expect(card).toHaveAttribute("data-question-status", "open");
  await expect(card.locator(".question-timer")).toHaveClass(/question-timer--urgent/);

  // Only the page clock moves; the server keeps its own deadline.
  await page.clock.fastForward("01:05");
  await expect(card).toHaveAttribute("data-question-status", "expired");
  await expect(card.getByText("Expired", { exact: true })).toBeVisible();
  await expect(card.getByRole("radio")).toHaveCount(0);
  const stored = await readAsBot(page.request, token, reply.id);
  expect(stored.question.status).toBe("open");
});
