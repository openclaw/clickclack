import { mkdir } from "node:fs/promises";
import path from "node:path";
import { expect, test } from "@playwright/test";

const e2ePort = Number(process.env.CLICKCLACK_E2E_PORT || "18082");
const clickClackOrigin = `http://127.0.0.1:${e2ePort}`;
const hostOrigin = `http://127.0.0.1:${e2ePort + 1}`;
const captureUiProof = process.env.CLICKCLACK_CAPTURE_UI_PROOF === "1";
const proofDir = path.join(process.cwd(), ".artifacts", "embed-theme");

type HostThemeTokens = Record<string, string>;

const lightTokens: HostThemeTokens = {
  surface: "#faf9f7",
  card: "#ffffff",
  elevated: "#f1efec",
  text: "#27272a",
  "text-strong": "#18181b",
  muted: "#71717a",
  border: "#e4e4e7",
  "border-strong": "#d4d4d8",
  accent: "#bd4531",
  "accent-fill": "#a33928",
  "accent-fg": "#ffffff",
  radius: "8px",
};

const darkTokens: HostThemeTokens = {
  surface: "#0e1015",
  card: "#161920",
  elevated: "#191c24",
  text: "#d4d4d8",
  "text-strong": "#f4f4f5",
  muted: "#8b8b94",
  border: "#1e2028",
  "border-strong": "#2e3040",
  accent: "#ff5c5c",
  "accent-fill": "#d13c3c",
  "accent-fg": "#ffffff",
  radius: "8px",
};

const customTokens: HostThemeTokens = {
  ...darkTokens,
  surface: "#171229",
  card: "#211a36",
  elevated: "#2c2246",
  text: "#efe8ff",
  border: "#493861",
  accent: "#c084fc",
  "accent-fill": "#9333ea",
};

async function captureProof(page: import("@playwright/test").Page, name: string) {
  if (!captureUiProof) return;
  await mkdir(proofDir, { recursive: true });
  await page.screenshot({ path: path.join(proofDir, `${name}.png`), fullPage: true });
}

