/**
 * PROJECT LOGOS — Live Telemetry Aggregator
 *
 * Subscribes to chatState and derives live aggregates for the
 * TelemetryRail right-hand indicators and SemanticMargin props.
 *
 * TelemetryRail feeds:  intents (distinct), personas (distinct),
 *   pipeline ("poll" or "ws"), tokens (message count).
 *
 * SemanticMargin feeds:  messageCount, intents[] (most recent N
 *   per-message intents from metadata).
 */

import { derived } from "svelte/store";
import { chatState } from "$lib/clickclack/chat";
import { readMessageMetadata } from "$lib/clickclack/types";

export interface TelemetrySnapshot {
  /** Distinct intent labels seen across all messages. */
  intents: number;
  /** Distinct persona labels seen across all messages. */
  personas: number;
  /** Pipeline mode: "poll" (current) or "ws" when WebSocket is active. */
  pipeline: string;
  /** Total message count in the active channel. */
  tokens: number;
}

export interface MarginSnapshot {
  /** Number of messages in the active channel. */
  messageCount: number;
  /** Per-message intent labels (most recent N at the end). */
  intents: string[];
}

/**
 * Live telemetry values suitable for TelemetryRail props.
 */
export const telemetrySnapshot = derived<typeof chatState, TelemetrySnapshot>(
  chatState,
  ($chat) => {
    const msgs = $chat.messages;

    const intents = new Set<string>();
    const personas = new Set<string>();

    for (const msg of msgs) {
      const m = readMessageMetadata(msg as Record<string, unknown>);
      if (typeof m.intent === "string" && m.intent) intents.add(m.intent);
      if (typeof m.persona === "string" && m.persona) personas.add(m.persona);
    }

    return {
      intents: intents.size,
      personas: personas.size,
      pipeline: $chat.realtime,
      tokens: msgs.length,
    };
  },
);

/**
 * Live margin values suitable for SemanticMargin props.
 * intents[] is the most-recent N per-message intent labels (latest last).
 */
export const marginSnapshot = derived<typeof chatState, MarginSnapshot>(
  chatState,
  ($chat) => {
    const msgs = $chat.messages;
    const intents: string[] = [];

    for (const msg of msgs) {
      const m = readMessageMetadata(msg as Record<string, unknown>);
      if (typeof m.intent === "string" && m.intent) {
        intents.push(m.intent);
      }
    }

    return {
      messageCount: msgs.length,
      intents,
    };
  },
);
