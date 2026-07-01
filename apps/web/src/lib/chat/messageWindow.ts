import type { Message } from "../types";

export type MessageWindowDirection = "replace" | "prepend" | "append" | "around";

export const INITIAL_MESSAGE_LIMIT = 100;
export const PAGE_MESSAGE_LIMIT = 50;
export const MAX_MESSAGE_WINDOW = 900;
export const RETAIN_MESSAGE_WINDOW = 700;
export const MAX_PROTECTED_MESSAGE_WINDOW = MAX_MESSAGE_WINDOW + PAGE_MESSAGE_LIMIT * 2;
export const MAX_RETAINED_MESSAGE_WINDOWS = 8;
export const MAX_RETAINED_SCROLL_STATES = 16;

type ProtectedBounds = { first: number; last: number };

export function trimMessageWindow(
  list: Message[],
  direction: MessageWindowDirection,
  protectedIDs: Set<string>,
): Message[] {
  if (list.length <= MAX_MESSAGE_WINDOW) return list;
  const protectedBounds = protectedMessageBounds(list, protectedIDs);
  if (direction === "prepend") {
    return sliceMessageWindow(list, 0, RETAIN_MESSAGE_WINDOW, protectedBounds, "start");
  }
  if (direction === "append") {
    return sliceMessageWindow(
      list,
      Math.max(0, list.length - RETAIN_MESSAGE_WINDOW),
      list.length,
      protectedBounds,
      "end",
    );
  }
  if (direction === "around") {
    const protectedIndex = protectedBounds?.first ?? -1;
    const center = protectedIndex >= 0 ? protectedIndex : Math.floor(list.length / 2);
    let start = Math.max(0, center - Math.floor(RETAIN_MESSAGE_WINDOW / 2));
    let end = Math.min(list.length, start + RETAIN_MESSAGE_WINDOW);
    start = Math.max(0, end - RETAIN_MESSAGE_WINDOW);
    return sliceMessageWindow(list, start, end, protectedBounds, "start");
  }
  return sliceMessageWindow(
    list,
    Math.max(0, list.length - RETAIN_MESSAGE_WINDOW),
    list.length,
    protectedBounds,
    "end",
  );
}

function protectedMessageBounds(
  list: Message[],
  protectedIDs: Set<string>,
): ProtectedBounds | null {
  if (protectedIDs.size === 0) return null;
  let first = -1;
  let last = -1;
  for (let i = 0; i < list.length; i++) {
    if (!protectedIDs.has(list[i].id)) continue;
    if (first < 0) first = i;
    last = i;
  }
  return first < 0 ? null : { first, last };
}

function sliceMessageWindow(
  list: Message[],
  desiredStart: number,
  desiredEnd: number,
  protectedBounds: ProtectedBounds | null,
  bias: "start" | "end",
): Message[] {
  let start = desiredStart;
  let end = desiredEnd;
  if (protectedBounds) {
    start = Math.min(start, protectedBounds.first);
    end = Math.max(end, protectedBounds.last + 1);
  }
  if (end - start > MAX_PROTECTED_MESSAGE_WINDOW) {
    if (bias === "start") {
      start = desiredStart;
      end = Math.min(list.length, start + MAX_PROTECTED_MESSAGE_WINDOW);
    } else {
      end = desiredEnd;
      start = Math.max(0, end - MAX_PROTECTED_MESSAGE_WINDOW);
    }
  }
  return list.slice(start, end);
}
