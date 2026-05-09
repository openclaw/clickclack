<script lang="ts">
  import { onDestroy, onMount, tick } from "svelte";
  import { APIError, api } from "./lib/api";
  import { probeMediaDimensions } from "./lib/media";
  import { gifLibrary } from "./lib/gifs";
  import { collectRecentPeople, dmTitle } from "./lib/chat/people";
  import { redirectTypingToComposer } from "./lib/chat/typeToFocus";
  import { connectRealtime, type RealtimeConnection } from "./lib/realtime.svelte";
  import { notifyTyping, stopTyping } from "./lib/typing";
  import ChatComposer from "./components/composer/ChatComposer.svelte";
  import ImageViewer from "./components/media/ImageViewer.svelte";
  import MessageList, { type MessageListHandle, type MessageListState } from "./components/messages/MessageList.svelte";
  import TypingIndicator, { TYPING_TTL_MS, type TypingEntry } from "./components/messages/TypingIndicator.svelte";
  import GuildRail from "./components/navigation/GuildRail.svelte";
  import Sidebar from "./components/navigation/Sidebar.svelte";
  import ProfilePane from "./components/profile/ProfilePane.svelte";
  import ProfileSettingsModal from "./components/profile/ProfileSettingsModal.svelte";
  import SearchResults from "./components/search/SearchResults.svelte";
  import ThreadEmptyState from "./components/thread/ThreadEmptyState.svelte";
  import ThreadPanel from "./components/thread/ThreadPanel.svelte";
  import Topbar from "./components/topbar/Topbar.svelte";
  import type { Channel, DirectConversation, Message, RealtimeEvent, SearchResult, ThreadState, Upload, User, Workspace } from "./lib/types";

  let user: User | null = null;
  let workspaces: Workspace[] = [];
  let channels: Channel[] = [];
  let directConversations: DirectConversation[] = [];
  let messages: Message[] = [];
  let replies: Message[] = [];
  let selectedWorkspaceID = "";
  let selectedChannelID = "";
  let selectedDirectID = "";
  let selectedThread: Message | null = null;
  let selectedThreadState: ThreadState | null = null;
  let selectedProfile: User | null = null;
  let selectedImage: { url: string; title: string } | null = null;
  let messageBody = "";
  let replyBody = "";
  let workspaceName = "";
  let channelName = "";
  let directMemberID = "";
  let searchQuery = "";
  let searchResults: SearchResult[] = [];
  let pendingUpload: Upload | null = null;
  let showGifPicker = false;
  let showProfileSettings = false;
  let gifQuery = "";
  let profileDisplayName = "";
  let profileHandle = "";
  let profileAvatarURL = "";
  let profileStatus = "";
  let profileStatusError = false;
  let status = "loading";
  let authRequired = false;
  let connected = false;
  let socket: RealtimeConnection | null = null;
  let messageList: MessageListHandle | null = null;
  let scrollMemory = new Map<string, MessageListState>();
  let viewKey = "";
  let viewRestoreState: MessageListState | undefined = undefined;
  let messagesLoading = true;
  let showWorkspaceCreate = false;
  let sidebarCollapsed = false;
  let mobileNavOpen = false;
  let replyTarget: Message | null = null;
  let replyContext: "channel" | "dm" | "thread" | null = null;
  let messageInput: HTMLTextAreaElement | null = null;
  let replyInput: HTMLTextAreaElement | null = null;
  let activeComposerContext: "message" | "thread" = "message";
  let typingEntries: TypingEntry[] = [];
  let typingSweeper: number | undefined;

  $: selectedWorkspace = workspaces.find((workspace) => workspace.id === selectedWorkspaceID);
  $: selectedChannel = channels.find((channel) => channel.id === selectedChannelID);
  $: selectedDirect = directConversations.find((conversation) => conversation.id === selectedDirectID);
  $: sidePanelOpen = selectedThread !== null || selectedProfile !== null;
  $: recentPeople = collectRecentPeople(messages, directConversations, user?.id || "");
  $: if (replyContext === "channel" && replyTarget && !messages.some((m) => m.id === replyTarget?.id)) clearReplyTarget();
  $: if (replyContext === "dm" && replyTarget && !messages.some((m) => m.id === replyTarget?.id)) clearReplyTarget();
  $: if (replyContext === "thread" && replyTarget && selectedThread && replyTarget.id !== selectedThread.id && !replies.some((r) => r.id === replyTarget?.id)) clearReplyTarget();
  $: filteredGifs = showGifPicker
    ? gifLibrary.filter((gif) => {
        const query = gifQuery.trim().toLowerCase();
        return !query || gif.title.toLowerCase().includes(query) || gif.tags.some((tag) => tag.includes(query));
      })
    : [];

  onMount(() => {
    void boot();
  });

  onDestroy(() => {
    socket?.close();
    socket = null;
    connected = false;
    stopTyping();
    if (typingSweeper) window.clearInterval(typingSweeper);
  });

  async function boot() {
    try {
      const me = await api<{ user: User }>("/api/me");
      user = me.user;
      await loadWorkspaces();
      status = "ready";
    } catch (error) {
      if (error instanceof APIError && (error.status === 401 || error.status === 403)) {
        authRequired = true;
        status = "auth";
        return;
      }
      status = error instanceof Error ? error.message : "Could not load ClickClack";
    }
  }

  function openProfileSettings() {
    if (!user) return;
    profileDisplayName = user.display_name;
    profileHandle = user.handle ? `@${user.handle}` : "";
    profileAvatarURL = user.avatar_url;
    profileStatus = "";
    profileStatusError = false;
    showProfileSettings = true;
  }

  async function saveProfile() {
    profileStatus = "";
    profileStatusError = false;
    try {
      const data = await api<{ user: User }>("/api/me", {
        method: "PATCH",
        body: JSON.stringify({
          display_name: profileDisplayName,
          handle: profileHandle,
          avatar_url: profileAvatarURL,
        }),
      });
      user = data.user;
      messages = messages.map((message) =>
        message.author?.id === user?.id ? { ...message, author: data.user } : message,
      );
      replies = replies.map((reply) =>
        reply.author?.id === user?.id ? { ...reply, author: data.user } : reply,
      );
      if (selectedThread?.author?.id === user.id) selectedThread = { ...selectedThread, author: data.user };
      profileStatus = "Saved";
      showProfileSettings = false;
    } catch (error) {
      profileStatus = error instanceof Error ? error.message : "Could not save profile";
      profileStatusError = true;
    }
  }

  async function loadWorkspaces() {
    const data = await api<{ workspaces: Workspace[] }>("/api/workspaces");
    workspaces = data.workspaces;
    selectedWorkspaceID = selectedWorkspaceID || workspaces[0]?.id || "";
    await loadChannels();
    await loadDirectConversations();
    if (workspaces.length === 0) status = "create a workspace";
    connectRealtimeSocket();
  }

  async function createWorkspace() {
    if (!workspaceName.trim()) return;
    const data = await api<{ workspace: Workspace }>("/api/workspaces", {
      method: "POST",
      body: JSON.stringify({ name: workspaceName })
    });
    workspaceName = "";
    showWorkspaceCreate = false;
    workspaces = [...workspaces, data.workspace];
    selectedWorkspaceID = data.workspace.id;
    mobileNavOpen = false;
    await loadChannels();
    await loadDirectConversations();
    connectRealtimeSocket();
  }

  async function selectWorkspace(workspaceID: string) {
    selectedWorkspaceID = workspaceID;
    mobileNavOpen = false;
    await loadChannels();
    await loadDirectConversations();
    connectRealtimeSocket();
  }

  async function loadChannels() {
    if (!selectedWorkspaceID) return;
    const data = await api<{ channels: Channel[] }>(`/api/workspaces/${selectedWorkspaceID}/channels`);
    channels = data.channels;
    selectedChannelID = channels.find((channel) => channel.id === selectedChannelID)?.id || channels[0]?.id || "";
    selectedThread = null;
    selectedProfile = null;
    activeComposerContext = "message";
    replies = [];
    await loadMessages();
    if (selectedChannelID && !selectedDirectID) void markChannelRead(selectedChannelID);
  }

  async function createChannel() {
    if (!selectedWorkspaceID || !channelName.trim()) return;
    const data = await api<{ channel: Channel }>(`/api/workspaces/${selectedWorkspaceID}/channels`, {
      method: "POST",
      body: JSON.stringify({ name: channelName, kind: "public" })
    });
    channelName = "";
    channels = [...channels, data.channel];
    selectedChannelID = data.channel.id;
    selectedDirectID = "";
    await loadMessages();
  }

  async function selectChannel(channelID: string) {
    selectedChannelID = channelID;
    selectedDirectID = "";
    selectedThread = null;
    selectedProfile = null;
    activeComposerContext = "message";
    mobileNavOpen = false;
    await loadMessages();
    void markChannelRead(channelID);
  }

  async function loadMessages() {
    captureScrollMemory();
    const targetKey = currentConversationKey();
    const isSwitching = targetKey !== viewKey;
    if (isSwitching) messagesLoading = true;
    try {
      if (selectedDirectID) {
        const data = await api<{ messages: Message[] }>(`/api/dms/${selectedDirectID}/messages`);
        if (currentConversationKey() !== targetKey) return;
        commitView(targetKey, data.messages);
        return;
      }
      if (!selectedChannelID) {
        commitView("", []);
        return;
      }
      const data = await api<{ messages: Message[] }>(`/api/channels/${selectedChannelID}/messages`);
      if (currentConversationKey() !== targetKey) return;
      commitView(targetKey, data.messages);
    } finally {
      if (currentConversationKey() === targetKey) messagesLoading = false;
    }
  }

  function currentConversationKey(): string {
    return selectedDirectID || selectedChannelID || "";
  }

  function maxChannelSeq(channelID: string): number {
    let max = 0;
    for (const m of messages) {
      if (m.channel_id !== channelID) continue;
      if (m.parent_message_id) continue;
      if (typeof m.channel_seq === "number" && m.channel_seq > max) max = m.channel_seq;
    }
    return max;
  }

  function maxDirectSeq(conversationID: string): number {
    let max = 0;
    for (const m of messages) {
      if (m.direct_conversation_id !== conversationID) continue;
      if (typeof m.channel_seq === "number" && m.channel_seq > max) max = m.channel_seq;
    }
    return max;
  }

  async function markChannelRead(channelID: string) {
    const channel = channels.find((c) => c.id === channelID);
    if (!channel) return;
    const seq = Math.max(channel.last_seq || 0, maxChannelSeq(channelID));
    if (seq <= 0 || seq <= (channel.last_read_seq || 0)) {
      // Still optimistically zero local unread count.
      if ((channel.unread_count || 0) > 0) {
        channels = channels.map((c) => (c.id === channelID ? { ...c, unread_count: 0 } : c));
      }
      return;
    }
    channels = channels.map((c) =>
      c.id === channelID ? { ...c, unread_count: 0, last_read_seq: seq } : c,
    );
    try {
      await api(`/api/channels/${channelID}/read`, { method: "POST", body: JSON.stringify({ seq }) });
    } catch {
      // Ignore — channel may be archived/inaccessible.
    }
  }

  async function markDirectRead(conversationID: string) {
    const dm = directConversations.find((c) => c.id === conversationID);
    if (!dm) return;
    const seq = Math.max(dm.last_seq || 0, maxDirectSeq(conversationID));
    if (seq <= 0 || seq <= (dm.last_read_seq || 0)) {
      if ((dm.unread_count || 0) > 0) {
        directConversations = directConversations.map((c) =>
          c.id === conversationID ? { ...c, unread_count: 0 } : c,
        );
      }
      return;
    }
    directConversations = directConversations.map((c) =>
      c.id === conversationID ? { ...c, unread_count: 0, last_read_seq: seq } : c,
    );
    try {
      await api(`/api/dms/${conversationID}/read`, { method: "POST", body: JSON.stringify({ seq }) });
    } catch {
      // Ignore.
    }
  }

  function markActiveViewRead() {
    if (selectedDirectID) {
      void markDirectRead(selectedDirectID);
      return;
    }
    if (selectedChannelID) {
      void markChannelRead(selectedChannelID);
    }
  }

  function captureScrollMemory() {
    if (!viewKey || !messageList) return;
    const captured = messageList.captureState();
    if (captured) scrollMemory.set(viewKey, captured);
  }

  function commitView(key: string, msgs: Message[]) {
    // Update viewKey + messages atomically so MessageList sees the swap as one tick.
    const switchingView = key !== viewKey;
    viewRestoreState = scrollMemory.get(key);
    // Preserve outgoing optimistic placeholders for this view that the server
    // hasn't echoed yet. Without this the placeholder would flicker out when a
    // sibling realtime event triggers a reload mid-flight.
    const localOptimistic = messages.filter(
      (m) => (m.status === "pending" || m.status === "failed") && belongsToView(m, key),
    );
    const localByID = new Map(localOptimistic.map((m) => [m.id, m]));
    const localByNonce = new Map(localOptimistic.filter((m) => m.nonce).map((m) => [m.nonce, m]));
    const merged = msgs.map((m) => {
      const local = localByID.get(m.id) || (m.nonce ? localByNonce.get(m.nonce) : undefined);
      if (!local) return m;
      if (m.nonce && pendingDrafts.has(m.nonce)) {
        return {
          ...m,
          nonce: local.nonce,
          status: local.status,
          attachments: local.attachments?.length ? local.attachments : m.attachments,
        };
      }
      return {
        ...m,
        nonce: local.nonce,
        attachments: local.attachments?.length ? local.attachments : m.attachments,
      };
    });
    const knownIDs = new Set(merged.map((m) => m.id));
    const knownNonces = new Set(merged.map((m) => m.nonce).filter(Boolean));
    const preserve = messages.filter(
      (m) =>
        (m.status === "pending" || m.status === "failed") &&
        m.id.startsWith("tmp_") &&
        !knownIDs.has(m.id) &&
        !(m.nonce && knownNonces.has(m.nonce)) &&
        belongsToView(m, key),
    );
    messages = preserve.length > 0 ? [...merged, ...preserve] : merged;
    viewKey = key;
    if (switchingView) {
      typingEntries = [];
      stopTyping();
    }
  }

  function belongsToView(message: Message, key: string): boolean {
    if (!key) return false;
    return message.channel_id === key || message.direct_conversation_id === key;
  }

  async function scrollMessagesToBottom() {
    await tick();
    messageList?.scrollToBottom();
  }

  type OutgoingDraft = {
    body: string;
    quotedMessageID?: string;
    upload?: Upload;
    workspaceID: string;
    channelID?: string;
    directConversationID?: string;
    viewKey: string;
  };

  let pendingDrafts = new Map<string, OutgoingDraft>();

  function newNonce(): string {
    if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
      return crypto.randomUUID().replace(/-/g, "");
    }
    return `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 10)}`;
  }

  function buildOptimisticMessage(nonce: string, draft: OutgoingDraft, id = `tmp_${nonce}`): Message {
    const now = new Date().toISOString();
    return {
      id,
      workspace_id: draft.workspaceID,
      channel_id: draft.channelID,
      direct_conversation_id: draft.directConversationID,
      author_id: user?.id || "",
      thread_root_id: id,
      body: draft.body,
      body_format: "markdown",
      created_at: now,
      author: user || undefined,
      attachments: draft.upload ? [draft.upload] : [],
      quoted_message_id: draft.quotedMessageID,
      nonce,
      status: "pending",
    };
  }

  async function sendMessage() {
    const body = messageBody.trim();
    if (!body) return;
    if (!selectedChannelID && !selectedDirectID) {
      status = "pick or create a channel";
      return;
    }
    stopTyping();
    const activeContext: "channel" | "dm" = selectedDirectID ? "dm" : "channel";
    const quote = replyTarget && replyContext === activeContext ? replyTarget : null;
    const draft: OutgoingDraft = {
      body,
      quotedMessageID: quote?.id,
      upload: pendingUpload || undefined,
      workspaceID: selectedWorkspaceID,
      channelID: selectedChannelID || undefined,
      directConversationID: selectedDirectID || undefined,
      viewKey: currentConversationKey(),
    };
    messageBody = "";
    if (quote) clearReplyTarget();
    pendingUpload = null;
    await dispatchDraft(draft);
  }

  async function dispatchDraft(draft: OutgoingDraft, existingNonce?: string, existingMessageID?: string) {
    const nonce = existingNonce ?? newNonce();
    const tmpID = `tmp_${nonce}`;
    const localID = existingMessageID ?? tmpID;
    pendingDrafts.set(nonce, draft);
    const placeholder = buildOptimisticMessage(nonce, draft, localID);
    if (existingNonce) {
      messages = messages.map((m) => (m.id === localID ? placeholder : m));
    } else if (currentConversationKey() === draft.viewKey) {
      messages = [...messages, placeholder];
      void scrollMessagesToBottom();
    }
    const path = draft.directConversationID
      ? `/api/dms/${draft.directConversationID}/messages`
      : `/api/channels/${draft.channelID}/messages`;
    const payload: Record<string, unknown> = { body: draft.body, nonce };
    if (draft.quotedMessageID) payload.quoted_message_id = draft.quotedMessageID;
    try {
      const data = await api<{ message: Message }>(path, {
        method: "POST",
        body: JSON.stringify(payload),
      });
      let message = data.message;
      if (draft.upload) {
        try {
          await api(`/api/messages/${message.id}/attachments`, {
            method: "POST",
            body: JSON.stringify({ upload_id: draft.upload.id }),
          });
          message = {
            ...message,
            attachments: [...(message.attachments || []), draft.upload],
          };
        } catch (err) {
          console.warn("attachment failed", err);
          const failedMessage: Message = {
            ...message,
            nonce,
            status: "failed",
            attachments: draft.upload ? [...(message.attachments || []), draft.upload] : message.attachments,
          };
          messages = messages.map((m) => (m.id === localID ? failedMessage : m));
          return;
        }
      }
      pendingDrafts.delete(nonce);
      // Replace placeholder with the real message (or append if a concurrent
      // realtime reload already removed our placeholder).
      const tmpIndex = messages.findIndex((m) => m.id === localID);
      if (tmpIndex >= 0) {
        messages = messages.map((m) => (m.id === localID ? message : m));
      } else if (messages.some((m) => m.id === message.id)) {
        messages = messages.map((m) => (m.id === message.id ? message : m));
      } else if (
        belongsToView(message, currentConversationKey()) &&
        !messages.some((m) => m.id === message.id)
      ) {
        messages = [...messages, message];
      }
    } catch (err) {
      console.warn("send failed", err);
      messages = messages.map((m) =>
        m.id === localID ? { ...m, status: "failed" as const } : m,
      );
    }
  }

  function retryFailedMessage(message: Message) {
    if (!message.nonce) return;
    const draft = pendingDrafts.get(message.nonce);
    if (!draft) {
      // We lost the draft (e.g., page reload). Best we can do is reuse the
      // placeholder body — but our pending tracker is in-memory only, so we
      // simply discard.
      discardFailedMessage(message);
      return;
    }
    void dispatchDraft({ ...draft, viewKey: draft.viewKey }, message.nonce, message.id);
  }

  function discardFailedMessage(message: Message) {
    if (message.nonce) pendingDrafts.delete(message.nonce);
    messages = messages.filter((m) => m.id !== message.id);
  }

  async function openThread(message: Message) {
    selectedProfile = null;
    selectedThread = message;
    activeComposerContext = "thread";
    const data = await api<{ root: Message; replies: Message[]; thread_state: ThreadState }>(`/api/messages/${message.id}/thread`);
    selectedThread = data.root;
    replies = data.replies;
    selectedThreadState = data.thread_state;
  }

  async function sendReply() {
    const body = replyBody.trim();
    if (!body || !selectedThread) return;
    const quote = replyTarget && replyContext === "thread" ? replyTarget : null;
    replyBody = "";
    const payload: Record<string, unknown> = { body };
    if (quote) payload.quoted_message_id = quote.id;
    const data = await api<{ message: Message; thread_state: ThreadState }>(`/api/messages/${selectedThread.id}/thread/replies`, {
      method: "POST",
      body: JSON.stringify(payload)
    });
    if (quote) clearReplyTarget();
    if (!replies.some((reply) => reply.id === data.message.id)) {
      replies = [...replies, data.message];
    }
    selectedThreadState = data.thread_state;
  }

  function setReplyTarget(message: Message, context: "channel" | "dm" | "thread") {
    replyTarget = message;
    replyContext = context;
    activeComposerContext = context === "thread" ? "thread" : "message";
  }

  function isModalOpen(): boolean {
    return selectedImage !== null || showProfileSettings;
  }

  function activeComposerTarget(): HTMLTextAreaElement | null {
    if (activeComposerContext === "thread" && selectedThread && replyInput) return replyInput;
    return messageInput;
  }

  function clearReplyTarget() {
    replyTarget = null;
    replyContext = null;
  }

  async function jumpToQuotedMessage(message: Message) {
    const targetID = message.quoted_message_id;
    if (!targetID) return;
    const scrolled = messageList?.scrollToMessage(targetID) ?? false;
    if (!scrolled) return;
    await tick();
    const node = document.querySelector<HTMLElement>(`[data-message-id="${CSS.escape(targetID)}"]`);
    if (!node) return;
    node.classList.add("highlight");
    window.setTimeout(() => node.classList.remove("highlight"), 1500);
  }

  async function searchMessages() {
    if (!selectedWorkspaceID || !searchQuery.trim()) {
      searchResults = [];
      return;
    }
    const data = await api<{ results: SearchResult[] }>(
      `/api/search?workspace_id=${encodeURIComponent(selectedWorkspaceID)}&q=${encodeURIComponent(searchQuery.trim())}`
    );
    searchResults = data.results;
  }

  function resetSearch() {
    searchQuery = "";
    searchResults = [];
  }

  async function openSearchResult(result: SearchResult) {
    searchResults = [];
    if (result.message.channel_id) {
      selectedChannelID = result.message.channel_id;
      selectedDirectID = "";
      await loadMessages();
    }
    if (result.message.direct_conversation_id) {
      selectedDirectID = result.message.direct_conversation_id;
      selectedChannelID = "";
      await loadMessages();
    }
  }

  async function uploadFile(event: Event) {
    const input = event.currentTarget as HTMLInputElement;
    const file = input.files?.[0];
    if (!file || !selectedWorkspaceID) return;
    const probe = await probeMediaDimensions(file);
    const form = new FormData();
    form.set("workspace_id", selectedWorkspaceID);
    form.set("file", file);
    if (probe.width > 0) form.set("width", String(probe.width));
    if (probe.height > 0) form.set("height", String(probe.height));
    if (probe.durationMS > 0) form.set("duration_ms", String(probe.durationMS));
    const data = await api<{ upload: Upload }>("/api/uploads", { method: "POST", body: form });
    pendingUpload = data.upload;
    input.value = "";
  }

  async function loadDirectConversations() {
    if (!selectedWorkspaceID) return;
    const data = await api<{ conversations: DirectConversation[] }>(`/api/dms?workspace_id=${selectedWorkspaceID}`);
    directConversations = data.conversations;
  }

  async function createDirectConversation() {
    if (!selectedWorkspaceID || !directMemberID.trim()) return;
    const data = await api<{ conversation: DirectConversation }>("/api/dms", {
      method: "POST",
      body: JSON.stringify({ workspace_id: selectedWorkspaceID, member_ids: [directMemberID.trim()] })
    });
    directMemberID = "";
    directConversations = [...directConversations, data.conversation];
    selectedDirectID = data.conversation.id;
    selectedChannelID = "";
    selectedThread = null;
    selectedProfile = null;
    activeComposerContext = "message";
    mobileNavOpen = false;
    await loadMessages();
  }

  async function selectDirectConversation(conversationID: string) {
    selectedDirectID = conversationID;
    selectedChannelID = "";
    selectedThread = null;
    selectedProfile = null;
    activeComposerContext = "message";
    mobileNavOpen = false;
    await loadMessages();
    void markDirectRead(conversationID);
  }

  async function startDirectWithUser(memberID: string) {
    if (!selectedWorkspaceID || !memberID) return;
    const existing = directConversations.find((conversation) =>
      conversation.members.some((member) => member.id === memberID),
    );
    if (existing) {
      selectedDirectID = existing.id;
      selectedChannelID = "";
      selectedThread = null;
      selectedProfile = null;
      activeComposerContext = "message";
      mobileNavOpen = false;
      await loadMessages();
      void markDirectRead(existing.id);
      return;
    }
    const data = await api<{ conversation: DirectConversation }>("/api/dms", {
      method: "POST",
      body: JSON.stringify({ workspace_id: selectedWorkspaceID, member_ids: [memberID] })
    });
    directConversations = [...directConversations, data.conversation];
    selectedDirectID = data.conversation.id;
    selectedChannelID = "";
    selectedThread = null;
    selectedProfile = null;
    activeComposerContext = "message";
    mobileNavOpen = false;
    await loadMessages();
    void markDirectRead(data.conversation.id);
  }

  function connectRealtimeSocket() {
    socket?.close();
    socket = null;
    connected = false;
    if (!selectedWorkspaceID) return;
    socket = connectRealtime({
      workspaceID: selectedWorkspaceID,
      onEvent: (event) => void handleEvent(event),
      onStatusChange: (next) => (connected = next),
    });
  }

  async function handleEvent(event: RealtimeEvent) {
    if (event.type === "typing.started" || event.type === "typing.stopped") {
      handleTypingEvent(event);
      return;
    }
    if (event.type === "channel.read" || event.type === "dm.read") {
      handleReadEvent(event);
      return;
    }
    if ((event.type === "channel.created" || event.type === "channel.updated") && event.workspace_id === selectedWorkspaceID) {
      await loadChannels();
      return;
    }
    if (event.type === "message.created") {
      handleUnreadBump(event);
    }
    if (
      (event.channel_id === selectedChannelID || event.payload.direct_conversation_id === selectedDirectID) &&
      (event.type === "message.created" || event.type === "message.updated" || event.type === "message.deleted")
    ) {
      // Optimistic-send echo: if this is our own outgoing message, the HTTP
      // response will swap the placeholder; skip the reload to avoid a flicker.
      const echoNonce = event.payload.nonce;
      if (event.type === "message.created" && echoNonce && pendingDrafts.has(echoNonce)) {
        return;
      }
      await loadMessages();
      // If the new message is in the active view and we're at the bottom,
      // advance the read pointer — don't strand the user with a stale unread.
      if (event.type === "message.created" && messageList?.isAtBottom() !== false) {
        if (selectedChannelID && event.channel_id === selectedChannelID) {
          void markChannelRead(selectedChannelID);
        } else if (selectedDirectID && event.payload.direct_conversation_id === selectedDirectID) {
          void markDirectRead(selectedDirectID);
        }
      }
    }
    const rootID = event.payload.root_message_id || event.payload.message_id;
    if (selectedThread && rootID === selectedThread.id) {
      await openThread(selectedThread);
    }
  }

  function handleReadEvent(event: RealtimeEvent) {
    const payload = event.payload as Record<string, unknown>;
    const userID = typeof payload.user_id === "string" ? payload.user_id : "";
    if (!userID || userID !== user?.id) return;
    const seqRaw = event.seq ?? payload.last_read_seq ?? payload.seq;
    const seq = typeof seqRaw === "number" ? seqRaw : Number(seqRaw) || 0;
    if (event.type === "channel.read") {
      const channelID = typeof payload.channel_id === "string" ? payload.channel_id : event.channel_id || "";
      if (!channelID) return;
      channels = channels.map((c) => {
        if (c.id !== channelID) return c;
        const next = Math.max(c.last_read_seq || 0, seq);
        return { ...c, last_read_seq: next, unread_count: Math.max(0, (c.last_seq || 0) - next) };
      });
    } else {
      const dmID = typeof payload.direct_conversation_id === "string" ? payload.direct_conversation_id : "";
      if (!dmID) return;
      directConversations = directConversations.map((c) => {
        if (c.id !== dmID) return c;
        const next = Math.max(c.last_read_seq || 0, seq);
        return { ...c, last_read_seq: next, unread_count: Math.max(0, (c.last_seq || 0) - next) };
      });
    }
  }

  function handleUnreadBump(event: RealtimeEvent) {
    const payload = event.payload as Record<string, unknown>;
    // Don't bump for own messages.
    const authorID = typeof payload.author_id === "string" ? payload.author_id : "";
    if (authorID && authorID === user?.id) return;
    // Threaded replies don't affect channel unread (channel_seq isn't assigned).
    if (payload.parent_message_id) return;
    const seqRaw = event.seq ?? payload.channel_seq ?? payload.seq;
    const seq = typeof seqRaw === "number" ? seqRaw : Number(seqRaw) || 0;
    const channelID = event.channel_id || (typeof payload.channel_id === "string" ? payload.channel_id : "");
    const dmID = typeof payload.direct_conversation_id === "string" ? payload.direct_conversation_id : "";
    if (channelID) {
      channels = channels.map((c) => {
        if (c.id !== channelID) return c;
        const lastSeq = seq > 0 ? Math.max(c.last_seq || 0, seq) : (c.last_seq || 0) + 1;
        const isActive = channelID === selectedChannelID && !selectedDirectID;
        const unread =
          isActive && messageList?.isAtBottom() !== false
            ? c.unread_count || 0
            : Math.max(0, lastSeq - (c.last_read_seq || 0));
        return { ...c, last_seq: lastSeq, unread_count: unread };
      });
    } else if (dmID) {
      directConversations = directConversations.map((c) => {
        if (c.id !== dmID) return c;
        const lastSeq = seq > 0 ? Math.max(c.last_seq || 0, seq) : (c.last_seq || 0) + 1;
        const isActive = dmID === selectedDirectID;
        const unread =
          isActive && messageList?.isAtBottom() !== false
            ? c.unread_count || 0
            : Math.max(0, lastSeq - (c.last_read_seq || 0));
        return { ...c, last_seq: lastSeq, unread_count: unread };
      });
    }
  }

  function handleTypingEvent(event: RealtimeEvent) {
    const payload = event.payload as Record<string, unknown>;
    const userID = typeof payload.user_id === "string" ? payload.user_id : "";
    if (!userID || userID === user?.id) return;
    const eventChannel = event.channel_id || (typeof payload.channel_id === "string" ? payload.channel_id : "");
    const eventDM = typeof payload.direct_conversation_id === "string" ? payload.direct_conversation_id : "";
    const matchesView =
      (selectedChannelID && eventChannel === selectedChannelID) ||
      (selectedDirectID && eventDM === selectedDirectID);
    if (!matchesView) return;
    if (event.type === "typing.stopped") {
      typingEntries = typingEntries.filter((entry) => entry.userID !== userID);
      return;
    }
    const author = lookupUser(userID);
    const next = typingEntries.filter((entry) => entry.userID !== userID);
    next.push({ userID, user: author, expiresAt: Date.now() + TYPING_TTL_MS });
    typingEntries = next;
    ensureTypingSweeper();
  }

  function lookupUser(userID: string): User | undefined {
    if (user?.id === userID) return user;
    const fromMessages = messages.find((msg) => msg.author?.id === userID)?.author;
    if (fromMessages) return fromMessages;
    for (const dm of directConversations) {
      const member = dm.members.find((m) => m.id === userID);
      if (member) return member;
    }
    return undefined;
  }

  function ensureTypingSweeper() {
    if (typingSweeper) return;
    typingSweeper = window.setInterval(() => {
      const now = Date.now();
      const next = typingEntries.filter((entry) => entry.expiresAt > now);
      if (next.length !== typingEntries.length) typingEntries = next;
      if (next.length === 0 && typingSweeper) {
        window.clearInterval(typingSweeper);
        typingSweeper = undefined;
      }
    }, 1000);
  }

  function notifyComposerTyping() {
    if (!selectedWorkspaceID) return;
    if (!selectedChannelID && !selectedDirectID) return;
    notifyTyping({
      workspaceID: selectedWorkspaceID,
      channelID: selectedChannelID || undefined,
      directConversationID: selectedDirectID || undefined,
    });
  }

  function openUserProfile(profile?: User | null) {
    if (!profile) return;
    selectedThread = null;
    selectedProfile = profile;
  }

  function handleComposerKey(event: KeyboardEvent) {
    if (event.key === "Escape" && replyTarget && replyContext !== "thread") {
      event.preventDefault();
      clearReplyTarget();
      return;
    }
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      void sendMessage();
    }
  }

  function handleReplyKey(event: KeyboardEvent) {
    if (event.key === "Escape" && replyTarget && replyContext === "thread") {
      event.preventDefault();
      clearReplyTarget();
      return;
    }
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      void sendReply();
    }
  }

  function openImageViewer(url: string, title: string) {
    selectedImage = { url, title };
  }

  function handleInlineImagePointerUp(event: PointerEvent) {
    const target = event.target;
    if (!(target instanceof HTMLImageElement)) return;
    if (!target.closest(".markdown")) return;
    event.preventDefault();
    openImageViewer(target.currentSrc || target.src, target.alt || "Image");
  }

  function appendToComposer(snippet: string) {
    const prefix = messageBody && !messageBody.endsWith("\n") ? "\n" : "";
    messageBody = `${messageBody}${prefix}${snippet}`;
  }

  function applyMarkdownWrap(before: string, after = before) {
    const placeholder = before === "```" ? "\ncode\n" : "text";
    appendToComposer(`${before}${placeholder}${after}`);
  }

  function pickGif(url: string, title: string) {
    appendToComposer(`![${title}](${url})`);
    showGifPicker = false;
    gifQuery = "";
  }

  function closeSidePanel() {
    if (replyContext === "thread") clearReplyTarget();
    selectedThread = null;
    selectedProfile = null;
    activeComposerContext = "message";
    replies = [];
  }

  function toggleSidePanelFromTopbar() {
    if (sidePanelOpen) closeSidePanel();
    else status = "pick a message to open its thread";
  }

  function closeModal() {
    selectedImage = null;
    showProfileSettings = false;
  }

  function closeMobileNav() {
    mobileNavOpen = false;
  }
