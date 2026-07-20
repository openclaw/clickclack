<script lang="ts">
  import { onDestroy, onMount, tick } from "svelte";
  import ImageViewer from "../media/ImageViewer.svelte";
  import ThreadPanel from "../thread/ThreadPanel.svelte";
  import { markdownImageViewerURL } from "../../lib/actions/markdown";
  import { APIError, api, apiResourceURL, readableAPIError } from "../../lib/api";
  import { initAppearance } from "../../lib/appearance";
  import { dmTitle } from "../../lib/chat/people";
  import {
    MessageEditController,
    type MessageEditSession,
  } from "../../lib/messageEditing.svelte";
  import { ReactionController } from "../../lib/reactions.svelte";
  import { connectRealtime, type RealtimeConnection } from "../../lib/realtime.svelte";
  import type {
    Channel,
    DirectConversation,
    Message,
    RealtimeEvent,
    RouteTarget,
    ThreadState,
    Upload,
    User,
  } from "../../lib/types";

  type Props = {
    workspaceRouteID: string;
    messageRouteID: string;
  };

  type ReplySubmission = {
    body: string;
    nonce: string;
    quotedMessageID?: string;
  };

  type ViewState = "loading" | "ready" | "auth" | "forbidden" | "not-found" | "error";

  let { workspaceRouteID, messageRouteID }: Props = $props();

  let viewState = $state<ViewState>("loading");
  let errorText = $state("");
  let user = $state<User | null>(null);
  const reactionController = new ReactionController(() => user?.id || "");
  const editController = new MessageEditController(revealEditSession);
  let route = $state<RouteTarget | null>(null);
  let root = $state<Message | null>(null);
  let replies = $state<Message[]>([]);
  let threadState = $state<ThreadState | null>(null);
  let directConversation = $state<DirectConversation | null>(null);
  let parentLabel = $state("Thread");
  let replyBody = $state("");
  let replyTarget = $state<Message | null>(null);
  let replyInput = $state<HTMLTextAreaElement | null>(null);
  let replyError = $state("");
  let replySending = $state(false);
  let selectedImage = $state<{ url: string; title: string } | null>(null);
  let socket: RealtimeConnection | null = null;
  let loadSerial = 0;
  let loadPending = false;
  let refreshPending = false;
  let refreshQueued = false;
  let failedSubmission: ReplySubmission | null = null;

  const replyDisabled = $derived(
    replySending || Boolean(directConversation && !directConversation.can_send),
  );
  const mentionPeople = $derived.by(() => {
    const people = new Map<string, User>();
    for (const person of directConversation?.members ?? []) {
      if (person.id && !person.deleted_at) people.set(person.id, person);
    }
    for (const message of root ? [root, ...replies] : replies) {
      if (message.author?.id && !message.author.deleted_at) {
        people.set(message.author.id, message.author);
      }
    }
    if (user?.id) people.set(user.id, user);
    return [...people.values()].slice(0, 24);
  });

  function newNonce(): string {
    if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
      return crypto.randomUUID().replace(/-/g, "");
    }
    return `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 10)}`;
  }

  async function revealEditSession(scope: string, session: MessageEditSession) {
    if (scope !== route?.target_id) return;
    for (let attempt = 0; attempt < 4; attempt += 1) {
      await tick();
      const editor = document
        .querySelector('[aria-label="Embedded thread"]')
        ?.querySelector(`[data-message-id="${CSS.escape(session.messageID)}"]`)
        ?.querySelector<HTMLTextAreaElement>('textarea[aria-label="Edit message"]');
      if (!editor) continue;
      editor.focus();
      return;
    }
  }

  function applyEditedMessage(updated: Message) {
    if (root?.id === updated.id) root = { ...root, ...updated };
    replies = replies.map((reply) =>
      reply.id === updated.id ? { ...reply, ...updated } : reply,
    );
  }

  function clearThread() {
    socket?.close();
    socket = null;
    route = null;
    root = null;
    reactionController.clear();
    editController.clear();
    replies = [];
    threadState = null;
    directConversation = null;
    replyTarget = null;
  }

  function handleLoadError(error: unknown) {
    clearThread();
    if (error instanceof APIError) {
      if (error.status === 401) {
        viewState = "auth";
        return;
      }
      if (error.status === 403) {
        viewState = "forbidden";
        return;
      }
      if (error.status === 404) {
        viewState = "not-found";
        return;
      }
    }
    errorText = readableAPIError(error, "Could not load this thread.");
    viewState = "error";
  }

  async function loadParent(target: RouteTarget, currentUser: User): Promise<void> {
    if (target.parent_type === "direct") {
      const data = await api<{ conversations: DirectConversation[] }>(
        `/api/dms?workspace_id=${encodeURIComponent(target.workspace_id)}`,
      );
      let conversation = data.conversations.find((item) => item.id === target.parent_id);
      if (!conversation) {
        const hidden = await api<{ conversation: DirectConversation }>(
          `/api/dms/${encodeURIComponent(target.parent_id)}`,
        );
        conversation = hidden.conversation;
      }
      directConversation = conversation;
      parentLabel = dmTitle(conversation, currentUser.id) || "Direct message";
      return;
    }
    const data = await api<{ channels: Channel[] }>(
      `/api/workspaces/${encodeURIComponent(target.workspace_id)}/channels`,
    );
    const channel = data.channels.find((item) => item.id === target.parent_id);
    if (!channel) throw new APIError(404, "Thread channel not found");
    directConversation = null;
    parentLabel = `#${channel.name}`;
  }

  async function loadThread() {
    if (loadPending) return;
    loadPending = true;
    const serial = ++loadSerial;
    if (viewState !== "auth") viewState = "loading";
    errorText = "";
    try {
      const me = await api<{ user: User }>("/api/me");
      const resolved = await api<{ route: RouteTarget }>(
        `/api/routes/${encodeURIComponent(workspaceRouteID)}/${encodeURIComponent(messageRouteID)}`,
      );
      if (resolved.route.target_type !== "thread") {
        throw new APIError(404, "Thread route not found");
      }
      const [thread] = await Promise.all([
        api<{ root: Message; replies: Message[]; thread_state: ThreadState }>(
          `/api/messages/${encodeURIComponent(resolved.route.target_id)}/thread`,
        ),
        loadParent(resolved.route, me.user),
      ]);
      if (serial !== loadSerial) return;
      user = me.user;
      route = resolved.route;
      root = { ...thread.root, thread_state: thread.thread_state };
      replies = thread.replies;
      reactionController.seedMessages([root, ...replies]);
      editController.reconcile(resolved.route.target_id, [root, ...replies]);
      threadState = thread.thread_state;
      viewState = "ready";
      connectSocket(resolved.route.workspace_id);
    } catch (error) {
      if (serial === loadSerial) handleLoadError(error);
    } finally {
      if (serial === loadSerial) loadPending = false;
    }
  }

  async function refreshThread() {
    if (!root || viewState !== "ready") return;
    const rootID = root.id;
    try {
      const thread = await api<{ root: Message; replies: Message[]; thread_state: ThreadState }>(
        `/api/messages/${encodeURIComponent(rootID)}/thread`,
      );
      if (!root || root.id !== rootID || viewState !== "ready") return;
      root = { ...thread.root, thread_state: thread.thread_state };
      replies = thread.replies;
      reactionController.seedMessages([root, ...replies]);
      editController.reconcile(route?.target_id || root.id, [root, ...replies]);
      threadState = thread.thread_state;
      if (
        replyTarget &&
        replyTarget.id !== root.id &&
        !replies.some((reply) => reply.id === replyTarget?.id)
      ) {
        replyTarget = null;
      }
    } catch (error) {
      if (error instanceof APIError && [401, 403, 404].includes(error.status)) {
        handleLoadError(error);
      } else {
        replyError = readableAPIError(error, "Could not refresh the thread.");
      }
    }
  }

  function queueRefresh() {
    if (refreshPending) {
      refreshQueued = true;
      return;
    }
    refreshPending = true;
    queueMicrotask(() => {
      void refreshThread().finally(() => {
        refreshPending = false;
        if (refreshQueued) {
          refreshQueued = false;
          queueRefresh();
        }
      });
    });
  }

  function eventBelongsToThread(event: RealtimeEvent): boolean {
    if (!root) return false;
    if (event.payload.root_message_id) return event.payload.root_message_id === root.id;
    const messageID = event.payload.message_id;
    return messageID === root.id || replies.some((reply) => reply.id === messageID);
  }

  function handleRealtimeEvent(event: RealtimeEvent) {
    if (
      eventBelongsToThread(event) &&
      (event.type === "reaction.added" || event.type === "reaction.removed")
    ) {
      reactionController.applyEvent(event);
      return;
    }
    if (
      eventBelongsToThread(event) &&
      (event.type === "thread.reply_created" ||
        event.type === "message.updated" ||
        event.type === "message.deleted")
    ) {
      queueRefresh();
    }
  }

  function connectSocket(workspaceID: string) {
    socket?.close();
    socket = connectRealtime({
      workspaceID,
      onEvent: handleRealtimeEvent,
      onStatusChange: (connected) => {
        if (connected) queueRefresh();
      },
    });
  }

  async function sendReply() {
    const body = replyBody.trim();
    if (!body || !root || replyDisabled) return;
    const threadRootID = root.id;
    const quote = replyTarget;
    const quotedMessageID = quote?.id;
    const submission =
      failedSubmission?.body === body && failedSubmission.quotedMessageID === quotedMessageID
        ? failedSubmission
        : { body, nonce: newNonce(), quotedMessageID };
    failedSubmission = null;
    replyError = "";
    replySending = true;
    const payload: Record<string, string> = { body, nonce: submission.nonce };
    if (quotedMessageID) payload.quoted_message_id = quotedMessageID;
    try {
      const data = await api<{ message: Message; thread_state: ThreadState }>(
        `/api/messages/${encodeURIComponent(threadRootID)}/thread/replies`,
        { method: "POST", body: JSON.stringify(payload) },
      );
      if (!root || root.id !== threadRootID) return;
      if (!replies.some((reply) => reply.id === data.message.id)) {
        replies = [...replies, data.message];
      }
      threadState = data.thread_state;
      if (replyBody.trim() === body) replyBody = "";
      if (quotedMessageID && replyTarget?.id === quotedMessageID) replyTarget = null;
    } catch (error) {
      if (error instanceof APIError && error.status === 401) {
        handleLoadError(error);
        return;
      }
      failedSubmission = submission;
      replyError = readableAPIError(error, "Could not post the reply.");
    } finally {
      replySending = false;
    }
  }

  function handleReplyKeydown(event: KeyboardEvent) {
    if (event.key === "Escape" && replyTarget) {
      event.preventDefault();
      replyTarget = null;
      return;
    }
    if (event.key === "Enter" && !event.shiftKey && !event.isComposing) {
      event.preventDefault();
      void sendReply();
    }
  }

  function setReplyTarget(message: Message) {
    if (replySending) return;
    replyTarget = message;
    void tick().then(() => replyInput?.focus());
  }

  function clearReplyTarget() {
    if (!replySending) replyTarget = null;
  }

  function jumpToQuote(message: Message) {
    if (!message.quoted_message_id) return;
    const target = document.querySelector<HTMLElement>(
      `[data-message-id="${CSS.escape(message.quoted_message_id)}"]`,
    );
    target?.scrollIntoView({ behavior: "smooth", block: "center" });
  }

  function handleInlineImagePointerUp(event: PointerEvent) {
    const url = markdownImageViewerURL(event);
    if (url) selectedImage = { url, title: "Message image" };
  }

  function openArtifact(upload: Upload) {
    const opened = window.open(apiResourceURL(`/api/uploads/${upload.id}`), "_blank", "noopener,noreferrer");
    if (opened) opened.opener = null;
  }

  function retryAuthOnFocus() {
    if (viewState === "auth" && document.visibilityState === "visible") void loadThread();
  }

  onMount(() => {
    initAppearance();
    void loadThread();
    window.addEventListener("focus", retryAuthOnFocus);
    document.addEventListener("visibilitychange", retryAuthOnFocus);
  });

  onDestroy(() => {
    loadSerial += 1;
    socket?.close();
    window.removeEventListener("focus", retryAuthOnFocus);
    document.removeEventListener("visibilitychange", retryAuthOnFocus);
  });
