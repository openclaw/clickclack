import { defineConfig, devices } from "@playwright/test";

const e2ePort = process.env.CLICKCLACK_E2E_PORT || "18082";

export default defineConfig({
  testDir: "tests/e2e",
  timeout: 30_000,
  expect: {
    timeout: 5_000,
  },
  use: {
    baseURL: `http://127.0.0.1:${e2ePort}`,
    headless: true,
    trace: "on-first-retry",
  },
  webServer: {
    command: `rm -rf data/e2e && pnpm build && go run ./apps/api/cmd/clickclack serve --addr 127.0.0.1:${e2ePort} --data ./data/e2e --dev-bootstrap=true`,
    url: `http://127.0.0.1:${e2ePort}`,
    reuseExistingServer: process.env.CLICKCLACK_REUSE_E2E_SERVER === "1",
    timeout: 120_000,
  },
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],
});
