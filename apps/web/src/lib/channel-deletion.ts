import type { Channel, ChannelDeletionPreview } from "./types";

// Deletion is confirmed by typing the channel's routing name, not its display title.
export function channelDeletionConfirmed(typed: string, channel: Pick<Channel, "name">): boolean {
  return typed.trim() === channel.name;
}

export function channelDeletionBlockerMessage(
  preview: Pick<ChannelDeletionPreview, "blocker">,
): string {
  switch (preview.blocker) {
    case "last_channel":
      return "A workspace must keep at least one channel. Create another channel first, or delete the workspace instead.";
    case "provisioned_channel":
      return "Guest sign-in recreates this channel, so it can't be deleted.";
    default:
      return "";
  }
}

export function channelDeletedNotice(
  channelTitle: string,
  deletedBy: string,
  byCurrentUser = false,
): string {
  if (byCurrentUser) return `Deleted #${channelTitle}.`;
  return deletedBy
    ? `#${channelTitle} was deleted by ${deletedBy}.`
    : `#${channelTitle} was deleted.`;
}
