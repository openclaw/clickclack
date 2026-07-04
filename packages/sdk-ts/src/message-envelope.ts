import type { components } from "./generated/openapi.js";

export const coordinationLanes = ["now", "waiting", "watch", "park"] as const;

export type CoordinationLane = (typeof coordinationLanes)[number];

export type MessageEnvelope = components["schemas"]["MessageEnvelope"];

export type MessageEnvelopeInput = Omit<MessageEnvelope, "receipts"> & {
  receipts?: readonly string[];
};

/**
 * Build the canonical coordination envelope used for ClickClack handoffs.
 *
 * The helper keeps the contract explicit and normalizes receipts into a fresh array so
 * downstream code can treat the envelope as durable data rather than a mutable scratchpad.
 */
export function buildMessageEnvelope(input: MessageEnvelopeInput): MessageEnvelope {
  return {
    ...input,
    receipts: [...(input.receipts ?? [])],
  };
}
