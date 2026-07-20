import { api } from "./api";
import { MAX_PROTECTED_MESSAGE_WINDOW, PAGE_MESSAGE_LIMIT } from "./chat/messageWindow";
import type { Message, ReactionSummary, RealtimeEvent } from "./types";

type ReactionIntent = "add" | "remove";

type ReactionEntry = {
  confirmed: ReactionSummary[];
  displayed: ReactionSummary[];
  complete: boolean;
  partialEmojis: Set<string>;
  revision: number;
  lastEventCursor: string;
  pendingIntent?: { emoji: string; intent: ReactionIntent };
  error: string;
};

type ReactionMutationResponse = {
  event?: Pick<RealtimeEvent, "cursor">;
  reactions: ReactionSummary[];
};

const MAX_REACTION_ENTRIES = MAX_PROTECTED_MESSAGE_WINDOW + PAGE_MESSAGE_LIMIT;

export class ReactionController {
  private entries = $state<Map<string, ReactionEntry>>(new Map());
  private revision = 0;
  private generation = 0;

  constructor(private readonly currentUserID: () => string) {}

  clear() {
    this.generation += 1;
    this.revision = 0;
    this.entries = new Map();
  }

  seedMessages(messages: Message[]) {
    if (messages.length === 0) return;
    const next = new Map(this.entries);
    let changed = false;
    for (const message of messages) {
      const seeded = normalizeReactions(message.reactions ?? []);
      const existing = next.get(message.id);
      if (!existing) {
        next.set(message.id, this.newEntry(seeded, true));
        changed = true;
        continue;
      }
      if (existing.complete) continue;
      const confirmed = mergePartialReactions(seeded, existing.confirmed, existing.partialEmojis);
      next.set(message.id, {
        ...existing,
        confirmed,
        displayed: applyIntent(confirmed, existing.pendingIntent),
        complete: true,
        partialEmojis: new Set(),
        revision: ++this.revision,
      });
      changed = true;
    }
    if (changed) this.entries = trimEntries(next);
  }

  reactionsFor(message: Message): ReactionSummary[] {
    return this.entries.get(message.id)?.displayed ?? message.reactions ?? [];
  }

  pending(messageID: string): boolean {
    return Boolean(this.entries.get(messageID)?.pendingIntent);
  }

  error(messageID: string): string {
    return this.entries.get(messageID)?.error ?? "";
  }

  applyEvent(event: RealtimeEvent) {
    if (event.type !== "reaction.added" && event.type !== "reaction.removed") return;
    const messageID = event.payload.message_id;
    const emoji = event.payload.emoji;
    const count = event.payload.count;
    if (!messageID || !emoji || typeof count !== "number") return;

    const existing = this.entries.get(messageID);
    if (existing?.lastEventCursor && event.cursor && event.cursor <= existing.lastEventCursor) {
      return;
    }
    const currentUserActed = event.payload.user_id === this.currentUserID();
    const confirmed = applyReactionEvent(
      existing?.confirmed ?? [],
      emoji,
      Math.max(0, count),
      event.type === "reaction.added",
      currentUserActed,
    );
    const pendingIntent = existing?.pendingIntent;
    const eventResolvesPendingIntent = currentUserActed && pendingIntent?.emoji === emoji;
    this.setEntry(messageID, {
      confirmed,
      displayed: eventResolvesPendingIntent ? confirmed : applyIntent(confirmed, pendingIntent),
      complete: existing?.complete ?? false,
      partialEmojis: existing?.complete
        ? new Set()
        : new Set([...(existing?.partialEmojis ?? []), emoji]),
      revision: ++this.revision,
      lastEventCursor: event.cursor || existing?.lastEventCursor || "",
      pendingIntent,
      error: "",
    });
  }

