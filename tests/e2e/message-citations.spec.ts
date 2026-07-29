import { expect, test, type Page } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { waitForAppReady } from "./app-ready";

async function captureAnnotatedProof(
  page: Page,
  proofPath: string | undefined,
  step: string,
  detail: string,
  citationURL: string,
) {
  if (!proofPath) return;
  await page.evaluate(
    ({ step, detail, citationURL }) => {
      const annotation = document.createElement("div");
      annotation.dataset.citationProofAnnotation = "true";
      annotation.setAttribute("role", "note");
      annotation.style.cssText = [
        "position:fixed",
        "z-index:2147483647",
        "top:12px",
        "left:50%",
        "transform:translateX(-50%)",
        "width:min(920px,calc(100vw - 48px))",
        "box-sizing:border-box",
        "padding:12px 16px",
        "border:2px solid #67e8f9",
        "border-radius:10px",
        "background:rgba(8,15,30,.96)",
        "box-shadow:0 12px 32px rgba(0,0,0,.45)",
        "color:#f8fafc",
        "font:600 16px/1.35 ui-sans-serif,system-ui,sans-serif",
        "pointer-events:none",
      ].join(";");
      const title = document.createElement("div");
      title.textContent = step;
      title.style.color = "#67e8f9";
      const description = document.createElement("div");
      description.textContent = detail;
      const url = document.createElement("div");
      url.textContent = `Canonical URL: ${citationURL}`;
      url.style.cssText =
        "margin-top:4px;color:#cbd5e1;font:500 13px/1.35 ui-monospace,SFMono-Regular,monospace;overflow-wrap:anywhere";
      annotation.append(title, description, url);
      document.body.append(annotation);
    },
    { step, detail, citationURL },
  );
  await page.screenshot({ path: proofPath, fullPage: true });
  await page.locator("[data-citation-proof-annotation]").evaluate((element) => element.remove());
}

