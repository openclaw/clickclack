import type { Message, ThreadState } from "../types";

export type MessageUpdate = Pick<Message, "id"> & Partial<Message>;

export function mergeMessageUpdate<T extends MessageUpdate>(message: T, updated: MessageUpdate): T {
  const merged = {
    ...message,
    ...updated,
    // Row reads omit the summary; thread counts have their own monotonic owner.
    thread_state: updated.thread_state
      ? latestThreadState(message.thread_state ?? null, updated.thread_state)
      : message.thread_state,
  };
  // Both stores emit UTC RFC3339Nano. Removing Z preserves fractional precision
  // and orders whole seconds before their fractions; Date would lose nanoseconds.
  if (
    message.deleted_at ||
    (!updated.deleted_at &&
      (message.edited_at?.replace(/Z$/, "") ?? "") > (updated.edited_at?.replace(/Z$/, "") ?? ""))
  ) {
    merged.body = message.body;
    merged.edited_at = message.edited_at;
    merged.deleted_at = message.deleted_at;
    // Deletion is terminal, including the attachments removed with its body.
    if (message.deleted_at) merged.attachments = message.attachments;
  }
  return merged;
}

export function latestThreadState(current: ThreadState | null, incoming: ThreadState): ThreadState {
  // Counts only grow; delayed receipts must not replace a newer summary of the same root.
  return current?.root_message_id === incoming.root_message_id &&
    current.reply_count > incoming.reply_count
    ? current
    : incoming;
}
