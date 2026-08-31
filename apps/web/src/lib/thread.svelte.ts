import { api, readableAPIError } from "./api";
import { newNonce } from "./chat/messages";
import type { Message, ThreadState, User } from "./types";

type ThreadSelection = { messageID: string; context: string };
type Submission = { body: string; quotedMessageID?: string; nonce: string };
type ReplyDraft = {
  body: string;
  quote: Message | null;
  sending: boolean;
  error: string;
  submission?: Submission;
};

export class ThreadController {
  root = $state<Message | null>(null);
  replies = $state<Message[]>([]);
  state = $state<ThreadState | null>(null);
  error = $state("");
  selection = $state.raw<ThreadSelection | null>(null);
  private drafts = $state<Map<string, ReplyDraft>>(new Map());
  private loadSerial = 0;
  private replyRevision = 0;

  constructor(private readonly context: () => string) {}

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
    this.loadSerial++;
    this.root = null;
    this.replies = [];
    this.state = null;
    this.error = "";
  }

  async refresh(shouldCommit: () => boolean = () => true): Promise<boolean> {
    const selection = this.selection;
    if (!this.isCurrent(selection)) return false;
    const serial = ++this.loadSerial;
    const replyRevision = this.replyRevision;
    const current = () => this.isCurrent(selection) && serial === this.loadSerial && shouldCommit();
    try {
      const data = await api<{ root: Message; replies: Message[]; thread_state: ThreadState }>(
        `/api/messages/${selection.messageID}/thread`,
      );
      if (!current()) return false;
      const sentDuringLoad = replyRevision !== this.replyRevision;
      const replies = new Map(data.replies.map((reply) => [reply.id, reply]));
      if (sentDuringLoad) {
        for (const reply of this.replies) if (!replies.has(reply.id)) replies.set(reply.id, reply);
      }
      this.replies = [...replies.values()].sort(
        (a, b) => (a.thread_seq ?? 0) - (b.thread_seq ?? 0),
      );
      this.root = data.root;
      this.reconcileState(data.thread_state);
      this.error = "";
      return true;
    } catch (error) {
      if (!current()) return false;
      this.error = readableAPIError(error, "Could not load the thread.");
      throw error;
    }
  }

  private reconcileState(incoming: ThreadState) {
    // Reply counts only grow; a held POST receipt can arrive after a newer realtime GET.
    if (!this.state || incoming.reply_count >= this.state.reply_count) this.state = incoming;
    if (this.root) this.root = { ...this.root, thread_state: this.state };
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
    this.replies = this.replies.map((reply) =>
      reply.author?.id === user.id ? { ...reply, author: user } : reply,
    );
    if (this.root?.author?.id === user.id) this.root = { ...this.root, author: user };
  }

  updateMessage(message: Message) {
    this.replies = this.replies.map((reply) =>
      reply.id === message.id ? { ...reply, ...message } : reply,
    );
    if (this.root?.id === message.id) this.root = { ...this.root, ...message };
    if (message.deleted_at && this.draft?.quote?.id === message.id) this.setQuote(null);
  }

  async send(): Promise<void> {
    const selection = this.selection;
    const draft = this.draft;
    const body = draft?.body.trim();
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
      // A send belongs to its root even if the user has closed and reopened it.
      if (this.selection?.messageID !== selection.messageID || !this.isCurrent(this.selection))
        return;
      this.replyRevision++;
      if (!this.replies.some((reply) => reply.id === data.message.id)) {
        this.replies = [...this.replies, data.message];
      }
      this.reconcileState(data.thread_state);
    } catch (error) {
      this.patchDraft(selection.messageID, {
        error: readableAPIError(error, "Could not post the reply."),
      });
    } finally {
      this.patchDraft(selection.messageID, { sending: false });
    }
  }
}
