import { defineConfig, devices } from "@playwright/test";

const e2ePort = process.env.CLICKCLACK_E2E_PORT || "18082";
const embedHostOrigin = `http://127.0.0.1:${Number(e2ePort) + 1}`;

export default defineConfig({
  testDir: "tests/e2e",
  timeout: 30_000,
  // CI shares one server across parallel workers; realtime traffic from a
  // sibling worker can occasionally shift UI mid-action. Retries keep that
  // long tail from failing runs while traces still record every failure.
  retries: process.env.CI ? 2 : 0,
  expect: {
    timeout: 5_000,
  },
  use: {
    baseURL: `http://127.0.0.1:${e2ePort}`,
    headless: true,
    trace: "retain-on-failure",
  },
  webServer: [
    {
      command: `rm -rf data/e2e && pnpm build && go run -tags clickclack_e2e_unsafe_callbacks ./apps/api/cmd/clickclack serve --addr 127.0.0.1:${e2ePort} --data ./data/e2e --dev-bootstrap=true --embed-frame-ancestors ${embedHostOrigin}`,
      url: `http://127.0.0.1:${e2ePort}`,
      reuseExistingServer: process.env.CLICKCLACK_REUSE_E2E_SERVER === "1",
      // A cold production SPA build can exceed two minutes before the Go server starts.
      timeout: 240_000,
    },
    {
      command: "node tests/e2e/fixtures/embed-theme-host.mjs",
      url: `${embedHostOrigin}/healthz`,
      reuseExistingServer: process.env.CLICKCLACK_REUSE_E2E_SERVER === "1",
      env: { CLICKCLACK_EMBED_HOST_PORT: String(Number(e2ePort) + 1) },
      timeout: 10_000,
    },
  ],
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],
});
