import { api, readableAPIError } from "./api";
import { newNonce } from "./chat/messages";
import { latestThreadState, mergeMessageUpdate, type MessageUpdate } from "./chat/messageUpdates";
import type { Message, RealtimeEvent, ThreadPage, ThreadState, User } from "./types";

type ThreadSelection = { messageID: string; context: string };
type Submission = { body: string; quotedMessageID?: string; nonce: string };
type ReplyDraft = {
  body: string;
  quote: Message | null;
  sending: boolean;
  error: string;
  submission?: Submission;
};
type Edge = "older" | "newer";
export type ThreadTarget = { messageID: string; threadSeq?: number };
export type ThreadScrollIntent = "preserve" | "latest" | ThreadTarget;
const THREAD_WINDOW_LIMIT = 300;
const RETAIN_REPLIES = 200;
const INITIAL_REPLIES = 100;
const PAGE_REPLIES = 50;
const seq = (message: Message) => message.thread_seq ?? 0;

export class ThreadController {
  root = $state<Message | null>(null);
  replies = $state<Message[]>([]);
  state = $state<ThreadState | null>(null);
  error = $state("");
  selection = $state.raw<ThreadSelection | null>(null);
  hasOlder = $state(false);
  hasNewer = $state(false);
  loading = $state({ older: false, newer: false });
  edgeError = $state({ older: "", newer: "" });
  following = $state(true);
  anchor: { messageID: string; offset: number } | null = null;
  editingID = "";
  beforeChange?: (intent: ThreadScrollIntent) => void;
  private drafts = $state<Map<string, ReplyDraft>>(new Map());
  private window = 0;
  private revision = 0;
  private rowRevisions = new Map<string, number>();
  private pageMutations = new Set<Map<string, MessageUpdate>>();
  private edgeOwners: Partial<Record<Edge, object>> = {};
  private knownTail = 0;
  private initialized = false;
  private coveredStateEvent = false;

  constructor(
    private readonly context: () => string,
    private readonly committed: (freshMessages?: Message[]) => void,
  ) {}