test("embedded channels follow the exact cross-origin host theme without changing account appearance", async ({
  page,
}) => {
  const workspaceResponse = await page.request.get("/api/workspaces");
  expect(workspaceResponse.ok()).toBe(true);
  const { workspaces } = (await workspaceResponse.json()) as {
    workspaces: { id: string; route_id: string }[];
  };
  const workspace = workspaces[0];
  const channelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: `embed-theme-${Date.now()}`, kind: "public" },
  });
  expect(channelResponse.ok()).toBe(true);
  const { channel } = (await channelResponse.json()) as {
    channel: { route_id: string; name: string };
  };

  const embedUrl = new URL(
    `/embed/channel/${workspace.route_id}/${channel.route_id}`,
    clickClackOrigin,
  );
  embedUrl.searchParams.set("theme", "light");
  embedUrl.searchParams.set("hostOrigin", hostOrigin);

  await page.route(`${hostOrigin}/theme-proof`, (route) =>
    route.fulfill({
      contentType: "text/html; charset=utf-8",
      body: `<!doctype html>
        <html><head><meta charset="utf-8"><title>Host theme proof</title>
        <style>
          :root { color-scheme: light; }
          body { margin: 0; padding: 24px; background: #faf9f7; color: #27272a;
            font: 14px system-ui, sans-serif; }
          main { display: grid; grid-template-columns: minmax(0, 1fr) minmax(320px, 460px);
            min-height: 620px; overflow: hidden; border: 1px solid #e4e4e7; border-radius: 8px; }
          section { padding: 24px; }
          iframe { width: 100%; height: 620px; border: 0; border-left: 1px solid #e4e4e7; }
        </style></head><body><main>
          <section><h1>OpenClaw host theme</h1><p>Cross-origin ClickClack sidebar</p></section>
          <iframe title="ClickClack discussion" src="${embedUrl.href}"></iframe>
        </main></body></html>`,
    }),
  );

  const appearanceWrites: string[] = [];
  page.on("request", (request) => {
    if (
      request.method() === "PATCH" &&
      new URL(request.url()).pathname === "/api/me" &&
      request.postData()?.includes("appearance_preferences")
    ) {
      appearanceWrites.push(request.postData() ?? "");
    }
  });

  await page.goto(`${hostOrigin}/theme-proof`);
  const embeddedPage = page.frameLocator('iframe[title="ClickClack discussion"]');
  await expect(embeddedPage.getByLabel("Embedded channel")).toBeVisible();
  await expect(embeddedPage.locator("html")).toHaveAttribute("data-color-mode", "light");
  const frame = page.frames().find((candidate) => candidate.url().startsWith(clickClackOrigin));
  expect(frame).toBeDefined();

  const storedAppearance = await frame!.evaluate(() => ({
    colorMode: localStorage.getItem("clickclack:color-mode:v1"),
    boardTheme: localStorage.getItem("clickclack:board-theme:v1"),
  }));
  appearanceWrites.length = 0;

  const postTheme = async (mode: "light" | "dark", tokens: HostThemeTokens) => {
    await page.evaluate(
      ({ mode, tokens, targetOrigin }) => {
        const frame = document.querySelector<HTMLIFrameElement>("iframe");
        if (!frame?.contentWindow) throw new Error("Theme proof frame is unavailable");
        document.documentElement.style.colorScheme = mode;
        document.body.style.backgroundColor = tokens.surface;
        document.body.style.color = tokens.text;
        frame.style.borderLeftColor = tokens.border;
        document.querySelector("main")?.style.setProperty("border-color", tokens.border);
        frame.contentWindow.postMessage(
          { type: "openclaw:widget-theme", mode, tokens },
          targetOrigin,
        );
      },
      { mode, tokens, targetOrigin: clickClackOrigin },
    );
  };

  await postTheme("light", lightTokens);
  await expect
    .poll(() =>
      frame!.evaluate(() => ({
        mode: document.documentElement.dataset.colorMode,
        background: getComputedStyle(document.documentElement).getPropertyValue("--bg").trim(),
        panel: getComputedStyle(document.documentElement).getPropertyValue("--panel").trim(),
        accent: getComputedStyle(document.documentElement).getPropertyValue("--accent").trim(),
      })),
    )
    .toEqual({ mode: "light", background: "#faf9f7", panel: "#ffffff", accent: "#bd4531" });
  await captureProof(page, "01-light");

  await postTheme("dark", darkTokens);
  await expect
    .poll(() =>
      frame!.evaluate(() => ({
        mode: document.documentElement.dataset.colorMode,
        background: getComputedStyle(document.documentElement).getPropertyValue("--bg").trim(),
        panel: getComputedStyle(document.documentElement).getPropertyValue("--panel").trim(),
        accent: getComputedStyle(document.documentElement).getPropertyValue("--accent").trim(),
      })),
    )
    .toEqual({ mode: "dark", background: "#0e1015", panel: "#161920", accent: "#ff5c5c" });
  await captureProof(page, "02-dark");

  await postTheme("dark", customTokens);
  await expect
    .poll(() =>
      frame!.evaluate(() => ({
        background: getComputedStyle(document.documentElement).getPropertyValue("--bg").trim(),
        panel: getComputedStyle(document.documentElement).getPropertyValue("--panel").trim(),
        accent: getComputedStyle(document.documentElement).getPropertyValue("--accent").trim(),
      })),
    )
    .toEqual({ background: "#171229", panel: "#211a36", accent: "#c084fc" });
  await captureProof(page, "03-custom");

  await frame!.evaluate(() => {
    window.postMessage(
      {
        type: "openclaw:widget-theme",
        mode: "light",
        tokens: { surface: "#ff0000", accent: "#ff0000" },
      },
      window.location.origin,
    );
  });
  await expect
    .poll(() =>
      frame!.evaluate(() => ({
        mode: document.documentElement.dataset.colorMode,
        background: getComputedStyle(document.documentElement).getPropertyValue("--bg").trim(),
      })),
    )
    .toEqual({ mode: "dark", background: "#171229" });

  await expect
    .poll(() =>
      frame!.evaluate(() => ({
        colorMode: localStorage.getItem("clickclack:color-mode:v1"),
        boardTheme: localStorage.getItem("clickclack:board-theme:v1"),
      })),
    )
    .toEqual(storedAppearance);
  expect(appearanceWrites).toEqual([]);
});
