import { api, readableAPIError } from "./api";
import type { Message } from "./types";

export type MessageEditSurface = "timeline" | "thread";

export type MessageEditSession = {
  messageID: string;
  surface: MessageEditSurface;
  originalBody: string;
  draft: string;
  error: string;
  saving: boolean;
  generation: number;
  touchedAt: number;
};

export type MessageEditStartResult = "started" | "cancelled" | "blocked" | "invalid";
export type MessageEditSaveResult = "saved" | "cancelled" | "failed" | "invalid";

const MAX_RETAINED_EDIT_SESSIONS = 12;

export function normalizeMessageBodyForServer(value: string): string {
  return value.replace(/^\p{White_Space}+/u, "").replace(/\p{White_Space}+$/u, "");
}

export class MessageEditController {
  private sessions = $state<Map<string, MessageEditSession>>(new Map());
  private generation = 0;
  private touchSequence = 0;

  constructor(
    private readonly reveal?: (scope: string, session: MessageEditSession) => void | Promise<void>,
  ) {}

  session(scope: string): MessageEditSession | undefined {
    return scope ? this.sessions.get(scope) : undefined;
  }

  isEditing(scope: string, messageID: string, surface: MessageEditSurface): boolean {
    const session = this.session(scope);
    return session?.messageID === messageID && session.surface === surface;
  }

  start(scope: string, message: Message, surface: MessageEditSurface): MessageEditStartResult {
    if (
      !scope ||
      !message.id ||
      message.deleted_at ||
      message.status === "pending" ||
      message.status === "failed"
    ) {
      return "invalid";
    }

    const current = this.sessions.get(scope);
    if (current) {
      if (current.messageID === message.id && current.surface === surface) {
        return this.cancel(scope) ? "cancelled" : "blocked";
      }
      const touched = this.touched(current);
      this.setSession(scope, touched);
      void this.reveal?.(scope, touched);
      return "blocked";
    }

    const session: MessageEditSession = {
      messageID: message.id,
      surface,
      originalBody: message.body,
      draft: message.body,
      error: "",
      saving: false,
      generation: ++this.generation,
      touchedAt: ++this.touchSequence,
    };
    this.setSession(scope, session);
    this.trimSessions(scope);
    return "started";
  }

  updateDraft(scope: string, draft: string) {
    const session = this.sessions.get(scope);
    if (!session || session.saving) return;
    this.setSession(scope, this.touched({ ...session, draft, error: "" }));
  }

  cancel(scope: string, surface?: MessageEditSurface): boolean {
    const session = this.sessions.get(scope);
    if (!session || session.saving || (surface && session.surface !== surface)) return false;
    this.deleteSession(scope);
    return true;
  }

  cancelMessage(scope: string, messageID: string): boolean {
    const session = this.sessions.get(scope);
    if (!session || session.messageID !== messageID) return false;
    return this.cancel(scope);
  }

  clear() {
    this.generation += 1;
    this.sessions = new Map();
  }

  reconcile(scope: string, messages: Message[]) {
    const session = this.sessions.get(scope);
    if (!session || session.saving) return;
    const current = messages.find((message) => message.id === session.messageID);
    if (!current) return;
    if (current.deleted_at) {
      this.deleteSession(scope);
      return;
    }
    if (current.body === session.originalBody) return;
    if (session.draft === session.originalBody) {
      this.setSession(
        scope,
        this.touched({
          ...session,
          originalBody: current.body,
          draft: current.body,
          error: "",
        }),
      );
      return;
    }
    this.setSession(
      scope,
      this.touched({
        ...session,
        originalBody: current.body,
        error: "This message changed elsewhere. Review your draft before saving.",
      }),
    );
  }

  async save(
    scope: string,
    message: Message,
    onSaved: (updated: Message) => void,
  ): Promise<MessageEditSaveResult> {
    const session = this.sessions.get(scope);
    if (!session || session.messageID !== message.id || session.saving) return "invalid";

    const normalizedBody = normalizeMessageBodyForServer(session.draft);
    if (!normalizedBody) {
      this.setSession(scope, this.touched({ ...session, error: "Message body is required" }));
      return "invalid";
    }
    if (normalizedBody === message.body) {
      this.deleteSession(scope);
      return "cancelled";
    }

    this.setSession(scope, this.touched({ ...session, error: "", saving: true }));
    const generation = session.generation;
    try {
      const data = await api<{ message: Message }>(`/api/messages/${message.id}`, {
        method: "PATCH",
        body: JSON.stringify({ body: session.draft }),
      });
      const current = this.sessions.get(scope);
      if (current?.generation !== generation) return "invalid";
      onSaved(data.message);
      this.deleteSession(scope);
      return "saved";
    } catch (error) {
      const current = this.sessions.get(scope);
      if (current?.generation !== generation) return "invalid";
      this.setSession(
        scope,
        this.touched({
          ...current,
          error: readableAPIError(error, "Could not edit message"),
        }),
      );
      return "failed";
    } finally {
      const current = this.sessions.get(scope);
      if (current?.generation === generation) {
        this.setSession(scope, this.touched({ ...current, saving: false }));
      }
    }
  }

  private touched(session: MessageEditSession): MessageEditSession {
    return { ...session, touchedAt: ++this.touchSequence };
  }

  private setSession(scope: string, session: MessageEditSession) {
    const next = new Map(this.sessions);
    next.delete(scope);
    next.set(scope, session);
    this.sessions = next;
  }

  private deleteSession(scope: string) {
    if (!this.sessions.has(scope)) return;
    const next = new Map(this.sessions);
    next.delete(scope);
    this.sessions = next;
  }

  private trimSessions(protectedScope: string) {
    if (this.sessions.size <= MAX_RETAINED_EDIT_SESSIONS) return;
    const candidates = [...this.sessions.entries()]
      .filter(([scope, session]) => scope !== protectedScope && !session.saving)
      .sort((left, right) => left[1].touchedAt - right[1].touchedAt);
    const removeCount = this.sessions.size - MAX_RETAINED_EDIT_SESSIONS;
    const next = new Map(this.sessions);
    for (const [scope] of candidates.slice(0, removeCount)) next.delete(scope);
    this.sessions = next;
  }
}
