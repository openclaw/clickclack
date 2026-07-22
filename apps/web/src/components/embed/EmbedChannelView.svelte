<script lang="ts">
  import { onDestroy, onMount, tick } from "svelte";
  import ChatComposer from "../composer/ChatComposer.svelte";
  import ImageViewer from "../media/ImageViewer.svelte";
  import MessageList, { type MessageListHandle } from "../messages/MessageList.svelte";
  import { markdownImageViewerURL } from "../../lib/actions/markdown";
  import { APIError, api, apiResourceURL, readableAPIError } from "../../lib/api";
  import { initAppearance } from "../../lib/appearance";
  import {
    MessageEditController,
    type MessageEditSession,
  } from "../../lib/messageEditing.svelte";
  import { ReactionController } from "../../lib/reactions.svelte";
  import { connectRealtime, type RealtimeConnection } from "../../lib/realtime.svelte";
  import type {
    Channel,
    Message,
    MessagePage,
    RealtimeEvent,
    RouteTarget,
    Upload,
    User,
  } from "../../lib/types";

  type Props = {
    workspaceRouteID: string;
    channelRouteID: string;
  };

  type MessageSubmission = {
    body: string;
    nonce: string;
    quotedMessageID?: string;
  };

  type ViewState = "loading" | "ready" | "auth" | "forbidden" | "not-found" | "error";

  let { workspaceRouteID, channelRouteID }: Props = $props();

  let viewState = $state<ViewState>("loading");
  let errorText = $state("");
  let user = $state<User | null>(null);
  const reactionController = new ReactionController(() => user?.id || "");
  const editController = new MessageEditController(revealEditSession);
  let route = $state<RouteTarget | null>(null);
  let channel = $state<Channel | null>(null);
  let messages = $state<Message[]>([]);
  let oldestSeq = $state(0);
  let newestSeq = $state(0);
  let hasOlder = $state(false);
  let hasNewer = $state(false);
  let loadingOlder = $state(false);
  let messageBody = $state("");
  let replyTarget = $state<Message | null>(null);
  let messageInput = $state<HTMLTextAreaElement | null>(null);
  let messageList = $state<MessageListHandle | null>(null);
  let sendError = $state("");
  let sending = $state(false);
  let selectedImage = $state<{ url: string; title: string } | null>(null);
  let socket: RealtimeConnection | null = null;
  let loadSerial = 0;
  let loadPending = false;
  let syncPending = false;
  let syncQueued = false;
  let failedSubmission: MessageSubmission | null = null;

  const mentionPeople = $derived.by(() => {
    const people = new Map<string, User>();
    for (const message of messages) {
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
    if (scope !== channel?.id) return;
    messageList?.scrollToMessage(session.messageID);
    for (let attempt = 0; attempt < 16; attempt += 1) {
      await tick();
      await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
      const editor = document
        .querySelector(".embed-channel-shell")
        ?.querySelector(`[data-message-id="${CSS.escape(session.messageID)}"]`)
        ?.querySelector<HTMLTextAreaElement>('textarea[aria-label="Edit message"]');
      if (!editor) continue;
      editor.focus();
      return;
    }
  }

  function applyEditedMessage(updated: Message) {
    messages = messages.map((message) =>
      message.id === updated.id ? { ...message, ...updated } : message,
    );
  }

  function mergeMessages(...lists: Message[][]): Message[] {
    const byID = new Map<string, Message>();
    for (const list of lists) {
      for (const message of list) byID.set(message.id, message);
    }
    return [...byID.values()].sort(
      (a, b) => (a.channel_seq || 0) - (b.channel_seq || 0),
    );
  }

  function applyPage(page: MessagePage, mode: "replace" | "prepend" | "append") {
    reactionController.seedMessages(page.messages);
    messages =
      mode === "replace"
        ? page.messages
        : mode === "prepend"
          ? mergeMessages(page.messages, messages)
          : mergeMessages(messages, page.messages);
    editController.reconcile(channel?.id || "", messages);
    oldestSeq = messages[0]?.channel_seq || page.oldest_seq || oldestSeq;
    newestSeq = messages[messages.length - 1]?.channel_seq || page.newest_seq || newestSeq;
    if (mode !== "append") hasOlder = page.has_older;
    if (mode !== "prepend") hasNewer = page.has_newer;
  }

  function clearChannel() {
    socket?.close();
    socket = null;
    route = null;
    channel = null;
    reactionController.clear();
    editController.clear();
    messages = [];
    oldestSeq = 0;
    newestSeq = 0;
    hasOlder = false;
    hasNewer = false;
    replyTarget = null;
  }

  function handleLoadError(error: unknown) {
    clearChannel();
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
    errorText = readableAPIError(error, "Could not load this channel.");
    viewState = "error";
  }

  async function loadChannel() {
    if (loadPending) return;
    loadPending = true;
    const serial = ++loadSerial;
    if (viewState !== "auth") viewState = "loading";
    errorText = "";
    try {
      const me = await api<{ user: User }>("/api/me");
      const resolved = await api<{ route: RouteTarget }>(
        `/api/routes/${encodeURIComponent(workspaceRouteID)}/${encodeURIComponent(channelRouteID)}`,
      );
      if (resolved.route.target_type !== "channel") {
        throw new APIError(404, "Channel route not found");
      }
      const [channelData, page] = await Promise.all([
        api<{ channels: Channel[] }>(
          `/api/workspaces/${encodeURIComponent(resolved.route.workspace_id)}/channels`,
        ),
        api<MessagePage>(
          `/api/channels/${encodeURIComponent(resolved.route.target_id)}/messages?limit=100`,
        ),
      ]);
      const resolvedChannel = channelData.channels.find(
        (candidate) => candidate.id === resolved.route.target_id,
      );
      if (!resolvedChannel) throw new APIError(404, "Channel not found");
      if (serial !== loadSerial) return;
      user = me.user;
      route = resolved.route;
      channel = resolvedChannel;
      applyPage(page, "replace");
      viewState = "ready";
      connectSocket(resolved.route.workspace_id);
    } catch (error) {
      if (serial === loadSerial) handleLoadError(error);
    } finally {
      if (serial === loadSerial) loadPending = false;
    }
  }

  async function loadOlderMessages() {
    if (!channel || loadingOlder || !hasOlder || oldestSeq <= 0) return;
    const channelID = channel.id;
    loadingOlder = true;
    try {
      const page = await api<MessagePage>(
        `/api/channels/${encodeURIComponent(channelID)}/messages?before_seq=${encodeURIComponent(String(oldestSeq))}&limit=100`,
      );
      if (channel?.id !== channelID || viewState !== "ready") return;
      applyPage(page, "prepend");
    } catch (error) {
      sendError = readableAPIError(error, "Could not load older messages.");
    } finally {
      loadingOlder = false;
    }
  }

  async function syncNewMessages() {
    if (!channel || viewState !== "ready") return;
    const channelID = channel.id;
    try {
      if (newestSeq <= 0) {
        const latest = await api<MessagePage>(
          `/api/channels/${encodeURIComponent(channelID)}/messages?limit=100`,
        );
        if (channel?.id === channelID && viewState === "ready") applyPage(latest, "replace");
        return;
      }
      let cursor = newestSeq;
      for (let pageCount = 0; pageCount < 20; pageCount++) {
        const page = await api<MessagePage>(
          `/api/channels/${encodeURIComponent(channelID)}/messages?after_seq=${encodeURIComponent(String(cursor))}&limit=100`,
        );
        if (channel?.id !== channelID || viewState !== "ready") return;
        applyPage(page, "append");
        const nextCursor = page.newest_seq || cursor;
        if (!page.has_newer || nextCursor <= cursor) return;
        cursor = nextCursor;
      }
      queueMessageSync();
    } catch (error) {
      if (error instanceof APIError && [401, 403, 404].includes(error.status)) {
        handleLoadError(error);
      } else {
        sendError = readableAPIError(error, "Could not recover newer messages.");
      }
    }
  }

  function queueMessageSync() {
    if (syncPending) {
      syncQueued = true;
      return;
    }
    syncPending = true;
    queueMicrotask(() => {
      void syncNewMessages().finally(() => {
        syncPending = false;
        if (syncQueued) {
          syncQueued = false;
          queueMessageSync();
        }
      });
    });
  }

  async function refreshMessage(messageID: string) {
    if (!messages.some((message) => message.id === messageID)) return;
    try {
      const data = await api<{ message: Message }>(
        `/api/messages/${encodeURIComponent(messageID)}`,
      );
      messages = messages.map((message) =>
        message.id === data.message.id ? data.message : message,
      );
      editController.reconcile(channel?.id || "", messages);
    } catch (error) {
      if (error instanceof APIError && [401, 403, 404].includes(error.status)) {
        handleLoadError(error);
      }
    }
  }

  async function refreshChannelMetadata() {
    if (!route || !channel) return;
    try {
      const data = await api<{ channels: Channel[] }>(
        `/api/workspaces/${encodeURIComponent(route.workspace_id)}/channels`,
      );
      const refreshed = data.channels.find((candidate) => candidate.id === channel?.id);
      if (!refreshed) throw new APIError(404, "Channel not found");
      channel = refreshed;
    } catch (error) {
      if (error instanceof APIError && [401, 403, 404].includes(error.status)) {
        handleLoadError(error);
      }
    }
  }

  function eventChannelID(event: RealtimeEvent): string {
    return event.channel_id || event.payload.channel_id || "";
  }

  function handleRealtimeEvent(event: RealtimeEvent) {
    if (!channel || eventChannelID(event) !== channel.id) return;
    if (event.type === "message.created") {
      queueMessageSync();
      return;
    }
    if (event.type === "message.updated" || event.type === "message.deleted") {
      if (event.payload.message_id) void refreshMessage(event.payload.message_id);
      return;
    }
    if (event.type === "reaction.added" || event.type === "reaction.removed") {
      reactionController.applyEvent(event);
      return;
    }
    if (event.type === "channel.updated") void refreshChannelMetadata();
  }

  function connectSocket(workspaceID: string) {
    socket?.close();
    socket = connectRealtime({
      workspaceID,
      onEvent: handleRealtimeEvent,
      onStatusChange: (connected) => {
        if (connected) queueMessageSync();
      },
    });
  }

  async function sendMessage() {
    const body = messageBody.trim();
    if (!body || !channel || sending) return;
    const channelID = channel.id;
    const quote = replyTarget;
    const quotedMessageID = quote?.id;
    const submission =
      failedSubmission?.body === body &&
      failedSubmission.quotedMessageID === quotedMessageID
        ? failedSubmission
        : { body, nonce: newNonce(), quotedMessageID };
    failedSubmission = null;
    sendError = "";
    sending = true;
    const payload: Record<string, string> = { body, nonce: submission.nonce };
    if (quotedMessageID) payload.quoted_message_id = quotedMessageID;
    try {
      const data = await api<{ message: Message }>(
        `/api/channels/${encodeURIComponent(channelID)}/messages`,
        { method: "POST", body: JSON.stringify(payload) },
      );
      if (channel?.id !== channelID || viewState !== "ready") return;
      messages = mergeMessages(messages, [data.message]);
      newestSeq = Math.max(newestSeq, data.message.channel_seq || 0);
      if (messageBody.trim() === body) messageBody = "";
      if (quotedMessageID && replyTarget?.id === quotedMessageID) replyTarget = null;
      await tick();
      await messageList?.scrollToBottom();
    } catch (error) {
      if (error instanceof APIError && error.status === 401) {
        handleLoadError(error);
        return;
      }
      failedSubmission = submission;
      sendError = readableAPIError(error, "Could not send this message.");
    } finally {
      sending = false;
    }
  }

  function handleComposerKeydown(event: KeyboardEvent) {
    if (event.key === "Escape" && replyTarget) {
      event.preventDefault();
      replyTarget = null;
      return;
    }
    if (event.key === "Enter" && !event.shiftKey && !event.isComposing) {
      event.preventDefault();
      void sendMessage();
    }
  }

  function setReplyTarget(message: Message) {
    if (sending) return;
    replyTarget = message;
    void tick().then(() => messageInput?.focus());
  }

  function jumpToQuote(message: Message) {
    if (message.quoted_message_id) messageList?.scrollToMessage(message.quoted_message_id);
  }

  function openThread(message: Message) {
    if (!message.route_id) return;
    const opened = window.open(
      `/app/${encodeURIComponent(workspaceRouteID)}/${encodeURIComponent(message.route_id)}`,
      "_blank",
      "noopener,noreferrer",
    );
    if (opened) opened.opener = null;
  }

  function handleInlineImagePointerUp(event: PointerEvent) {
    const url = markdownImageViewerURL(event);
    if (url) selectedImage = { url, title: "Message image" };
  }

  function openArtifact(upload: Upload) {
    const opened = window.open(
      apiResourceURL(`/api/uploads/${upload.id}`),
      "_blank",
      "noopener,noreferrer",
    );
    if (opened) opened.opener = null;
  }

  function retryAuthOnFocus() {
    if (viewState === "auth" && document.visibilityState === "visible") void loadChannel();
  }

  onMount(() => {
    initAppearance();
    void loadChannel();
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
  <title>{channel ? `#${channel.name} · ClickClack` : "ClickClack channel"}</title>
  <meta name="color-scheme" content="light dark" />
</svelte:head>

{#if viewState === "ready" && channel && route}
  <main class="embed-channel-shell" aria-label="Embedded channel">
    <header class="embed-channel-header">
      <div>
        <span class="embed-channel-glyph" aria-hidden="true">#</span>
        <h1>{channel.name}</h1>
        {#if channel.archived_at}<span class="embed-channel-status">Archived</span>{/if}
      </div>
    </header>
    <MessageList
      {messages}
      selectedChannel={channel}
      viewKey={channel.id}
      {hasOlder}
      {hasNewer}
      {loadingOlder}
      currentUserID={user?.id}
      {reactionController}
      {editController}
      editScope={channel.id}
      onMessageEdited={applyEditedMessage}
      onListRef={(handle) => (messageList = handle)}
      onActivateMessageComposer={() => {}}
      onInlineImagePointerUp={handleInlineImagePointerUp}
      onOpenProfile={() => {}}
      onReply={(message) => setReplyTarget(message)}
      onOpenThread={openThread}
      onJumpToQuote={jumpToQuote}
      onOpenImage={(url, title) => (selectedImage = { url, title })}
      onOpenArtifact={openArtifact}
      onLoadOlder={() => void loadOlderMessages()}
      onLoadNewer={() => queueMessageSync()}
    />
    <div class="embed-channel-composer-dock">
      {#if sendError}<p class="embed-notice" role="status">{sendError}</p>{/if}
      <ChatComposer
        value={messageBody}
        placeholder={`Message #${channel.name}`}
        ariaLabel="Message body"
        submitLabel="Send"
        formClass="composer embed-channel-composer"
        disabled={sending}
        replyTarget={replyTarget}
        showToolbar
        {mentionPeople}
        onValue={(value) => (messageBody = value)}
        onSubmit={() => void sendMessage()}
        onKeydown={handleComposerKeydown}
        onFocus={() => (sendError = "")}
        onInputRef={(node) => (messageInput = node)}
        onClearReply={() => (replyTarget = null)}
      />
    </div>
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
      <h1>Channel unavailable</h1>
      <p>You are not a member of this workspace or do not have permission to view this channel.</p>
    </section>
  </main>
{:else if viewState === "not-found"}
  <main class="embed-state-shell">
    <section class="embed-state-card" role="alert">
      <h1>Channel not found</h1>
      <p>This channel may have been removed, or the link may be incorrect.</p>
    </section>
  </main>
{:else if viewState === "error"}
  <main class="embed-state-shell">
    <section class="embed-state-card" role="alert">
      <h1>Could not load this channel</h1>
      <p>{errorText}</p>
      <button class="embed-action" type="button" onclick={() => void loadChannel()}>Try again</button>
    </section>
  </main>
{:else}
  <main class="embed-state-shell" aria-label="Loading channel">
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
  .embed-channel-shell {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    grid-template-rows: auto minmax(0, 1fr) auto;
    height: 100vh;
    height: 100dvh;
    min-width: 0;
    overflow: hidden;
    background: var(--bg);
  }

  .embed-channel-header {
    display: flex;
    min-height: 44px;
    align-items: center;
    padding: 6px 12px;
    border-bottom: 1px solid var(--line);
    background: var(--panel);
  }

  .embed-channel-header > div {
    display: flex;
    min-width: 0;
    align-items: center;
    gap: 7px;
  }

  .embed-channel-header h1 {
    overflow: hidden;
    margin: 0;
    color: var(--text-strong);
    font-family: var(--font-display);
    font-size: 15px;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .embed-channel-glyph {
    color: var(--accent);
    font-weight: 800;
  }

  .embed-channel-status {
    padding: 2px 6px;
    border: 1px solid var(--line);
    border-radius: 999px;
    color: var(--muted);
    font-family: var(--font-mono);
    font-size: 9px;
    font-weight: 700;
    letter-spacing: 0.05em;
    text-transform: uppercase;
  }

  .embed-channel-shell :global(.messages) {
    min-height: 0;
  }

  .embed-channel-composer-dock {
    position: relative;
  }

  .embed-channel-composer-dock :global(.embed-channel-composer) {
    --composer-card-top: 6px;
    --composer-overlay-inset-x: 14px;

    padding: 6px 14px 12px;
  }

  .embed-notice {
    position: absolute;
    right: 14px;
    bottom: calc(100% - 2px);
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
    animation: embed-pulse 900ms ease-in-out infinite alternate;
  }

  .embed-loading span:nth-child(2) {
    animation-delay: 150ms;
  }

  .embed-loading span:nth-child(3) {
    animation-delay: 300ms;
  }

  @keyframes embed-pulse {
    to {
      opacity: 0.25;
      transform: translateY(-4px);
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .embed-loading span {
      animation: none;
    }
  }
</style>
