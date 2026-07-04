import type { components } from "./generated/openapi.js";
import {
  buildMessageEnvelope,
  type MessageEnvelope,
  type MessageEnvelopeInput,
} from "./message-envelope.js";

export type ClickClackMessage = components["schemas"]["Message"];

export type CoordinationHandoffInput = {
  envelope: MessageEnvelopeInput;
  message: Pick<
    ClickClackMessage,
    | "id"
    | "body"
    | "created_at"
    | "author_id"
    | "workspace_id"
    | "channel_id"
    | "direct_conversation_id"
    | "thread_root_id"
    | "route_id"
    | "kind"
    | "turn_id"
  >;
  summary?: string;
};

export type CoordinationHandoff = MessageEnvelope & {
  source_system: "clickclack";
  source_message_id: string;
  source_message_author_id: string;
  source_message_body: string;
  source_message_kind?: ClickClackMessage["kind"];
  source_message_turn_id?: string;
  source_message_route_id?: string;
  source_workspace_id: string;
  source_channel_id?: string;
  source_direct_conversation_id?: string;
  source_thread_root_id: string;
  source_message_created_at: string;
  summary: string;
};

/**
 * Build a transport-ready coordination handoff from a ClickClack message.
 *
 * This does not send anything. It produces a durable JSON shape that a later
 * transport can post to a mailbox, memory store, or routing service.
 */
export function buildCoordinationHandoff(input: CoordinationHandoffInput): CoordinationHandoff {
  const envelope = buildMessageEnvelope(input.envelope);

  return {
    ...envelope,
    source_system: "clickclack",
    source_message_id: input.message.id,
    source_message_author_id: input.message.author_id,
    source_message_body: input.message.body,
    source_message_kind: input.message.kind,
    source_message_turn_id: input.message.turn_id,
    source_message_route_id: input.message.route_id,
    source_workspace_id: input.message.workspace_id,
    source_channel_id: input.message.channel_id,
    source_direct_conversation_id: input.message.direct_conversation_id,
    source_thread_root_id: input.message.thread_root_id,
    source_message_created_at: input.message.created_at,
    summary: input.summary ?? input.message.body,
  };
}
