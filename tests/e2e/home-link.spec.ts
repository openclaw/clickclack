import { expect, test } from "@playwright/test";
import { execFileSync, spawn } from "node:child_process";
import { once } from "node:events";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { waitForAppReady } from "./app-ready";

let testDir: string;
let binary: string;

test.beforeAll(() => {
  testDir = mkdtempSync(join(tmpdir(), "clickclack-home-link-"));
  binary = process.env.CLICKCLACK_HOME_LINK_TEST_BINARY || join(testDir, "clickclack");
  if (!process.env.CLICKCLACK_HOME_LINK_TEST_BINARY) {
    execFileSync("go", ["build", "-buildvcs=false", "-o", binary, "./apps/api/cmd/clickclack"]);
  }
});
test.afterAll(() => rmSync(testDir, { recursive: true, force: true }));

for (const scenario of [
  { name: "defaults", url: "", label: "", wantLabel: "cc" },
  { name: "label only", url: "", label: "Portal", wantLabel: "Portal" },
  { name: "explicit root and label", url: "/", label: "Portal", wantLabel: "Portal" },
  { name: "product path", url: " /portal?from=chat#latest ", label: "", wantLabel: "cc" },
  { name: "full custom link", url: "external", label: "Product", wantLabel: "Product" },
  {
    name: "long Unicode label",
    url: "",
    label: "運用ポータル🦞".repeat(4),
    wantLabel: "運用ポータル🦞".repeat(4),
  },
]) {
  test(`home link uses real server configuration: ${scenario.name}`, async ({
    browser,
  }, testInfo) => {
    const port = 19178 + testInfo.parallelIndex;
    const origin = `http://127.0.0.1:${port}`;
    // A second hostname on this same synthetic server is an external origin.
    const homeURL = scenario.url === "external" ? `http://localhost:${port}/` : scenario.url;
    const configPath = join(testDir, "config.json");
    writeFileSync(
      configPath,
      JSON.stringify({
        addr: `127.0.0.1:${port}`,
        data: join(testDir, "data"),
        dev_bootstrap: true,
        home_url: homeURL,
        home_label: scenario.label,
      }),
    );
    const server = spawn(binary, ["serve", "--config", configPath], {
      env: { PATH: process.env.PATH, HOME: testDir, SystemRoot: process.env.SystemRoot },
      stdio: ["ignore", "ignore", "pipe"],
    });
    let serverLog = "";
    server.stderr.on("data", (data) => {
      serverLog += data.toString();
    });
    const stopped = once(server, "exit");
    try {
      await expect
        .poll(async () => {
          if (server.exitCode !== null) throw new Error(serverLog);
          return fetch(`${origin}/readyz`)
            .then((response) => response.ok)
            .catch(() => false);
        })
        .toBe(true);

      for (const capability of ["browser", "native frame", "integrated titlebar"]) {
        await test.step(capability, async () => {
          const context = await browser.newContext({ viewport: { width: 1280, height: 860 } });
          try {
            if (capability !== "browser") {
              await context.addInitScript((integratedTitleBar) => {
                Object.assign(window, {
                  clickclackDesktop: {
                    integratedTitleBar,
                    notify: async () => true,
                    onNavigate: () => () => {},
                    onQuickCompose: () => () => {},
                    openSettings: () => {},
                    platform: "darwin",
                    setActiveRoute: () => {},
                    setUnreadCount: () => {},
                    signInWithGitHub: async () => true,
                  },
                });
              }, capability === "integrated titlebar");
            }
            const page = await context.newPage();
            const response = page.waitForResponse(`${origin}/api/home-link`);
            await page.goto(`${origin}/app`);
            expect(await (await response).json()).toEqual({
              url: homeURL.trim() || "/",
              label: scenario.wantLabel,
            });
            await waitForAppReady(page);
            const home = page.locator(".guild-rail .guild.home");
            const href =
              capability === "integrated titlebar" && (!homeURL || homeURL === "/")
                ? "/app"
                : homeURL.trim() || "/";
            await expect(home).toHaveText(scenario.wantLabel);
            await expect(home).toHaveAttribute("href", href);
            await expect(home).toHaveAccessibleName(
              scenario.wantLabel === "cc" ? "ClickClack home" : `${scenario.wantLabel} home`,
            );
            const bounds = await home.evaluate((element) => {
              const tile = element.getBoundingClientRect();
              const label = element.querySelector("span")!.getBoundingClientRect();
              return {
                left: label.left - tile.left,
                right: tile.right - label.right,
                top: label.top - tile.top,
                bottom: tile.bottom - label.bottom,
              };
            });
            expect(bounds.left).toBeGreaterThanOrEqual(0);
            expect(bounds.right).toBeGreaterThanOrEqual(0);
            expect(bounds.top).toBeGreaterThanOrEqual(0);
            expect(bounds.bottom).toBeGreaterThanOrEqual(0);
            await home.click();
            await expect
              .poll(() => {
                const actual = new URL(page.url());
                const expected = new URL(href, origin);
                return (
                  actual.origin === expected.origin &&
                  (href === "/app"
                    ? actual.pathname.startsWith("/app")
                    : actual.pathname + actual.search + actual.hash ===
                      expected.pathname + expected.search + expected.hash)
                );
              })
              .toBe(true);
          } finally {
            await context.close();
          }
        });
      }
    } finally {
      server.kill("SIGTERM");
      await stopped;
    }
  });
}
