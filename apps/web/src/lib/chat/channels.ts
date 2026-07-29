import type { Channel } from "../types";

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
