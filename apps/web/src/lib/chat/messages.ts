import type { Message } from "../types";

export type MessageGroup = {
  key: string;
  dayLabel: string | null;
  messages: Message[];
  authorName: string;
  authorHandle: string;
  authorAvatarURL: string;
  authorID: string;
  timestamp: string;
};

export function quoteSnippet(text: string | undefined, max = 120): string {
  if (!text) return "";
  if (max <= 0) return "";
  const collapsed = text.replace(/\s+/g, " ").trim();
  if (collapsed.length <= max) return collapsed;
  if (max <= 3) return ".".repeat(max);
  return `${collapsed.slice(0, max - 3)}...`;
}

export function quotedAuthorName(message: Message): string {
  return message.quoted_author?.display_name || "Unknown";
}

export function threadSummary(message: Message, selectedThreadID?: string): string {
  if (selectedThreadID === message.id) return "Open";
  const count = message.thread_state?.reply_count || 0;
  if (count === 0) return "No replies yet";
  return `${count} ${count === 1 ? "reply" : "replies"}`;
}

export function threadActivityLabel(message: Message): string {
  const count = message.thread_state?.reply_count || 0;
  if (count === 0) return "Thread";
  return `${count} ${count === 1 ? "reply" : "replies"}`;
}

export function threadActivityTime(message: Message): string {
  const value = message.thread_state?.last_reply_at;
  if (!value) return "";
  return timeAgo(value);
}

function timeAgo(value: string): string {
  const timestamp = new Date(value).getTime();
  if (!Number.isFinite(timestamp)) return "";
  const delta = Math.max(0, Date.now() - timestamp);
  const minutes = Math.floor(delta / 60000);
  if (minutes < 1) return "now";
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h`;
  return `${Math.floor(hours / 24)}d`;
}

export function dayLabel(value: string): string {
  const date = new Date(value);
  const today = new Date();
  const yesterday = new Date();
  yesterday.setDate(today.getDate() - 1);
  const sameDay = (a: Date, b: Date) =>
    a.getFullYear() === b.getFullYear() &&
    a.getMonth() === b.getMonth() &&
    a.getDate() === b.getDate();
  if (sameDay(date, today)) return "Today";
  if (sameDay(date, yesterday)) return "Yesterday";
  return new Intl.DateTimeFormat(undefined, {
    weekday: "long",
    month: "long",
    day: "numeric",
  }).format(date);
}

export function groupMessages(list: Message[]): MessageGroup[] {
  const groups: MessageGroup[] = [];
  let lastDay = "";
  let lastAuthor = "";
  let lastTime = 0;
  for (const message of list) {
    const created = new Date(message.created_at);
    const dayKey = created.toDateString();
    const authorID = message.author?.id || message.author_id || "local";
    const dayChanged = dayKey !== lastDay;
    const newAuthor = authorID !== lastAuthor;
    const tooFarApart = created.getTime() - lastTime > 5 * 60 * 1000;
    if (dayChanged || newAuthor || tooFarApart || groups.length === 0) {
      groups.push({
        key: message.id,
        dayLabel: dayChanged ? dayLabel(message.created_at) : null,
        messages: [message],
        authorName: message.author?.display_name || "Local User",
        authorHandle: message.author?.handle || "",
        authorAvatarURL: message.author?.avatar_url || "",
        authorID,
        timestamp: message.created_at,
      });
    } else {
      groups[groups.length - 1].messages.push(message);
    }
    lastDay = dayKey;
    lastAuthor = authorID;
    lastTime = created.getTime();
  }
  return groups;
}