  async toggle(message: Message, emoji: string) {
    if (!this.currentUserID() || !emoji) return;
    const entry = this.ensureEntry(message);
    if (entry.pendingIntent) return;

    const intent: ReactionIntent = reactedByMe(entry.displayed, emoji) ? "remove" : "add";
    const generation = this.generation;
    const revisionAtStart = entry.revision;
    const pendingIntent = { emoji, intent };
    this.setEntry(message.id, {
      ...entry,
      displayed: applyIntent(entry.confirmed, pendingIntent),
      pendingIntent,
      error: "",
    });

    try {
      const result =
        intent === "remove"
          ? await api<ReactionMutationResponse>(
              `/api/messages/${message.id}/reactions/${encodeURIComponent(emoji)}`,
              { method: "DELETE" },
            )
          : await api<ReactionMutationResponse>(`/api/messages/${message.id}/reactions`, {
              method: "POST",
              body: JSON.stringify({ emoji }),
            });
      if (generation !== this.generation) return;
      const current = this.entries.get(message.id);
      if (!current) return;
      const responseCursor = result.event?.cursor ?? "";
      const responseIsCurrent =
        current.revision === revisionAtStart ||
        Boolean(
          responseCursor && (!current.lastEventCursor || responseCursor >= current.lastEventCursor),
        );
      if (responseIsCurrent) {
        const confirmed = normalizeReactions(result.reactions);
        this.setEntry(message.id, {
          ...current,
          confirmed,
          displayed: applyIntent(confirmed, current.pendingIntent),
          complete: true,
          partialEmojis: new Set(),
          revision: ++this.revision,
        });
      }
    } catch (error) {
      if (generation !== this.generation) return;
      const recoveryRevision = this.entries.get(message.id)?.revision;
      try {
        const data = await api<{ message: Message }>(`/api/messages/${message.id}`);
        if (
          generation === this.generation &&
          this.entries.get(message.id)?.revision === recoveryRevision
        ) {
          const confirmed = normalizeReactions(data.message.reactions ?? []);
          const current = this.entries.get(message.id);
          if (current) {
            this.setEntry(message.id, {
              ...current,
              confirmed,
              displayed: applyIntent(confirmed, current.pendingIntent),
              complete: true,
              partialEmojis: new Set(),
              revision: ++this.revision,
            });
          }
        }
      } catch {
        // A newer realtime event remains safer than replacing it after ambiguous failure.
      }
      const current = this.entries.get(message.id);
      if (generation === this.generation && current) {
        this.setEntry(message.id, {
          ...current,
          error: intentSatisfied(current.confirmed, pendingIntent) ? "" : readableError(error),
        });
      }
    } finally {
      if (generation === this.generation) {
        const current = this.entries.get(message.id);
        if (current?.pendingIntent === pendingIntent) {
          this.setEntry(message.id, {
            ...current,
            displayed: current.confirmed,
            pendingIntent: undefined,
          });
        }
      }
    }
  }

  private ensureEntry(message: Message): ReactionEntry {
    const existing = this.entries.get(message.id);
    if (existing) return existing;
    const entry = this.newEntry(normalizeReactions(message.reactions ?? []), true);
    this.setEntry(message.id, entry);
    return entry;
  }

  private newEntry(confirmed: ReactionSummary[], complete: boolean): ReactionEntry {
    return {
      confirmed,
      displayed: confirmed,
      complete,
      partialEmojis: new Set(),
      revision: ++this.revision,
      lastEventCursor: "",
      error: "",
    };
  }

  private setEntry(messageID: string, entry: ReactionEntry) {
    const next = new Map(this.entries);
    next.delete(messageID);
    next.set(messageID, entry);
    this.entries = trimEntries(next);
  }
}

function normalizeReactions(reactions: ReactionSummary[]): ReactionSummary[] {
  const byEmoji = new Map<string, ReactionSummary>();
  for (const reaction of reactions) {
    if (!reaction.emoji || reaction.count <= 0) continue;
    byEmoji.set(reaction.emoji, {
      emoji: reaction.emoji,
      count: reaction.count,
      reacted_by_me: reaction.reacted_by_me,
    });
  }
  return sortReactions([...byEmoji.values()]);
}