</script>

<svelte:head>
  <title>ClickClack thread</title>
  <meta name="color-scheme" content="light dark" />
</svelte:head>

{#if viewState === "ready" && root && route}
  <main class="embed-shell">
    <section class="thread open" aria-label="Embedded thread">
      <ThreadPanel
        {root}
        {replies}
        {threadState}
        {replyBody}
        {replyTarget}
        {mentionPeople}
        {replyDisabled}
        headerLabel={parentLabel}
        headerDetail={`Thread · ${threadState?.reply_count ?? replies.length} ${(threadState?.reply_count ?? replies.length) === 1 ? "reply" : "replies"}`}
        openHref={route.canonical_path}
        currentUserID={user?.id}
        {reactionController}
        {editController}
        editScope={route.target_id}
        onMessageEdited={applyEditedMessage}
        reactionsDisabled={Boolean(directConversation && !directConversation.can_send)}
        onReplyBody={(value) => (replyBody = value)}
        onSubmitReply={() => void sendReply()}
        onReplyKeydown={handleReplyKeydown}
        onReplyFocus={() => (replyError = "")}
        onReplyInputRef={(node) => (replyInput = node)}
        onSetReplyTarget={(message) => setReplyTarget(message)}
        onClearReply={clearReplyTarget}
        onActivateThreadComposer={() => {}}
        onInlineImagePointerUp={handleInlineImagePointerUp}
        onJumpToQuote={jumpToQuote}
        onOpenImage={(url, title) => (selectedImage = { url, title })}
        onOpenArtifact={openArtifact}
      />
    </section>
    {#if replyError}<p class="embed-notice" role="status">{replyError}</p>{/if}
  </main>
{:else if viewState === "auth"}
  <main class="embed-state-shell">
    <section class="embed-state-card" aria-label="Sign in">
      <div class="embed-mark" aria-hidden="true">cc</div>
      <h1>Sign in to ClickClack</h1>
      <p>Open ClickClack in a new tab, sign in, then return here. This panel will reconnect automatically.</p>
      <a class="embed-action" href="/app" target="_blank" rel="noopener">Open ClickClack</a>
    </section>
  </main>
{:else if viewState === "forbidden"}
  <main class="embed-state-shell">
    <section class="embed-state-card" role="alert">
      <h1>Thread unavailable</h1>
      <p>You are not a member of this workspace or do not have permission to view this thread.</p>
    </section>
  </main>
{:else if viewState === "not-found"}
  <main class="embed-state-shell">
    <section class="embed-state-card" role="alert">
      <h1>Thread not found</h1>
      <p>This thread may have been removed, or the link may be incorrect.</p>
    </section>
  </main>
{:else if viewState === "error"}
  <main class="embed-state-shell">
    <section class="embed-state-card" role="alert">
      <h1>Could not load this thread</h1>
      <p>{errorText}</p>
      <button class="embed-action" type="button" onclick={() => void loadThread()}>Try again</button>
    </section>
  </main>
{:else}
  <main class="embed-state-shell" aria-label="Loading thread">
    <div class="embed-loading" aria-hidden="true"><span></span><span></span><span></span></div>
  </main>
{/if}

{#if selectedImage}
  <ImageViewer
    url={selectedImage.url}
    title={selectedImage.title}
    onClose={() => (selectedImage = null)}
  />
{/if}

<style>
  .embed-shell {
    position: relative;
    height: 100vh;
    height: 100dvh;
    min-width: 0;
    overflow: hidden;
    background: var(--panel);
  }

  .embed-shell :global(.thread) {
    position: relative;
    inset: auto;
    z-index: auto;
    height: 100%;
    width: 100%;
    max-width: none;
    border-left: 0;
    box-shadow: none;
  }

  .embed-shell :global(.thread > header) {
    min-height: 54px;
    padding-block: 9px;
  }

  .embed-notice {
    position: absolute;
    right: 14px;
    bottom: 78px;
    left: 14px;
    z-index: 2;
    margin: 0;
    padding: 9px 11px;
    border: 1px solid color-mix(in srgb, var(--danger) 40%, var(--line));
    border-radius: var(--radius);
    background: color-mix(in srgb, var(--danger) 9%, var(--panel));
    color: var(--danger);
    font-size: 12px;
    box-shadow: var(--shadow);
  }

  .embed-state-shell {
    display: grid;
    min-height: 100vh;
    min-height: 100dvh;
    place-items: center;
    padding: 24px;
    background:
      radial-gradient(color-mix(in srgb, var(--text) 5%, transparent) 1px, transparent 1.5px) 0 0 / 18px 18px,
      var(--bg);
  }

  .embed-state-card {
    display: grid;
    gap: 12px;
    width: min(100%, 360px);
    padding: 28px;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius-xl);
    background: var(--panel);
    box-shadow: var(--key-edge), var(--shadow);
    text-align: center;
  }

  .embed-state-card h1,
  .embed-state-card p {
    margin: 0;
  }

  .embed-state-card h1 {
    color: var(--text-strong);
    font-family: var(--font-display);
    font-size: 22px;
  }

  .embed-state-card p {
    color: var(--muted);
    font-size: 13px;
    line-height: 1.5;
  }

  .embed-mark {
    display: grid;
    width: 42px;
    height: 42px;
    margin: 0 auto 2px;
    place-items: center;
    border-radius: 11px;
    background: linear-gradient(135deg, var(--brand-a), var(--brand-b));
    color: var(--brand-contrast);
    font-weight: 800;
  }

  .embed-action {
    display: inline-flex;
    min-height: 40px;
    align-items: center;
    justify-content: center;
    margin-top: 4px;
    padding: 0 16px;
    border: 1px solid color-mix(in srgb, var(--accent) 40%, var(--line));
    border-radius: var(--radius);
    background: var(--accent);
    color: var(--accent-contrast);
    font: inherit;
    font-weight: 700;
    text-decoration: none;
    cursor: pointer;
  }

  .embed-action:hover {
    background: var(--accent-hover);
  }

  .embed-loading {
    display: flex;
    gap: 7px;
  }

  .embed-loading span {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--accent);
    animation: embed-pulse 1.2s ease-in-out infinite;
  }

  .embed-loading span:nth-child(2) { animation-delay: 120ms; }
  .embed-loading span:nth-child(3) { animation-delay: 240ms; }

  @keyframes embed-pulse {
    0%, 100% { opacity: 0.35; transform: translateY(0); }
    50% { opacity: 1; transform: translateY(-3px); }
  }
</style>