</script>

<svelte:head>
  <meta name="color-scheme" content="light dark" />
</svelte:head>

<svelte:window
  onkeydown={(event) => {
    if (event.key === "Escape") {
      if (isModalOpen()) {
        closeModal();
      } else if (mobileNavOpen) {
        event.preventDefault();
        closeMobileNav();
        return;
      } else if (replyTarget) {
        event.preventDefault();
        clearReplyTarget();
        return;
      }
    }
    redirectTypingToComposer(event, {
      authRequired,
      isModalOpen: () => isModalOpen() || mobileNavOpen,
      messageInput,
      replyInput,
      target: activeComposerTarget,
    });
  }}
/>

{#if authRequired}
  <main class="auth-shell">
    <section class="auth-panel" aria-label="Sign in">
      <div class="auth-brand">
        <div class="mark">cc</div>
        <div class="brand-text">
          <strong>ClickClack</strong>
          <span>OpenClaw workspace chat</span>
        </div>
      </div>
      <div class="auth-copy">
        <h1>Welcome back.</h1>
        <p>Workspace chat for the OpenClaw crew. Sign in with the GitHub account that's a member of the org.</p>
      </div>
      <a class="github-login" href="/api/auth/github/start">
        <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">
          <path fill="currentColor" d="M12 .5C5.65.5.5 5.65.5 12c0 5.08 3.29 9.39 7.86 10.91.58.1.79-.25.79-.56v-2c-3.2.69-3.87-1.37-3.87-1.37-.52-1.32-1.27-1.67-1.27-1.67-1.04-.71.08-.7.08-.7 1.15.08 1.76 1.18 1.76 1.18 1.02 1.75 2.68 1.25 3.34.96.1-.74.4-1.25.73-1.54-2.55-.29-5.24-1.28-5.24-5.69 0-1.26.45-2.29 1.18-3.1-.12-.29-.51-1.46.11-3.05 0 0 .96-.31 3.15 1.18a10.94 10.94 0 0 1 5.74 0c2.19-1.49 3.15-1.18 3.15-1.18.62 1.59.23 2.76.12 3.05.74.81 1.18 1.84 1.18 3.1 0 4.42-2.69 5.39-5.25 5.68.41.36.78 1.06.78 2.13v3.16c0 .31.21.67.8.56 4.56-1.52 7.85-5.83 7.85-10.91C23.5 5.65 18.35.5 12 .5z"/>
        </svg>
        Continue with GitHub
      </a>
      <p class="auth-foot">Limited to active members of the OpenClaw org.</p>
    </section>
  </main>
{:else}
<div
  class="shell"
  class:nav-open={mobileNavOpen}
  class:sidebar-collapsed={sidebarCollapsed}
  class:thread-open={sidePanelOpen}
>
  <button
    class="mobile-nav-toggle"
    type="button"
    aria-label="Toggle navigation"
    aria-controls="workspace-navigation"
    aria-expanded={mobileNavOpen}
    onclick={() => (mobileNavOpen = !mobileNavOpen)}
  >
    {#if mobileNavOpen}&times;{:else}<span class="bars"><i></i><i></i><i></i></span>{/if}
  </button>

  {#if mobileNavOpen}
    <button
      type="button"
      class="mobile-nav-backdrop"
      aria-label="Close navigation"
      onclick={closeMobileNav}
    ></button>
  {/if}

  <GuildRail
    {workspaces}
    {selectedWorkspaceID}
    {workspaceName}
    {showWorkspaceCreate}
    onSelectWorkspace={(workspaceID) => void selectWorkspace(workspaceID)}
    onToggleWorkspaceCreate={() => (showWorkspaceCreate = !showWorkspaceCreate)}
    onWorkspaceName={(value) => (workspaceName = value)}
    onCreateWorkspace={() => void createWorkspace()}
  />

  <Sidebar
    workspaceName={selectedWorkspace?.name}
    {status}
    {connected}
    {sidebarCollapsed}
    {channels}
    {directConversations}
    {recentPeople}
    currentUser={user}
    {selectedChannelID}
    {selectedDirectID}
    {selectedProfile}
    {channelName}
    {directMemberID}
    onToggleCollapse={() => (sidebarCollapsed = !sidebarCollapsed)}
    onSelectChannel={(channelID) => void selectChannel(channelID)}
    onChannelName={(value) => (channelName = value)}
    onCreateChannel={() => void createChannel()}
    onSelectDirect={(conversationID) => void selectDirectConversation(conversationID)}
    onDirectMemberID={(value) => (directMemberID = value)}
    onCreateDirect={() => void createDirectConversation()}
    onOpenProfile={openUserProfile}
    onOpenSettings={openProfileSettings}
  />

  <main class="timeline" inert={mobileNavOpen}>
    <Topbar
      {selectedDirect}
      {selectedChannel}
      workspaceName={selectedWorkspace?.name}
      currentUserID={user?.id}
      {searchQuery}
      {sidePanelOpen}
      threadOpen={selectedThread !== null}
      onSearchQuery={(value) => (searchQuery = value)}
      onSearch={() => void searchMessages()}
      onResetSearch={resetSearch}
      onToggleThread={toggleSidePanelFromTopbar}
      onPinnedItems={() => (status = "no pinned items")}
    />

    <SearchResults
      results={searchResults}
      onClose={() => (searchResults = [])}
      onOpenResult={(result) => void openSearchResult(result)}
    />

    <MessageList
      {messages}
      {selectedDirect}
      {selectedChannel}
      restoreState={viewRestoreState}
      {viewKey}
      loading={messagesLoading}
      unreadCount={(selectedChannel?.unread_count || selectedDirect?.unread_count || 0)}
      selectedThreadID={selectedThread?.id}
      currentUserID={user?.id}
      onListRef={(handle) => (messageList = handle)}
      onActivateMessageComposer={() => (activeComposerContext = "message")}
      onInlineImagePointerUp={handleInlineImagePointerUp}
      onOpenProfile={openUserProfile}
      onReply={setReplyTarget}
      onOpenThread={openThread}
      onJumpToQuote={(message) => void jumpToQuotedMessage(message)}
      onOpenImage={openImageViewer}
      onReachedBottom={markActiveViewRead}
      onRetry={retryFailedMessage}
      onDiscard={discardFailedMessage}
    />

    <TypingIndicator entries={typingEntries} currentUserID={user?.id} />

    <ChatComposer
      value={messageBody}
      placeholder={selectedDirect ? `Message ${dmTitle(selectedDirect, user?.id)}` : selectedChannel ? `Message #${selectedChannel.name}` : "Pick a channel to start"}
      ariaLabel="Message body"
      submitLabel="Send"
      pendingUpload={pendingUpload}
      replyTarget={replyTarget && replyContext === (selectedDirectID ? "dm" : "channel") ? replyTarget : null}
      showUpload
      showToolbar
      showGifPicker={showGifPicker}
      gifQuery={gifQuery}
      filteredGifs={filteredGifs}
      onValue={(value) => {
        const previous = messageBody;
        messageBody = value;
        if (value.trim() && value !== previous) notifyComposerTyping();
        else if (!value.trim()) stopTyping();
      }}
      onSubmit={() => void sendMessage()}
      onKeydown={handleComposerKey}
      onFocus={() => (activeComposerContext = "message")}
      onInputRef={(node) => (messageInput = node)}
      onUploadFile={uploadFile}
      onRemoveUpload={() => (pendingUpload = null)}
      onClearReply={clearReplyTarget}
      onApplyMarkdownWrap={applyMarkdownWrap}
      onAppendToComposer={appendToComposer}
      onToggleGif={() => (showGifPicker = !showGifPicker)}
      onGifQuery={(value) => (gifQuery = value)}
      onPickGif={pickGif}
    />
  </main>

  <aside
    class="thread"
    class:open={sidePanelOpen}
    inert={mobileNavOpen}
    aria-label={selectedProfile ? "Profile pane" : "Thread pane"}
  >
    {#if selectedThread}
      <ThreadPanel
        root={selectedThread}
        {replies}
        threadState={selectedThreadState}
        {replyBody}
        replyTarget={replyTarget && replyContext === "thread" ? replyTarget : null}
        onClose={closeSidePanel}
        onReplyBody={(value) => (replyBody = value)}
        onSubmitReply={() => void sendReply()}
        onReplyKeydown={handleReplyKey}
        onReplyFocus={() => (activeComposerContext = "thread")}
        onReplyInputRef={(node) => (replyInput = node)}
        onSetReplyTarget={setReplyTarget}
        onClearReply={clearReplyTarget}
        onActivateThreadComposer={() => (activeComposerContext = "thread")}
        onInlineImagePointerUp={handleInlineImagePointerUp}
        onJumpToQuote={(message) => void jumpToQuotedMessage(message)}
        onOpenImage={openImageViewer}
      />
    {:else if selectedProfile}
      <ProfilePane
        profile={selectedProfile}
        currentUser={user}
        workspaceName={selectedWorkspace?.name}
        onClose={closeSidePanel}
        onEdit={openProfileSettings}
        onMessage={(memberID) => void startDirectWithUser(memberID)}
        onSetStatus={() => (status = "status messages are coming soon")}
      />
    {:else}
      <ThreadEmptyState />
    {/if}
  </aside>
</div>
{#if showProfileSettings && user}
  <ProfileSettingsModal
    {user}
    displayName={profileDisplayName}
    handle={profileHandle}
    avatarURL={profileAvatarURL}
    status={profileStatus}
    statusError={profileStatusError}
    onDisplayName={(value) => (profileDisplayName = value)}
    onHandle={(value) => (profileHandle = value)}
    onAvatarURL={(value) => (profileAvatarURL = value)}
    onClose={closeModal}
    onSave={() => void saveProfile()}
  />
{/if}
{#if selectedImage}
  <ImageViewer url={selectedImage.url} title={selectedImage.title} onClose={closeModal} />
{/if}
{/if}