function mergePartialReactions(
  complete: ReactionSummary[],
  partial: ReactionSummary[],
  partialEmojis: Set<string>,
): ReactionSummary[] {
  const byEmoji = new Map(complete.map((reaction) => [reaction.emoji, reaction]));
  const partialByEmoji = new Map(partial.map((reaction) => [reaction.emoji, reaction]));
  for (const emoji of partialEmojis) {
    const reaction = partialByEmoji.get(emoji);
    if (reaction) byEmoji.set(emoji, reaction);
    else byEmoji.delete(emoji);
  }
  return sortReactions([...byEmoji.values()]);
}

function applyIntent(
  confirmed: ReactionSummary[],
  pending?: { emoji: string; intent: ReactionIntent },
): ReactionSummary[] {
  if (!pending) return confirmed;
  const next = confirmed.map((reaction) => ({ ...reaction }));
  const index = next.findIndex((reaction) => reaction.emoji === pending.emoji);
  if (pending.intent === "remove") {
    if (index < 0 || !next[index].reacted_by_me) return next;
    if (next[index].count <= 1) next.splice(index, 1);
    else {
      next[index] = {
        ...next[index],
        count: next[index].count - 1,
        reacted_by_me: false,
      };
    }
  } else if (index < 0) {
    next.push({ emoji: pending.emoji, count: 1, reacted_by_me: true });
  } else if (!next[index].reacted_by_me) {
    next[index] = {
      ...next[index],
      count: next[index].count + 1,
      reacted_by_me: true,
    };
  }
  return sortReactions(next);
}

function applyReactionEvent(
  confirmed: ReactionSummary[],
  emoji: string,
  count: number,
  added: boolean,
  currentUserActed: boolean,
): ReactionSummary[] {
  const next = confirmed.map((reaction) => ({ ...reaction }));
  const index = next.findIndex((reaction) => reaction.emoji === emoji);
  if (count === 0) {
    if (index >= 0) next.splice(index, 1);
    return sortReactions(next);
  }
  const reactedByMe = currentUserActed ? added : index >= 0 ? next[index].reacted_by_me : false;
  const summary = { emoji, count, reacted_by_me: reactedByMe };
  if (index >= 0) next[index] = summary;
  else next.push(summary);
  return sortReactions(next);
}

function reactedByMe(reactions: ReactionSummary[], emoji: string): boolean {
  return reactions.some((reaction) => reaction.emoji === emoji && reaction.reacted_by_me);
}

function intentSatisfied(
  reactions: ReactionSummary[],
  pending: { emoji: string; intent: ReactionIntent },
): boolean {
  return reactedByMe(reactions, pending.emoji) === (pending.intent === "add");
}

function sortReactions(reactions: ReactionSummary[]): ReactionSummary[] {
  return reactions.sort((a, b) => b.count - a.count || a.emoji.localeCompare(b.emoji));
}

function trimEntries(entries: Map<string, ReactionEntry>): Map<string, ReactionEntry> {
  let remaining = entries.size;
  while (entries.size > MAX_REACTION_ENTRIES && remaining > 0) {
    remaining -= 1;
    const oldestMessageID = entries.keys().next().value;
    if (!oldestMessageID) break;
    const entry = entries.get(oldestMessageID);
    entries.delete(oldestMessageID);
    if (entry?.pendingIntent) entries.set(oldestMessageID, entry);
  }
  return entries;
}

function readableError(error: unknown): string {
  if (!(error instanceof Error)) return "Could not update reaction";
  try {
    const body = JSON.parse(error.message) as { error?: string };
    return body.error || "Could not update reaction";
  } catch {
    return error.message || "Could not update reaction";
  }
}