test("channel root citations copy, reopen, highlight, and keep their URL after the first reply", async ({
  browser,
  page,
}) => {
  test.setTimeout(45_000);
  await page.addInitScript(() => {
    window.addEventListener("DOMContentLoaded", () => {
      const highlightedMessageIDs: string[] = [];
      Object.assign(window, { highlightedMessageIDs });
      new MutationObserver(() => {
        for (const row of document.querySelectorAll<HTMLElement>("[data-message-id].highlight")) {
          const id = row.dataset.messageId;
          if (id && !highlightedMessageIDs.includes(id)) highlightedMessageIDs.push(id);
        }
      }).observe(document.body, { attributes: true, attributeFilter: ["class"], subtree: true });
    });
  });
  const suffix = randomUUID().replaceAll("-", "").slice(0, 12);
  const workspaceResponse = await page.request.post("/api/workspaces", {
    data: { name: `Citation proof ${suffix}` },
  });
  expect(workspaceResponse.ok()).toBe(true);
  const { workspace } = (await workspaceResponse.json()) as {
    workspace: { id: string; route_id: string };
  };
  const channelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: `citation-proof-${suffix}`, kind: "public" },
  });
  expect(channelResponse.ok()).toBe(true);
  const { channel } = (await channelResponse.json()) as {
    channel: { id: string; route_id: string };
  };
  const messageResponse = await page.request.post(`/api/channels/${channel.id}/messages`, {
    data: { body: `Stable citation ${suffix}` },
  });
  expect(messageResponse.ok()).toBe(true);
  const { message } = (await messageResponse.json()) as {
    message: { id: string; route_id: string; body: string };
  };
  expect(message.route_id).toMatch(/^M[A-Z0-9]{16}$/);

  await page.goto(`/app/${workspace.route_id}/${channel.route_id}`);
  await waitForAppReady(page);
  const row = page.locator(`[data-message-id="${message.id}"]`);
  await expect(row).toBeVisible();
  await page.evaluate(() => {
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: {
        writeText: async (value: string) => Object.assign(window, { copiedCitation: value }),
      },
    });
    window.__CLICKCLACK_CONFIG__ = {
      ...window.__CLICKCLACK_CONFIG__,
      frontendBaseUrl: "https://chat.example.test",
    };
  });
  const expectedPath = `/app/${workspace.route_id}/${message.route_id}`;
  const expectedURL = new URL(expectedPath, page.url()).toString();
  await row.hover();
  await row.getByRole("button", { name: "More actions" }).click();
  await captureAnnotatedProof(
    page,
    process.env.MESSAGE_CITATION_COPY_FRAME_PATH,
    "1 · Copy a stable citation",
    "The channel-root action menu exposes Copy link.",
    expectedURL,
  );
  await row.getByRole("menuitem", { name: "Copy link" }).click();
  await expect
    .poll(() => page.evaluate(() => Reflect.get(window, "copiedCitation")))
    .toBe(new URL(expectedPath, "https://chat.example.test").toString());

  await page.reload();
  await waitForAppReady(page);
  await page.evaluate(() => {
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: {
        writeText: async (value: string) => Object.assign(window, { copiedCitation: value }),
      },
    });
  });
  await row.hover();
  await row.getByRole("button", { name: "More actions" }).click();
  await row.getByRole("menuitem", { name: "Copy link" }).click();
  await expect
    .poll(() => page.evaluate(() => Reflect.get(window, "copiedCitation")))
    .toBe(expectedURL);

  const mobileContext = await browser.newContext({
    baseURL: new URL(page.url()).origin,
    hasTouch: true,
    isMobile: true,
    viewport: { width: 390, height: 720 },
  });
  const mobilePage = await mobileContext.newPage();
  await mobilePage.goto(`/app/${workspace.route_id}/${channel.route_id}`);
  await waitForAppReady(mobilePage);
  await mobilePage.evaluate(() => {
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: {
        writeText: async (value: string) => Object.assign(window, { copiedCitation: value }),
      },
    });
  });
  const mobileRow = mobilePage.locator(`[data-message-id="${message.id}"]`);
  const mobileTrigger = mobileRow.getByRole("button", { name: "More actions" });
  await mobileTrigger.focus();
  await mobilePage.keyboard.press("Enter");
  const actionSheet = mobilePage.getByRole("dialog", { name: "Message actions" });
  await actionSheet.getByRole("button", { name: "Copy link" }).click();
  await expect(actionSheet).toBeHidden();
  await expect
    .poll(() => mobilePage.evaluate(() => Reflect.get(window, "copiedCitation")))
    .toBe(expectedURL);
  await mobileContext.close();

  await page.goto(expectedURL);
  await waitForAppReady(page);
  await expect(page).toHaveURL(expectedURL);
  await expect
    .poll(() => page.evaluate(() => Reflect.get(window, "highlightedMessageIDs")))
    .toContain(message.id);
  await expect(page.locator(".thread.open")).toHaveCount(0);
  if (process.env.MESSAGE_CITATION_HIGHLIGHT_PROOF_PATH) {
    await page.screenshot({
      path: process.env.MESSAGE_CITATION_HIGHLIGHT_PROOF_PATH,
      fullPage: true,
    });
  }
  await captureAnnotatedProof(
    page,
    process.env.MESSAGE_CITATION_HIGHLIGHT_FRAME_PATH,
    "2 · Open the citation before replies",
    "The canonical route opens the channel and highlights its root message.",
    expectedURL,
  );

  const replyResponse = await page.request.post(`/api/messages/${message.id}/thread/replies`, {
    data: { body: `First reply ${suffix}` },
  });
  expect(replyResponse.ok()).toBe(true);
  const { message: reply } = (await replyResponse.json()) as {
    message: { route_id?: string };
  };
  expect(reply.route_id ?? "").toBe("");
  await page.reload();
  await waitForAppReady(page);
  await expect(page).toHaveURL(expectedURL);
  await expect(page.locator(".thread.open")).toBeVisible();
  await expect(page.locator(".thread-root", { hasText: message.body })).toBeVisible();
  await expect(page.locator(".reply", { hasText: `First reply ${suffix}` })).toBeVisible();
  await captureAnnotatedProof(
    page,
    process.env.MESSAGE_CITATION_THREAD_FRAME_PATH,
    "3 · Add the first reply",
    "The same URL now opens the thread pane; the citation did not change.",
    expectedURL,
  );

  const root = page.locator(".thread-root");
  await page.evaluate(() => {
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText: async () => Promise.reject(new Error("clipboard denied")) },
    });
  });
  await root.getByRole("button", { name: "Copy link" }).click();
  const fallback = page.getByRole("dialog", { name: "Copy message link" });
  await expect(fallback).toBeVisible();
  const input = fallback.getByLabel("Message link");
  await expect(input).toHaveValue(expectedURL);
  await expect(input).toBeFocused();
  await expect(input).toHaveJSProperty("selectionStart", 0);
  await expect(input).toHaveJSProperty("selectionEnd", expectedURL.length);
  if (process.env.MESSAGE_CITATION_FALLBACK_PROOF_PATH) {
    await page.screenshot({
      path: process.env.MESSAGE_CITATION_FALLBACK_PROOF_PATH,
      fullPage: true,
    });
  }
  await captureAnnotatedProof(
    page,
    process.env.MESSAGE_CITATION_FALLBACK_FRAME_PATH,
    "4 · Recover from clipboard denial",
    "The same URL is visible, focused, and selected in the accessible fallback.",
    expectedURL,
  );
});
