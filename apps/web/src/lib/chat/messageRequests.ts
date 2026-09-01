import type { Message, ThreadPage, User } from "../types";
import { MAX_PROTECTED_MESSAGE_WINDOW } from "./messageWindow";
import { latestThreadState, mergeMessageUpdate, type MessageUpdate } from "./messageUpdates";

export type AuthorUpdate = Pick<User, "id"> & Partial<User>;
type MessageData = { messages: Message[] } | ThreadPage | { message: Message };
type RequestMutations = {
  messages: Map<string, MessageUpdate>;
  authors: Map<string, AuthorUpdate>;
  current: () => boolean;
};

export class MessageRequests {
  private reads = new Set<RequestMutations>();

  constructor(private readonly scope: () => string) {}

  get pending(): boolean {
    return this.reads.size > 0;
  }

  clear() {
    this.reads.clear();
  }

  prune() {
    // Retire authority too: returning to the same conversation cannot revive an old read.
    for (const read of this.reads) if (!read.current()) this.reads.delete(read);
  }

  updateMessage(updated: MessageUpdate): (message: Message) => Message {
    for (const read of this.reads) {
      // One overflow sentinel bounds a snapshot held across a continuous event burst.
      if (read.messages.size + read.authors.size <= MAX_PROTECTED_MESSAGE_WINDOW) {
        const previous = read.messages.get(updated.id);
        read.messages.set(updated.id, previous ? mergeMessageUpdate(previous, updated) : updated);
      }
    }
    return (message) =>
      message.id === updated.id ? mergeMessageUpdate(message, updated) : message;
  }

  updateAuthor(updated: AuthorUpdate): (message: Message) => Message {
    for (const read of this.reads) {
      if (read.messages.size + read.authors.size <= MAX_PROTECTED_MESSAGE_WINDOW) {
        read.authors.set(updated.id, { ...read.authors.get(updated.id), ...updated });
      }
    }
    const apply = (author?: User) =>
      author?.id === updated.id ? { ...author, ...updated } : author;
    return (message) => ({
      ...message,
      author: apply(message.author),
      quoted_author: apply(message.quoted_author),
    });
  }

  async run<T extends MessageData>(
    request: () => Promise<T>,
    commit: (data: T) => void,
    shouldCommit: () => boolean = () => true,
  ): Promise<T | undefined> {
    const scope = this.scope();
    const mutations: RequestMutations = {
      messages: new Map(),
      authors: new Map(),
      current: () => this.reads.has(mutations) && this.scope() === scope && shouldCommit(),
    };
    this.reads.add(mutations);
    try {
      const data = await request();
      if (!mutations.current()) return;
      if (mutations.messages.size + mutations.authors.size > MAX_PROTECTED_MESSAGE_WINDOW) {
        throw new Error("Too many messages changed while loading. Please try again.");
      }
      const replay = (message: Message): Message => {
        const updated = mutations.messages.get(message.id);
        if (updated) message = mergeMessageUpdate(message, updated);
        return {
          ...message,
          author: message.author && {
            ...message.author,
            ...mutations.authors.get(message.author.id),
          },
          quoted_author: message.quoted_author && {
            ...message.quoted_author,
            ...mutations.authors.get(message.quoted_author.id),
          },
        };
      };
      if ("messages" in data) data.messages = data.messages.map(replay);
      else if ("message" in data) data.message = replay(data.message);
      else {
        data.root = replay(data.root);
        data.replies = data.replies.map(replay);
        data.thread_state = latestThreadState(data.root.thread_state ?? null, data.thread_state);
      }
      // The request includes companion metadata; replay and admission never yield.
      commit(data);
      return data;
    } catch (error) {
      if (mutations.current()) throw error;
    } finally {
      this.reads.delete(mutations);
    }
  }
}