  get draft(): ReplyDraft | undefined {
    return this.selection ? this.drafts.get(this.selection.messageID) : undefined;
  }
  select(messageID: string, optimisticRoot?: Message): ThreadSelection {
    if (this.selection?.messageID === messageID && this.isCurrent(this.selection))
      return this.selection;
    this.close();
    this.selection = { messageID, context: this.context() };
    this.root = optimisticRoot ?? null;
    this.state = optimisticRoot?.thread_state ?? null;
    return this.selection;
  }
  isCurrent(selection: ThreadSelection | null): selection is ThreadSelection {
    return (
      selection !== null && this.selection === selection && selection.context === this.context()
    );
  }
  close() {
    this.selection = null;
    this.window++;
    this.rowRevisions.clear();
    this.pageMutations.clear();
    this.edgeOwners = {};
    this.knownTail = 0;
    this.root = null;
    this.replies = [];
    this.state = null;
    this.error = "";
    this.hasOlder = this.hasNewer = false;
    this.loading = { older: false, newer: false };
    this.edgeError = { older: "", newer: "" };
    this.initialized = this.coveredStateEvent = false;
    this.following = true;
    this.anchor = null;
    this.editingID = "";
  }
  private owner(shouldCommit: () => boolean = () => true) {
    const selection = this.selection,
      window = this.window;
    return {
      selection,
      current: () => this.isCurrent(selection) && this.window === window && shouldCommit(),
    };
  }
  private async request(selection: ThreadSelection, options: Record<string, number | boolean>) {
    const query = new URLSearchParams(
      Object.entries(options).map(([key, value]) => [key, String(value)]),
    );
    // Receipts may arrive after a row was paged out but before this snapshot commits.
    const mutations = new Map<string, MessageUpdate>();
    this.pageMutations.add(mutations);
    try {
      const page = await api<ThreadPage>(`/api/messages/${selection.messageID}/thread?${query}`);
      if (mutations.size > THREAD_WINDOW_LIMIT) {
        throw new Error("Too many replies changed while loading. Please try again.");
      }
      const replay = (message: Message) => {
        const updated = mutations.get(message.id);
        return updated ? mergeMessageUpdate(message, updated) : message;
      };
      page.root = replay(page.root);
      page.replies = page.replies.map(replay);
      return page;
    } finally {
      this.pageMutations.delete(mutations);
    }
  }
  private mergeSnapshot(
    incoming: Message,
    current: Message | null | undefined,
    revision: number,
  ): Message {
    if (!current) return incoming;
    // Local revisions protect metadata changed during the read; edit timestamps
    // still decide the body when a late acknowledgement belongs to an older edit.
    return (this.rowRevisions.get(incoming.id) ?? 0) > revision
      ? mergeMessageUpdate(incoming, current)
      : mergeMessageUpdate(current, incoming);
  }
  private metadata(page: ThreadPage, revision: number) {
    this.root = this.mergeSnapshot(page.root, this.root, revision);
    this.rowRevisions.set(page.root.id, ++this.revision);
    this.knownTail = Math.max(this.knownTail, page.newest_seq);
    this.reconcileState(page.thread_state);
    this.error = "";
    // Retained replies may still await their refresh page; only this snapshot is fresh.
    this.committed([page.root, ...page.replies]);
  }
  private reconcileState(incoming: ThreadState) {
    this.state = latestThreadState(this.state, incoming);
    if (this.root) this.root = { ...this.root, thread_state: this.state };
  }
  private merge(
    incoming: Message[],
    revision: number,
    edge: Edge,
    intent: ThreadScrollIntent = "preserve",
  ) {
    this.beforeChange?.(intent);
    const rows = new Map(this.replies.map((reply) => [reply.id, reply]));
    for (const reply of incoming) {
      rows.set(reply.id, this.mergeSnapshot(reply, rows.get(reply.id), revision));
      this.rowRevisions.set(reply.id, ++this.revision);
    }
    let replies = [...rows.values()].sort((a, b) => seq(a) - seq(b));
    if (replies.length > THREAD_WINDOW_LIMIT) {
      let start = edge === "older" ? 0 : replies.length - RETAIN_REPLIES;
      let end = Math.min(replies.length, start + RETAIN_REPLIES);
      const protectedIndices = [this.following ? "" : this.anchor?.messageID, this.editingID]
        .map((id) => replies.findIndex((reply) => reply.id === id))
        .filter((index) => index >= 0);
      if (protectedIndices.length) {
        start = Math.min(start, ...protectedIndices);
        end = Math.max(end, ...protectedIndices.map((index) => index + 1));
        if (end - start > THREAD_WINDOW_LIMIT) {
          if (edge === "newer") end = start + THREAD_WINDOW_LIMIT;
          else start = end - THREAD_WINDOW_LIMIT;
        }
      }
      if (start > 0) this.hasOlder = true;
      if (end < replies.length) this.hasNewer = true;
      replies = replies.slice(start, end);
    }
    this.replies = replies;
    this.pruneRevisions();
    this.committed();
  }
  private pruneRevisions() {
    const retained = new Set([this.root?.id, ...this.replies.map((reply) => reply.id)]);
    for (const id of this.rowRevisions.keys()) if (!retained.has(id)) this.rowRevisions.delete(id);
  }
  private selectWindow() {
    this.window++;
    this.pageMutations.clear();
    this.edgeOwners = {};
    this.loading = { older: false, newer: false };
    this.edgeError = { older: "", newer: "" };
  }
  async open(shouldCommit: () => boolean = () => true): Promise<boolean> {
    if (this.initialized && this.isCurrent(this.selection)) return shouldCommit();
    return this.latest(shouldCommit);
  }
  async latest(shouldCommit: () => boolean = () => true): Promise<boolean> {
    this.selectWindow();
    return this.replace({ latest: true, limit: INITIAL_REPLIES }, "latest", shouldCommit);
  }
  private async replace(
    options: Record<string, number | boolean>,
    intent: ThreadScrollIntent,
    shouldCommit: () => boolean,
  ) {
    const { selection, current } = this.owner(shouldCommit),
      revision = this.revision;
    if (!selection || !current()) return false;
    try {
      const page = await this.request(selection, options);
      if (!current()) return false;
      this.beforeChange?.(intent);
      const existing = new Map(this.replies.map((reply) => [reply.id, reply]));
      let replies = page.replies.map((reply) =>
        this.mergeSnapshot(reply, existing.get(reply.id), revision),
      );
      // A delayed latest snapshot must retain a newer contiguous interval already observed.
      if (intent === "latest" && page.replies.some((reply) => existing.has(reply.id))) {
        replies = [...replies, ...this.replies.filter((reply) => seq(reply) > page.newest_seq)];
      }
      this.replies = replies.slice(-THREAD_WINDOW_LIMIT);
      for (const reply of this.replies) this.rowRevisions.set(reply.id, ++this.revision);
      this.pruneRevisions();
      this.hasOlder = page.has_older || replies.length > this.replies.length;
      this.hasNewer =
        page.has_newer || seq(this.replies.at(-1) ?? ({} as Message)) < this.knownTail;
      this.metadata(page, revision);
      this.initialized = true;
      if (intent === "latest") this.following = true;
      return true;
    } catch (error) {
      if (!current()) return false;
      this.error = readableAPIError(error, "Could not load the thread.");
      throw error;
    }
  }
  async target(target: ThreadTarget, shouldCommit: () => boolean = () => true): Promise<boolean> {
    this.selectWindow();
    const owner = this.owner(shouldCommit);
    if (!owner.selection || !owner.current()) return false;
    try {
      const loaded =
        this.root?.id === target.messageID
          ? this.root
          : this.replies.find((reply) => reply.id === target.messageID);
      if (loaded) {
        if (loaded.deleted_at) throw new Error("This message was deleted.");
        this.following = false;
        this.beforeChange?.(target);
        return true;
      }
      let threadSeq = target.threadSeq;
      if (!threadSeq) {
        const message = (await api<{ message: Message }>(`/api/messages/${target.messageID}`))
          .message;
        if (!owner.current()) return false;
        if (
          message.thread_root_id !== owner.selection.messageID ||
          !message.thread_seq ||
          message.deleted_at
        )
          throw new Error("This reply is no longer available in this thread.");
        threadSeq = message.thread_seq;
      }
      if (!owner.current()) return false;
      if (
        !(await this.replace(
          { around_seq: threadSeq, limit: INITIAL_REPLIES },
          target,
          owner.current,
        ))
      )
        return false;
      if (!this.replies.some((reply) => reply.id === target.messageID && !reply.deleted_at))
        throw new Error("This reply is no longer available.");
      this.following = false;
      return true;
    } catch (error) {
      if (!owner.current()) return false;
      this.error = readableAPIError(error, "Could not open this reply.");
      return false;
    }
  }
  async loadEdge(
    edge: Edge,
    shouldCommit: () => boolean = () => true,
    limit = PAGE_REPLIES,
  ): Promise<boolean> {
    if (this.loading[edge]) return false;
    const { selection, current } = this.owner(shouldCommit),
      revision = this.revision;
    if (!selection || !current()) return false;
    const pivot =
      edge === "older"
        ? seq(this.replies[0] ?? ({} as Message))
        : seq(this.replies.at(-1) ?? ({} as Message));
    const operation = {};
    this.edgeOwners[edge] = operation;
    this.loading[edge] = true;
    this.edgeError[edge] = "";
    try {
      const page = await this.request(selection, {
        [edge === "older" ? "before_seq" : "after_seq"]: pivot,
        limit,
      });
      if (!current()) return false;
      this.metadata(page, revision);
      if (pivot && !this.replies.some((reply) => seq(reply) === pivot)) return false;
      if (edge === "older") this.hasOlder = page.has_older;
      else this.hasNewer = page.has_newer;
      this.merge(page.replies, revision, edge);
      return true;
    } catch (error) {
      if (!current()) return false;
      this.edgeError[edge] = readableAPIError(error, `Could not load ${edge} replies.`);
      throw error;
    } finally {
      if (this.edgeOwners[edge] === operation) {
        delete this.edgeOwners[edge];
        this.loading[edge] = false;
      }
    }
  }
  // Reconcile the captured retained interval. Layout never blocks durable ingestion.
  async refresh(shouldCommit: () => boolean = () => true): Promise<boolean> {
    if (!this.initialized) return this.open(shouldCommit);
    const { selection, current } = this.owner(shouldCommit),
      revision = this.revision;
    if (!selection || !current()) return false;
    if (!this.replies.length) return this.latest(shouldCommit);
    const high = seq(this.replies.at(-1)!);
    let cursor = seq(this.replies[0]) - 1;
    try {
      while (cursor < high) {
        const page = await this.request(selection, { after_seq: cursor, limit: 200 });
        if (!current()) return false;
        this.metadata(page, revision);
        this.merge(
          page.replies.filter((reply) => seq(reply) <= high),
          revision,
          "newer",
        );
        if (page.newest_seq <= cursor) break;
        cursor = page.newest_seq;
        this.hasNewer = page.has_newer || page.newest_seq > high;
      }
      if (this.following) await this.catchUp(shouldCommit);
      return current();
    } catch (error) {
      if (!current()) return false;
      this.error = readableAPIError(error, "Could not refresh the thread.");
      throw error;
    }
  }
  private async catchUp(shouldCommit: () => boolean) {
    const { selection, current } = this.owner(shouldCommit),
      revision = this.revision;
    if (!selection || !current()) return;
    const tail = await this.request(selection, { latest: true, limit: 1 });
    if (!current()) return;
    this.metadata(tail, revision);
    const newest = seq(this.replies.at(-1) ?? ({} as Message));
    if (newest >= tail.newest_seq) {
      this.hasNewer = false;
      return;
    }
    if (this.hasNewer && !this.following) return;
    // A fixed tail and row budget bound catch-up even under continuous publication.
    for (
      let fetched = 0;
      current() &&
      seq(this.replies.at(-1) ?? ({} as Message)) < tail.newest_seq &&
      fetched < THREAD_WINDOW_LIMIT;
      fetched += PAGE_REPLIES
    ) {
      if (!(await this.loadEdge("newer", current))) break;
      if (this.hasNewer && !this.following && this.replies.length >= THREAD_WINDOW_LIMIT) break;
    }
    if (current() && seq(this.replies.at(-1) ?? ({} as Message)) < tail.newest_seq)
      this.hasNewer = true;
  }
  async handleEvent(event: RealtimeEvent, shouldCommit: () => boolean): Promise<boolean> {
    const id = event.payload.message_id;
    const belongs =
      event.payload.root_message_id === this.selection?.messageID ||
      id === this.root?.id ||
      this.replies.some((reply) => reply.id === id);
    if (!belongs || !this.isCurrent(this.selection)) return false;
    const operation = this.owner(shouldCommit);
    try {
      if (event.type === "thread.reply_created") {
        await this.catchUp(shouldCommit);
        if (operation.current()) this.coveredStateEvent = true;
      } else if (event.type === "thread.state_updated") {
        if (this.coveredStateEvent) this.coveredStateEvent = false;
        else await this.catchUp(shouldCommit);
      } else if ((event.type === "message.updated" || event.type === "message.deleted") && id) {
        const { current } = this.owner(shouldCommit),
          revision = this.revision;
        const data = await api<{ message: Message }>(`/api/messages/${id}`);
        if (current()) {
          const existing =
            this.root?.id === id ? this.root : this.replies.find((reply) => reply.id === id);
          this.updateMessage(this.mergeSnapshot(data.message, existing, revision));
        }
      } else return false;
      return true;
    } catch (error) {
      if (operation.current()) throw error;
      return false;
    }
  }
  updateDraft(body: string) {
    const id = this.selection?.messageID;
    if (!id || this.draft?.sending) return;
    this.patchDraft(id, { body, error: "" });
  }

