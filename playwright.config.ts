import { defineConfig, devices } from "@playwright/test";
import { generateKeyPairSync } from "node:crypto";

const e2ePort = process.env.CLICKCLACK_E2E_PORT || "18082";
const embedHostOrigin = `http://127.0.0.1:${Number(e2ePort) + 1}`;
const pushRelayPort = String(Number(e2ePort) + 2);

// A throwaway VAPID pair for this run. Web push keys are credentials, so the
// suite generates its own and nothing is ever checked in.
const vapid = generateVAPIDKeys();

export default defineConfig({
  testDir: "tests/e2e",
  timeout: 30_000,
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
      env: {
        CLICKCLACK_WEBPUSH_VAPID_PUBLIC_KEY: vapid.publicKey,
        CLICKCLACK_WEBPUSH_VAPID_PRIVATE_KEY: vapid.privateKey,
        CLICKCLACK_WEBPUSH_SUBJECT: "mailto:e2e@clickclack.test",
      },
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
    {
      command: "node tests/e2e/fixtures/push-relay.mjs",
      url: `http://127.0.0.1:${pushRelayPort}/healthz`,
      reuseExistingServer: process.env.CLICKCLACK_REUSE_E2E_SERVER === "1",
      env: { CLICKCLACK_PUSH_RELAY_PORT: pushRelayPort },
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

// generateVAPIDKeys returns the P-256 pair in the base64url encoding the
// server and the browser both expect: the uncompressed public point, and the
// raw private scalar.
function generateVAPIDKeys(): { publicKey: string; privateKey: string } {
  const pair = generateKeyPairSync("ec", { namedCurve: "prime256v1" });
  const publicJWK = pair.publicKey.export({ format: "jwk" }) as { x: string; y: string };
  const privateJWK = pair.privateKey.export({ format: "jwk" }) as { d: string };
  const point = Buffer.concat([
    Buffer.from([4]),
    Buffer.from(publicJWK.x, "base64url"),
    Buffer.from(publicJWK.y, "base64url"),
  ]);
  return { publicKey: point.toString("base64url"), privateKey: privateJWK.d };
}
