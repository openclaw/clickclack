import type { Channel } from "../types";

// Matches the recovery advice of the API's 409 for posts to an archived channel.
export const ARCHIVED_CHANNEL_NOTICE =
  "This channel is archived; choose an active channel or ask a workspace owner to restore it";

export function channelDisplayTitle(channel: Pick<Channel, "name" | "display_title">): string {
  return channel.display_title?.trim() || channel.name;
}

export function safeExternalChannelURL(value?: string): string {
  const trimmed = value?.trim();
  if (!trimmed) return "";
  try {
    const url = new URL(trimmed);
    return url.protocol === "https:" || url.protocol === "http:" ? url.href : "";
  } catch {
    return "";
  }
}
