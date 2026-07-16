import { isAbsolute } from "node:path";

export type HermesConnectorConfig = {
  clickclackBaseUrl: string;
  clickclackBotToken: string;
  clickclackWorkspaceId: string;
  hermesBaseUrl: string;
  hermesApiKey?: string;
  historyLimit: number;
  maxReplyChars: number;
  maxConcurrentRuns: number;
  runTimeoutMs: number;
  reconnectMs: number;
  allowedUserIds: ReadonlySet<string>;
  allowedChannelIds: ReadonlySet<string>;
  cursorFile: string;
  instructions?: string;
};

type Environment = Record<string, string | undefined>;

const DEFAULT_HERMES_URL = "http://127.0.0.1:8642";

export function loadConfig(env: Environment): HermesConnectorConfig {
  const required = [
    "CLICKCLACK_BASE_URL",
    "CLICKCLACK_BOT_TOKEN",
    "CLICKCLACK_WORKSPACE_ID",
    "HERMES_CONNECTOR_ALLOWED_USER_IDS",
    "HERMES_CONNECTOR_CURSOR_FILE",
  ] as const;
  const missing = required.filter((key) => !env[key]?.trim());
  if (missing.length > 0) {
    throw new Error(`Missing required environment variables: ${missing.join(", ")}`);
  }

  const clickclackBaseUrl = normalizeHttpUrl(env.CLICKCLACK_BASE_URL!, "CLICKCLACK_BASE_URL");
  const clickclackUrl = new URL(clickclackBaseUrl);
  if (
    !isLoopbackHost(clickclackUrl.hostname) &&
    clickclackUrl.protocol !== "https:" &&
    env.CLICKCLACK_CONNECTOR_ALLOW_INSECURE_REMOTE_HTTP?.trim().toLowerCase() !== "true"
  ) {
    throw new Error(
      "Non-loopback CLICKCLACK_BASE_URL must use HTTPS; set CLICKCLACK_CONNECTOR_ALLOW_INSECURE_REMOTE_HTTP=true only for a trusted development network",
    );
  }
  const hermesBaseUrl = normalizeHttpUrl(
    env.HERMES_API_URL?.trim() || DEFAULT_HERMES_URL,
    "HERMES_API_URL",
  );
  const hermesApiKey = env.HERMES_API_KEY?.trim() || undefined;
  const cursorFile = env.HERMES_CONNECTOR_CURSOR_FILE!.trim();
  if (!isAbsolute(cursorFile)) {
    throw new Error("HERMES_CONNECTOR_CURSOR_FILE must be an absolute path");
  }
  const hermesUrl = new URL(hermesBaseUrl);
  const remoteHermes = !isLoopbackHost(hermesUrl.hostname);
  if (!hermesApiKey && remoteHermes) {
    throw new Error("HERMES_API_KEY is required for a non-loopback Hermes API server");
  }
  if (
    remoteHermes &&
    hermesUrl.protocol !== "https:" &&
    env.HERMES_CONNECTOR_ALLOW_INSECURE_REMOTE_HTTP?.trim().toLowerCase() !== "true"
  ) {
    throw new Error(
      "Non-loopback HERMES_API_URL must use HTTPS; set HERMES_CONNECTOR_ALLOW_INSECURE_REMOTE_HTTP=true only for a trusted development network",
    );
  }

  return {
    clickclackBaseUrl,
    clickclackBotToken: env.CLICKCLACK_BOT_TOKEN!.trim(),
    clickclackWorkspaceId: env.CLICKCLACK_WORKSPACE_ID!.trim(),
    hermesBaseUrl,
    ...(hermesApiKey ? { hermesApiKey } : {}),
    historyLimit: parseInteger(env, "HERMES_CONNECTOR_HISTORY_LIMIT", 20, 0, 200),
    maxReplyChars: parseInteger(env, "HERMES_CONNECTOR_MAX_REPLY_CHARS", 100_000, 1, 1_000_000),
    maxConcurrentRuns: parseInteger(env, "HERMES_CONNECTOR_MAX_CONCURRENT_RUNS", 4, 1, 32),
    runTimeoutMs: parseInteger(
      env,
      "HERMES_CONNECTOR_RUN_TIMEOUT_MS",
      1_800_000,
      1_000,
      86_400_000,
    ),
    reconnectMs: parseInteger(env, "HERMES_CONNECTOR_RECONNECT_MS", 2_000, 50, 60_000),
    allowedUserIds: parseIdList(
      env.HERMES_CONNECTOR_ALLOWED_USER_IDS!,
      "HERMES_CONNECTOR_ALLOWED_USER_IDS",
    ),
    allowedChannelIds: parseIdList(
      env.HERMES_CONNECTOR_ALLOWED_CHANNEL_IDS ?? "",
      "HERMES_CONNECTOR_ALLOWED_CHANNEL_IDS",
    ),
    cursorFile,
    ...(env.HERMES_CONNECTOR_INSTRUCTIONS?.trim()
      ? { instructions: env.HERMES_CONNECTOR_INSTRUCTIONS.trim() }
      : {}),
  };
}

function parseIdList(value: string, name: string): ReadonlySet<string> {
  const raw = value.trim();
  if (!raw) return new Set();
  const values = raw.split(",").map((entry) => entry.trim());
  if (values.some((entry) => !entry || entry === "*" || /\s/u.test(entry))) {
    throw new Error(`${name} must be a comma-separated list of explicit IDs`);
  }
  return new Set(values);
}

function normalizeHttpUrl(value: string, name: string): string {
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    throw new Error(`${name} must be a valid HTTP(S) URL`);
  }
  if (url.protocol !== "http:" && url.protocol !== "https:") {
    throw new Error(`${name} must use http: or https:`);
  }
  if (url.username || url.password) {
    throw new Error(`${name} must not contain credentials`);
  }
  if (url.search || url.hash) {
    throw new Error(`${name} must not contain a query string or fragment`);
  }
  return url.toString().replace(/\/$/u, "");
}

function isLoopbackHost(host: string): boolean {
  const normalized = host.replace(/^\[|\]$/gu, "").toLowerCase();
  return normalized === "localhost" || normalized === "127.0.0.1" || normalized === "::1";
}

function parseInteger(
  env: Environment,
  name: string,
  fallback: number,
  minimum: number,
  maximum: number,
): number {
  const raw = env[name]?.trim();
  if (!raw) return fallback;
  if (!/^\d+$/u.test(raw)) {
    throw new Error(`${name} must be an integer between ${minimum} and ${maximum}`);
  }
  const value = Number(raw);
  if (!Number.isSafeInteger(value) || value < minimum || value > maximum) {
    throw new Error(`${name} must be an integer between ${minimum} and ${maximum}`);
  }
  return value;
}
