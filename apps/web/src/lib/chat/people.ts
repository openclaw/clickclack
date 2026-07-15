import type { DirectConversation, Message, User } from "../types";

export function workspaceInitial(name: string): string {
  const trimmed = name.trim();
  if (!trimmed) return "?";
  const parts = trimmed.split(/\s+/);
  if (parts.length >= 2) return (parts[0][0] + parts[1][0]).toUpperCase();
  return trimmed.slice(0, 2).toUpperCase();
}

export function avatarInitial(name?: string | null): string {
  if (!name) return "?";
  const trimmed = name.trim();
  return trimmed ? trimmed[0].toUpperCase() : "?";
}

export function handleLabel(value?: string | null): string {
  return value ? `@${value}` : "";
}

export function userHandle(user?: User | null): string {
  return user?.handle || user?.former_handle || "";
}

export function isDeletedBot(user?: User | null): boolean {
  return user?.kind === "bot" && !!user.deleted_at;
}

export function userDisplayLabel(user?: User | null, fallback = "Local User"): string {
  const name = user?.display_name || fallback;
  return isDeletedBot(user) ? `${name} (deleted bot)` : name;
}

export function avatarHue(seed: string): number {
  let hash = 0;
  for (let i = 0; i < seed.length; i++) hash = (hash * 31 + seed.charCodeAt(i)) >>> 0;
  return hash % 360;
}

export function dmAvatarUser(conversation: DirectConversation, currentUserID?: string): User {
  return (
    conversation.members.find((member) => member.id !== currentUserID) || conversation.members[0]
  );
}

export function dmTitle(conversation: DirectConversation, currentUserID?: string): string {
  const others = conversation.members.filter((member) => member.id !== currentUserID);
  const list = others.length > 0 ? others : conversation.members;
  return list.map((member) => userDisplayLabel(member)).join(", ");
}

export function collectRecentPeople(
  messageList: Message[],
  conversations: DirectConversation[],
  currentUserID: string,
): User[] {
  const people = new Map<string, User>();
  for (const conversation of conversations) {
    for (const member of conversation.members) {
      if (member.id && member.id !== currentUserID && !member.deleted_at) {
        people.set(member.id, member);
      }
    }
  }
  for (const message of [...messageList].reverse()) {
    const author = message.author;
    if (author?.id && author.id !== currentUserID && !author.deleted_at) {
      people.set(author.id, author);
    }
  }
  return [...people.values()].slice(0, 12);
}

export function directConversationForUser(
  conversations: DirectConversation[],
  memberID: string,
): DirectConversation | undefined {
  return conversations.find((conversation) =>
    conversation.members.some((member) => member.id === memberID),
  );
}
