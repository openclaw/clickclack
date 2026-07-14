import { ClickClackClient } from "@clickclack/sdk-ts";

import { loadConfig } from "./config.ts";
import { HermesClickClackConnector, type ConnectorLogger } from "./connector.ts";
import { runEventGateway } from "./gateway.ts";
import { HermesClient } from "./hermes-client.ts";

const logger: ConnectorLogger = {
  info: (...values) => console.log("[hermes-clickclack]", ...values),
  warn: (...values) => console.warn("[hermes-clickclack]", ...values),
  error: (...values) => console.error("[hermes-clickclack]", ...values),
};

async function main(): Promise<void> {
  const config = loadConfig(process.env);
  const abort = new AbortController();
  const shutdown = (signal: string) => {
    logger.info(`${signal} received; stopping`);
    abort.abort();
  };
  process.once("SIGINT", shutdown);
  process.once("SIGTERM", shutdown);

  const clickclack = new ClickClackClient({
    baseUrl: config.clickclackBaseUrl,
    token: config.clickclackBotToken,
  });
  const hermes = new HermesClient({
    baseUrl: config.hermesBaseUrl,
    apiKey: config.hermesApiKey,
  });

  const [bot, capabilities] = await Promise.all([
    clickclack.me(),
    hermes.assertCompatible(abort.signal),
  ]);
  if (bot.kind !== "bot") {
    throw new Error("CLICKCLACK_BOT_TOKEN must authenticate a ClickClack bot user");
  }
  logger.info(`connected as @${bot.handle}; Hermes model=${capabilities.model ?? "default"}`);

  const connector = new HermesClickClackConnector({
    clickclack,
    hermes,
    workspaceId: config.clickclackWorkspaceId,
    bot,
    historyLimit: config.historyLimit,
    maxReplyChars: config.maxReplyChars,
    maxConcurrentRuns: config.maxConcurrentRuns,
    runTimeoutMs: config.runTimeoutMs,
    instructions: config.instructions,
    signal: abort.signal,
    logger,
  });

  try {
    await runEventGateway({
      client: clickclack,
      workspaceId: config.clickclackWorkspaceId,
      signal: abort.signal,
      reconnectMs: config.reconnectMs,
      onEvent: (event) => connector.scheduleEvent(event),
      logger,
    });
  } finally {
    await connector.waitForIdle();
    process.removeListener("SIGINT", shutdown);
    process.removeListener("SIGTERM", shutdown);
  }
}

main().catch((error: unknown) => {
  logger.error("fatal connector error", error);
  process.exitCode = 1;
});
