<script lang="ts">
  import { goto } from "$app/navigation";
  import { onDestroy, onMount, tick } from "svelte";
  import { APIError, api } from "./lib/api";
  import { probeMediaDimensions } from "./lib/media";
  import { gifLibrary } from "./lib/gifs";
  import { markdownImageViewerURL } from "./lib/actions/markdownGifs";
  import {
    INITIAL_MESSAGE_LIMIT,
    MAX_RETAINED_MESSAGE_WINDOWS,
    MAX_RETAINED_SCROLL_STATES,
    PAGE_MESSAGE_LIMIT,
    trimMessageWindow as trimMessageWindowMessages,
    type MessageWindowDirection,
  } from "./lib/chat/messageWindow";
  import { collectRecentPeople, dmTitle } from "./lib/chat/people";
  import { redirectTypingToComposer, rememberTypeToFocusPointer } from "./lib/chat/typeToFocus";
  import { connectRealtime, type RealtimeConnection } from "./lib/realtime.svelte";
  import { notifyTyping, stopTyping } from "./lib/typing";
  import ChatComposer from "./components/composer/ChatComposer.svelte";
  import ImageViewer from "./components/media/ImageViewer.svelte";
  import MessageList, {
    type MessageListHandle,
    type MessageListState,
    type MessageListViewportState,
  } from "./components/messages/MessageList.svelte";
  import TypingIndicator, { TYPING_TTL_MS, type TypingEntry } from "./components/messages/TypingIndicator.svelte";
  import AgentProgress, { AGENT_PROGRESS_TTL_MS, type AgentProgressTurn } from "./components/messages/AgentProgress.svelte";
  import CreateChannelModal from "./components/navigation/CreateChannelModal.svelte";
  import CreateDirectModal from "./components/navigation/CreateDirectModal.svelte";
  import GuildRail from "./components/navigation/GuildRail.svelte";
  import Sidebar from "./components/navigation/Sidebar.svelte";
  import ProfilePane from "./components/profile/ProfilePane.svelte";
  import ProfileSettingsModal from "./components/profile/ProfileSettingsModal.svelte";
  import SearchResults from "./components/search/SearchResults.svelte";
  import ThreadEmptyState from "./components/thread/ThreadEmptyState.svelte";
  import ThreadPanel from "./components/thread/ThreadPanel.svelte";
  import Topbar from "./components/topbar/Topbar.svelte";
  import type { Channel, DirectConversation, MemberModeration, Message, MessagePage, RealtimeEvent, RouteTarget, SearchResult, SlashCommand, ThreadState, Upload, User, Workspace } from "./lib/types";

  const LIVE_EDGE_TOLERANCE_PX = 96;
  const LAST_CHANNEL_STORAGE_PREFIX = "clickclack:last-channel:v1:";
  const BROWSER_NOTIFICATIONS_STORAGE_PREFIX = "clickclack:browser-notifications-enabled:v1:";
  const MOBILE_NAV_MEDIA_QUERY = "(max-width: 820px)";

  export let routeWorkspaceID = "";
  export let routeTargetID = "";

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
  let moderationMembers: MemberModeration[] = [];
  let slashCommands: SlashCommand[] = [];
  let mentionPeople: User[] = [];
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
  let showCreateChannel = false;
  let showCreateDirect = false;
  let gifQuery = "";
  let profileDisplayName = "";
  let profileHandle = "";
  let profileAvatarURL = "";
  let profilePushoverEnabled = false;
  let profilePushoverUserKey = "";
  let browserNotificationsSupported = false;
  let browserNotificationsEnabled = false;
  let browserNotificationPermission: NotificationPermission | "unsupported" = "default";
  let profileStatus = "";
  let profileStatusError = false;
  let status = "loading";
  let authRequired = false;
  let connected = false;
  let socket: RealtimeConnection | null = null;
  let messageList: MessageListHandle | null = null;
  let scrollMemory = new Map<string, MessageListState>();
  let messageWindows = new Map<string, MessageWindow>();
  let loadingMessagePages = new Set<string>();
  let olderPageState: HistoryEdgeState = "idle";
  let newerPageState: HistoryEdgeState = "idle";
  let pendingOlderPageIntent = false;
  let pendingNewerPageIntent = false;
  let activeHasOlder = false;
  let activeHasNewer = false;
  let activeLoadingOlder = false;
  let activeLoadingNewer = false;
  let unreadMarkers = new Map<string, UnreadMarker>();
  let suppressAutoReadUntil = 0;
  let viewKey = "";
  let viewRestoreState: MessageListState | undefined = undefined;
  let activeConversationKey = "";
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
  let agentProgressTurns: AgentProgressTurn[] = [];
  let agentProgressSweeper: number | undefined;
  let appliedRouteKey = "";
  let routeApplySerial = 0;

  type MessageWindow = Omit<MessagePage, "messages"> & {
    messages: Message[];
  };

  type HistoryEdgeState = "idle" | "loading" | "settling";
  type UnreadMarker = {
    boundarySeq: number;
    since: string;
  };
  type StoredLastChannel = {
    id?: string;
    routeID?: string;
  };

  $: selectedWorkspace = workspaces.find((workspace) => workspace.id === selectedWorkspaceID);
  $: currentWorkspaceRole = selectedWorkspace?.role || "";
  $: selectedProfileModeration = selectedProfile
    ? moderationMembers.find((member) => member.user.id === selectedProfile?.id)
    : undefined;
  $: selectedChannel = channels.find((channel) => channel.id === selectedChannelID);
  $: selectedDirect = directConversations.find((conversation) => conversation.id === selectedDirectID);
  $: activeConversationKey = selectedDirectID || selectedChannelID || "";
  $: activeUnreadState = selectedDirectID
    ? directConversations.find((conversation) => conversation.id === selectedDirectID) || {}
    : selectedChannelID
      ? channels.find((channel) => channel.id === selectedChannelID) || {}
      : {};
  $: activeUnreadCount = unreadCountForKey(activeConversationKey, activeUnreadState);
  $: activeUnreadBoundarySeq = activeUnreadCount > 0 ? activeUnreadState.last_read_seq || 0 : 0;
  $: activeUnreadBoundaryLoaded = activeUnreadCount > 0
    ? unreadBoundaryLoadedForKey(activeConversationKey, activeUnreadBoundarySeq, messageWindows)
    : false;
  $: activeUnreadSince = activeUnreadCount > 0
    ? unreadSinceForKey(activeConversationKey, activeUnreadBoundarySeq, messageWindows)
    : "";
  $: sidePanelOpen = selectedThread !== null || selectedProfile !== null;
  $: recentPeople = collectRecentPeople(messages, directConversations, user?.id || "");
  $: mentionPeople = collectMentionPeople(user, recentPeople, moderationMembers, selectedDirect);
  $: if (replyContext === "channel" && replyTarget && !messages.some((m) => m.id === replyTarget?.id)) clearReplyTarget();
  $: if (replyContext === "dm" && replyTarget && !messages.some((m) => m.id === replyTarget?.id)) clearReplyTarget();
  $: if (replyContext === "thread" && replyTarget && selectedThread && replyTarget.id !== selectedThread.id && !replies.some((r) => r.id === replyTarget?.id)) clearReplyTarget();
  $: if (status === "ready" && user && routeKey(routeWorkspaceID, routeTargetID) !== appliedRouteKey) {
    void applyRoute(routeWorkspaceID, routeTargetID);
  }
  $: filteredGifs = showGifPicker
    ? gifLibrary.filter((gif) => {
        const query = gifQuery.trim().toLowerCase();
        return !query || gif.title.toLowerCase().includes(query) || gif.tags.some((tag) => tag.includes(query));
      })
    : [];

  onMount(() => {
    syncBrowserNotificationState();
    void boot();
    const mobileNavMedia = window.matchMedia(MOBILE_NAV_MEDIA_QUERY);
    const handleMobileNavBreakpoint = () => {
      mobileNavOpen = false;
    };
    mobileNavMedia.addEventListener("change", handleMobileNavBreakpoint);
    return () => mobileNavMedia.removeEventListener("change", handleMobileNavBreakpoint);
  });

  onDestroy(() => {
    socket?.close();
    socket = null;
    connected = false;
    stopTyping();
    if (typingSweeper) window.clearInterval(typingSweeper);
    if (agentProgressSweeper) window.clearInterval(agentProgressSweeper);
  });

  async function boot() {
    try {
      const me = await api<{ user: User }>("/api/me");
      user = me.user;
      syncBrowserNotificationState();
      await loadWorkspaces();
      if (workspaces.length === 0) {
        status = "create a workspace";
        return;
      }
      await applyRoute(routeWorkspaceID, routeTargetID);
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
    syncBrowserNotificationState();
    profileDisplayName = user.display_name;
    profileHandle = user.handle ? `@${user.handle}` : "";
    profileAvatarURL = user.avatar_url;
    profilePushoverEnabled = user.notification_settings?.pushover_enabled ?? false;
    profilePushoverUserKey = user.notification_settings?.pushover_user_key ?? "";
    profileStatus = "";
    profileStatusError = false;
    showProfileSettings = true;
  }

  function syncBrowserNotificationState() {
    browserNotificationsSupported = typeof Notification !== "undefined";
    browserNotificationPermission = browserNotificationsSupported ? Notification.permission : "unsupported";
    const storedEnabled = storedBrowserNotificationsEnabled();
    browserNotificationsEnabled = browserNotificationPermission === "granted" && storedEnabled;
    if (storedEnabled && browserNotificationPermission !== "granted") {
      storeBrowserNotificationsEnabled(false);
    }
  }

  function browserNotificationsStorageKey(): string {
    return user?.id ? `${BROWSER_NOTIFICATIONS_STORAGE_PREFIX}${user.id}` : "";
  }

  function storedBrowserNotificationsEnabled(): boolean {
    const key = browserNotificationsStorageKey();
    if (!key) return false;
    try {
      return window.localStorage.getItem(key) === "enabled";
    } catch {
      return false;
    }
  }

  function storeBrowserNotificationsEnabled(enabled: boolean): boolean {
    const key = browserNotificationsStorageKey();
    if (!key) return false;
    try {
      if (enabled) {
        window.localStorage.setItem(key, "enabled");
      } else {
        window.localStorage.removeItem(key);
      }
      return true;
    } catch {
      return false;
    }
  }

  async function setBrowserNotificationsEnabled(enabled: boolean) {
    profileStatus = "";
    profileStatusError = false;
    if (!enabled) {
      storeBrowserNotificationsEnabled(false);
      browserNotificationsEnabled = false;
      profileStatus = "Browser notifications disabled";
      return;
    }
    if (typeof Notification === "undefined") {
      browserNotificationsSupported = false;
      browserNotificationPermission = "unsupported";
      browserNotificationsEnabled = false;
      profileStatus = "Browser notifications are not supported";
      profileStatusError = true;
      return;
    }
    const permission =
      Notification.permission === "default" ? await Notification.requestPermission() : Notification.permission;
    browserNotificationsSupported = true;
    browserNotificationPermission = permission;
    if (permission === "granted") {
      browserNotificationsEnabled = storeBrowserNotificationsEnabled(true);
      profileStatus = browserNotificationsEnabled
        ? "Browser notifications enabled"
        : "Browser notification preference could not be saved";
      profileStatusError = !browserNotificationsEnabled;
      return;
    }
    storeBrowserNotificationsEnabled(false);
    browserNotificationsEnabled = false;
    profileStatus = permission === "denied"
      ? "Browser notifications are blocked by this browser"
      : "Browser notifications were not enabled";
    profileStatusError = true;
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
          notification_settings: {
            pushover_enabled: profilePushoverEnabled,
            pushover_user_key: profilePushoverUserKey,
          },
        }),
      });
      user = data.user;
      setActiveMessages(messages.map((message) =>
        message.author?.id === user?.id ? { ...message, author: data.user } : message,
      ));
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
  }

  function routeKey(workspaceID = "", targetID = ""): string {
    return `${workspaceID || ""}/${targetID || ""}`;
  }

  function appHref(workspaceID = selectedWorkspaceID, targetID = ""): string {
    const workspaceRouteID = routeWorkspaceIDFor(workspaceID);
    if (!workspaceRouteID) return "/app";
    const workspacePath = `/app/${encodeURIComponent(workspaceRouteID)}`;
    const targetRouteID = routeTargetIDFor(targetID);
    return targetRouteID ? `${workspacePath}/${encodeURIComponent(targetRouteID)}` : workspacePath;
  }

  function routeWorkspaceIDFor(workspaceID = selectedWorkspaceID): string {
    if (!workspaceID) return "";
    return workspaces.find((workspace) => workspace.id === workspaceID || workspace.route_id === workspaceID)?.route_id || workspaceID;
  }

  function routeTargetIDFor(targetID = ""): string {
    if (!targetID) return "";
    return channels.find((channel) => channel.id === targetID || channel.route_id === targetID)?.route_id ||
      directConversations.find((conversation) => conversation.id === targetID || conversation.route_id === targetID)?.route_id ||
      (selectedThread?.id === targetID ? selectedThread.route_id || "" : "") ||
      messages.find((message) => message.id === targetID)?.route_id ||
      targetID;
  }

  async function navigateToApp(workspaceID = selectedWorkspaceID, targetID = "", replaceState = false) {
    const path = appHref(workspaceID, targetID);
    if (window.location.pathname === path) return;
    await goto(path, { replaceState, noScroll: true, keepFocus: true });
  }

  function clearRoutePanelState() {
    selectedThread = null;
    selectedThreadState = null;
    selectedProfile = null;
    activeComposerContext = "message";
    replies = [];
    mobileNavOpen = false;
  }

  function defaultTargetID(workspaceID = selectedWorkspaceID): string {
    return storedLastChannelID(workspaceID) ||
      channels.find((channel) => channel.name.toLowerCase() === "guest")?.id ||
      channels[0]?.id ||
      directConversations[0]?.id ||
      "";
  }

  function workspaceForID(workspaceID = selectedWorkspaceID): Workspace | undefined {
    return workspaces.find((workspace) => workspace.id === workspaceID || workspace.route_id === workspaceID);
  }

  function lastChannelStorageKey(workspaceID = selectedWorkspaceID): string {
    const workspace = workspaceForID(workspaceID);
    const keyID = workspace?.route_id || workspace?.id || workspaceID;
    return keyID ? `${LAST_CHANNEL_STORAGE_PREFIX}${keyID}` : "";
  }

  function parseStoredLastChannel(raw: string): StoredLastChannel {
    try {
      const parsed = JSON.parse(raw) as StoredLastChannel;
      return {
        id: typeof parsed.id === "string" ? parsed.id : "",
        routeID: typeof parsed.routeID === "string" ? parsed.routeID : "",
      };
    } catch {
      return { id: raw };
    }
  }

  function storedLastChannelID(workspaceID = selectedWorkspaceID): string {
    const key = lastChannelStorageKey(workspaceID);
    if (!key) return "";
    let stored: StoredLastChannel;
    try {
      const raw = window.localStorage.getItem(key);
      if (!raw) return "";
      stored = parseStoredLastChannel(raw);
    } catch {
      return "";
    }
    const channel = channels.find((candidate) =>
      candidate.id === stored.id || candidate.route_id === stored.routeID,
    );
    if (channel) return channel.id;
    try {
      window.localStorage.removeItem(key);
    } catch {
      // Ignore unavailable storage; falling back to normal channel order is safe.
    }
    return "";
  }

  function rememberLastChannel(workspaceID: string, channelID: string) {
    if (!workspaceID || !channelID) return;
    const channel = channels.find((candidate) => candidate.id === channelID);
    if (!channel) return;
    const key = lastChannelStorageKey(workspaceID);
    if (!key) return;
    try {
      window.localStorage.setItem(key, JSON.stringify({ id: channel.id, routeID: channel.route_id }));
    } catch {
      // Ignore unavailable storage; explicit routed URLs still restore the view.
    }
  }

  async function applyRoute(workspaceIDParam = "", targetIDParam = "") {
    const serial = ++routeApplySerial;
    const requestedRouteKey = routeKey(workspaceIDParam, targetIDParam);
    const routeTarget = targetIDParam.trim()
      ? await resolveRouteTarget(workspaceIDParam, targetIDParam)
      : null;
    if (serial !== routeApplySerial) return;
    const workspace = routeTarget
      ? workspaces.find((candidate) => candidate.id === routeTarget.workspace_id)
      : workspaces.find((candidate) => candidate.id === workspaceIDParam || candidate.route_id === workspaceIDParam) || workspaces[0];
    if (!workspace) {
      commitMessageWindow("", pageToWindow({ messages: [], oldest_seq: 0, newest_seq: 0, has_older: false, has_newer: false }), "replace");
      appliedRouteKey = requestedRouteKey;
      return;
    }
    const canonicalRouteKey = routeTarget
      ? routeKey(routeTarget.workspace_route_id, routeTarget.target_route_id)
      : routeKey(workspace.route_id, "");

    const workspaceChanged = selectedWorkspaceID !== workspace.id;
    if (workspaceChanged) {
      captureScrollMemory();
      selectedWorkspaceID = workspace.id;
      selectedChannelID = "";
      selectedDirectID = "";
      selectedThread = null;
      selectedThreadState = null;
      selectedProfile = null;
      activeComposerContext = "message";
      replies = [];
      resetSearch();
      resetHistoryPaging();
      messagesLoading = true;
      connectRealtimeSocket();
    }

    if (workspaceChanged || channels.length === 0) await loadChannels(false, false);
    if (serial !== routeApplySerial) return;
    if (workspaceChanged || directConversations.length === 0) await loadDirectConversations();
    if (workspaceChanged) await Promise.all([loadModerationMembers(), loadSlashCommands()]);
    if (serial !== routeApplySerial) return;

    if (routeTarget) {
      const routeTargetAvailable = await ensureResolvedRouteTargetLoaded(routeTarget, serial);
      if (serial !== routeApplySerial) return;
      if (!routeTargetAvailable) {
        clearRoutePanelState();
        await navigateToApp(workspace.id, defaultTargetID(), true);
        return;
      }
    }

    if (routeTarget?.canonical_path && window.location.pathname !== routeTarget.canonical_path) {
      appliedRouteKey = canonicalRouteKey;
      await goto(routeTarget.canonical_path, { replaceState: true, noScroll: true, keepFocus: true });
      if (serial !== routeApplySerial) return;
    }

    if (routeTarget?.target_type === "channel" && channels.some((channel) => channel.id === routeTarget.target_id)) {
      const targetID = routeTarget.target_id;
      const sameConversation =
        !workspaceChanged && selectedChannelID === targetID && !selectedDirectID && viewKey === targetID;
      selectedChannelID = targetID;
      selectedDirectID = "";
      rememberLastChannel(workspace.id, targetID);
      clearRoutePanelState();
      if (sameConversation) {
        appliedRouteKey = canonicalRouteKey;
        updateActiveMessageWindowFlags(targetID);
        return;
      }
      await loadMessages();
      if (serial !== routeApplySerial) return;
      appliedRouteKey = canonicalRouteKey;
      return;
    }

    if (routeTarget?.target_type === "direct" && directConversations.some((conversation) => conversation.id === routeTarget.target_id)) {
      const targetID = routeTarget.target_id;
      const sameConversation =
        !workspaceChanged && selectedDirectID === targetID && !selectedChannelID && viewKey === targetID;
      selectedDirectID = targetID;
      selectedChannelID = "";
      clearRoutePanelState();
      if (sameConversation) {
        appliedRouteKey = canonicalRouteKey;
        updateActiveMessageWindowFlags(targetID);
        return;
      }
      await loadMessages();
      if (serial !== routeApplySerial) return;
      appliedRouteKey = canonicalRouteKey;
      return;
    }

    if (routeTarget?.target_type === "thread") {
      const resolved = await applyThreadRoute(routeTarget);
      if (serial !== routeApplySerial) return;
      if (resolved) {
        appliedRouteKey = canonicalRouteKey;
        return;
      }
    }

    const fallbackTargetID = defaultTargetID();
    clearRoutePanelState();
    if (!fallbackTargetID) {
      selectedChannelID = "";
      selectedDirectID = "";
      await loadMessages();
      appliedRouteKey = requestedRouteKey;
      if (workspaceIDParam !== workspace.route_id || targetIDParam) await navigateToApp(workspace.id, "", true);
      return;
    }
    await navigateToApp(workspace.id, fallbackTargetID, true);
  }

  async function ensureResolvedRouteTargetLoaded(route: RouteTarget, serial: number): Promise<boolean> {
    if (route.target_type === "channel") {
      if (!channels.some((channel) => channel.id === route.target_id)) await loadChannels(false, false, false);
      return serial === routeApplySerial && channels.some((channel) => channel.id === route.target_id);
    }
    if (route.target_type === "direct") {
      if (!directConversations.some((conversation) => conversation.id === route.target_id)) await loadDirectConversations();
      return serial === routeApplySerial && directConversations.some((conversation) => conversation.id === route.target_id);
    }
    if (route.parent_type === "channel" && route.parent_id) {
      if (!channels.some((channel) => channel.id === route.parent_id)) await loadChannels(false, false, false);
      return serial === routeApplySerial && channels.some((channel) => channel.id === route.parent_id);
    }
    if (route.parent_type === "direct" && route.parent_id) {
      if (!directConversations.some((conversation) => conversation.id === route.parent_id)) await loadDirectConversations();
      return serial === routeApplySerial && directConversations.some((conversation) => conversation.id === route.parent_id);
    }
    return true;
  }

  async function resolveRouteTarget(workspaceID: string, targetID: string): Promise<RouteTarget | null> {
    try {
      const data = await api<{ route: RouteTarget }>(
        `/api/routes/${encodeURIComponent(workspaceID)}/${encodeURIComponent(targetID)}`,
      );
      return data.route;
    } catch (error) {
      if (error instanceof APIError && (error.status === 403 || error.status === 404)) return null;
      throw error;
    }
  }

  async function applyThreadRoute(route: RouteTarget): Promise<boolean> {
    if (route.workspace_id !== selectedWorkspaceID) return false;
    const parentChannelID = route.parent_type === "channel" ? route.parent_id || "" : "";
    const parentDirectID = route.parent_type === "direct" ? route.parent_id || "" : "";
    if (parentChannelID) {
      if (!channels.some((channel) => channel.id === parentChannelID)) return false;
      selectedChannelID = parentChannelID;
      selectedDirectID = "";
      rememberLastChannel(route.workspace_id, parentChannelID);
    } else if (parentDirectID) {
      if (!directConversations.some((conversation) => conversation.id === parentDirectID)) return false;
      selectedDirectID = parentDirectID;
      selectedChannelID = "";
    } else {
      return false;
    }
    const sameThread = selectedThread?.id === route.target_id && viewKey === currentConversationKey();
    selectedProfile = null;
    activeComposerContext = "thread";
    mobileNavOpen = false;
    await refreshThread(route.target_id);
    if (!sameThread && selectedThread) await loadMessagesAround(selectedThread);
    return true;
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
    mobileNavOpen = false;
    await applyRoute(data.workspace.route_id || data.workspace.id, "");
    await navigateToApp(data.workspace.id);
    status = "ready";
  }

  async function selectWorkspace(workspaceID: string) {
    mobileNavOpen = false;
    await navigateToApp(workspaceID);
  }

  async function loadChannels(loadInitialMessages = true, selectFallback = true, resetSidePanel = true) {
    if (!selectedWorkspaceID) return;
    const data = await api<{ channels: Channel[] }>(`/api/workspaces/${selectedWorkspaceID}/channels`);
    channels = data.channels;
    if (selectFallback) {
      selectedChannelID = channels.find((channel) => channel.id === selectedChannelID)?.id || channels[0]?.id || "";
    } else if (selectedChannelID && !channels.some((channel) => channel.id === selectedChannelID)) {
      selectedChannelID = "";
    }
    if (resetSidePanel) {
      selectedThread = null;
      selectedProfile = null;
      activeComposerContext = "message";
      replies = [];
    }
    if (loadInitialMessages) await loadMessages();
  }

  async function loadModerationMembers() {
    moderationMembers = [];
    if (!selectedWorkspaceID || (currentWorkspaceRole !== "owner" && currentWorkspaceRole !== "moderator")) return;
    try {
      const data = await api<{ members: MemberModeration[] }>(`/api/workspaces/${selectedWorkspaceID}/moderation/members`);
      moderationMembers = data.members;
    } catch {
      moderationMembers = [];
    }
  }

  async function loadSlashCommands() {
    slashCommands = [];
    if (!selectedWorkspaceID) return;
    try {
      const data = await api<{ slash_commands: SlashCommand[] }>(`/api/workspaces/${selectedWorkspaceID}/slash-commands`);
      slashCommands = data.slash_commands;
    } catch {
      slashCommands = [];
    }
  }

  function collectMentionPeople(
    currentUser: User | null,
    recent: User[],
    members: MemberModeration[],
    direct: DirectConversation | undefined,
  ): User[] {
    const people = new Map<string, User>();
    for (const member of members) {
      if (member.user.id) people.set(member.user.id, member.user);
    }
    for (const person of direct?.members || []) {
      if (person.id) people.set(person.id, person);
    }
    for (const person of recent) {
      if (person.id) people.set(person.id, person);
    }
    if (currentUser?.id) people.set(currentUser.id, currentUser);
    return [...people.values()].slice(0, 24);
  }

  async function updateMemberModeration(userID: string, body: Record<string, unknown>) {
    if (!selectedWorkspaceID) return;
    const data = await api<{ member: MemberModeration }>(`/api/workspaces/${selectedWorkspaceID}/moderation/members/${userID}`, {
      method: "PATCH",
      body: JSON.stringify(body),
    });
    moderationMembers = [
      ...moderationMembers.filter((member) => member.user.id !== userID),
      data.member,
    ];
    await loadChannels(false, false, false);
    status = "ready";
  }

  async function createChannel() {
    if (!selectedWorkspaceID || !channelName.trim()) return;
    const data = await api<{ channel: Channel }>(`/api/workspaces/${selectedWorkspaceID}/channels`, {
      method: "POST",
      body: JSON.stringify({ name: channelName, kind: "public" })
    });
    channelName = "";
    channels = [...channels, data.channel];
    showCreateChannel = false;
    await navigateToApp(selectedWorkspaceID, data.channel.id);
  }

  async function selectChannel(channelID: string) {
    mobileNavOpen = false;
    rememberLastChannel(selectedWorkspaceID, channelID);
    const targetPath = appHref(selectedWorkspaceID, channelID);
    if (
      channelID === selectedChannelID &&
      !selectedDirectID &&
      window.location.pathname === targetPath
    ) {
      return;
    }
    await navigateToApp(selectedWorkspaceID, channelID);
  }

  async function loadMessages() {
    captureScrollMemory();
    const targetKey = currentConversationKey();
    const isSwitching = targetKey !== viewKey;
    resetHistoryPaging();
    if (isSwitching) {
      messagesLoading = true;
    }
    try {
      if (!selectedDirectID && !selectedChannelID) {
        commitMessageWindow("", pageToWindow({ messages: [], oldest_seq: 0, newest_seq: 0, has_older: false, has_newer: false }), "replace");
        return;
      }
      const data = await api<MessagePage>(messagePagePath(initialMessagePageQuery()));
      if (currentConversationKey() !== targetKey) return;
      commitMessageWindow(targetKey, pageToWindow(data), "replace");
    } finally {
      if (currentConversationKey() === targetKey) messagesLoading = false;
    }
  }

  async function loadLatestMessages() {
    const targetKey = currentConversationKey();
    if (!targetKey) return;
    resetHistoryPaging();
    messagesLoading = true;
    scrollMemory.set(targetKey, { atBottom: true });
    try {
      const data = await api<MessagePage>(messagePagePath(`limit=${INITIAL_MESSAGE_LIMIT}`));
      if (currentConversationKey() !== targetKey) return;
      commitMessageWindow(targetKey, pageToWindow(data), "replace");
    } finally {
      if (currentConversationKey() === targetKey) messagesLoading = false;
    }
  }

  function initialMessagePageQuery(): string {
    const unreadState = activeConversationUnreadState();
    const unreadCount = unreadState.unread_count || 0;
    const lastReadSeq = unreadState.last_read_seq || 0;
    if (unreadCount > 0) {
      return `around_seq=${encodeURIComponent(String(lastReadSeq + 1))}&limit=${INITIAL_MESSAGE_LIMIT}`;
    }
    return `limit=${INITIAL_MESSAGE_LIMIT}`;
  }

  function activeConversationUnreadState(): { unread_count?: number; last_read_seq?: number; last_seq?: number } {
    if (selectedDirectID) {
      return directConversations.find((conversation) => conversation.id === selectedDirectID) || {};
    }
    if (selectedChannelID) {
      return channels.find((channel) => channel.id === selectedChannelID) || {};
    }
    return {};
  }

  function messagePagePath(query: string): string {
    const base = selectedDirectID
      ? `/api/dms/${selectedDirectID}/messages`
      : `/api/channels/${selectedChannelID}/messages`;
    return query ? `${base}?${query}` : base;
  }

  function pageToWindow(page: MessagePage): MessageWindow {
    return {
      messages: page.messages,
      oldest_seq: page.oldest_seq,
      newest_seq: page.newest_seq,
      has_older: page.has_older,
      has_newer: page.has_newer,
    };
  }

  function commitMessageWindow(
    key: string,
    window: MessageWindow,
    direction: MessageWindowDirection,
  ) {
    const trimmedMessages = trimMessageWindow(key, window.messages, direction);
    const firstSeq = trimmedMessages[0]?.channel_seq || 0;
    const lastSeq = trimmedMessages[trimmedMessages.length - 1]?.channel_seq || 0;
    const droppedOlder = firstSeq > (window.messages[0]?.channel_seq || firstSeq);
    const droppedNewer = lastSeq < (window.messages[window.messages.length - 1]?.channel_seq || lastSeq);
    const nextWindow: MessageWindow = {
      messages: trimmedMessages,
      oldest_seq: firstSeq,
      newest_seq: lastSeq,
      has_older: window.has_older || droppedOlder,
      has_newer: window.has_newer || droppedNewer,
    };
    rememberMessageWindow(key, nextWindow);
    updateActiveMessageWindowFlags(key, nextWindow);
    commitView(key, trimmedMessages);
  }

  function rememberMessageWindow(key: string, window: MessageWindow) {
    if (!key) return;
    messageWindows.delete(key);
    messageWindows.set(key, window);
    pruneInactiveMessageWindows(key);
    pruneInactiveScrollMemory(key);
    messageWindows = new Map(messageWindows);
  }

  function pruneInactiveMessageWindows(activeKey: string) {
    let remainingPasses = messageWindows.size + 1;
    while (messageWindows.size > MAX_RETAINED_MESSAGE_WINDOWS && remainingPasses > 0) {
      remainingPasses--;
      const oldestKey = messageWindows.keys().next().value;
      if (!oldestKey) return;
      if (oldestKey === activeKey) {
        const activeWindow = messageWindows.get(oldestKey);
        messageWindows.delete(oldestKey);
        if (activeWindow) messageWindows.set(oldestKey, activeWindow);
        continue;
      }
      messageWindows.delete(oldestKey);
    }
  }

  function pruneInactiveScrollMemory(activeKey: string) {
    let remainingPasses = scrollMemory.size + 1;
    while (scrollMemory.size > MAX_RETAINED_SCROLL_STATES && remainingPasses > 0) {
      remainingPasses--;
      const oldestKey = scrollMemory.keys().next().value;
      if (!oldestKey) return;
      if (oldestKey === activeKey) {
        const activeState = scrollMemory.get(oldestKey);
        scrollMemory.delete(oldestKey);
        if (activeState) scrollMemory.set(oldestKey, activeState);
        continue;
      }
      scrollMemory.delete(oldestKey);
    }
  }

  function updateActiveMessageWindowFlags(key: string, window = messageWindows.get(key)) {
    if (key !== currentConversationKey()) return;
    activeHasOlder = window?.has_older || false;
    activeHasNewer = window?.has_newer || false;
  }

  function markMessageWindowHasNewer(key: string) {
    const window = messageWindows.get(key);
    if (!key || !window) return;
    rememberMessageWindow(key, { ...window, has_newer: true });
    updateActiveMessageWindowFlags(key);
  }

  function setHistoryEdgeState(direction: "older" | "newer", state: HistoryEdgeState) {
    if (direction === "older") {
      olderPageState = state;
      activeLoadingOlder = state === "loading";
    } else {
      newerPageState = state;
      activeLoadingNewer = state === "loading";
    }
  }

  function resetHistoryPaging() {
    loadingMessagePages = new Set();
    olderPageState = "idle";
    newerPageState = "idle";
    pendingOlderPageIntent = false;
    pendingNewerPageIntent = false;
    activeLoadingOlder = false;
    activeLoadingNewer = false;
  }

  function mergeMessageWindows(left: Message[], right: Message[]): Message[] {
    const byID = new Map<string, Message>();
    for (const message of [...left, ...right]) {
      byID.set(message.id, message);
    }
    return [...byID.values()].sort((a, b) => (a.channel_seq || 0) - (b.channel_seq || 0));
  }

  function protectedMessageIDs(key: string): Set<string> {
    const ids = new Set<string>();
    const unreadBoundary = unreadBoundarySeqForKey(key);
    if (unreadBoundary >= 0) {
      const firstUnread = firstUnreadMessageForKey(key, messages, unreadBoundary);
      if (firstUnread) ids.add(firstUnread.id);
    }
    const scrollAnchor = scrollMemory.get(key)?.anchorMessageID;
    if (scrollAnchor) ids.add(scrollAnchor);
    if (selectedThread && belongsToView(selectedThread, key)) ids.add(selectedThread.id);
    if (replyTarget && belongsToView(replyTarget, key)) ids.add(replyTarget.id);
    for (const message of messages) {
      if ((message.status === "pending" || message.status === "failed") && belongsToView(message, key)) {
        ids.add(message.id);
      }
    }
    return ids;
  }

  function trimMessageWindow(key: string, list: Message[], direction: MessageWindowDirection): Message[] {
    return trimMessageWindowMessages(list, direction, protectedMessageIDs(key));
  }

  function requestOlderMessages() {
    if (olderPageState !== "idle") {
      pendingOlderPageIntent = true;
      return;
    }
    void loadOlderMessages();
  }

  function requestNewerMessages(queueIfBusy = false) {
    if (newerPageState !== "idle") {
      if (queueIfBusy) pendingNewerPageIntent = true;
      return;
    }
    void loadNewerMessages();
  }

  async function loadOlderMessages() {
    const key = currentConversationKey();
    const window = messageWindows.get(key);
    const loadKey = `${key}:older`;
    if (olderPageState !== "idle") {
      pendingOlderPageIntent = true;
      return;
    }
    if (!key || !window?.has_older || window.oldest_seq <= 0 || loadingMessagePages.has(loadKey)) return;
    loadingMessagePages.add(loadKey);
    pendingOlderPageIntent = false;
    setHistoryEdgeState("older", "loading");
    captureScrollMemory();
    let committed = false;
    try {
      const data = await api<MessagePage>(messagePagePath(`before_seq=${encodeURIComponent(String(window.oldest_seq))}&limit=${PAGE_MESSAGE_LIMIT}`));
      if (currentConversationKey() !== key) return;
      const merged = mergeMessageWindows(data.messages, messages);
      commitMessageWindow(key, {
        messages: merged,
        oldest_seq: data.oldest_seq || window.oldest_seq,
        newest_seq: window.newest_seq,
        has_older: data.has_older,
        has_newer: window.has_newer,
      }, "prepend");
      committed = true;
      setHistoryEdgeState("older", "settling");
    } catch (error) {
      if (currentConversationKey() === key) {
        status = error instanceof Error ? error.message : "Could not load older messages";
      }
    } finally {
      loadingMessagePages.delete(loadKey);
      if (currentConversationKey() === key && !committed) setHistoryEdgeState("older", "idle");
    }
  }

  async function loadNewerMessages() {
    const key = currentConversationKey();
    const window = messageWindows.get(key);
    const loadKey = `${key}:newer`;
    if (newerPageState !== "idle") {
      pendingNewerPageIntent = true;
      return;
    }
    if (!key || loadingMessagePages.has(loadKey)) return;
    if (!window || window.newest_seq <= 0) {
      await loadMessages();
      return;
    }
    loadingMessagePages.add(loadKey);
    pendingNewerPageIntent = false;
    setHistoryEdgeState("newer", "loading");
    let committed = false;
    try {
      const data = await api<MessagePage>(messagePagePath(`after_seq=${encodeURIComponent(String(window.newest_seq))}&limit=${PAGE_MESSAGE_LIMIT}`));
      if (currentConversationKey() !== key) return;
      if (data.messages.length === 0) {
        commitMessageWindow(key, { ...window, has_newer: data.has_newer }, "append");
        committed = true;
        setHistoryEdgeState("newer", "settling");
        return;
      }
      const merged = mergeMessageWindows(messages, data.messages);
      commitMessageWindow(
        key,
        {
          messages: merged,
          oldest_seq: window.oldest_seq,
          newest_seq: data.newest_seq || window.newest_seq,
          has_older: window.has_older,
          has_newer: data.has_newer,
        },
        "append",
      );
      committed = true;
      setHistoryEdgeState("newer", "settling");
    } catch (error) {
      if (currentConversationKey() === key) {
        status = error instanceof Error ? error.message : "Could not load newer messages";
      }
    } finally {
      loadingMessagePages.delete(loadKey);
      if (currentConversationKey() === key && !committed) setHistoryEdgeState("newer", "idle");
    }
  }

  function handleHistorySettled(state: MessageListViewportState) {
    const shouldLoadOlder =
      olderPageState === "settling" && pendingOlderPageIntent && state.nearOlder && activeHasOlder;
    const shouldLoadNewer =
      newerPageState === "settling" && pendingNewerPageIntent && state.nearNewer && activeHasNewer;

    if (olderPageState === "settling") setHistoryEdgeState("older", "idle");
    if (newerPageState === "settling") setHistoryEdgeState("newer", "idle");

    pendingOlderPageIntent = false;
    pendingNewerPageIntent = false;

    if (shouldLoadOlder) requestOlderMessages();
    if (shouldLoadNewer) requestNewerMessages();
  }

  function currentConversationKey(): string {
    return selectedDirectID || selectedChannelID || "";
  }

  function maxChannelSeq(channelID: string, list = messages): number {
    let max = 0;
    for (const m of list) {
      if (m.channel_id !== channelID) continue;
      if (m.parent_message_id) continue;
      if (typeof m.channel_seq === "number" && m.channel_seq > max) max = m.channel_seq;
    }
    return max;
  }

  function maxDirectSeq(conversationID: string, list = messages): number {
    let max = 0;
    for (const m of list) {
      if (m.direct_conversation_id !== conversationID) continue;
      if (typeof m.channel_seq === "number" && m.channel_seq > max) max = m.channel_seq;
    }
    return max;
  }

  function unreadCountForKey(
    key: string,
    state: { unread_count?: number; last_read_seq?: number; last_seq?: number },
  ): number {
    if (!key) return 0;
    return state.unread_count || 0;
  }

  async function markChannelRead(channelID: string, seq: number) {
    const channel = channels.find((c) => c.id === channelID);
    if (!channel) return;
    if (seq <= 0 || seq <= (channel.last_read_seq || 0)) return;
    channels = channels.map((c) =>
      c.id === channelID
        ? (() => {
            const lastSeq = Math.max(c.last_seq || 0, seq);
            return {
              ...c,
              last_seq: lastSeq,
              unread_count: seq >= lastSeq ? 0 : c.unread_count || 0,
              last_read_seq: seq,
            };
          })()
        : c,
    );
    try {
      await api(`/api/channels/${channelID}/read`, { method: "POST", body: JSON.stringify({ seq }) });
    } catch {
      // Ignore — channel may be archived/inaccessible.
    }
  }

  async function markDirectRead(conversationID: string, seq: number) {
    const dm = directConversations.find((c) => c.id === conversationID);
    if (!dm) return;
    if (seq <= 0 || seq <= (dm.last_read_seq || 0)) return;
    directConversations = directConversations.map((c) =>
      c.id === conversationID
        ? (() => {
            const lastSeq = Math.max(c.last_seq || 0, seq);
            return {
              ...c,
              last_seq: lastSeq,
              unread_count: seq >= lastSeq ? 0 : c.unread_count || 0,
              last_read_seq: seq,
            };
          })()
        : c,
    );
    try {
      await api(`/api/dms/${conversationID}/read`, { method: "POST", body: JSON.stringify({ seq }) });
    } catch {
      // Ignore.
    }
  }

  function latestReadSeqForKey(key: string): number {
    const windowNewestSeq = messageWindows.get(key)?.newest_seq || 0;
    const channel = channels.find((c) => c.id === key);
    if (channel) {
      return Math.max(
        channel.last_seq || 0,
        (channel.last_read_seq || 0) + (channel.unread_count || 0),
        maxChannelSeq(key),
        windowNewestSeq,
      );
    }
    const dm = directConversations.find((c) => c.id === key);
    if (dm) {
      return Math.max(
        dm.last_seq || 0,
        (dm.last_read_seq || 0) + (dm.unread_count || 0),
        maxDirectSeq(key),
        windowNewestSeq,
      );
    }
    return 0;
  }

  function reachedReadSeqForKey(key: string): number {
    const window = messageWindows.get(key);
    if (!window || window.has_newer) return 0;
    const channel = channels.find((c) => c.id === key);
    if (channel) return maxChannelSeq(key);
    const dm = directConversations.find((c) => c.id === key);
    if (dm) return maxDirectSeq(key);
    return 0;
  }

  function markActiveViewRead(options: { all?: boolean; seq?: number } = {}) {
    if (!options.all && Date.now() < suppressAutoReadUntil) return;
    const key = currentConversationKey() || viewKey;
    if (!key) return;
    if (!options.all && !options.seq) {
      const boundarySeq = unreadBoundarySeqForKey(key);
      if (boundarySeq >= 0 && !unreadBoundaryLoadedForKey(key, boundarySeq)) return;
    }
    const seq = options.all
      ? Math.max(options.seq || 0, latestReadSeqForKey(key))
      : options.seq || reachedReadSeqForKey(key);
    if (seq <= 0) return;
    const isDirect = directConversations.some((conversation) => conversation.id === key);
    if (isDirect) {
      void markDirectRead(key, seq);
      if (options.all) clearUnreadLocally(key, seq);
      return;
    }
    if (channels.some((channel) => channel.id === key)) {
      void markChannelRead(key, seq);
      if (options.all) clearUnreadLocally(key, seq);
    }
  }

  function clearUnreadLocally(key: string, seq: number) {
    unreadMarkers.delete(key);
    unreadMarkers = new Map(unreadMarkers);
    channels = channels.map((c) =>
      c.id === key
        ? {
            ...c,
            last_seq: Math.max(c.last_seq || 0, seq),
            last_read_seq: Math.max(c.last_read_seq || 0, seq),
            unread_count: 0,
          }
        : c,
    );
    directConversations = directConversations.map((c) =>
      c.id === key
        ? {
            ...c,
            last_seq: Math.max(c.last_seq || 0, seq),
            last_read_seq: Math.max(c.last_read_seq || 0, seq),
            unread_count: 0,
          }
        : c,
    );
  }

  function lastReadSeqForKey(key: string): number {
    const channel = channels.find((c) => c.id === key);
    if (channel) return channel.last_read_seq || 0;
    const dm = directConversations.find((c) => c.id === key);
    return dm?.last_read_seq || 0;
  }

  function unreadBoundarySeqForKey(key: string): number {
    const channel = channels.find((c) => c.id === key);
    if (channel) return unreadCountForKey(key, channel) > 0 ? channel.last_read_seq || 0 : -1;
    const dm = directConversations.find((c) => c.id === key);
    return dm && unreadCountForKey(key, dm) > 0 ? dm.last_read_seq || 0 : -1;
  }

  function firstUnreadMessageForKey(key: string, list: Message[], lastReadSeq: number): Message | null {
    for (const message of list) {
      if (!belongsToView(message, key)) continue;
      if (message.parent_message_id) continue;
      if (message.author?.id === user?.id || message.author_id === user?.id) continue;
      const seq = message.channel_seq;
      if (typeof seq === "number" && seq > lastReadSeq) return message;
    }
    return null;
  }

  function rememberUnreadMarkerForMessages(key: string, list: Message[]) {
    if (!key) return;
    const state = unreadStateForKey(key);
    const unreadCount = unreadCountForKey(key, state);
    if (unreadCount <= 0) {
      unreadMarkers.delete(key);
      unreadMarkers = new Map(unreadMarkers);
      return;
    }
    const boundarySeq = state.last_read_seq || 0;
    const existing = unreadMarkers.get(key);
    if (existing?.boundarySeq === boundarySeq && existing.since) return;
    if (!unreadBoundaryLoadedForKey(key, boundarySeq)) return;
    const firstUnread = firstUnreadMessageForKey(key, list, boundarySeq);
    if (!firstUnread) return;
    unreadMarkers = new Map(unreadMarkers).set(key, {
      boundarySeq,
      since: formatMessageClock(firstUnread.created_at),
    });
  }

  function rememberUnreadMarkerFromEvent(key: string, boundarySeq: number, createdAt: string) {
    if (!key || !createdAt) return;
    const existing = unreadMarkers.get(key);
    if (existing?.boundarySeq === boundarySeq && existing.since) return;
    unreadMarkers = new Map(unreadMarkers).set(key, {
      boundarySeq,
      since: formatMessageClock(createdAt),
    });
  }

  function eventMessageSeq(event: RealtimeEvent): number {
    const payload = event.payload as Record<string, unknown>;
    const seqRaw = payload.channel_seq ?? event.seq ?? payload.seq;
    return typeof seqRaw === "number" ? seqRaw : Number(seqRaw) || 0;
  }

  function messageEventScope(event: RealtimeEvent): { channelID: string; dmID: string } {
    const payload = event.payload as Record<string, unknown>;
    return {
      channelID: event.channel_id || (typeof payload.channel_id === "string" ? payload.channel_id : ""),
      dmID: typeof payload.direct_conversation_id === "string" ? payload.direct_conversation_id : "",
    };
  }

  function maybeShowBrowserNotification(event: RealtimeEvent, affectsActiveView: boolean) {
    if (event.type !== "message.created") return;
    if (!browserNotificationsEnabled) return;
    if (typeof Notification === "undefined" || Notification.permission !== "granted") return;
    if (document.visibilityState === "visible" && affectsActiveView) return;
    const payload = event.payload as Record<string, unknown>;
    const authorID = typeof payload.author_id === "string" ? payload.author_id : "";
    if (authorID && authorID === user?.id) return;
    const { channelID, dmID } = messageEventScope(event);
    const channel = channels.find((candidate) => candidate.id === channelID);
    const author = lookupUser(authorID);
    const authorName = author?.display_name || "ClickClack";
    const place = channel ? `#${channel.name}` : "Direct message";
    const rawBody = typeof payload.body === "string" ? payload.body : "New message";
    const messageID = typeof payload.message_id === "string" ? payload.message_id : `${channelID || dmID}:${event.seq || Date.now()}`;
    try {
      const notification = new Notification(`${authorName} in ${place}`, {
        body: notificationBody(rawBody),
        tag: `clickclack:${messageID}`,
        icon: "/favicon.svg",
      });
      notification.onclick = () => {
        window.focus();
        notification.close();
        if (channelID) {
          void selectChannel(channelID);
        } else if (dmID) {
          void selectDirectConversation(dmID);
        }
      };
    } catch {
      // Browsers can still reject notifications despite granted permission.
    }
  }

  function notificationBody(body: string): string {
    const stripped = body
      .replace(/!\[[^\]]*]\([^)]+\)/g, "[image]")
      .replace(/\[[^\]]+]\(([^)]+)\)/g, "$1")
      .replace(/[`*_>#|]/g, "")
      .replace(/\s+/g, " ")
      .trim();
    if (!stripped) return "New message";
    return stripped.length > 180 ? `${stripped.slice(0, 177)}...` : stripped;
  }

  function messageEventAlreadyAccounted(event: RealtimeEvent): boolean {
    if (event.type !== "message.created") return false;
    const seq = eventMessageSeq(event);
    if (seq <= 0) return false;
    const { channelID, dmID } = messageEventScope(event);
    if (channelID) {
      const channel = channels.find((c) => c.id === channelID);
      return seq <= (channel?.last_seq || 0);
    }
    if (dmID) {
      const dm = directConversations.find((c) => c.id === dmID);
      return seq <= (dm?.last_seq || 0);
    }
    return false;
  }

  function unreadStateForKey(key: string): { unread_count?: number; last_read_seq?: number; last_seq?: number } {
    return channels.find((c) => c.id === key) || directConversations.find((c) => c.id === key) || {};
  }

  function unreadBoundaryLoadedForKey(
    key: string,
    boundarySeq: number,
    windows = messageWindows,
  ): boolean {
    if (!key || boundarySeq < 0) return false;
    const window = windows.get(key);
    if (!window || window.messages.length === 0) return false;
    const targetSeq = boundarySeq + 1;
    return window.oldest_seq <= targetSeq && window.newest_seq >= targetSeq;
  }

  function unreadSinceForKey(key: string, lastReadSeq: number, windows = messageWindows): string {
    const marker = unreadMarkers.get(key);
    if (marker?.boundarySeq === lastReadSeq) return marker.since;
    if (!unreadBoundaryLoadedForKey(key, lastReadSeq, windows)) return "";
    const firstUnread = firstUnreadMessageForKey(key, messages, lastReadSeq);
    if (!firstUnread) return "";
    return formatMessageClock(firstUnread.created_at);
  }

  function formatMessageClock(value: string): string {
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return "";
    return new Intl.DateTimeFormat(undefined, {
      hour: "numeric",
      minute: "2-digit",
    }).format(date);
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
    rememberUnreadMarkerForMessages(key, messages);
    if (switchingView) {
      typingEntries = [];
      agentProgressTurns = [];
      stopTyping();
    }
  }

  function setActiveMessages(nextMessages: Message[], direction: MessageWindowDirection = "append") {
    const key = currentConversationKey();
    const window = key ? messageWindows.get(key) : undefined;
    if (!key || !window) {
      messages = nextMessages;
      return;
    }
    const scopedMessages = nextMessages.filter((message) => belongsToView(message, key));
    const trimmedMessages = trimMessageWindow(key, scopedMessages, direction);
    const sequencedMessages = trimmedMessages.filter((message) => (message.channel_seq || 0) > 0);
    const oldestSeq = sequencedMessages[0]?.channel_seq || window.oldest_seq;
    const newestSeq = sequencedMessages[sequencedMessages.length - 1]?.channel_seq || window.newest_seq;
    const droppedOlder = messageSeq(trimmedMessages[0]) > messageSeq(scopedMessages[0]);
    const droppedNewer =
      messageSeq(trimmedMessages[trimmedMessages.length - 1]) <
      messageSeq(scopedMessages[scopedMessages.length - 1]);
    messages = trimmedMessages;
    rememberMessageWindow(key, {
      ...window,
      messages: trimmedMessages,
      oldest_seq: oldestSeq,
      newest_seq: newestSeq,
      has_older: window.has_older || droppedOlder,
      has_newer: window.has_newer || droppedNewer,
    });
    updateActiveMessageWindowFlags(key);
  }

  function messageSeq(message: Message | undefined): number {
    return message?.channel_seq || 0;
  }

  function belongsToView(message: Message, key: string): boolean {
    if (!key) return false;
    return message.channel_id === key || message.direct_conversation_id === key;
  }

  async function scrollMessagesToBottom() {
    await tick();
    await messageList?.scrollToBottom();
  }

  function isAtLiveEdge(): boolean {
    return messageList?.isNearBottom(LIVE_EDGE_TOLERANCE_PX) !== false;
  }

  async function revealOwnSentMessage() {
    await scrollMessagesToBottom();
  }

  async function jumpToLiveChat() {
    try {
      if (activeHasNewer || activeUnreadCount > 0) await loadLatestMessages();
      await scrollMessagesToBottom();
      markActiveViewRead({ all: true });
      await scrollMessagesToBottom();
    } catch (error) {
      status = error instanceof Error ? error.message : "Could not jump to latest messages";
    }
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
    const shouldRevealSentMessage = !existingNonce && currentConversationKey() === draft.viewKey;
    const shouldRefreshLatestAfterSend = shouldRevealSentMessage && activeHasNewer;
    pendingDrafts.set(nonce, draft);
    const placeholder = buildOptimisticMessage(nonce, draft, localID);
    if (existingNonce) {
      setActiveMessages(messages.map((m) => (m.id === localID ? placeholder : m)));
    } else if (currentConversationKey() === draft.viewKey) {
      setActiveMessages([...messages, placeholder]);
      void revealOwnSentMessage();
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
          setActiveMessages(messages.map((m) => (m.id === localID ? failedMessage : m)));
          return;
        }
      }
      pendingDrafts.delete(nonce);
      // Replace placeholder with the real message (or append if a concurrent
      // realtime reload already removed our placeholder).
      const tmpIndex = messages.findIndex((m) => m.id === localID);
      if (tmpIndex >= 0) {
        setActiveMessages(messages.map((m) => (m.id === localID ? message : m)));
      } else if (messages.some((m) => m.id === message.id)) {
        setActiveMessages(messages.map((m) => (m.id === message.id ? message : m)));
      } else if (
        belongsToView(message, currentConversationKey()) &&
        !messages.some((m) => m.id === message.id)
      ) {
        setActiveMessages([...messages, message]);
      }
      if (currentConversationKey() === draft.viewKey && shouldRevealSentMessage) {
        if (shouldRefreshLatestAfterSend) {
          await loadLatestMessages();
        }
        await revealOwnSentMessage();
        markActiveViewRead({ all: true, seq: message.channel_seq || 0 });
      }
    } catch (err) {
      console.warn("send failed", err);
      setActiveMessages(messages.map((m) =>
        m.id === localID ? { ...m, status: "failed" as const } : m,
      ));
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
    setActiveMessages(messages.filter((m) => m.id !== message.id));
  }

  async function openThread(message: Message) {
    await refreshThread(message.id, message);
    if (selectedWorkspaceID && selectedThread?.route_id && window.location.pathname !== appHref(selectedWorkspaceID, selectedThread.id)) {
      await navigateToApp(selectedWorkspaceID, selectedThread.id);
    }
  }

  async function refreshThread(messageID: string, optimisticRoot?: Message) {
    selectedProfile = null;
    if (optimisticRoot) selectedThread = optimisticRoot;
    activeComposerContext = "thread";
    const data = await api<{ root: Message; replies: Message[]; thread_state: ThreadState }>(`/api/messages/${messageID}/thread`);
    selectedThread = data.root;
    setActiveMessages(messages.map((message) => message.id === data.root.id ? data.root : message));
    replies = data.replies;
    selectedThreadState = data.thread_state;
  }

  async function sendReply() {
    const body = replyBody.trim();
    if (!body || !selectedThread) return;
    const quote = replyTarget && replyContext === "thread" ? replyTarget : null;
    replyBody = "";
    const payload: Record<string, unknown> = { body, nonce: newNonce() };
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
    return selectedImage !== null || showProfileSettings || showCreateChannel || showCreateDirect;
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
    if (scrolled) {
      await highlightMessage(targetID);
      return;
    }
    const data = await api<{ message: Message }>(`/api/messages/${targetID}`);
    if (!belongsToView(data.message, currentConversationKey())) return;
    await loadMessagesAround(data.message);
  }

  async function jumpToUnreadBoundary() {
    suppressAutoReadUntil = Date.now() + 1200;
    if (activeUnreadBoundaryLoaded && messageList?.scrollToDivider(false)) return;
    await loadUnreadBoundaryAround();
  }

  async function loadUnreadBoundaryAround() {
    const key = currentConversationKey();
    if (!key) return;
    suppressAutoReadUntil = Date.now() + 1200;
    const lastReadSeq = lastReadSeqForKey(key);
    const marker = unreadMarkers.get(key);
    const boundarySeq = marker?.boundarySeq === lastReadSeq ? marker.boundarySeq : lastReadSeq;
    const seq = boundarySeq + 1;
    if (seq <= 0) return;
    await loadMessagesAroundSeq(seq);
    await tick();
    messageList?.scrollToDivider(false);
  }

  async function highlightMessage(messageID: string) {
    await tick();
    const node = document.querySelector<HTMLElement>(`[data-message-id="${CSS.escape(messageID)}"]`);
    if (!node) return;
    node.classList.add("highlight");
    window.setTimeout(() => node.classList.remove("highlight"), 1500);
  }

  async function searchMessages() {
    if (!selectedWorkspaceID || !searchQuery.trim()) {
      searchResults = [];
      return;
    }
    if (selectedDirectID) {
      searchResults = [];
      return;
    }
    const params = new URLSearchParams({ workspace_id: selectedWorkspaceID, q: searchQuery.trim() });
    if (selectedChannelID) params.set("channel_id", selectedChannelID);
    const data = await api<{ results: SearchResult[] }>(`/api/search?${params.toString()}`);
    searchResults = data.results;
  }

  function resetSearch() {
    searchQuery = "";
    searchResults = [];
  }

  async function openSearchResult(result: SearchResult) {
    searchResults = [];
    const targetID = result.message.channel_id || result.message.direct_conversation_id || "";
    if (!selectedWorkspaceID || !targetID) return;
    if (currentConversationKey() !== targetID) {
      await navigateToApp(selectedWorkspaceID, targetID);
      await applyRoute(selectedWorkspaceID, targetID);
    }
    if (currentConversationKey() === targetID) await loadMessagesAround(result.message);
  }

  async function loadMessagesAround(target: Message) {
    const seq = target.channel_seq || 0;
    if (seq <= 0) {
      await loadMessages();
      return;
    }
    await loadMessagesAroundSeq(seq, target.id);
  }

  async function loadMessagesAroundSeq(seq: number, targetMessageID = "") {
    const targetKey = currentConversationKey();
    if (!targetKey) return;
    if (targetMessageID) {
      scrollMemory.set(targetKey, { atBottom: false, anchorMessageID: targetMessageID, anchorPixelOffset: 0 });
    }
    const isSwitching = targetKey !== viewKey;
    resetHistoryPaging();
    if (isSwitching) {
      messagesLoading = true;
    }
    try {
      const data = await api<MessagePage>(messagePagePath(`around_seq=${encodeURIComponent(String(seq))}&limit=${INITIAL_MESSAGE_LIMIT}`));
      if (currentConversationKey() !== targetKey) return;
      commitMessageWindow(targetKey, pageToWindow(data), "around");
      await tick();
      if (targetMessageID) {
        messageList?.scrollToMessage(targetMessageID);
        await highlightMessage(targetMessageID);
      }
    } finally {
      if (currentConversationKey() === targetKey) messagesLoading = false;
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

  async function createDirectConversation(memberID = directMemberID) {
    const trimmed = memberID.trim();
    if (!selectedWorkspaceID || !trimmed) return;
    const data = await api<{ conversation: DirectConversation }>("/api/dms", {
      method: "POST",
      body: JSON.stringify({ workspace_id: selectedWorkspaceID, member_ids: [trimmed] })
    });
    directMemberID = "";
    showCreateDirect = false;
    directConversations = [...directConversations, data.conversation];
    mobileNavOpen = false;
    await navigateToApp(selectedWorkspaceID, data.conversation.id);
  }

  async function startDirectFromModal(memberID: string) {
    const trimmed = memberID.trim();
    if (!trimmed) return;
    await startDirectWithUser(trimmed);
    directMemberID = "";
    showCreateDirect = false;
  }

  async function selectDirectConversation(conversationID: string) {
    mobileNavOpen = false;
    const targetPath = appHref(selectedWorkspaceID, conversationID);
    if (
      conversationID === selectedDirectID &&
      !selectedChannelID &&
      window.location.pathname === targetPath
    ) {
      return;
    }
    await navigateToApp(selectedWorkspaceID, conversationID);
  }

  async function startDirectWithUser(memberID: string) {
    const trimmed = memberID.trim();
    if (!selectedWorkspaceID || !trimmed) return;
    const existing = directConversations.find((conversation) =>
      conversation.members.some((member) => member.id === trimmed),
    );
    if (existing) {
      mobileNavOpen = false;
      await navigateToApp(selectedWorkspaceID, existing.id);
      return;
    }
    const data = await api<{ conversation: DirectConversation }>("/api/dms", {
      method: "POST",
      body: JSON.stringify({ workspace_id: selectedWorkspaceID, member_ids: [trimmed] })
    });
    directConversations = [...directConversations, data.conversation];
    mobileNavOpen = false;
    await navigateToApp(selectedWorkspaceID, data.conversation.id);
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
    if (event.type === "agent.progress") {
      handleAgentProgressEvent(event);
      return;
    }
    if (event.type === "channel.read" || event.type === "dm.read") {
      handleReadEvent(event);
      return;
    }
    if ((event.type === "channel.created" || event.type === "channel.updated") && event.workspace_id === selectedWorkspaceID) {
      await loadChannels(false, false, false);
      return;
    }
    if (event.type === "member.moderation_updated" && event.workspace_id === selectedWorkspaceID) {
      const selectedDirectBeforeModeration = selectedDirectID;
      const affectsCurrentUser = event.payload.user_id === user?.id;
      await loadWorkspaces();
      await loadModerationMembers();
      await loadChannels(false, affectsCurrentUser, affectsCurrentUser);
      if (affectsCurrentUser) {
        await loadDirectConversations();
        if (selectedDirectBeforeModeration) {
          if (directConversations.some((conversation) => conversation.id === selectedDirectBeforeModeration)) {
            selectedDirectID = selectedDirectBeforeModeration;
            selectedChannelID = "";
          } else {
            selectedDirectID = "";
          }
        }
        if (!selectedChannelID && !selectedDirectID) {
          commitMessageWindow("", pageToWindow({ messages: [], oldest_seq: 0, newest_seq: 0, has_older: false, has_newer: false }), "replace");
          return;
        }
        await loadMessages();
      }
      return;
    }
    if (messageEventAlreadyAccounted(event)) return;
    const affectsActiveView =
      event.channel_id === selectedChannelID || event.payload.direct_conversation_id === selectedDirectID;
    maybeShowBrowserNotification(event, affectsActiveView);
    if (event.type === "message.created" && !affectsActiveView) {
      handleUnreadBump(event);
    }
    if (
      affectsActiveView &&
      (event.type === "message.created" || event.type === "message.updated" || event.type === "message.deleted")
    ) {
      // Optimistic-send echo: if this is our own outgoing message, the HTTP
      // response will swap the placeholder; skip the reload to avoid a flicker.
      const echoNonce = event.payload.nonce;
      if (event.type === "message.created" && echoNonce && pendingDrafts.has(echoNonce)) {
        return;
      }
      // Snapshot stuck-to-bottom state BEFORE mutating messages. Once the
      // reload completes, virtua's scrollSize grows while offset is unchanged
      // and the cached atBottom flag flips to false — we'd lose the signal.
      const wasAtLiveEdge = isAtLiveEdge();
      if (event.type === "message.created" && !wasAtLiveEdge) {
        suppressAutoReadUntil = Date.now() + 1200;
        markMessageWindowHasNewer(currentConversationKey());
      } else if (event.type === "message.created") {
        await loadNewerMessages();
      } else {
        await loadMessages();
      }
      if (event.type === "message.created") {
        handleUnreadBump(event, wasAtLiveEdge);
      }
      // Drive the scroll explicitly from here rather than relying on the
      // MessageList $effect: its cached atBottom may already have flipped.
      if (event.type === "message.created" && wasAtLiveEdge) {
        await scrollMessagesToBottom();
        markActiveViewRead({ all: true, seq: eventMessageSeq(event) });
      }
    }
    const rootID = event.payload.root_message_id || event.payload.message_id;
    if (selectedThread && rootID === selectedThread.id) {
      await refreshThread(selectedThread.id, selectedThread);
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
        return { ...c, last_read_seq: next, unread_count: next >= (c.last_seq || 0) ? 0 : c.unread_count || 0 };
      });
    } else {
      const dmID = typeof payload.direct_conversation_id === "string" ? payload.direct_conversation_id : "";
      if (!dmID) return;
      directConversations = directConversations.map((c) => {
        if (c.id !== dmID) return c;
        const next = Math.max(c.last_read_seq || 0, seq);
        return { ...c, last_read_seq: next, unread_count: next >= (c.last_seq || 0) ? 0 : c.unread_count || 0 };
      });
    }
  }

  function handleUnreadBump(event: RealtimeEvent, activeWasAtBottom?: boolean) {
    const payload = event.payload as Record<string, unknown>;
    // Don't bump for own messages.
    const authorID = typeof payload.author_id === "string" ? payload.author_id : "";
    if (authorID && authorID === user?.id) return;
    // Threaded replies don't affect channel unread (channel_seq isn't assigned).
    if (payload.parent_message_id) return;
    const seq = eventMessageSeq(event);
    const { channelID, dmID } = messageEventScope(event);
    if (channelID) {
      const isActive = channelID === selectedChannelID && !selectedDirectID;
      const activeAtBottom = isActive ? activeWasAtBottom ?? isAtLiveEdge() : false;
      const channel = channels.find((c) => c.id === channelID);
      const incomingSeq = seq > 0 ? seq : (channel?.last_seq || 0) + 1;
      if (isActive && !activeAtBottom && (channel?.unread_count || 0) === 0) {
        rememberUnreadMarkerFromEvent(channelID, channel?.last_read_seq || 0, event.created_at);
      }
      channels = channels.map((c) => {
        if (c.id !== channelID) return c;
        const lastSeq = Math.max(c.last_seq || 0, incomingSeq);
        const lastReadSeq =
          isActive && !activeAtBottom && (c.unread_count || 0) === 0
            ? Math.max(c.last_read_seq || 0, incomingSeq - 1)
            : c.last_read_seq || 0;
        const unread = activeAtBottom ? 0 : (c.unread_count || 0) + 1;
        return { ...c, last_seq: lastSeq, last_read_seq: lastReadSeq, unread_count: unread };
      });
    } else if (dmID) {
      const isActive = dmID === selectedDirectID;
      const activeAtBottom = isActive ? activeWasAtBottom ?? isAtLiveEdge() : false;
      const dm = directConversations.find((c) => c.id === dmID);
      const incomingSeq = seq > 0 ? seq : (dm?.last_seq || 0) + 1;
      if (isActive && !activeAtBottom && (dm?.unread_count || 0) === 0) {
        rememberUnreadMarkerFromEvent(dmID, dm?.last_read_seq || 0, event.created_at);
      }
      directConversations = directConversations.map((c) => {
        if (c.id !== dmID) return c;
        const lastSeq = Math.max(c.last_seq || 0, incomingSeq);
        const lastReadSeq =
          isActive && !activeAtBottom && (c.unread_count || 0) === 0
            ? Math.max(c.last_read_seq || 0, incomingSeq - 1)
            : c.last_read_seq || 0;
        const unread = activeAtBottom ? 0 : (c.unread_count || 0) + 1;
        return { ...c, last_seq: lastSeq, last_read_seq: lastReadSeq, unread_count: unread };
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

  function handleAgentProgressEvent(event: RealtimeEvent) {
    const payload = event.payload as Record<string, unknown>;
    const eventChannel = event.channel_id || (typeof payload.channel_id === "string" ? payload.channel_id : "");
    const eventDM = typeof payload.direct_conversation_id === "string" ? payload.direct_conversation_id : "";
    const matchesView =
      (selectedChannelID && eventChannel === selectedChannelID) ||
      (selectedDirectID && eventDM === selectedDirectID);
    if (!matchesView) return;
    const turnId = typeof payload.turn_id === "string" ? payload.turn_id : "";
    const op = typeof payload.op === "string" ? payload.op : "";
    if (!turnId || !op) return;
    if (op === "clear") {
      agentProgressTurns = agentProgressTurns.filter((turn) => turn.turnId !== turnId);
      return;
    }
    const line = payload.line as Record<string, unknown> | undefined;
    const lineId = line && typeof line.id === "string" ? line.id : "";
    if (!lineId) return;
    const text = line && typeof line.text === "string" ? line.text : "";
    const title = line && typeof line.title === "string" ? line.title : "";
    const incomingText = text || title;
    const incomingToolName =
      line && typeof line.tool_name === "string"
        ? line.tool_name
        : typeof line?.toolName === "string"
          ? (line.toolName as string)
          : undefined;
    const incomingStatus = line && typeof line.status === "string" ? line.status : undefined;
    const incomingKind = line && typeof line.kind === "string" ? line.kind : undefined;
    // Finalize/update frames legitimately carry only { id, kind, status } and no
    // text/toolName. Merge onto the prior line so a status-only finalize still
    // applies (the line dims) instead of being dropped and left live until TTL.
    const existing = agentProgressTurns.find((turn) => turn.turnId === turnId);
    const prior = existing?.lines.find((l) => l.id === lineId);
    const view = {
      id: lineId,
      kind: incomingKind ?? prior?.kind ?? "lifecycle",
      text: incomingText || prior?.text || "",
      toolName: incomingToolName ?? prior?.toolName,
      status: incomingStatus ?? prior?.status,
      finalized: op === "finalize" || (prior?.finalized ?? false),
    };
    // Only drop a brand-new line that carries nothing renderable. An update for
    // an existing line must always apply, even when this frame omits content.
    if (!prior && !view.text && !view.toolName) return;
    const userId = typeof payload.user_id === "string" ? payload.user_id : "";
    const expiresAt = Date.now() + AGENT_PROGRESS_TTL_MS;
    if (!existing) {
      agentProgressTurns = [...agentProgressTurns, { turnId, userId, lines: [view], expiresAt }];
    } else {
      const lines = existing.lines.some((l) => l.id === lineId)
        ? existing.lines.map((l) => (l.id === lineId ? view : l))
        : [...existing.lines, view];
      agentProgressTurns = agentProgressTurns.map((turn) =>
        turn.turnId === turnId ? { ...turn, lines, expiresAt } : turn,
      );
    }
    ensureAgentProgressSweeper();
  }

  function ensureAgentProgressSweeper() {
    if (agentProgressSweeper) return;
    agentProgressSweeper = window.setInterval(() => {
      const now = Date.now();
      const next = agentProgressTurns.filter((turn) => turn.expiresAt > now);
      if (next.length !== agentProgressTurns.length) agentProgressTurns = next;
      if (next.length === 0 && agentProgressSweeper) {
        window.clearInterval(agentProgressSweeper);
        agentProgressSweeper = undefined;
      }
    }, 1000);
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
    if (
      (currentWorkspaceRole === "owner" || currentWorkspaceRole === "moderator") &&
      !moderationMembers.some((member) => member.user.id === profile.id)
    ) {
      void loadModerationMembers();
    }
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
    openImageViewer(markdownImageViewerURL(target), target.alt || "Image");
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
    const threadWasOpen = selectedThread !== null;
    const parentTargetID = currentConversationKey();
    if (replyContext === "thread") clearReplyTarget();
    selectedThread = null;
    selectedProfile = null;
    activeComposerContext = "message";
    replies = [];
    if (threadWasOpen && selectedWorkspaceID && parentTargetID) {
      void navigateToApp(selectedWorkspaceID, parentTargetID);
    }
  }

  function toggleSidePanelFromTopbar() {
    if (sidePanelOpen) closeSidePanel();
    else status = "pick a message to open its thread";
  }

  function handleWindowKeydown(event: KeyboardEvent) {
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
      } else {
        // Esc with no modal/reply jumps you to live chat.
        event.preventDefault();
        void jumpToLiveChat();
        return;
      }
    }
    if (
      mobileNavOpen &&
      event.key.length === 1 &&
      !event.ctrlKey &&
      !event.metaKey &&
      !event.altKey &&
      !event.isComposing
    ) {
      const active = document.activeElement;
      if (
        !(active instanceof HTMLInputElement) &&
        !(active instanceof HTMLTextAreaElement) &&
        !(active instanceof HTMLSelectElement) &&
        !(active instanceof HTMLElement && active.isContentEditable)
      ) {
        event.preventDefault();
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
  }

  function closeModal() {
    selectedImage = null;
    showProfileSettings = false;
    showCreateChannel = false;
    showCreateDirect = false;
  }

  function closeMobileNav() {
    mobileNavOpen = false;
  }

  function handleSidebarCollapse() {
    if (mobileNavOpen && window.matchMedia(MOBILE_NAV_MEDIA_QUERY).matches) {
      closeMobileNav();
      return;
    }
    sidebarCollapsed = !sidebarCollapsed;
  }
</script>

<svelte:head>
  <meta name="color-scheme" content="light dark" />
</svelte:head>

<svelte:window onkeydowncapture={handleWindowKeydown} onpointerdowncapture={rememberTypeToFocusPointer} />

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
        <h1>Welcome.</h1>
        <p>Sign in with GitHub to join the guest room.</p>
      </div>
      <a class="github-login" href="/api/auth/github/start">
        <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">
          <path fill="currentColor" d="M12 .5C5.65.5.5 5.65.5 12c0 5.08 3.29 9.39 7.86 10.91.58.1.79-.25.79-.56v-2c-3.2.69-3.87-1.37-3.87-1.37-.52-1.32-1.27-1.67-1.27-1.67-1.04-.71.08-.7.08-.7 1.15.08 1.76 1.18 1.76 1.18 1.02 1.75 2.68 1.25 3.34.96.1-.74.4-1.25.73-1.54-2.55-.29-5.24-1.28-5.24-5.69 0-1.26.45-2.29 1.18-3.1-.12-.29-.51-1.46.11-3.05 0 0 .96-.31 3.15 1.18a10.94 10.94 0 0 1 5.74 0c2.19-1.49 3.15-1.18 3.15-1.18.62 1.59.23 2.76.12 3.05.74.81 1.18 1.84 1.18 3.1 0 4.42-2.69 5.39-5.25 5.68.41.36.78 1.06.78 2.13v3.16c0 .31.21.67.8.56 4.56-1.52 7.85-5.83 7.85-10.91C23.5 5.65 18.35.5 12 .5z"/>
        </svg>
        Continue with GitHub
      </a>
      <p class="auth-foot">Any GitHub account can join.</p>
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
    hrefForWorkspace={(workspaceID) => appHref(workspaceID)}
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
    onToggleCollapse={handleSidebarCollapse}
    hrefForChannel={(channelID) => appHref(selectedWorkspaceID, channelID)}
    hrefForDirect={(conversationID) => appHref(selectedWorkspaceID, conversationID)}
    onSelectChannel={(channelID) => void selectChannel(channelID)}
    onCreateChannel={() => (showCreateChannel = true)}
    onSelectDirect={(conversationID) => void selectDirectConversation(conversationID)}
    onCreateDirect={() => (showCreateDirect = true)}
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
      unreadCount={activeUnreadCount}
      unreadBoundarySeq={activeUnreadBoundarySeq}
      unreadBoundaryLoaded={activeUnreadBoundaryLoaded}
      unreadSince={activeUnreadSince}
      hasOlder={activeHasOlder}
      hasNewer={activeHasNewer}
      loadingOlder={activeLoadingOlder}
      loadingNewer={activeLoadingNewer}
      prepending={olderPageState !== "idle"}
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
      onLoadOlder={requestOlderMessages}
      onLoadNewer={(source) => requestNewerMessages(source === "wheel")}
      onJumpToUnread={() => void jumpToUnreadBoundary()}
      onHistorySettled={handleHistorySettled}
      onReachedBottom={markActiveViewRead}
      onMarkRead={(readThroughSeq) => {
        markActiveViewRead({ all: true, seq: readThroughSeq });
      }}
      onRetry={retryFailedMessage}
      onDiscard={discardFailedMessage}
    />

    <AgentProgress turns={agentProgressTurns} />

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
      slashCommands={selectedChannelID ? slashCommands : []}
      {mentionPeople}
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
        {mentionPeople}
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
        currentUserRole={currentWorkspaceRole}
        moderation={selectedProfileModeration}
        onClose={closeSidePanel}
        onEdit={openProfileSettings}
        onMessage={(memberID) => void startDirectWithUser(memberID)}
        onApprove={(memberID) => void updateMemberModeration(memberID, { role: "member", clear_timeout: true, blocked: false })}
        onTimeout={(memberID) => void updateMemberModeration(memberID, { timeout_minutes: 60 })}
        onBlock={(memberID) => void updateMemberModeration(memberID, { blocked: true })}
        onUnblock={(memberID) => void updateMemberModeration(memberID, { blocked: false, clear_timeout: true })}
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
    pushoverEnabled={profilePushoverEnabled}
    pushoverUserKey={profilePushoverUserKey}
    {browserNotificationsSupported}
    {browserNotificationsEnabled}
    {browserNotificationPermission}
    status={profileStatus}
    statusError={profileStatusError}
    onDisplayName={(value) => (profileDisplayName = value)}
    onHandle={(value) => (profileHandle = value)}
    onAvatarURL={(value) => (profileAvatarURL = value)}
    onPushoverEnabled={(value) => (profilePushoverEnabled = value)}
    onPushoverUserKey={(value) => (profilePushoverUserKey = value)}
    onBrowserNotificationsEnabled={(value) => void setBrowserNotificationsEnabled(value)}
    onClose={closeModal}
    onSave={() => void saveProfile()}
  />
{/if}
{#if showCreateChannel}
  <CreateChannelModal
    {channelName}
    status=""
    onChannelName={(value) => (channelName = value)}
    onClose={closeModal}
    onCreate={() => void createChannel()}
  />
{/if}
{#if showCreateDirect}
  <CreateDirectModal
    people={recentPeople}
    currentUserID={user?.id}
    memberID={directMemberID}
    onMemberID={(value) => (directMemberID = value)}
    onClose={closeModal}
    onStart={(memberID) => void startDirectFromModal(memberID)}
  />
{/if}
{#if selectedImage}
  <ImageViewer url={selectedImage.url} title={selectedImage.title} onClose={closeModal} />
{/if}
{/if}
