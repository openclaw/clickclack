import { expect, test, type Page } from "@playwright/test";

async function clickThreadRootReplyButton(page: Page) {
  const button = page.locator(".thread-root .reply-quote-btn");
  await expect(button).toBeVisible();
  await button.evaluate((node: HTMLButtonElement) => {
    node.focus();
    node.click();
  });
}

test.describe("type-to-focus composer", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/app");
    await page.getByRole("link", { name: "# general" }).click();
    await expect(page.getByRole("heading", { name: "#general" })).toBeVisible();
  });

  test("typing while no input is focused redirects keystrokes to the channel composer", async ({
    page,
  }) => {
    const composer = page.getByLabel("Message body");
    await page.locator("body").click({ position: { x: 5, y: 5 } });
    await expect(composer).not.toBeFocused();

    await page.keyboard.type("hello world");
    await expect(composer).toBeFocused();
    await expect(composer).toHaveValue("hello world");
  });

  test("typing after clicking the active channel redirects to the composer", async ({ page }) => {
    const composer = page.getByLabel("Message body");
    const activeChannel = page.getByRole("link", { name: "# general" });

    await activeChannel.click();
    await expect(activeChannel).toBeFocused();
    await expect
      .poll(async () => activeChannel.evaluate((el) => getComputedStyle(el).outlineStyle))
      .toBe("none");

    await page.keyboard.type("nav draft");
    await expect(composer).toBeFocused();
    await expect(composer).toHaveValue("nav draft");
  });

  test("modifier-key combos are not redirected", async ({ page }) => {
    const composer = page.getByLabel("Message body");
    await page.locator(".messages").click({ position: { x: 10, y: 10 } });
    await expect(composer).not.toBeFocused();

    await page.keyboard.press("Control+a");
    await page.keyboard.press("Meta+r").catch(() => {});
    await expect(composer).not.toBeFocused();
  });

  test("typing in an existing input does not jump focus to the composer", async ({ page }) => {
    const composer = page.getByLabel("Message body");
    const search = page.getByPlaceholder(/search/i).first();
    if ((await search.count()) === 0) test.skip(true, "no search input in this build");
    await search.click();
    await page.keyboard.type("abc");
    await expect(composer).not.toBeFocused();
    await expect(search).toBeFocused();
  });

  test("space key does not scroll the page and lands in the composer", async ({ page }) => {
    const composer = page.getByLabel("Message body");
    const messages = page.locator(".messages");
    await messages.click({ position: { x: 10, y: 10 } });
    const beforeScroll = await messages.evaluate((el) => el.scrollTop);
    await page.keyboard.press("Space");
    const afterScroll = await messages.evaluate((el) => el.scrollTop);
    expect(afterScroll).toBe(beforeScroll);
    await expect(composer).toBeFocused();
    await expect(composer).toHaveValue(" ");
  });

  test("redirect targets the thread composer when a thread is open", async ({ page }) => {
    const rootBody = `thread root ${Date.now()}`;
    await page.getByLabel("Message body").fill(rootBody);
    await page.getByRole("button", { name: "Send" }).click();
    const row = page.locator(".message-row:not(.is-pending)", {
      has: page.locator(".markdown").filter({ hasText: rootBody }),
    });
    await row.hover();
    await row.getByRole("button", { name: "Open thread" }).click();
    const threadComposer = page.getByLabel("Reply body");
    await expect(threadComposer).toBeVisible();

    await expect(threadComposer).not.toBeFocused();
    await page.keyboard.type("inside thread");
    await expect(threadComposer).toBeFocused();
    await expect(threadComposer).toHaveValue("inside thread");
  });

  test("typing after chat action buttons still redirects to the active composer", async ({
    page,
  }) => {
    const rootBody = `button focus root ${Date.now()}`;
    await page.getByLabel("Message body").fill(rootBody);
    await page.getByRole("button", { name: "Send" }).click();
    const row = page.locator(".message-row:not(.is-pending)", {
      has: page.locator(".markdown").filter({ hasText: rootBody }),
    });

    await row.hover();
    await row.getByRole("button", { name: "Open thread" }).click();
    const threadComposer = page.getByLabel("Reply body");
    await expect(threadComposer).toBeVisible();

    await row.hover();
    await row.getByRole("button", { name: "Reply" }).click();
    const composer = page.getByLabel("Message body");
    await expect(composer).not.toBeFocused();
    await page.keyboard.type("channel draft");
    await expect(composer).toBeFocused();
    await expect(composer).toHaveValue("channel draft");
    await composer.fill("");

    await clickThreadRootReplyButton(page);
    await expect(threadComposer).not.toBeFocused();
    await page.keyboard.type("thread draft");
    await expect(threadComposer).toBeFocused();
    await expect(threadComposer).toHaveValue("thread draft");
  });

  test("typing with selected thread quote text does not redirect to composer", async ({ page }) => {
    const rootBody = `thread quote root ${Date.now()}`;
    await page.getByLabel("Message body").fill(rootBody);
    await page.getByRole("button", { name: "Send" }).click();
    const row = page.locator(".message-row:not(.is-pending)", {
      has: page.locator(".markdown").filter({ hasText: rootBody }),
    });
    await row.hover();
    await row.getByRole("button", { name: "Open thread" }).click();
    const threadComposer = page.getByLabel("Reply body");
    await expect(threadComposer).toBeVisible();

    await clickThreadRootReplyButton(page);
    await threadComposer.fill("quoted reply");
    await page
      .locator("form.reply-composer")
      .getByRole("button", { name: "Reply", exact: true })
      .click();

    const quoteSnippet = page.locator(".thread .quote-block .quote-snippet", {
      hasText: rootBody,
    });
    await expect(quoteSnippet).toBeVisible();
    await quoteSnippet.evaluate((el) => {
      const range = document.createRange();
      range.selectNodeContents(el);
      const selection = window.getSelection();
      selection?.removeAllRanges();
      selection?.addRange(range);
    });

    await page.keyboard.type("x");
    await expect(threadComposer).not.toBeFocused();
    await expect(threadComposer).toHaveValue("");
  });

  test("composer auto-grows with newlines and shrinks back after send", async ({ page }) => {
    const composer = page.getByLabel("Message body");
    await composer.click();
    const initialHeight = await composer.evaluate((el) => el.getBoundingClientRect().height);

    await composer.fill("line one\nline two\nline three\nline four\nline five");
    const grownHeight = await composer.evaluate((el) => el.getBoundingClientRect().height);
    expect(grownHeight).toBeGreaterThan(initialHeight + 30);

    const max = await composer.evaluate((el) => parseFloat(getComputedStyle(el).maxHeight));
    expect(grownHeight).toBeLessThanOrEqual(max + 1);

    await composer.fill(Array.from({ length: 60 }, (_, i) => `row ${i}`).join("\n"));
    const cappedHeight = await composer.evaluate((el) => el.getBoundingClientRect().height);
    expect(cappedHeight).toBeLessThanOrEqual(max + 1);
    const scrollable = await composer.evaluate((el) => el.scrollHeight > el.clientHeight + 1);
    expect(scrollable).toBe(true);

    await composer.fill("");
    await page.waitForTimeout(50);
    const afterClearHeight = await composer.evaluate((el) => el.getBoundingClientRect().height);
    expect(Math.abs(afterClearHeight - initialHeight)).toBeLessThan(2);
  });

  test("global Escape clears the reply target even when composer is not focused", async ({
    page,
  }) => {
    const composer = page.getByLabel("Message body");
    await composer.fill("the original draft");
    await page.getByRole("button", { name: "Send" }).click();

    const originalRow = page.locator(".message-row", {
      has: page.locator(".markdown").filter({ hasText: "the original draft" }),
    });
    await originalRow.hover();
    await originalRow.getByRole("button", { name: "Reply" }).click();

    const chip = page.getByLabel("Replying to message");
    await expect(chip).toBeVisible();

    // Move focus away from the composer.
    await page.locator("body").click({ position: { x: 5, y: 5 } });
    await expect(composer).not.toBeFocused();

    await page.keyboard.press("Escape");
    await expect(chip).toHaveCount(0);
  });
});