  setQuote(quote: Message | null) {
    const id = this.selection?.messageID;
    if (!id || this.draft?.sending) return;
    this.patchDraft(id, { quote });
  }

  private patchDraft(id: string, patch: Partial<ReplyDraft>) {
    const draft = {
      body: "",
      quote: null,
      sending: false,
      error: "",
      ...this.drafts.get(id),
      ...patch,
    };
    const next = new Map(this.drafts);
    if (!draft.body && !draft.quote && !draft.sending && !draft.error) next.delete(id);
    else next.set(id, draft);
    this.drafts = next;
  }

  updateAuthor(user: User) {
    for (const message of this.root ? [this.root, ...this.replies] : this.replies) {
      if (message.author?.id === user.id)
        this.updateMessage({
          id: message.id,
          thread_root_id: message.thread_root_id,
          author: user,
        });
    }
  }
  updateMessage(message: MessageUpdate) {
    if (message.deleted_at && this.draft?.quote?.id === message.id) this.setQuote(null);
    if (message.thread_root_id !== this.selection?.messageID) return;
    for (const mutations of this.pageMutations) {
      // One overflow sentinel bounds memory even when a page is held during a burst.
      if (mutations.size <= THREAD_WINDOW_LIMIT) {
        const previous = mutations.get(message.id);
        mutations.set(message.id, previous ? mergeMessageUpdate(previous, message) : message);
      }
    }
    this.beforeChange?.("preserve");
    this.rowRevisions.set(message.id, ++this.revision);
    this.replies = this.replies.map((reply) =>
      reply.id === message.id ? mergeMessageUpdate(reply, message) : reply,
    );
    if (this.root?.id === message.id) this.root = mergeMessageUpdate(this.root, message);
    this.pruneRevisions();
    this.committed();
  }
  async send(onError?: (error: unknown) => void): Promise<void> {
    const { selection, current } = this.owner(),
      draft = this.draft,
      body = draft?.body.trim();
    if (!this.isCurrent(selection) || !this.root || !draft || !body || draft.sending) return;
    const quotedMessageID = draft.quote?.id;
    const submission =
      draft.submission?.body === body && draft.submission.quotedMessageID === quotedMessageID
        ? draft.submission
        : { body, quotedMessageID, nonce: newNonce() };
    this.patchDraft(selection.messageID, { sending: true, error: "", submission });
    try {
      const data = await api<{ message: Message; thread_state: ThreadState }>(
        `/api/messages/${selection.messageID}/thread/replies`,
        {
          method: "POST",
          body: JSON.stringify({
            body,
            nonce: submission.nonce,
            quoted_message_id: quotedMessageID,
          }),
        },
      );
      this.patchDraft(selection.messageID, { body: "", quote: null, submission: undefined });
      if (this.selection?.messageID !== selection.messageID || !this.isCurrent(this.selection))
        return;
      this.revision++;
      this.reconcileState(data.thread_state);
      this.committed();
      if (!current()) return;
      if (
        !this.hasNewer &&
        !this.replies.some((reply) => reply.id === data.message.id) &&
        seq(data.message) === seq(this.replies.at(-1) ?? ({} as Message)) + 1
      )
        this.merge([data.message], this.revision, "newer", "latest");
      try {
        await this.latest();
      } catch {
        /* The send committed; the window error offers a separate retry. */
      }
    } catch (error) {
      this.patchDraft(selection.messageID, {
        error: readableAPIError(error, "Could not post the reply."),
      });
      if (this.isCurrent(selection)) onError?.(error);
    } finally {
      this.patchDraft(selection.messageID, { sending: false });
    }
  }
}
