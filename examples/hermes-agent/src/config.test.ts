import assert from "node:assert/strict";
import test from "node:test";

import { loadConfig } from "./config.ts";

const required = {
  CLICKCLACK_BASE_URL: "https://clickclack.example/",
  CLICKCLACK_BOT_TOKEN: "ccb_test",
  CLICKCLACK_WORKSPACE_ID: "wsp_1",
  HERMES_CONNECTOR_ALLOWED_USER_IDS: "usr_1, usr_2",
  HERMES_CONNECTOR_CURSOR_FILE: "/var/lib/clickclack-hermes/cursor.json",
};

test("loadConfig normalizes URLs and parses bounded integer options", () => {
  const config = loadConfig({
    ...required,
    HERMES_API_URL: "http://127.0.0.1:8642/",
    HERMES_CONNECTOR_HISTORY_LIMIT: "12",
    HERMES_CONNECTOR_MAX_REPLY_CHARS: "5000",
    HERMES_CONNECTOR_MAX_CONCURRENT_RUNS: "6",
    HERMES_CONNECTOR_RUN_TIMEOUT_MS: "9000",
    HERMES_CONNECTOR_RECONNECT_MS: "250",
  });

  assert.equal(config.clickclackBaseUrl, "https://clickclack.example");
  assert.equal(config.hermesBaseUrl, "http://127.0.0.1:8642");
  assert.equal(config.historyLimit, 12);
  assert.equal(config.maxReplyChars, 5000);
  assert.equal(config.maxConcurrentRuns, 6);
  assert.equal(config.runTimeoutMs, 9000);
  assert.equal(config.reconnectMs, 250);
  assert.deepEqual(config.allowedUserIds, new Set(["usr_1", "usr_2"]));
  assert.deepEqual(config.allowedChannelIds, new Set<string>());
  assert.equal(config.cursorFile, "/var/lib/clickclack-hermes/cursor.json");
});

test("loadConfig reports every missing required security variable", () => {
  assert.throws(
    () => loadConfig({}),
    /CLICKCLACK_BASE_URL, CLICKCLACK_BOT_TOKEN, CLICKCLACK_WORKSPACE_ID, HERMES_CONNECTOR_ALLOWED_USER_IDS, HERMES_CONNECTOR_CURSOR_FILE/,
  );
});

test("loadConfig parses an explicit channel allowlist", () => {
  const config = loadConfig({
    ...required,
    HERMES_CONNECTOR_ALLOWED_CHANNEL_IDS: "chn_1,chn_2, chn_1",
  });

  assert.deepEqual(config.allowedChannelIds, new Set(["chn_1", "chn_2"]));
});

test("loadConfig requires an absolute cursor file path", () => {
  assert.throws(
    () => loadConfig({ ...required, HERMES_CONNECTOR_CURSOR_FILE: "state/cursor.json" }),
    /HERMES_CONNECTOR_CURSOR_FILE must be an absolute path/,
  );
});

test("loadConfig refuses credentials embedded in URLs", () => {
  assert.throws(
    () => loadConfig({ ...required, HERMES_API_URL: "https://user:pass@hermes.example" }),
    /must not contain credentials/,
  );
});

test("loadConfig requires an API key for a non-loopback Hermes server", () => {
  assert.throws(
    () => loadConfig({ ...required, HERMES_API_URL: "https://hermes.example" }),
    /HERMES_API_KEY is required/,
  );
  assert.equal(
    loadConfig({
      ...required,
      HERMES_API_URL: "https://hermes.example",
      HERMES_API_KEY: "secret",
    }).hermesApiKey,
    "secret",
  );
});

test("loadConfig refuses plaintext HTTP for a remote ClickClack server", () => {
  assert.throws(
    () => loadConfig({ ...required, CLICKCLACK_BASE_URL: "http://clickclack.example" }),
    /CLICKCLACK_BASE_URL must use HTTPS/,
  );
  assert.equal(
    loadConfig({
      ...required,
      CLICKCLACK_BASE_URL: "http://clickclack.internal",
      CLICKCLACK_CONNECTOR_ALLOW_INSECURE_REMOTE_HTTP: "true",
    }).clickclackBaseUrl,
    "http://clickclack.internal",
  );
});

test("loadConfig refuses plaintext HTTP for a remote Hermes server", () => {
  assert.throws(
    () =>
      loadConfig({
        ...required,
        HERMES_API_URL: "http://hermes.example",
        HERMES_API_KEY: "secret",
      }),
    /must use HTTPS/,
  );
  assert.equal(
    loadConfig({
      ...required,
      HERMES_API_URL: "http://hermes.internal",
      HERMES_API_KEY: "secret",
      HERMES_CONNECTOR_ALLOW_INSECURE_REMOTE_HTTP: "true",
    }).hermesBaseUrl,
    "http://hermes.internal",
  );
});

test("loadConfig rejects loose or out-of-range integers", () => {
  assert.throws(
    () => loadConfig({ ...required, HERMES_CONNECTOR_HISTORY_LIMIT: "10items" }),
    /HERMES_CONNECTOR_HISTORY_LIMIT/,
  );
  assert.throws(
    () => loadConfig({ ...required, HERMES_CONNECTOR_RECONNECT_MS: "49" }),
    /HERMES_CONNECTOR_RECONNECT_MS/,
  );
});
