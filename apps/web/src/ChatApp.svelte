<script lang="ts">
  import { afterNavigate, goto } from "$app/navigation";
  import { onDestroy, onMount, tick } from "svelte";
  import { toStore } from "svelte/store";
  import {
    DEFAULT_HOME_LINK,
    homeLinkTitle,
    loadHomeLink,
    type HomeLink,
  } from "./lib/home-link";
  import { APIError, api, apiResourceURL, apiURL, authMethods, frontendBaseURL, readableAPIError } from "./lib/api";
  import { requestCurrentUser } from "./lib/appearance";
  import { desktop } from "./lib/desktop";
  import { probeMediaDimensions } from "./lib/media";
  import { markdownImageViewerURL } from "./lib/actions/markdown";
  import {
    INITIAL_MESSAGE_LIMIT,
    MAX_RETAINED_MESSAGE_WINDOWS,
    MAX_RETAINED_SCROLL_STATES,
    PAGE_MESSAGE_LIMIT,
    trimMessageWindow as trimMessageWindowMessages,
    type MessageWindowDirection,
  } from "./lib/chat/messageWindow";
  import { collectMentionPeople, collectRecentPeople, dmTitle } from "./lib/chat/people";
  import { coalesceAgentActivity } from "./lib/chat/agent-activity";
  import { newNonce } from "./lib/chat/messages";
  import { MessageRequests, type AuthorUpdate } from "./lib/chat/messageRequests";
  import { mergeMessageUpdate, type MessageUpdate } from "./lib/chat/messageUpdates";
  import { channelDisplayTitle } from "./lib/chat/channels";
  import { redirectTypingToComposer, rememberTypeToFocusPointer } from "./lib/chat/typeToFocus";
  import {
    MessageEditController,
    type MessageEdit,
    type MessageEditSession,
  } from "./lib/messageEditing.svelte";
  import { connectRealtime, WorkspaceUnavailableError, type RealtimeConnection } from "./lib/realtime.svelte";
  import { ThreadController } from "./lib/thread.svelte";
  import { ReactionController } from "./lib/reactions.svelte";
  import { notifyTyping, stopTyping } from "./lib/typing";
  import ChatComposer from "./components/composer/ChatComposer.svelte";
  import ArtifactViewer from "./components/artifacts/ArtifactViewer.svelte";
  import ImageViewer from "./components/media/ImageViewer.svelte";
  import KeystrokeMark from "./components/KeystrokeMark.svelte";
  import MessageList, {
    type MessageListHandle,
    type MessageListState,
    type MessageListViewportState,
  } from "./components/messages/MessageList.svelte";
  import DeleteMessageModal from "./components/messages/DeleteMessageModal.svelte";
  import TypingIndicator, { TYPING_TTL_MS, type TypingEntry } from "./components/messages/TypingIndicator.svelte";
  import AgentProgress, { AGENT_PROGRESS_TTL_MS, type AgentProgressTurn } from "./components/messages/AgentProgress.svelte";
  import AgentResponding from "./components/messages/AgentResponding.svelte";
  import CreateChannelModal from "./components/navigation/CreateChannelModal.svelte";
  import CreateDirectModal from "./components/navigation/CreateDirectModal.svelte";
  import GuildRail from "./components/navigation/GuildRail.svelte";
  import Sidebar from "./components/navigation/Sidebar.svelte";
  import ProfilePane from "./components/profile/ProfilePane.svelte";
  import PinnedPanel from "./components/pins/PinnedPanel.svelte";
  import SearchResults from "./components/search/SearchResults.svelte";
  import ChannelSettingsModal from "./components/settings/ChannelSettingsModal.svelte";
  import SettingsModal from "./components/settings/SettingsModal.svelte";
  import ThreadEmptyState from "./components/thread/ThreadEmptyState.svelte";
  import ThreadPanel from "./components/thread/ThreadPanel.svelte";
  import DesktopTitlebar from "./components/topbar/DesktopTitlebar.svelte";
  import Topbar from "./components/topbar/Topbar.svelte";
  import { workspaceSettingsPath, type AccountSettingsSectionId } from "./lib/settings";
  import { agentProgressTurnKey, respondingAgentNames } from "./lib/agent-responding";
  import { listAllWorkspaceMembers, memberLoadErrorMessage } from "./lib/workspace-members";
  import type { Channel, ChannelNotificationPreference, DirectConversation, MemberModeration, Message, MessagePage, RealtimeEvent, RouteTarget, SearchResult, SearchScope, SearchSession, SlashCommand, ThreadPage, Topic, Upload, User, Workspace, WorkspaceBotCommand } from "./lib/types";
  import { dispatchSlashCommand, findRegisteredCommand, listBotCommands, splitSlashDraft } from "./lib/commands";

  const LIVE_EDGE_TOLERANCE_PX = 96;
  const LAST_CHANNEL_STORAGE_PREFIX = "clickclack:last-channel:v1:";
  const BROWSER_NOTIFICATIONS_STORAGE_PREFIX = "clickclack:browser-notifications-enabled:v1:";
  const CHANNEL_NOTIFICATION_STORAGE_PREFIX = "clickclack:channel-notification:v1:";
  const MOBILE_NAV_MEDIA_QUERY = "(max-width: 820px)";
  const SHOW_AGENT_ACTIVITY_STORAGE_KEY = "clickclack:show-agent-activity:v1";
  const HIDE_COMMENTARY_STORAGE_KEY = "clickclack:hide-commentary:v1";
  const HIDE_TOOL_CALLS_STORAGE_KEY = "clickclack:hide-tool-calls:v1";
  const USER_ALIGN_STORAGE_KEY = "clickclack:user-align:v1";
  const OTHER_ALIGN_STORAGE_KEY = "clickclack:other-align:v1";
  const appSessionStartedAt = Date.now();
  const integratedTitleBar = desktop?.integratedTitleBar === true;
  let homeLink: HomeLink = DEFAULT_HOME_LINK;

  export let routeWorkspaceID = "";
  export let routeTargetID = "";

  let user: User | null = null;
  const reactionController = new ReactionController(() => user?.id || "");
  const editController = new MessageEditController(revealEditSession);
  let workspaces: Workspace[] = [];
  let channels: Channel[] = [];
  let topics: Topic[] = [];
  let channelNotifPreference: ChannelNotificationPreference | null = null;
  let channelNotifPreferences = new Map<string, ChannelNotificationPreference>();
  let notificationMessageSeqs = new Map<string, number>();
  let channelNotifSaving = false;
  let directConversations: DirectConversation[] = [];
  let messages: Message[] = [];
  $: replies = $threadView.replies;
  let selectedWorkspaceID = "";
  let selectedChannelID = "";
  let selectedDirectID = "";
  const thread = new ThreadController(() => `${selectedWorkspaceID}:${currentConversationKey()}`, reconcileThread);
  // This legacy component consumes the rune-based owner through a reactive store.
  const threadView = toStore(() => ({
    root: thread.root,
    selection: thread.selection,
    replies: thread.replies,
    state: thread.state,
    draft: thread.draft,
    error: thread.error,
  }));
  let selectedComposerTopicID = "";
  let activeTopicFilterID = "";
  let topicFilterGeneration = 0;
  let topicConversationKey = "";
  let outgoingMessages = new Map<string, OutgoingMessage>();
  let selectedProfile: User | null = null;
  let pinnedPanelOpen = false;
  let pinnedMessages: Message[] = [];
  let pinnedMessagesLoading = false;
  let pinnedMessagesError = "";
  let pinnedMessageIDs = new Set<string>();
  let pinnedMessagesLoadSerial = 0;
  let moderationMembers: MemberModeration[] = [];
  let workspaceMemberUsers: User[] = [];
  let slashCommands: SlashCommand[] = [];
  let botCommands: WorkspaceBotCommand[] = [];
  // Local-only feedback for slash dispatch: Slack-style ephemeral responses
  // ("only visible to you") and dispatch failures. Never part of the message
  // stream.
  let composerNotice: { kind: "ephemeral" | "error"; text: string } | null = null;
  let mentionPeople: User[] = [];
  let mentionAttentionUserID = "";
  let selectedImage: { url: string; title: string } | null = null;
  let selectedArtifact: Upload | null = null;
  let artifactConversationKey = "";
  let artifactTrigger: HTMLElement | null = null;
  let artifactViewerElement: HTMLElement | null = null;
  let shellElement: HTMLElement | null = null;
  let artifactModalInertElements = new Set<HTMLElement>();
  let messageBody = "";
  let workspaceName = "";
  let channelName = "";
  let directMemberID = "";
  let createPending: "workspace" | "channel" | "direct" | null = null;
  let workspaceCreateError = "";
  let channelCreateError = "";
  let directCreateError = "";
  let createActionSerial = 0;
  let searchQuery = "";
  // A search session owns the right pane until it is closed or replaced.
  // Opening a thread from a result "detours" the pane to that thread while the
  // session (results, cursor, scroll, active row) survives for Back.
  let searchSession: SearchSession | null = null;
  let searchThreadDetour = false;
  let searchReturnScrollTop = 0;
  let searchRequestID = 0;
  let pendingUpload: Upload | null = null;
  let uploadWorkspaceID = "";
  let uploadController: AbortController | null = null;

  $: if (uploadWorkspaceID && uploadWorkspaceID !== selectedWorkspaceID) clearPendingUpload();
  let settingsModalOpen = false;
  let settingsModalSection: AccountSettingsSectionId = "profile";
  let channelSettingsOpen = false;
  let channelSettingsSaving = false;
  let channelSettingsError = "";
  let showCreateChannel = false;
  let showCreateDirect = false;
  let browserNotificationsEnabled = false;
  // Client-only preferences for agent activity. Consecutive same-turn
  // agent_commentary/agent_tool rows are coalesced into one preamble block;
  // these two independent flags drop the commentary prose and/or the tool-call
  // sub-items from that block. When both are set the block is omitted entirely.
  // Default: show both. Persisted in localStorage like other client prefs.
  let hideCommentary = false;
  let hideToolCalls = false;
  // Self-message alignment: "left" (default, matches the legacy layout) or
  // "right". Persisted client-side and applied as a root data attribute so the
  // messages.css mirror rules can flip the self group without prop drilling.
  let userAlign: "left" | "right" = "left";
  let otherAlign: "left" | "right" = "left";
  let appReady = false;
  let authRequired = false;
  let desktopAuthStatus = "";
  const enabledAuthMethods = authMethods();
  const githubAuthEnabled = enabledAuthMethods.includes("github");
  const passwordAuthEnabled = enabledAuthMethods.includes("password");
  let passwordIdentifier = "";
  let passwordSecret = "";
  let magicToken = "";
  let authSubmitting = false;
  let authError = "";
  const authLead = passwordAuthEnabled
    ? githubAuthEnabled
      ? "Sign in with your ClickClack account, or continue with GitHub."
      : "Sign in with your ClickClack account."
    : githubAuthEnabled
      ? "Sign in with GitHub to join the guest room."
      : "Sign in with a token from your ClickClack administrator.";
  const authFoot = githubAuthEnabled && !passwordAuthEnabled ? "Any GitHub account can join." : "";
  let connected = false;
  let realtimeError = "";
  let realtimeInitializedWorkspaceID = "";
  let pendingRealtimeWorkspaceID = "";
  let socket: RealtimeConnection | null = null;
  let messageList: MessageListHandle | null = null;
  let scrollMemory = new Map<string, MessageListState>();
  let messageWindows = new Map<string, MessagePage>();
  const messageRequests = new MessageRequests(() => [user?.id, selectedWorkspaceID, currentConversationKey()].join(":"));
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
  let mobileNavViewport = false;
  let replyTarget: Message | null = null;
  let replyContext: "channel" | "dm" | null = null;
  let messageInput: HTMLTextAreaElement | null = null;
  let replyInput: HTMLTextAreaElement | null = null;
  let activeComposerContext: "message" | "thread" = "message";
  let typingEntries: TypingEntry[] = [];
  let typingSweeper: number | undefined;
  let agentProgressTurns: AgentProgressTurn[] = [];
  let agentProgressSweeper: number | undefined;
  let activityClock = Date.now();
  let activityClockSweeper: number | undefined;
  let activeRouteKey = "";
  let routeApplySerial = 0;
  let messageLoadGeneration = 0;
  let workspacesLoadSerial = 0;
  let channelsLoadSerial = 0;
  let topicsLoadSerial = 0;
  let directConversationsLoadSerial = 0;
  let moderationMembersLoadSerial = 0;
  let workspaceMembersLoadSerial = 0;
  let workspaceMembersAbort: AbortController | null = null;
  let workspaceMembersError = "";
  let slashCommandsLoadSerial = 0;
  let botCommandsLoadSerial = 0;
  let channelNotifLoadSerial = 0;
  let realtimeReconcileSerial = 0;
  let slashDispatchGeneration = 0;
  let hiddenDirectUndo: HiddenDirectUndo | null = null;
  let hiddenDirectUndoTimer: ReturnType<typeof setTimeout> | undefined;
  let deletingMessageIDs = new Set<string>();
  let pendingDeleteMessage: Message | null = null;
  let deleteMessageError = "";

  type HistoryEdgeState = "idle" | "loading" | "settling";
  type HiddenDirectUndo = {
    conversation: DirectConversation;
    restoreRoute: boolean;
    title: string;
  };
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
  $: canDeleteAnyMessage = currentWorkspaceRole === "owner";
  $: selectedProfileModeration = selectedProfile
    ? moderationMembers.find((member) => member.user.id === selectedProfile?.id)
    : undefined;
  $: selectedChannel = channels.find((channel) => channel.id === selectedChannelID);
  $: canManageSelectedChannel =
    Boolean(selectedChannel) &&
    (currentWorkspaceRole === "owner" || currentWorkspaceRole === "moderator");
  $: eligibleTopics = topicsForChannel(topics, selectedChannelID);
  $: activeTopic = eligibleTopics.find((topic) => topic.id === activeTopicFilterID);
  $: void loadChannelNotifPreference(selectedChannelID, selectedDirectID);
  $: selectedDirect = directConversations.find((conversation) => conversation.id === selectedDirectID);
  $: selectedDirectWritable = selectedDirect?.can_send ?? true;
  $: activeConversationKey = selectedDirectID || selectedChannelID || "";
  $: resetTopicStateForConversation(activeConversationKey);
  // Slash-dispatch notices are scoped to the conversation they fired in.
  $: clearComposerNoticeFor(activeConversationKey);
  // Bot-declared menus surface in channels; in DMs only for bots that are
  // party to the conversation.
  $: composerBotCommands = selectedChannelID
    ? botCommands
    : selectedDirect
      ? botCommands.filter((command) => selectedDirect?.members?.some((member) => member.id === command.bot.id))
      : [];
  $: activeUnreadState = selectedDirectID
    ? directConversations.find((conversation) => conversation.id === selectedDirectID) || {}
    : selectedChannelID
      ? channels.find((channel) => channel.id === selectedChannelID) || {}
      : {};
  $: activeUnreadCount = unreadCountForKey(activeConversationKey, activeUnreadState);
  $: desktopUnreadCount = appReady
    ? channels.reduce((total, channel) => total + (channel.unread_count || 0), 0) +
      directConversations.reduce((total, conversation) => total + (conversation.unread_count || 0), 0)
    : 0;
  $: desktop?.setUnreadCount(desktopUnreadCount);
  $: activeUnreadBoundarySeq = activeUnreadCount > 0 ? activeUnreadState.last_read_seq || 0 : 0;
  $: activeUnreadBoundaryLoaded = activeUnreadCount > 0
    ? unreadBoundaryLoadedForKey(activeConversationKey, activeUnreadBoundarySeq, messageWindows)
    : false;
  $: activeUnreadSince = activeUnreadCount > 0
    ? unreadSinceForKey(activeConversationKey, activeUnreadBoundarySeq, messageWindows)
    : "";
  // Coalesce consecutive same-turn agent activity rows into one preamble block
  // per turn, applying the two visibility flags. Ordinary messages pass through
  // untouched and keep their order.
  $: visibleMessages = coalesceAgentActivity(
    messages,
    { hideCommentary, hideToolCalls },
    activityClock,
  );

  function resetTopicStateForConversation(conversationKey: string) {
    if (conversationKey === topicConversationKey) return;
    topicConversationKey = conversationKey;
    selectedComposerTopicID = "";
    updateActiveTopicFilter("");
  }

  function updateActiveTopicFilter(topicID: string) {
    if (topicID === activeTopicFilterID) return;
    activeTopicFilterID = topicID;
    topicFilterGeneration += 1;
  }

  function activeMessageScopeKey(): string {
    return `${selectedWorkspaceID}:${currentConversationKey()}:${activeTopicFilterID}:${topicFilterGeneration}:${messageLoadGeneration}`;
  }

  function topicsForChannel(source: Topic[], channelID: string): Topic[] {
    if (!channelID) return [];
    return source.filter(
      (topic) => !topic.archived_at && (!topic.channel_id || topic.channel_id === channelID),
    );
  }
  // High-level "agent turn is live" signal: any tracked turn that still has an
  // unfinalized line. Drives the compact AgentResponding status above the
  // composer; clears as soon as every line finalizes or the turn is cleared.
  $: agentResponding = agentProgressTurns.some((turn) =>
    turn.lines.some((line) => !line.finalized),
  );
  $: pinnedMessageIDs = new Set(pinnedMessages.map((message) => message.id));
  $: activeRespondingAgentNames = respondingAgentNames(agentProgressTurns, botCommands, lookupUser);
  $: sidePanelOpen = pinnedPanelOpen || $threadView.selection !== null || selectedProfile !== null || selectedArtifact !== null;
  // The shared right-pane slot renders search or thread, never both.
  $: searchPaneVisible = searchSession !== null && !searchThreadDetour;
  $: if (selectedArtifact && artifactConversationKey && artifactConversationKey !== activeConversationKey) {
    selectedArtifact = null;
    artifactConversationKey = "";
    artifactTrigger = null;
  }
  $: syncArtifactModalInert(
    mobileNavViewport && selectedArtifact !== null,
    artifactViewerElement,
  );
  $: recentPeople = collectRecentPeople(messages, directConversations, user?.id || "");
  $: mentionPeople = collectMentionPeople(user, recentPeople, workspaceMemberUsers, selectedDirect);
  $: mentionAttentionUserID =
    user?.id &&
    (selectedDirectID ||
      (selectedChannelID &&
        channelNotifPreference !== null &&
        channelNotifPreference !== "muted"))
      ? user.id
      : "";
  $: if (replyContext === "channel" && replyTarget && !messages.some((m) => m.id === replyTarget?.id)) clearReplyTarget();
  $: if (replyContext === "dm" && replyTarget && !messages.some((m) => m.id === replyTarget?.id)) clearReplyTarget();
  // Observe route inputs, not bookkeeping changed by a local pane selection.
  $: if (appReady) {
    followRoute(routeWorkspaceID, routeTargetID);
  }

  afterNavigate(() => {
    desktop?.setActiveRoute(`${window.location.pathname}${window.location.search}${window.location.hash}`);
  });

  onMount(() => {
    loadActivityPrefs();
    void loadHomeLink((path) => api<unknown>(path)).then((link) => {
      homeLink = link;
    });
    activityClockSweeper = window.setInterval(() => {
      activityClock = Date.now();
    }, 30_000);
    syncBrowserNotificationState();
    void boot();
    const mobileNavMedia = window.matchMedia(MOBILE_NAV_MEDIA_QUERY);
    const handleMobileNavBreakpoint = () => {
      mobileNavOpen = false;
      mobileNavViewport = mobileNavMedia.matches;
    };
    handleMobileNavBreakpoint();
    const stopDesktopNavigate = desktop?.onNavigate((route) => {
      void goto(route, { keepFocus: true, noScroll: true });
    });
    const stopDesktopQuickCompose = desktop?.onQuickCompose(() => focusActiveComposer());
    mobileNavMedia.addEventListener("change", handleMobileNavBreakpoint);
    return () => {
      mobileNavMedia.removeEventListener("change", handleMobileNavBreakpoint);
      stopDesktopNavigate?.();
      stopDesktopQuickCompose?.();
    };
  });

  function focusActiveComposer() {
    void tick().then(() => {
      const input = activeComposerContext === "thread" ? replyInput : messageInput;
      input?.focus();
    });
  }

  async function signInWithGitHub(event: MouseEvent) {
    if (!desktop) return;
    event.preventDefault();
    desktopAuthStatus = "Opening GitHub in your browser…";
    try {
      await desktop.signInWithGitHub();
      desktopAuthStatus = "Finish signing in in your browser. ClickClack will complete here automatically.";
    } catch {
      desktopAuthStatus = "Could not open your browser. Try again.";
    }
  }

  // A fresh document prevents account-scoped state from crossing sign-ins.
  async function completeSignIn(path: string, body: Record<string, string>) {
    if (authSubmitting) return;
    authSubmitting = true;
    authError = "";
    try {
      await api(path, { method: "POST", body: JSON.stringify(body) });
      window.location.reload();
    } catch (error) {
      authError = readableAPIError(error, "Could not sign in.");
      authSubmitting = false;
    }
  }

  function submitPasswordLogin(event: SubmitEvent) {
    event.preventDefault();
    void completeSignIn("/api/auth/password/login", {
      identifier: passwordIdentifier,
      password: passwordSecret,
    });
  }

  function submitMagicToken(event: SubmitEvent) {
    event.preventDefault();
    void completeSignIn("/api/auth/magic/consume", { token: magicToken });
  }

  function loadActivityPrefs() {
    try {
      // New flags default off (both shown). Migrate the legacy single toggle:
      // if the operator had previously hidden all activity, carry that forward
      // as both flags hidden.
      const legacyHidden = window.localStorage.getItem(SHOW_AGENT_ACTIVITY_STORAGE_KEY) === "0";
      hideCommentary = window.localStorage.getItem(HIDE_COMMENTARY_STORAGE_KEY) === "1" || legacyHidden;
      hideToolCalls = window.localStorage.getItem(HIDE_TOOL_CALLS_STORAGE_KEY) === "1" || legacyHidden;
      userAlign = window.localStorage.getItem(USER_ALIGN_STORAGE_KEY) === "right" ? "right" : "left";
      otherAlign = window.localStorage.getItem(OTHER_ALIGN_STORAGE_KEY) === "right" ? "right" : "left";
    } catch {
      hideCommentary = false;
      hideToolCalls = false;
      userAlign = "left";
      otherAlign = "left";
    }
    applyMessageAlignments();
  }

  function applyMessageAlignments() {
    try {
      document.documentElement.setAttribute("data-user-align", userAlign);
      document.documentElement.setAttribute("data-other-align", otherAlign);
    } catch {
      // Non-DOM context (SSR/tests); the in-memory pref still applies on mount.
    }
  }

  function setUserAlign(value: "left" | "right") {
    userAlign = value;
    applyMessageAlignments();
    try {
      window.localStorage.setItem(USER_ALIGN_STORAGE_KEY, value);
    } catch {
      // Ignore unavailable storage; the in-memory pref still applies this session.
    }
  }

  function setOtherAlign(value: "left" | "right") {
    otherAlign = value;
    applyMessageAlignments();
    try {
      window.localStorage.setItem(OTHER_ALIGN_STORAGE_KEY, value);
    } catch {
      // Ignore unavailable storage; the in-memory pref still applies this session.
    }
  }

  function setHideCommentary(value: boolean) {
    hideCommentary = value;
    try {
      window.localStorage.setItem(HIDE_COMMENTARY_STORAGE_KEY, value ? "1" : "0");
    } catch {
      // Ignore unavailable storage; the in-memory pref still applies this session.
    }
  }

  function setHideToolCalls(value: boolean) {
    hideToolCalls = value;
    try {
      window.localStorage.setItem(HIDE_TOOL_CALLS_STORAGE_KEY, value ? "1" : "0");
    } catch {
      // Ignore unavailable storage; the in-memory pref still applies this session.
    }
  }

  onDestroy(() => {
    clearPendingUpload();
    thread.close();
    routeApplySerial += 1;
    messageLoadGeneration += 1;
    messageRequests.clear();
    workspaceMembersLoadSerial += 1;
    workspaceMembersAbort?.abort();
    socket?.close();
    socket = null;
    connected = false;
    stopTyping();
    if (typingSweeper) window.clearInterval(typingSweeper);
    if (agentProgressSweeper) window.clearInterval(agentProgressSweeper);
    if (activityClockSweeper) window.clearInterval(activityClockSweeper);
    if (hiddenDirectUndoTimer) clearTimeout(hiddenDirectUndoTimer);
    syncArtifactModalInert(false, null);
  });

  async function boot() {
    try {
      const me = await requestCurrentUser();
      user = me.user;
      syncBrowserNotificationState();
      await loadWorkspaces();
      // Let workspace projections settle before admitting routes in a later flush.
      appReady = true;
    } catch (error) {
      handleAppLoadError(error);
    }
  }

  function handleAppLoadError(error: unknown) {
    if (error instanceof APIError && (error.status === 401 || error.status === 403)) {
      socket?.close();
      socket = null;
      settingsModalOpen = false;
      authRequired = true;
      appReady = false;
      return;
    }
    composerNotice = { kind: "error", text: readableAPIError(error, "Could not load ClickClack") };
  }

  function openProfileSettings() {
    if (!user) return;
    settingsModalSection = "profile";
    settingsModalOpen = true;
  }

  function openWorkspaceSettings() {
    const workspaceID = selectedWorkspace?.route_id || selectedWorkspaceID || routeWorkspaceID;
    if (!workspaceID) return;
    void goto(workspaceSettingsPath(workspaceID));
  }

  function openChannelSettings() {
    if (!canManageSelectedChannel) return;
    channelSettingsError = "";
    channelSettingsOpen = true;
  }

  async function setSelectedChannelArchived(archived: boolean) {
    const channel = selectedChannel;
    if (!channel || !canManageSelectedChannel || channelSettingsSaving) return;
    channelSettingsSaving = true;
    channelSettingsError = "";
    try {
      const data = await api<{ channel: Channel }>(`/api/channels/${channel.id}`, {
        method: "PATCH",
        body: JSON.stringify({ archived }),
      });
      channels = channels.map((candidate) =>
        candidate.id === data.channel.id ? data.channel : candidate,
      );
      channelSettingsOpen = false;
    } catch (error) {
      channelSettingsError = readableAPIError(
        error,
        archived ? "Could not archive channel" : "Could not restore channel",
      );
    } finally {
      channelSettingsSaving = false;
    }
  }

  function handleSettingsUserUpdated(updated: User) {
    user = updated;
    updateActiveAuthor(updated);
    thread.updateAuthor(updated);
  }

  function syncBrowserNotificationState() {
    if (desktop) {
      browserNotificationsEnabled = storedBrowserNotificationsEnabled();
      return;
    }
    const storedEnabled = storedBrowserNotificationsEnabled();
    browserNotificationsEnabled = typeof Notification !== "undefined" &&
      Notification.permission === "granted" &&
      storedEnabled;
    if (storedEnabled && !browserNotificationsEnabled) {
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

  async function loadWorkspaces() {
    const serial = ++workspacesLoadSerial;
    const data = await api<{ workspaces: Workspace[] }>("/api/workspaces");
    if (serial !== workspacesLoadSerial) return;
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

  async function ensureMessageLink(message: Message): Promise<string> {
    if (!message.channel_id || message.parent_message_id) throw new Error("Only channel roots have links");
    const data = await api<{ message: Message }>(`/api/messages/${message.id}/route`, {
      method: "POST",
    });
    if (!data.message.route_id) throw new Error("Message route was not allocated");
    updateActiveMessage({ id: data.message.id, route_id: data.message.route_id });
    const workspaceRouteID = routeWorkspaceIDFor(data.message.workspace_id);
    if (!workspaceRouteID) throw new Error("Workspace route is unavailable");
    const path = `/app/${encodeURIComponent(workspaceRouteID)}/${encodeURIComponent(data.message.route_id)}`;
    return new URL(path, `${frontendBaseURL()}/`).toString();
  }

  function notificationHref(targetID: string): string {
    const targetRouteID = channels.find((channel) => channel.id === targetID)?.route_id ||
      directConversations.find((conversation) => conversation.id === targetID)?.route_id;
    if (targetRouteID) return appHref(selectedWorkspaceID, targetRouteID);
    if (!selectedWorkspaceID || !targetID) return "/app";
    // Unknown realtime targets still form a valid legacy pair; the route API canonicalizes it.
    return `/app/${encodeURIComponent(selectedWorkspaceID)}/${encodeURIComponent(targetID)}`;
  }

  function routeWorkspaceIDFor(workspaceID = selectedWorkspaceID): string {
    if (!workspaceID) return "";
    return workspaces.find((workspace) => workspace.id === workspaceID || workspace.route_id === workspaceID)?.route_id || workspaceID;
  }

  function routeTargetIDFor(targetID = ""): string {
    if (!targetID) return "";
    return channels.find((channel) => channel.id === targetID || channel.route_id === targetID)?.route_id ||
      directConversations.find((conversation) => conversation.id === targetID || conversation.route_id === targetID)?.route_id ||
      (thread.root?.id === targetID ? thread.root.route_id || "" : "") ||
      messages.find((message) => message.id === targetID)?.route_id ||
      targetID;
  }

  async function navigateToApp(workspaceID = selectedWorkspaceID, targetID = "", replaceState = false) {
    const path = appHref(workspaceID, targetID);
    if (window.location.pathname === path) return;
    await goto(path, { replaceState, noScroll: true, keepFocus: true });
  }

  function followRoute(workspaceID: string, targetID: string) {
    if (routeKey(workspaceID, targetID) !== activeRouteKey) void applyRoute(workspaceID, targetID);
  }

  function commitSelectedRoute() {
    routeApplySerial++;
    // The conversation is already selected; replaying it would clear the new pane.
    activeRouteKey = routeKey(routeWorkspaceIDFor(), routeTargetIDFor(currentConversationKey()));
    void navigateToApp(selectedWorkspaceID, currentConversationKey());
  }

  function clearRoutePanelState() {
    // Navigating away abandons a thread borrowed from search; drop the session
    // too so the pane doesn't linger invisibly.
    if (searchThreadDetour) resetSearch();
    thread.close();
    selectedProfile = null;
    pinnedPanelOpen = false;
    activeComposerContext = "message";
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

  function connectPendingRealtime(workspaceID: string) {
    if (pendingRealtimeWorkspaceID !== workspaceID) return;
    pendingRealtimeWorkspaceID = "";
    connectRealtimeSocket();
  }

  async function applyRoute(workspaceIDParam = "", targetIDParam = "") {
    const serial = ++routeApplySerial;
    // Record admission, not completion: cancelled routes must not suppress a later visit.
    activeRouteKey = routeKey(workspaceIDParam, targetIDParam);
    if (targetIDParam !== thread.selection?.messageID && targetIDParam !== thread.root?.route_id) thread.close();
    try {
      reactionController.clear();
      const routeTarget = targetIDParam.trim()
        ? await resolveRouteTarget(workspaceIDParam, targetIDParam)
        : null;
      if (serial !== routeApplySerial) return;
      const workspace = routeTarget
        ? workspaces.find((candidate) => candidate.id === routeTarget.workspace_id)
        : workspaces.find((candidate) => candidate.id === workspaceIDParam || candidate.route_id === workspaceIDParam) || workspaces[0];
      if (!workspace) {
        commitMessageWindow("", { messages: [], oldest_seq: 0, newest_seq: 0, has_older: false, has_newer: false }, "replace");
        return;
      }
      const workspaceChanged = selectedWorkspaceID !== workspace.id;
      if (workspaceChanged) {
        captureScrollMemory();
        editController.clear();
        messageRequests.clear();
        resetCreateActions();
        selectedWorkspaceID = workspace.id;
        topicsLoadSerial += 1;
        slashCommandsLoadSerial += 1;
        botCommandsLoadSerial += 1;
        workspaceMembersLoadSerial += 1;
        workspaceMembersAbort?.abort();
        workspaceMembersError = "";
        slashCommands = [];
        botCommands = [];
        topics = [];
        workspaceMemberUsers = [];
        selectedChannelID = "";
        selectedDirectID = "";
        thread.close();
        selectedProfile = null;
        activeComposerContext = "message";
        resetSearch();
        resetHistoryPaging();
        messagesLoading = true;
        pendingRealtimeWorkspaceID = workspace.id;
      }

      if (workspaceChanged || channels.length === 0) await loadChannels(false, false);
      if (serial !== routeApplySerial) return;
      if (workspaceChanged || directConversations.length === 0) await loadDirectConversations();
      if (workspaceChanged) {
        await Promise.all([
          loadModerationMembers(),
          loadSlashCommands(workspace.id),
          loadBotCommands(workspace.id),
          loadTopics(workspace.id),
        ]);
        // Mention targets are progressively available; they must not block
        // navigation while a large workspace is paginated in the background.
        void loadWorkspaceMembers(workspace.id);
      }
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
        activeRouteKey = routeKey(routeTarget.workspace_route_id, routeTarget.target_route_id);
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
          updateActiveMessageWindowFlags(targetID);
          connectPendingRealtime(workspace.id);
          return;
        }
        resetTopicStateForConversation(targetID);
        await Promise.all([loadMessages(), loadPinnedMessages()]);
        if (serial !== routeApplySerial) return;
        connectPendingRealtime(workspace.id);
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
          updateActiveMessageWindowFlags(targetID);
          connectPendingRealtime(workspace.id);
          return;
        }
        resetTopicStateForConversation(targetID);
        await loadMessages();
        if (serial !== routeApplySerial) return;
        connectPendingRealtime(workspace.id);
        return;
      }

      if (routeTarget?.target_type === "thread") {
        const resolved = await applyThreadRoute(routeTarget, serial);
        if (serial !== routeApplySerial) return;
        if (resolved) connectPendingRealtime(workspace.id);
        return;
      }

      const fallbackTargetID = defaultTargetID();
      clearRoutePanelState();
      if (!fallbackTargetID) {
        selectedChannelID = "";
        selectedDirectID = "";
        resetTopicStateForConversation("");
        await loadMessages();
        if (workspaceIDParam !== workspace.route_id || targetIDParam) await navigateToApp(workspace.id, "", true);
        connectPendingRealtime(workspace.id);
        return;
      }
      await navigateToApp(workspace.id, fallbackTargetID, true);
    } catch (error) {
      if (serial === routeApplySerial) handleAppLoadError(error);
    }
  }

  async function ensureResolvedRouteTargetLoaded(route: RouteTarget, serial: number): Promise<boolean> {
    if (route.target_type === "channel") {
      if (!channels.some((channel) => channel.id === route.target_id)) await loadChannels(false, false, false);
      return serial === routeApplySerial && channels.some((channel) => channel.id === route.target_id);
    }
    if (route.target_type === "direct") {
      if (!directConversations.some((conversation) => conversation.id === route.target_id)) {
        await loadDirectConversations();
        if (!directConversations.some((conversation) => conversation.id === route.target_id)) {
          const data = await api<{ conversation: DirectConversation }>(`/api/dms/${route.target_id}`);
          upsertDirectConversation(data.conversation);
        }
      }
      return serial === routeApplySerial && directConversations.some((conversation) => conversation.id === route.target_id);
    }
    if (route.parent_type === "channel" && route.parent_id) {
      if (!channels.some((channel) => channel.id === route.parent_id)) await loadChannels(false, false, false);
      return serial === routeApplySerial && channels.some((channel) => channel.id === route.parent_id);
    }
    if (route.parent_type === "direct" && route.parent_id) {
      if (!directConversations.some((conversation) => conversation.id === route.parent_id)) {
        await loadDirectConversations();
        if (!directConversations.some((conversation) => conversation.id === route.parent_id)) {
          const data = await api<{ conversation: DirectConversation }>(`/api/dms/${route.parent_id}`);
          upsertDirectConversation(data.conversation);
        }
      }
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

  async function applyThreadRoute(route: RouteTarget, serial: number): Promise<boolean> {
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
    messageRequests.prune();
    const sameThread = thread.root?.id === route.target_id && viewKey === currentConversationKey();
    selectedProfile = null;
    pinnedPanelOpen = false;
    activeComposerContext = "thread";
    mobileNavOpen = false;
    if (!await selectThread(route.target_id, undefined, () => serial === routeApplySerial)) return false;
    const selection = thread.selection;
    if (parentChannelID) await loadPinnedMessages();
    if (!thread.isCurrent(selection) || serial !== routeApplySerial) return false;
    if (
      !sameThread &&
      parentChannelID &&
      thread.root &&
      (thread.root.thread_state?.reply_count ?? 0) === 0
    ) {
      const root = thread.root;
      thread.close();
      activeComposerContext = "message";
      await loadMessagesAround(root);
      return true;
    }
    if (!sameThread && thread.root) await loadMessagesAround(thread.root);
    return true;
  }

  function resetCreateActions() {
    createActionSerial++;
    createPending = null;
    workspaceCreateError = "";
    channelCreateError = "";
    directCreateError = "";
    showWorkspaceCreate = false;
    showCreateChannel = false;
    showCreateDirect = false;
  }

  function toggleWorkspaceCreate() {
    const open = !showWorkspaceCreate;
    resetCreateActions();
    showWorkspaceCreate = open;
  }

  function openCreateChannel() {
    resetCreateActions();
    showCreateChannel = true;
  }

  function openCreateDirect() {
    resetCreateActions();
    showCreateDirect = true;
    void loadWorkspaceMembers();
  }

  async function createWorkspace() {
    if (createPending === "workspace" || !workspaceName.trim()) return;
    const workspaceID = selectedWorkspaceID;
    const routeSerial = routeApplySerial;
    const request = ++createActionSerial;
    const isCurrent = () => request === createActionSerial && routeSerial === routeApplySerial && workspaceID === selectedWorkspaceID;
    createPending = "workspace";
    workspaceCreateError = "";
    try {
      const data = await api<{ workspace: Workspace }>("/api/workspaces", {
        method: "POST",
        body: JSON.stringify({ name: workspaceName })
      });
      // A committed create remains discoverable after its form loses ownership.
      if (!workspaces.some((workspace) => workspace.id === data.workspace.id)) {
        workspaces = [...workspaces, data.workspace];
      }
      if (!isCurrent()) return;
      workspaceName = "";
      showWorkspaceCreate = false;
      mobileNavOpen = false;
      await navigateToApp(data.workspace.id);
    } catch (error) {
      if (isCurrent()) workspaceCreateError = readableAPIError(error, "Could not create workspace");
    } finally {
      if (request === createActionSerial) createPending = null;
    }
  }

  async function selectWorkspace(workspaceID: string) {
    mobileNavOpen = false;
    await navigateToApp(workspaceID);
  }

  async function loadChannels(loadInitialMessages = true, selectFallback = true, resetSidePanel = true) {
    const workspaceID = selectedWorkspaceID;
    if (!workspaceID) return;
    const serial = ++channelsLoadSerial;
    const data = await api<{ channels: Channel[] }>(`/api/workspaces/${workspaceID}/channels`);
    if (serial !== channelsLoadSerial || workspaceID !== selectedWorkspaceID) return;
    channels = data.channels;
    if (selectFallback) {
      selectedChannelID =
        channels.find((channel) => channel.id === selectedChannelID)?.id ||
        channels.find((channel) => !channel.archived_at)?.id ||
        channels[0]?.id ||
        "";
    } else if (selectedChannelID && !channels.some((channel) => channel.id === selectedChannelID)) {
      selectedChannelID = "";
    }
    if (resetSidePanel) {
      if (searchThreadDetour) resetSearch();
      thread.close();
      selectedProfile = null;
      activeComposerContext = "message";
    }
    if (loadInitialMessages) await loadMessages();
  }

  async function loadTopics(workspaceID = selectedWorkspaceID) {
    const serial = ++topicsLoadSerial;
    const conversationKey = currentConversationKey();
    const channelID = selectedChannelID;
    let clearedInvalidFilter = false;
    if (!workspaceID) {
      topics = [];
      return;
    }
    try {
      const data = await api<{ topics: Topic[] }>(`/api/workspaces/${workspaceID}/topics`);
      if (serial !== topicsLoadSerial || workspaceID !== selectedWorkspaceID) return;
      topics = data.topics;
      if (currentConversationKey() !== conversationKey || selectedChannelID !== channelID) return;
      const eligibleTopicIDs = new Set(
        topicsForChannel(data.topics, channelID).map((topic) => topic.id),
      );
      if (selectedComposerTopicID && !eligibleTopicIDs.has(selectedComposerTopicID)) {
        selectedComposerTopicID = "";
      }
      if (activeTopicFilterID && !eligibleTopicIDs.has(activeTopicFilterID)) {
        updateActiveTopicFilter("");
        scrollMemory.delete(currentConversationKey());
        messageWindows.delete(currentConversationKey());
        clearedInvalidFilter = true;
      }
    } catch {
      // A transient refresh failure is not authoritative. Preserve the last
      // known topics so an active filter and its clear control stay visible.
      return;
    }
    if (!clearedInvalidFilter || currentConversationKey() !== conversationKey) return;
    try {
      await loadLatestMessages();
    } catch (error) {
      if (currentConversationKey() !== conversationKey || activeTopicFilterID) return;
      updateActiveMessages();
      composerNotice = {
        kind: "error",
        text:
          error instanceof Error
            ? `Topic changed, but messages could not reload: ${error.message}`
            : "Topic changed, but messages could not reload",
      };
    }
  }

  function activateMessageComposer() {
    activeComposerContext = "message";
    if (selectedChannelID) void loadTopics();
  }

  async function loadModerationMembers(workspaceID = selectedWorkspaceID) {
    const serial = ++moderationMembersLoadSerial;
    if (!workspaceID || workspaceID !== selectedWorkspaceID || (currentWorkspaceRole !== "owner" && currentWorkspaceRole !== "moderator")) {
      if (serial === moderationMembersLoadSerial) moderationMembers = [];
      return;
    }
    try {
      const data = await api<{ members: MemberModeration[] }>(`/api/workspaces/${workspaceID}/moderation/members`);
      if (serial !== moderationMembersLoadSerial || workspaceID !== selectedWorkspaceID) return;
      moderationMembers = data.members;
    } catch {
      if (serial === moderationMembersLoadSerial && workspaceID === selectedWorkspaceID) {
        moderationMembers = [];
      }
    }
  }

  async function loadWorkspaceMembers(workspaceID = selectedWorkspaceID) {
    const serial = ++workspaceMembersLoadSerial;
    workspaceMembersAbort?.abort();
    const controller = new AbortController();
    workspaceMembersAbort = controller;
    workspaceMembersError = "";
    if (!workspaceID) {
      workspaceMemberUsers = [];
      return;
    }
    try {
      const members = await listAllWorkspaceMembers({ workspaceID, limit: 100, signal: controller.signal });
      if (serial !== workspaceMembersLoadSerial || workspaceID !== selectedWorkspaceID) return;
      workspaceMemberUsers = members.map((member) => member.user);
    } catch (error) {
      if (!controller.signal.aborted && serial === workspaceMembersLoadSerial && workspaceID === selectedWorkspaceID) {
        workspaceMemberUsers = [];
        workspaceMembersError = memberLoadErrorMessage(error);
      }
    }
  }

  async function loadSlashCommands(workspaceID = selectedWorkspaceID) {
    const serial = ++slashCommandsLoadSerial;
    if (!workspaceID) {
      slashCommands = [];
      return;
    }
    try {
      const data = await api<{ slash_commands: SlashCommand[] }>(`/api/workspaces/${workspaceID}/slash-commands`);
      if (serial !== slashCommandsLoadSerial || workspaceID !== selectedWorkspaceID) return;
      slashCommands = data.slash_commands;
    } catch {
      if (serial === slashCommandsLoadSerial && workspaceID === selectedWorkspaceID) {
        slashCommands = [];
      }
    }
  }

  async function loadBotCommands(
    workspaceID = selectedWorkspaceID,
    propagateError = false,
  ) {
    const serial = ++botCommandsLoadSerial;
    if (!workspaceID) {
      botCommands = [];
      return;
    }
    try {
      const commands = await listBotCommands(workspaceID);
      if (serial !== botCommandsLoadSerial || workspaceID !== selectedWorkspaceID) return;
      botCommands = commands;
    } catch (error) {
      const isCurrent = serial === botCommandsLoadSerial && workspaceID === selectedWorkspaceID;
      if (isCurrent) botCommands = [];
      if (propagateError && isCurrent) throw error;
    }
  }

  function clearComposerNoticeFor(_conversationKey: string) {
    slashDispatchGeneration += 1;
    composerNotice = null;
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
  }

  async function createChannel() {
    if (createPending === "channel" || !selectedWorkspaceID || !channelName.trim()) return;
    const workspaceID = selectedWorkspaceID;
    const routeSerial = routeApplySerial;
    const request = ++createActionSerial;
    const isCurrent = () => request === createActionSerial && routeSerial === routeApplySerial && workspaceID === selectedWorkspaceID;
    createPending = "channel";
    channelCreateError = "";
    try {
      const data = await api<{ channel: Channel }>(`/api/workspaces/${workspaceID}/channels`, {
        method: "POST",
        body: JSON.stringify({ name: channelName, kind: "public" })
      });
      if (workspaceID === selectedWorkspaceID && !channels.some((channel) => channel.id === data.channel.id)) {
        channels = [...channels, data.channel];
      }
      if (!isCurrent()) return;
      channelName = "";
      showCreateChannel = false;
      await navigateToApp(workspaceID, data.channel.id);
    } catch (error) {
      if (isCurrent()) channelCreateError = readableAPIError(error, "Could not create channel");
    } finally {
      if (request === createActionSerial) createPending = null;
    }
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

  async function loadChannelNotifPreference(channelID: string, directID: string) {
    const serial = ++channelNotifLoadSerial;
    channelNotifPreference = null;
    if (!channelID || directID) return;
    try {
      const data = await api<{ preference: ChannelNotificationPreference }>(
        `/api/channels/${channelID}/notification-settings`,
      );
      if (serial !== channelNotifLoadSerial || channelID !== selectedChannelID || selectedDirectID) return;
      rememberChannelNotifPreference(channelID, data.preference);
      channelNotifPreference = data.preference;
    } catch {
      if (serial === channelNotifLoadSerial && channelID === selectedChannelID && !selectedDirectID) {
        const fallback = lastKnownChannelNotifPreference(channelID) || "all";
        rememberChannelNotifPreference(channelID, fallback);
        channelNotifPreference = fallback;
      }
    }
  }

  async function cycleChannelNotifPreference() {
    const channelID = selectedChannelID;
    if (!channelID || selectedDirectID || channelNotifSaving || !channelNotifPreference) return;
    const serial = ++channelNotifLoadSerial;
    const cycle: ChannelNotificationPreference[] = ["all", "mentions", "muted"];
    const current = channelNotifPreference;
    const next = cycle[(cycle.indexOf(current) + 1) % cycle.length];
    channelNotifPreference = next;
    rememberChannelNotifPreference(channelID, next);
    channelNotifSaving = true;
    try {
      await api(`/api/channels/${channelID}/notification-settings`, {
        method: "PATCH",
        body: JSON.stringify({ preference: next }),
      });
    } catch {
      if (channelNotifPreferences.get(channelID) === next) {
        rememberChannelNotifPreference(channelID, current);
      }
      if (serial === channelNotifLoadSerial && channelID === selectedChannelID && !selectedDirectID) {
        channelNotifPreference = current;
      }
    } finally {
      channelNotifSaving = false;
    }
  }

  function beginMessageLoad(loading = false): () => boolean {
    resetHistoryPaging();
    messagesLoading = loading;
    messageLoadGeneration += 1;
    // Page selection changes do not retire updates for this conversation's rows.
    messageRequests.prune();
    const scopeKey = activeMessageScopeKey();
    return () => activeMessageScopeKey() === scopeKey;
  }

  async function replaceMessageWindow(query: string, direction: MessageWindowDirection = "replace", isCurrent = beginMessageLoad()) {
    const targetKey = currentConversationKey();
    if (targetKey !== viewKey) messagesLoading = true;
    try {
      if (!targetKey) {
        if (isCurrent()) commitMessageWindow("", { messages: [], oldest_seq: 0, newest_seq: 0, has_older: false, has_newer: false }, direction);
        return;
      }
      await messageRequests.run(() => api<MessagePage>(messagePagePath(query)), (data) => {
        let window = data;
        const previous = messageWindows.get(targetKey);
        // A latest snapshot predating the current tail cannot erase newer confirmed rows.
        if (!window.has_newer && previous && previous.newest_seq > window.newest_seq) {
          window = previous.oldest_seq > window.newest_seq ? previous : {
            ...window,
            messages: mergeMessageWindows(window.messages, previous.messages.filter((message) => messageSeq(message) > window.newest_seq)),
            newest_seq: previous.newest_seq,
            has_newer: previous.has_newer,
          };
        }
        commitMessageWindow(targetKey, window, direction);
      }, isCurrent);
    } catch (error) {
      if (isCurrent()) throw error;
    } finally {
      if (isCurrent()) messagesLoading = false;
    }
  }

  async function loadMessages(preserveScroll = true) {
    if (preserveScroll) captureScrollMemory();
    return replaceMessageWindow(initialMessagePageQuery());
  }

  async function loadLatestMessages(isCurrent = beginMessageLoad()) {
    const targetKey = currentConversationKey();
    if (!targetKey) return;
    messagesLoading = true;
    scrollMemory.set(targetKey, { atBottom: true });
    return replaceMessageWindow(`limit=${INITIAL_MESSAGE_LIMIT}`, "replace", isCurrent);
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
    const params = new URLSearchParams(query);
    if (!selectedDirectID && activeTopicFilterID) params.set("topic_id", activeTopicFilterID);
    const resolvedQuery = params.toString();
    return resolvedQuery ? `${base}?${resolvedQuery}` : base;
  }

  async function setTopicFilter(topicID: string) {
    if (!selectedChannelID || topicID === activeTopicFilterID) return;
    const conversationKey = currentConversationKey();
    const previousTopicID = activeTopicFilterID;
    const previousComposerTopicID = selectedComposerTopicID;
    const previousWindow = messageWindows.get(conversationKey);
    const previousScroll = scrollMemory.get(conversationKey);
    updateActiveTopicFilter(topicID);
    const generation = topicFilterGeneration;
    if (topicID) selectedComposerTopicID = topicID;
    scrollMemory.delete(conversationKey);
    messageWindows.delete(conversationKey);
    try {
      await loadLatestMessages();
    } catch (error) {
      if (
        currentConversationKey() !== conversationKey ||
        topicFilterGeneration !== generation
      ) {
        return;
      }
      updateActiveTopicFilter(previousTopicID);
      if (selectedComposerTopicID === topicID) {
        selectedComposerTopicID = previousComposerTopicID;
      }
      messageWindows.delete(conversationKey);
      const rollbackGeneration = topicFilterGeneration;
      try {
        await loadLatestMessages();
        if (
          currentConversationKey() !== conversationKey ||
          topicFilterGeneration !== rollbackGeneration
        ) {
          return;
        }
        if (previousScroll) {
          scrollMemory.set(conversationKey, previousScroll);
          viewRestoreState = previousScroll;
        }
      } catch {
        if (
          currentConversationKey() !== conversationKey ||
          topicFilterGeneration !== rollbackGeneration
        ) {
          return;
        }
        if (previousWindow) {
          rememberMessageWindow(conversationKey, previousWindow);
          commitView(conversationKey, previousWindow.messages);
          updateActiveMessageWindowFlags(conversationKey, previousWindow);
        } else {
          updateActiveMessages();
        }
        if (previousScroll) {
          scrollMemory.set(conversationKey, previousScroll);
          viewRestoreState = previousScroll;
        }
      }
      composerNotice = {
        kind: "error",
        text:
          error instanceof Error
            ? `Topic could not change: ${error.message}`
            : "Topic could not change",
      };
    }
  }

  function commitMessageWindow(
    key: string,
    window: MessagePage,
    direction: MessageWindowDirection,
  ) {
    const confirmed = outgoingForView(key)
      .flatMap((outgoing) => outgoing.receipt ? [outgoing.receipt] : [])
      .sort((a, b) => messageSeq(a) - messageSeq(b));
    let newestSeq = messageSeq(window.messages.at(-1));
    // Receipts extend a fetched live interval only when they prove the next sequence.
    // Topic-filtered gaps and older history must still be fetched through the page API.
    if (!window.has_newer) {
      for (const message of confirmed) {
        if (messageSeq(message) === newestSeq + 1) {
          window = { ...window, messages: [...window.messages, message] };
          newestSeq = messageSeq(message);
        }
      }
    }
    if (confirmed.some((message) => messageSeq(message) > newestSeq)) window = { ...window, has_newer: true };
    const trimmedMessages = trimMessageWindow(key, window.messages, direction);
    const firstSeq = trimmedMessages[0]?.channel_seq || 0;
    const lastSeq = trimmedMessages[trimmedMessages.length - 1]?.channel_seq || 0;
    const droppedOlder = firstSeq > (window.messages[0]?.channel_seq || firstSeq);
    const droppedNewer = lastSeq < (window.messages[window.messages.length - 1]?.channel_seq || lastSeq);
    const nextWindow: MessagePage = {
      messages: trimmedMessages,
      oldest_seq: firstSeq,
      newest_seq: lastSeq,
      has_older: window.has_older || droppedOlder,
      has_newer: window.has_newer || droppedNewer,
    };
    rememberMessageWindow(key, nextWindow);
    updateActiveMessageWindowFlags(key, nextWindow);
    // An append must not consume the position captured for a pending history page.
    if (direction !== "append" || key !== viewKey) viewRestoreState = scrollMemory.get(key);
    commitView(key, trimmedMessages);
  }

  function rememberMessageWindow(key: string, window: MessagePage) {
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
    const editSession = editController.session(key);
    if (editSession) ids.add(editSession.messageID);
    if (thread.root && belongsToView(thread.root, key)) ids.add(thread.root.id);
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
    const scopeKey = activeMessageScopeKey();
    const window = messageWindows.get(key);
    const loadKey = `${scopeKey}:older`;
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
      await messageRequests.run(() => api<MessagePage>(messagePagePath(`before_seq=${encodeURIComponent(String(window.oldest_seq))}&limit=${PAGE_MESSAGE_LIMIT}`)), (data) => {
        const currentWindow = messageWindows.get(key);
        if (!currentWindow) return;
        commitMessageWindow(key, {
          ...currentWindow,
          messages: mergeMessageWindows(data.messages, currentWindow.messages),
          has_older: data.has_older,
        }, "prepend");
        committed = true;
        setHistoryEdgeState("older", "settling");
      }, () => activeMessageScopeKey() === scopeKey);
    } catch (error) {
      if (activeMessageScopeKey() === scopeKey) {
        composerNotice = { kind: "error", text: readableAPIError(error, "Could not load older messages") };
      }
    } finally {
      loadingMessagePages.delete(loadKey);
      if (activeMessageScopeKey() === scopeKey && !committed) setHistoryEdgeState("older", "idle");
    }
  }

  async function loadNewerMessages() {
    const key = currentConversationKey();
    const scopeKey = activeMessageScopeKey();
    const window = messageWindows.get(key);
    const loadKey = `${scopeKey}:newer`;
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
      await messageRequests.run(() => api<MessagePage>(messagePagePath(`after_seq=${encodeURIComponent(String(window.newest_seq))}&limit=${PAGE_MESSAGE_LIMIT}`)), (data) => {
        committed = appendMessagePage(key, data, window.newest_seq);
        if (committed) setHistoryEdgeState("newer", "settling");
      }, () => activeMessageScopeKey() === scopeKey);
    } catch (error) {
      if (activeMessageScopeKey() === scopeKey) {
        composerNotice = { kind: "error", text: readableAPIError(error, "Could not load newer messages") };
      }
    } finally {
      loadingMessagePages.delete(loadKey);
      if (activeMessageScopeKey() === scopeKey && !committed) setHistoryEdgeState("newer", "idle");
    }
  }

  async function loadNewerMessagesFromRealtime(isCurrent: () => boolean): Promise<void> {
    const targetKey = currentConversationKey();
    const scopeKey = activeMessageScopeKey();
    if (!selectedWorkspaceID || !targetKey) return;

    // The durable event queue serializes these fetches, independently of frames.
    const window = messageWindows.get(targetKey);
    if (!window || window.newest_seq <= 0) {
      await loadMessages();
      return;
    }
    await messageRequests.run(
      () => api<MessagePage>(messagePagePath(
        `after_seq=${encodeURIComponent(String(window.newest_seq))}&limit=${PAGE_MESSAGE_LIMIT}`,
      )),
      (data) => { appendMessagePage(targetKey, data, window.newest_seq); },
      () => isCurrent() && activeMessageScopeKey() === scopeKey,
    );
  }

  function appendMessagePage(key: string, data: MessagePage, afterSeq: number): boolean {
    const currentWindow = messageWindows.get(key);
    if (!currentWindow) return false;
    const responseNewestSeq = data.newest_seq || afterSeq;
    const hasNewer =
      responseNewestSeq > currentWindow.newest_seq
        ? data.has_newer
        : responseNewestSeq < currentWindow.newest_seq
          ? currentWindow.has_newer
          : currentWindow.has_newer || data.has_newer;
    commitMessageWindow(
      key,
      {
        ...currentWindow,
        messages: mergeMessageWindows(currentWindow.messages, data.messages),
        has_newer: hasNewer,
      },
      "append",
    );
    return true;
  }

  function handleHistorySettled(state: MessageListViewportState) {
    const shouldLoadOlder =
      olderPageState === "settling" && pendingOlderPageIntent && state.nearOlder && activeHasOlder;
    const shouldLoadNewer =
      newerPageState === "settling" && pendingNewerPageIntent && state.nearNewer && activeHasNewer;

    if (olderPageState === "settling") {
      setHistoryEdgeState("older", "idle");
      pendingOlderPageIntent = false;
    }
    if (newerPageState === "settling") {
      setHistoryEdgeState("newer", "idle");
      pendingNewerPageIntent = false;
    }

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
    if (activeTopicFilterID && channels.some((channel) => channel.id === key)) return;
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

  async function notificationPreferenceForChannel(channelID: string): Promise<ChannelNotificationPreference | null> {
    try {
      const data = await api<{ preference: ChannelNotificationPreference }>(
        `/api/channels/${channelID}/notification-settings`,
      );
      rememberChannelNotifPreference(channelID, data.preference);
      if (channelID === selectedChannelID && !selectedDirectID && !channelNotifSaving) {
        channelNotifPreference = data.preference;
      }
      return data.preference;
    } catch {
      // Preserve a last-known restrictive choice; otherwise retain the historical all default.
      return lastKnownChannelNotifPreference(channelID) || "all";
    }
  }

  function channelNotifStorageKey(channelID: string): string {
    return user?.id && channelID ? `${CHANNEL_NOTIFICATION_STORAGE_PREFIX}${user.id}:${channelID}` : "";
  }

  function storedChannelNotifPreference(channelID: string): ChannelNotificationPreference | null {
    const key = channelNotifStorageKey(channelID);
    if (!key) return null;
    try {
      const value = window.localStorage.getItem(key);
      return value === "all" || value === "mentions" || value === "muted" ? value : null;
    } catch {
      return null;
    }
  }

  function lastKnownChannelNotifPreference(channelID: string): ChannelNotificationPreference | null {
    return channelNotifPreferences.get(channelID) || storedChannelNotifPreference(channelID);
  }

  function rememberChannelNotifPreference(
    channelID: string,
    preference: ChannelNotificationPreference,
  ) {
    channelNotifPreferences = new Map(channelNotifPreferences).set(channelID, preference);
    const key = channelNotifStorageKey(channelID);
    if (!key) return;
    try {
      window.localStorage.setItem(key, preference);
    } catch {
      // The in-memory value remains authoritative for this session.
    }
  }

  async function maybeShowBrowserNotification(event: RealtimeEvent, affectsActiveView: boolean) {
    if (event.type !== "message.created" && event.type !== "thread.reply_created") return;
    const { channelID, dmID } = messageEventScope(event);
    const seq = eventMessageSeq(event);
    if (event.type === "message.created" && seq > 0) {
      const key = channelID || dmID;
      if (seq <= (notificationMessageSeqs.get(key) || 0)) return;
      // Track received events independently of snapshots, before awaiting delivery,
      // so a handler retry cannot repeat the alert.
      notificationMessageSeqs.set(key, seq);
    }
    const payload = event.payload as Record<string, unknown>;
    const kind = typeof payload.kind === "string" ? payload.kind : "";
    if (kind === "agent_commentary" || kind === "agent_tool") return;
    if (!browserNotificationsEnabled) return;
    if (document.visibilityState === "visible" && affectsActiveView) return;
    if (channelID) {
      const preference = await notificationPreferenceForChannel(channelID);
      if (!preference || !browserNotificationsEnabled) return;
      const stillAffectsActiveView = channelID === selectedChannelID && !selectedDirectID;
      if (document.visibilityState === "visible" && stillAffectsActiveView) return;
      if (preference === "muted") return;
      if (preference === "mentions" && !event.mentioned_user_ids?.includes(user?.id || "")) return;
    }
    const messageID =
      typeof payload.message_id === "string"
        ? payload.message_id
        : `${event.channel_id || ""}:${event.seq || Date.now()}`;
    let authorID = typeof payload.author_id === "string" ? payload.author_id : "";
    let rawBody = typeof payload.body === "string" ? payload.body : "New message";
    if (event.type === "thread.reply_created" && typeof payload.message_id === "string") {
      try {
        const data = await api<{ message: Message }>(`/api/messages/${payload.message_id}`);
        authorID = data.message.author_id;
        rawBody = data.message.body;
      } catch {
        // A generic alert is safer than copying message content into durable event metadata.
      }
    }
    if (authorID && authorID === user?.id) return;
    if (!browserNotificationsEnabled) return;
    if (document.visibilityState === "visible") {
      const stillAffectsActiveView =
        (Boolean(channelID) && channelID === selectedChannelID) ||
        (Boolean(dmID) && dmID === selectedDirectID);
      if (stillAffectsActiveView) return;
    }
    const channel = channels.find((candidate) => candidate.id === channelID);
    const author = lookupUser(authorID);
    const authorName = author?.display_name || "ClickClack";
    const place = channel ? `#${channelDisplayTitle(channel)}` : "Direct message";
    if (desktop) {
      void desktop.notify({
        body: notificationBody(rawBody),
        route: notificationHref(channelID || dmID),
        tag: `clickclack:${messageID}`,
        title: `${authorName} in ${place}`,
      });
      return;
    }
    if (typeof Notification === "undefined" || Notification.permission !== "granted") return;
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

  async function loadUnknownDirectConversationFromEvent(event: RealtimeEvent): Promise<boolean> {
    const payload = event.payload as Record<string, unknown>;
    const dmID = typeof payload.direct_conversation_id === "string" ? payload.direct_conversation_id : "";
    if (!dmID || directConversations.some((conversation) => conversation.id === dmID)) return false;
    await loadDirectConversations();
    return directConversations.some((conversation) => conversation.id === dmID);
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
    // Unfetched sends overlay the canonical window without changing its page cursors.
    for (const [nonce, outgoing] of outgoingMessages) {
      if (!outgoing.message.status && (outgoing.draft.viewKey !== key ||
        (activeTopicFilterID && outgoing.draft.topicID !== activeTopicFilterID))) outgoingMessages.delete(nonce);
    }
    const localOptimistic = outgoingForView(key).map((outgoing) => outgoing.message);
    const localByID = new Map(localOptimistic.map((m) => [m.id, m]));
    const localByNonce = new Map(localOptimistic.filter((m) => m.nonce).map((m) => [m.nonce, m]));
    reactionController.seedMessages(msgs);
    const merged = msgs.map((m) => {
      const local = localByID.get(m.id) || (m.nonce ? localByNonce.get(m.nonce) : undefined);
      if (!local) return m;
      if (!local.status && (local.attachments || []).every((attachment) => m.attachments?.some((fresh) => fresh.id === attachment.id))) {
        outgoingMessages.delete(local.nonce!);
      }
      return {
        ...m,
        nonce: local.nonce,
        status: local.status,
        attachments: local.attachments?.length ? local.attachments : m.attachments,
      };
    });
    const knownIDs = new Set(merged.map((m) => m.id));
    const knownNonces = new Set(merged.map((m) => m.nonce).filter(Boolean));
    const preserve = localOptimistic.filter(
      (m) =>
        !knownIDs.has(m.id) &&
        !(m.nonce && knownNonces.has(m.nonce)),
    );
    messages = [...merged, ...preserve].sort((a, b) => (a.channel_seq || Infinity) - (b.channel_seq || Infinity));
    editController.reconcile(key, messages);
    viewKey = key;
    rememberUnreadMarkerForMessages(key, messages);
    if (switchingView) {
      typingEntries = [];
      agentProgressTurns = [];
      stopTyping();
    }
  }

  function updateActiveMessage(updated: MessageUpdate) {
    updateActiveMessages(messageRequests.updateMessage(updated));
  }

  function updateActiveAuthor(updated: AuthorUpdate) {
    updateActiveMessages(messageRequests.updateAuthor(updated));
  }

  function updateActiveMessages(update?: (message: Message) => Message) {
    const key = currentConversationKey();
    const window = key ? messageWindows.get(key) : undefined;
    if (update) {
      for (const outgoing of outgoingMessages.values()) {
        const local = outgoing.message;
        // Server metadata cannot complete or discard an in-flight send or attachment.
        outgoing.message = { ...update(local), status: local.status, attachments: local.attachments };
        if (outgoing.receipt) outgoing.receipt = update(outgoing.receipt);
      }
    }
    const canonical = update ? window?.messages.map(update) || [] : window?.messages || [];
    if (window) commitMessageWindow(key, { ...window, messages: canonical }, "append");
    else commitView(key, canonical);
  }

  function messageSeq(message: Message | undefined): number {
    return message?.channel_seq || 0;
  }

  function belongsToView(message: Message, key: string): boolean {
    if (!key) return false;
    return message.channel_id === key || message.direct_conversation_id === key;
  }

  async function scrollMessagesToBottom(isCurrent: () => boolean = () => true) {
    await tick();
    if (isCurrent()) await messageList?.scrollToBottom();
  }

  function isAtLiveEdge(): boolean {
    return messageList?.isFollowing() || messageList?.isNearBottom(LIVE_EDGE_TOLERANCE_PX) !== false;
  }

  async function jumpToLiveChat(revealSentMessage = false) {
    const reload = messagesLoading || activeHasNewer || activeUnreadCount > 0;
    const isCurrent = beginMessageLoad();
    try {
      if (reload) await loadLatestMessages(isCurrent);
      await scrollMessagesToBottom(isCurrent);
      if (!isCurrent()) return;
      if (!activeHasNewer) markActiveViewRead({ all: true });
      await scrollMessagesToBottom(isCurrent);
    } catch (error) {
      if (isCurrent()) {
        composerNotice = { kind: "error", text: readableAPIError(error, "Could not jump to latest messages") };
        // The send receipt is confirmed even when missing history cannot reload.
        if (revealSentMessage) await scrollMessagesToBottom(isCurrent);
      }
    }
  }

  type OutgoingDraft = {
    body: string;
    quotedMessageID?: string;
    upload?: Upload;
    workspaceID: string;
    channelID?: string;
    directConversationID?: string;
    topicID?: string;
    topicFilterID: string;
    topicFilterGeneration: number;
    viewKey: string;
  };

  type OutgoingMessage = {
    draft: OutgoingDraft;
    message: Message;
    receipt?: Message;
  };

  function buildOptimisticMessage(nonce: string, draft: OutgoingDraft, id = `tmp_${nonce}`): Message {
    const now = new Date().toISOString();
    return {
      id,
      workspace_id: draft.workspaceID,
      channel_id: draft.channelID,
      direct_conversation_id: draft.directConversationID,
      topic_id: draft.topicID,
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
    if (selectedDirect && !selectedDirectWritable) {
      composerNotice = { kind: "error", text: "This conversation has no active recipient" };
      return;
    }
    if (!selectedChannelID && !selectedDirectID) {
      composerNotice = { kind: "ephemeral", text: "Pick or create a channel to send a message." };
      return;
    }
    stopTyping();
    composerNotice = null;
    const dispatchGeneration = ++slashDispatchGeneration;
    const conversationKey = currentConversationKey();
    const activeContext: "channel" | "dm" = selectedDirectID ? "dm" : "channel";
    const quote = replyTarget && replyContext === activeContext ? replyTarget : null;
    // Registered HTTP slash commands dispatch through the hook endpoint in
    // channels (Slack semantics: the invocation itself is never posted).
    // Bot-declared and unknown commands fall through to a plain message.
    if (selectedChannelID && !selectedDirectID && !pendingUpload && !quote) {
      const slash = splitSlashDraft(body);
      const registered = slash ? findRegisteredCommand(slashCommands, slash.command) : undefined;
      if (slash && registered) {
        messageBody = "";
        clearPendingUpload();
        await dispatchRegisteredCommand(
          selectedChannelID,
          slash.command,
          slash.text,
          body,
          conversationKey,
          dispatchGeneration,
        );
        return;
      }
    }
    const draft: OutgoingDraft = {
      body,
      quotedMessageID: quote?.id,
      upload: pendingUpload || undefined,
      workspaceID: selectedWorkspaceID,
      channelID: selectedChannelID || undefined,
      directConversationID: selectedDirectID || undefined,
      topicID: selectedChannelID ? selectedComposerTopicID || undefined : undefined,
      topicFilterID: activeTopicFilterID,
      topicFilterGeneration,
      viewKey: currentConversationKey(),
    };
    messageBody = "";
    if (quote) clearReplyTarget();
    clearPendingUpload();
    await dispatchDraft(draft);
  }

  async function dispatchRegisteredCommand(
    channelID: string,
    command: string,
    text: string,
    draftBody: string,
    conversationKey: string,
    dispatchGeneration: number,
  ) {
    try {
      const result = await dispatchSlashCommand(channelID, command, text);
      if (
        dispatchGeneration !== slashDispatchGeneration ||
        currentConversationKey() !== conversationKey
      ) {
        return;
      }
      // `in_channel` responses are posted as the bot server-side and arrive
      // over realtime like any other bot message; nothing to render here.
      if (result.text && result.response_type !== "in_channel") {
        composerNotice = { kind: "ephemeral", text: result.text };
      }
    } catch (err) {
      console.warn("slash dispatch failed", err);
      if (
        dispatchGeneration !== slashDispatchGeneration ||
        currentConversationKey() !== conversationKey
      ) {
        return;
      }
      composerNotice = {
        kind: "error",
        text: err instanceof APIError && err.message ? messageFromAPIError(err) : `${command} failed`,
      };
      // Give the draft back so the invocation isn't lost.
      if (!messageBody.trim()) messageBody = draftBody;
    }
  }

  function messageFromAPIError(err: APIError): string {
    try {
      const parsed = JSON.parse(err.message) as { error?: string };
      if (parsed && typeof parsed.error === "string" && parsed.error) return parsed.error;
    } catch {
      // Fall through to the raw body.
    }
    return err.message;
  }

  function outgoingForView(viewKey: string): OutgoingMessage[] {
    return [...outgoingMessages.values()].filter(
      ({ draft }) => draft.viewKey === viewKey &&
        (!activeTopicFilterID || draft.topicID === activeTopicFilterID),
    );
  }

  async function revealFailedDraft(
    outgoing: OutgoingMessage,
    failedMessage: Message,
    notice: string,
    isCurrent: () => boolean,
  ) {
    const { draft } = outgoing;
    outgoing.message = failedMessage;
    if (currentConversationKey() !== draft.viewKey) return;
    let reloadFailed = false;
    const originalFilterStillActive =
      activeTopicFilterID === draft.topicFilterID &&
      topicFilterGeneration === draft.topicFilterGeneration;
    if (
      isCurrent() && originalFilterStillActive &&
      activeTopicFilterID &&
      draft.topicID !== activeTopicFilterID
    ) {
      updateActiveTopicFilter("");
      scrollMemory.delete(draft.viewKey);
      messageWindows.delete(draft.viewKey);
      isCurrent = beginMessageLoad();
      try {
        await loadLatestMessages(isCurrent);
      } catch {
        reloadFailed = true;
      }
    }
    if (currentConversationKey() !== draft.viewKey) return;
    if (activeTopicFilterID && draft.topicID !== activeTopicFilterID) {
      if (!messageBody.trim()) messageBody = draft.body;
      composerNotice = {
        kind: "error",
        text: `${notice} Clear the active topic filter to recover the draft.`,
      };
      return;
    }
    updateActiveMessages();
    composerNotice = {
      kind: "error",
      text: reloadFailed ? `${notice} The unfiltered timeline could not reload.` : notice,
    };
    await scrollMessagesToBottom(isCurrent);
  }

  async function dispatchDraft(draft: OutgoingDraft, existingNonce?: string, existingMessageID?: string) {
    const revealGeneration = messageLoadGeneration;
    const isCurrent = () => messageLoadGeneration === revealGeneration && currentConversationKey() === draft.viewKey;
    const nonce = existingNonce ?? newNonce();
    const tmpID = `tmp_${nonce}`;
    const localID = existingMessageID ?? tmpID;
    const matchesTopicFilter = !activeTopicFilterID || draft.topicID === activeTopicFilterID;
    const shouldRevealSentMessage =
      !existingNonce && currentConversationKey() === draft.viewKey && matchesTopicFilter;
    const placeholder = buildOptimisticMessage(nonce, draft, localID);
    const outgoing: OutgoingMessage = { draft, message: placeholder, receipt: outgoingMessages.get(nonce)?.receipt };
    outgoingMessages.set(nonce, outgoing);
    if (currentConversationKey() === draft.viewKey && matchesTopicFilter) {
      updateActiveMessages();
      if (!existingNonce) void scrollMessagesToBottom();
    }
    const path = draft.directConversationID
      ? `/api/dms/${draft.directConversationID}/messages`
      : `/api/channels/${draft.channelID}/messages`;
    const payload: Record<string, unknown> = { body: draft.body, nonce };
    if (draft.quotedMessageID) payload.quoted_message_id = draft.quotedMessageID;
    if (draft.topicID) payload.topic_id = draft.topicID;
    try {
      let message = outgoing.receipt || (await api<{ message: Message }>(path, {
        method: "POST",
        body: JSON.stringify(payload),
      })).message;
      // The text is durable before attachment linking, including when linking fails.
      outgoing.receipt = message;
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
          await revealFailedDraft(
            outgoing,
            failedMessage,
            "The message was sent, but its attachment failed. Retry or discard it below.",
            isCurrent,
          );
          return;
        }
      }
      outgoing.receipt = message;
      outgoing.message = { ...message, nonce };
      if (currentConversationKey() === draft.viewKey) updateActiveMessages();
      else outgoingMessages.delete(nonce);
    } catch (err) {
      console.warn("send failed", err);
      await revealFailedDraft(
        outgoing,
        { ...placeholder, status: "failed" },
        "The message failed to send. Retry or discard it below.",
        isCurrent,
      );
      return;
    }
    if (isCurrent() && shouldRevealSentMessage) await jumpToLiveChat(true);
  }

  function retryFailedMessage(message: Message) {
    if (!message.nonce) return;
    const draft = outgoingMessages.get(message.nonce)?.draft;
    if (draft) void dispatchDraft(draft, message.nonce, message.id);
  }

  function discardFailedMessage(message: Message) {
    if (!message.nonce) return;
    const outgoing = outgoingMessages.get(message.nonce);
    if (outgoing?.receipt) outgoing.message = { ...outgoing.receipt, nonce: message.nonce };
    else outgoingMessages.delete(message.nonce);
    updateActiveMessages();
  }

  async function revealEditSession(scope: string, session: MessageEditSession) {
    let selection = thread.selection;
    const current = () => scope === activeConversationKey &&
      editController.session(scope)?.generation === session.generation &&
      (session.surface !== "thread" || thread.selection === selection);
    if (!current()) return;
    // Revealing an existing edit supersedes any route still resolving.
    routeApplySerial++;
    if (session.surface === "thread") {
      if (session.threadRootID && selection?.messageID !== session.threadRootID) {
        pinnedPanelOpen = false;
        thread.select(session.threadRootID);
        selection = thread.selection;
        if (!await selectThread(session.threadRootID, undefined, current)) return;
      }
      if (!await thread.target({ messageID: session.messageID, threadSeq: session.threadSeq }, current)) return;
      if (!current()) return;
      if (thread.root?.route_id) await navigateToApp(selectedWorkspaceID, thread.root.id);
    } else {
      messageList?.scrollToMessage(session.messageID);
      await navigateToApp(selectedWorkspaceID, thread.root?.id || currentConversationKey());
    }
    for (let attempt = 0; attempt < 16; attempt += 1) {
      await tick();
      await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
      if (!current()) return;
      const editor = document.querySelector(
        session.surface === "timeline" ? "main.timeline" : '[aria-label="Thread pane"]',
      )?.querySelector(`[data-message-id="${CSS.escape(session.messageID)}"]`)
        ?.querySelector<HTMLTextAreaElement>('textarea[aria-label="Edit message"]');
      if (!editor) continue;
      editor.focus({ preventScroll: true });
      return;
    }
  }

  function applyEditedMessage(updated: MessageEdit) {
    updateActiveMessage(updated);
    thread.updateMessage(updated);
    pinnedMessages = pinnedMessages.map((current) =>
      current.id === updated.id ? mergeMessageUpdate(current, updated) : current,
    );
  }

  function requestMessageDelete(message: Message) {
    if (!message.id || message.deleted_at || deletingMessageIDs.has(message.id)) return;
    pendingDeleteMessage = message;
    deleteMessageError = "";
  }

  async function confirmMessageDelete() {
    const message = pendingDeleteMessage;
    if (!message || deletingMessageIDs.has(message.id)) return;
    deletingMessageIDs = new Set([...deletingMessageIDs, message.id]);
    deleteMessageError = "";
    try {
      const data = await api<{ message: Message }>(`/api/messages/${message.id}`, { method: "DELETE" });
      const deleted = data.message;
      editController.cancelMessage(currentConversationKey(), deleted.id);
      updateActiveMessage(deleted);
      thread.updateMessage(deleted);
      pinnedMessages = pinnedMessages.filter((current) => current.id !== deleted.id);
      if (replyTarget?.id === deleted.id) clearReplyTarget();
      pendingDeleteMessage = null;
    } catch (error) {
      deleteMessageError = error instanceof Error ? error.message : "Could not delete message";
    } finally {
      const next = new Set(deletingMessageIDs);
      next.delete(message.id);
      deletingMessageIDs = next;
    }
  }

  async function openThread(message: Message) {
    routeApplySerial++;
    resetSearch();
    pinnedPanelOpen = false;
    const loaded = await selectThread(message.id, message);
    if (loaded && selectedWorkspaceID && thread.root?.route_id) {
      await navigateToApp(selectedWorkspaceID, thread.root.id);
    }
  }

  async function selectThread(
    messageID: string,
    optimisticRoot?: Message,
    shouldCommit: () => boolean = () => true,
  ): Promise<boolean> {
    selectedArtifact = null;
    artifactConversationKey = "";
    selectedProfile = null;
    activeComposerContext = "thread";
    thread.select(messageID, optimisticRoot);
    try {
      return await thread.open(shouldCommit);
    } catch {
      // The owner retains the load error for the pane; background refreshes still reject.
      return false;
    }
  }

  function reconcileThread(freshMessages: Message[] = []) {
    const root = thread.root;
    if (!root) return;
    reactionController.seedMessages(freshMessages);
    editController.reconcile(currentConversationKey(), [root, ...thread.replies]);
    // Thread view commits own its summary, not the timeline's body or author snapshot.
    updateActiveMessage({ id: root.id, thread_state: root.thread_state });
  }

  async function refreshThreadSummary(messageID: string) {
    await messageRequests.run(() => api<ThreadPage>(`/api/messages/${messageID}/thread?latest=true&limit=1`), (data) => {
      const root = { ...data.root, thread_state: data.thread_state };
      reactionController.seedMessages([root]);
      updateActiveMessage(root);
    });
  }

  async function refreshActiveMessage(messageID: string, isCurrent: () => boolean) {
    if (!messages.some((message) => message.id === messageID) && !messageRequests.pending) return;
    await messageRequests.run(() => api<{ message: Message }>(`/api/messages/${messageID}`), (data) => updateActiveMessage(data.message), isCurrent);
  }

  function shouldRefreshThreadSummary(rootID: string, event: RealtimeEvent): boolean {
    const root = messages.find((message) => message.id === rootID);
    if (!root) return false;
    const eventTime = new Date(event.created_at).getTime();
    if (Number.isFinite(eventTime) && eventTime < appSessionStartedAt) return false;
    const lastReplyAt = root.thread_state?.last_reply_at;
    if (!lastReplyAt) return true;
    const knownTime = new Date(lastReplyAt).getTime();
    if (!Number.isFinite(knownTime) || !Number.isFinite(eventTime)) return true;
    return eventTime > knownTime;
  }

  async function sendReply() {
    if (selectedDirect && !selectedDirectWritable) return;
    await thread.send();
  }

  function setReplyTarget(message: Message, context: "channel" | "dm" | "thread") {
    if (context === "thread") {
      thread.setQuote(message);
    } else {
      replyTarget = message;
      replyContext = context;
    }
    activeComposerContext = context === "thread" ? "thread" : "message";
  }

  function isModalOpen(): boolean {
    return pendingDeleteMessage !== null || selectedImage !== null || settingsModalOpen || channelSettingsOpen || showCreateChannel || showCreateDirect;
  }

  function activeComposerTarget(): HTMLTextAreaElement | null {
    if (activeComposerContext === "thread" && thread.root && replyInput) return replyInput;
    return messageInput;
  }

  function clearReplyTarget() {
    replyTarget = null;
    replyContext = null;
  }

  async function jumpToQuotedMessage(message: Message) {
    const targetID = message.quoted_message_id;
    if (!targetID) return;
    if (message.parent_message_id && message.thread_root_id === thread.selection?.messageID) {
      await thread.target({ messageID: targetID });
      return;
    }
    const isCurrent = beginMessageLoad();
    const scrolled = messageList?.scrollToMessage(targetID) ?? false;
    if (scrolled) {
      await highlightMessage(targetID, isCurrent);
      return;
    }
    const data = await api<{ message: Message }>(`/api/messages/${targetID}`);
    if (!isCurrent() || !belongsToView(data.message, currentConversationKey())) return;
    await loadMessagesAround(data.message);
  }

  async function jumpToUnreadBoundary() {
    beginMessageLoad();
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
  }

  async function highlightMessage(messageID: string, isCurrent: () => boolean) {
    for (let attempt = 0; attempt < 16; attempt += 1) {
      await tick();
      await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
      if (!isCurrent()) return;
      const node = document.querySelector<HTMLElement>(
        `[data-message-id="${CSS.escape(messageID)}"]`,
      );
      if (!node) continue;
      node.classList.add("highlight");
      window.setTimeout(() => node.classList.remove("highlight"), 1500);
      return;
    }
  }

  async function searchMessages() {
    if (!selectedWorkspaceID || !searchQuery.trim()) {
      resetSearch();
      return;
    }
    const query = searchQuery.trim();
    // Search takes over the shared right pane: retire whatever occupies it.
    if (selectedArtifact) closeArtifactViewer();
    if (thread.root || selectedProfile) closeSidePanel();
    const requestID = ++searchRequestID;
    const scope: SearchScope =
      selectedDirectID && selectedDirect
        ? {
            workspaceID: selectedWorkspaceID,
            channelID: "",
            directConversationID: selectedDirectID,
            label: `@${dmTitle(selectedDirect, user?.id)}`,
          }
        : selectedChannelID && selectedChannel
          ? {
              workspaceID: selectedWorkspaceID,
              channelID: selectedChannelID,
              directConversationID: "",
              label: `#${channelDisplayTitle(selectedChannel)}`,
            }
          : { workspaceID: selectedWorkspaceID, channelID: "", directConversationID: "", label: "" };
    searchThreadDetour = false;
    searchReturnScrollTop = 0;
    const session: SearchSession = {
      query,
      scope,
      results: [],
      nextCursor: null,
      state: "loading",
      error: "",
      loadingMore: false,
      moreError: "",
      activeResultID: "",
    };
    searchSession = session;
    try {
      const data = await api<{ results: SearchResult[]; next_cursor: string | null }>(
        `/api/search?${searchPageParams(session).toString()}`,
      );
      if (requestID !== searchRequestID) return;
      searchSession = { ...session, results: data.results, nextCursor: data.next_cursor, state: "ready" };
    } catch (error) {
      if (requestID !== searchRequestID) return;
      searchSession = {
        ...session,
        state: "error",
        error: error instanceof APIError ? error.message : "Search is unavailable right now.",
      };
    }
  }

  function searchPageParams(session: SearchSession, cursor = ""): URLSearchParams {
    const params = new URLSearchParams({ workspace_id: session.scope.workspaceID, q: session.query });
    if (session.scope.directConversationID) params.set("direct_conversation_id", session.scope.directConversationID);
    else if (session.scope.channelID) params.set("channel_id", session.scope.channelID);
    if (cursor) params.set("cursor", cursor);
    return params;
  }

  async function loadMoreSearchResults() {
    const session = searchSession;
    if (!session || session.state !== "ready" || !session.nextCursor || session.loadingMore) return;
    const requestID = searchRequestID;
    searchSession = { ...session, loadingMore: true, moreError: "" };
    try {
      const data = await api<{ results: SearchResult[]; next_cursor: string | null }>(
        `/api/search?${searchPageParams(session, session.nextCursor).toString()}`,
      );
      if (requestID !== searchRequestID || !searchSession) return;
      const seen = new Set(searchSession.results.map((result) => result.id));
      searchSession = {
        ...searchSession,
        results: [...searchSession.results, ...data.results.filter((result) => !seen.has(result.id))],
        nextCursor: data.next_cursor,
        loadingMore: false,
      };
    } catch (error) {
      if (requestID !== searchRequestID || !searchSession) return;
      searchSession = {
        ...searchSession,
        loadingMore: false,
        moreError: error instanceof APIError ? error.message : "Couldn’t load more results.",
      };
    }
  }

  function resetSearch() {
    searchRequestID += 1;
    searchQuery = "";
    searchSession = null;
    searchThreadDetour = false;
    searchReturnScrollTop = 0;
  }

  function searchResultContext(result: SearchResult): string {
    if (result.channel_name) return `#${result.channel_name}`;
    if (result.direct_conversation_id) {
      const conversation = directConversations.find((item) => item.id === result.direct_conversation_id);
      return conversation ? `@${dmTitle(conversation, user?.id)}` : "Direct message";
    }
    return "";
  }

  async function openSearchResult(result: SearchResult) {
    const session = searchSession;
    const targetID = result.channel_id || result.direct_conversation_id || "";
    if (!session || !selectedWorkspaceID || !targetID) return;
    // A result owns the pane even while Back's parent route is still resolving.
    routeApplySerial++;
    searchSession = { ...session, activeResultID: result.id };
    if (currentConversationKey() !== targetID) {
      await navigateToApp(selectedWorkspaceID, targetID);
      await applyRoute(routeWorkspaceIDFor(selectedWorkspaceID), routeTargetIDFor(targetID));
    }
    if (currentConversationKey() !== targetID) return;
    if (result.parent_message_id) {
      // Thread reply: the thread borrows the pane; the session stays for Back.
      const returnScrollTop =
        document.querySelector<HTMLElement>(".search-results-scroll")?.scrollTop ?? 0;
      searchReturnScrollTop = returnScrollTop;
      const requestID = searchRequestID;
      searchThreadDetour = true;
      const loaded = await selectThread(
        result.thread_root_id,
        undefined,
        () =>
          requestID === searchRequestID &&
          searchThreadDetour &&
          searchSession?.activeResultID === result.id,
      );
      if (!loaded) return;
      const targetCurrent = () => requestID === searchRequestID && searchThreadDetour && searchSession?.activeResultID === result.id;
      if (!await thread.target({ messageID: result.id, threadSeq: result.thread_seq }, targetCurrent)) return;
      if (!targetCurrent()) return;
      if (thread.root?.route_id) await navigateToApp(selectedWorkspaceID, thread.root.id);
      await tick();
      if (targetCurrent()) document.querySelector<HTMLElement>(".thread .thread-back")?.focus({ preventScroll: true });
      return;
    }
    await navigateToApp(selectedWorkspaceID, targetID);
    if (result.channel_seq && result.channel_seq > 0) {
      await loadMessagesAroundSeq(result.channel_seq, result.id);
      return;
    }
    await loadMessages();
  }

  async function returnToSearchFromThread() {
    routeApplySerial++;
    if (!searchSession || !searchThreadDetour) return;
    const parentTargetID = currentConversationKey();
    thread.close();
    selectedProfile = null;
    activeComposerContext = "message";
    searchThreadDetour = false;
    if (selectedWorkspaceID && parentTargetID) {
      await navigateToApp(selectedWorkspaceID, parentTargetID);
    }
    await tick();
    const scroll = document.querySelector<HTMLElement>(".search-results-scroll");
    if (scroll) scroll.scrollTop = searchReturnScrollTop;
    const active = searchSession.activeResultID
      ? document.querySelector<HTMLElement>(
          `.search-result[data-result-id="${CSS.escape(searchSession.activeResultID)}"]`,
        )
      : null;
    active?.focus({ preventScroll: true });
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
    const clearingTopic = Boolean(activeTopicFilterID);
    if (clearingTopic) {
      updateActiveTopicFilter("");
      messageWindows.delete(targetKey);
    }
    const isCurrent = beginMessageLoad(clearingTopic);
    if (targetMessageID) {
      scrollMemory.set(targetKey, { atBottom: false, anchorMessageID: targetMessageID, anchorPixelOffset: 0 });
    }
    await replaceMessageWindow(`around_seq=${encodeURIComponent(String(seq))}&limit=${INITIAL_MESSAGE_LIMIT}`, "around", isCurrent);
    await tick();
    if (!isCurrent()) return;
    if (targetMessageID) {
      messageList?.scrollToMessage(targetMessageID);
      await highlightMessage(targetMessageID, isCurrent);
    } else {
      messageList?.scrollToDivider(false);
    }
  }

  function clearPendingUpload() {
    uploadController?.abort();
    uploadController = null;
    uploadWorkspaceID = "";
    pendingUpload = null;
  }

  async function uploadFile(event: Event) {
    const input = event.currentTarget as HTMLInputElement;
    const file = input.files?.[0];
    const workspaceID = selectedWorkspaceID;
    if (!file || !workspaceID) return;
    input.value = "";
    uploadController?.abort();
    const controller = new AbortController();
    uploadController = controller;
    uploadWorkspaceID = workspaceID;
    composerNotice = null;
    const isCurrent = () => uploadController === controller && !controller.signal.aborted && selectedWorkspaceID === workspaceID;
    try {
      const probe = await probeMediaDimensions(file, controller.signal);
      if (!isCurrent()) return;
      const form = new FormData();
      form.set("workspace_id", workspaceID);
      form.set("file", file);
      if (probe.width > 0) form.set("width", String(probe.width));
      if (probe.height > 0) form.set("height", String(probe.height));
      if (probe.durationMS > 0) form.set("duration_ms", String(probe.durationMS));
      const data = await api<{ upload: Upload }>("/api/uploads", { method: "POST", body: form, signal: controller.signal });
      if (isCurrent()) pendingUpload = data.upload;
    } catch (error) {
      if (isCurrent()) composerNotice = { kind: "error", text: readableAPIError(error, "Could not upload file") };
    } finally {
      if (uploadController === controller) uploadController = null;
    }
  }

  async function loadDirectConversations(workspaceID = selectedWorkspaceID) {
    const serial = ++directConversationsLoadSerial;
    if (!workspaceID) {
      directConversations = [];
      return;
    }
    const data = await api<{ conversations: DirectConversation[] }>(`/api/dms?workspace_id=${workspaceID}`);
    if (serial !== directConversationsLoadSerial || workspaceID !== selectedWorkspaceID) return;
    directConversations = data.conversations;
    if (
      selectedDirectID &&
      !directConversations.some((conversation) => conversation.id === selectedDirectID)
    ) {
      selectedDirectID = "";
    }
  }

  function upsertDirectConversation(conversation: DirectConversation) {
    directConversations = directConversations.some((item) => item.id === conversation.id)
      ? directConversations.map((item) => (item.id === conversation.id ? conversation : item))
      : [...directConversations, conversation];
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
    if (createPending === "direct" || !selectedWorkspaceID || !trimmed) return;
    const workspaceID = selectedWorkspaceID;
    const routeSerial = routeApplySerial;
    const request = ++createActionSerial;
    const isCurrent = () => request === createActionSerial && routeSerial === routeApplySerial && workspaceID === selectedWorkspaceID;
    createPending = "direct";
    directCreateError = "";
    try {
      // The server owns exact membership, duplicate prevention, and reopening.
      const data = await api<{ conversation: DirectConversation }>("/api/dms", {
        method: "POST",
        body: JSON.stringify({ workspace_id: workspaceID, member_ids: [trimmed] })
      });
      if (workspaceID === selectedWorkspaceID && !directConversations.some((conversation) => conversation.id === data.conversation.id)) {
        upsertDirectConversation(data.conversation);
      }
      if (!isCurrent()) return;
      directMemberID = "";
      showCreateDirect = false;
      clearRoutePanelState();
      await navigateToApp(workspaceID, data.conversation.id);
    } catch (error) {
      if (isCurrent()) directCreateError = readableAPIError(error, "Could not start direct message");
    } finally {
      if (request === createActionSerial) createPending = null;
    }
  }

  function clearHiddenDirectUndo() {
    if (hiddenDirectUndoTimer) clearTimeout(hiddenDirectUndoTimer);
    hiddenDirectUndoTimer = undefined;
    hiddenDirectUndo = null;
  }

  function scheduleHiddenDirectUndo(conversation: DirectConversation, restoreRoute: boolean) {
    clearHiddenDirectUndo();
    hiddenDirectUndo = {
      conversation,
      restoreRoute,
      title: dmTitle(conversation, user?.id),
    };
    hiddenDirectUndoTimer = setTimeout(() => {
      hiddenDirectUndo = null;
      hiddenDirectUndoTimer = undefined;
    }, 8000);
  }

  async function undoHideDirectConversation() {
    const undo = hiddenDirectUndo;
    if (!undo) return;
    clearHiddenDirectUndo();
    try {
      const data = await api<{ conversation: DirectConversation }>(`/api/dms/${undo.conversation.id}/open`, {
        method: "POST"
      });
      upsertDirectConversation(data.conversation);
      if (undo.restoreRoute) {
        await navigateToApp(undo.conversation.workspace_id, data.conversation.id);
      }
    } catch (error) {
      composerNotice = { kind: "error", text: readableAPIError(error, "Could not restore direct message") };
    }
  }

  async function hideDirectConversation(conversationID: string) {
    if (!conversationID) return;
    const conversation = directConversations.find((item) => item.id === conversationID);
    const restoreRoute = selectedDirectID === conversationID;
    await api(`/api/dms/${conversationID}`, { method: "DELETE" });
    directConversations = directConversations.filter((conversation) => conversation.id !== conversationID);
    if (conversation) scheduleHiddenDirectUndo(conversation, restoreRoute);
    if (restoreRoute) {
      clearRoutePanelState();
      const fallbackID = channels[0]?.id || "";
      selectedDirectID = "";
      selectedChannelID = fallbackID;
      if (fallbackID) rememberLastChannel(selectedWorkspaceID, fallbackID);
      await navigateToApp(selectedWorkspaceID, fallbackID);
      await loadMessages();
    }
  }

  function connectRealtimeSocket() {
    realtimeReconcileSerial += 1;
    socket?.close();
    socket = null;
    connected = false;
    if (!selectedWorkspaceID) return;
    const workspaceID = selectedWorkspaceID;
    socket = connectRealtime({
      workspaceID,
      onEvent: handleEvent,
      onOpen: async (isCurrent, authoritativeResync) => {
        const preserveScroll = realtimeInitializedWorkspaceID === workspaceID;
        await reconcileRealtimeState(
          workspaceID,
          isCurrent,
          preserveScroll,
          authoritativeResync,
        );
        if (!isCurrent()) return;
        if (!preserveScroll || authoritativeResync) {
          // Initial snapshots suppress historical alerts; ordinary refreshes must not consume live events.
          notificationMessageSeqs = new Map(
            [...channels, ...directConversations].map((conversation) => [conversation.id, conversation.last_seq || 0]),
          );
        }
        realtimeInitializedWorkspaceID = workspaceID;
        realtimeError = "";
      },
      onError: (error) => {
        if (workspaceID === selectedWorkspaceID) {
          if (error instanceof WorkspaceUnavailableError) {
            // A newer workspace route may still be waiting for resolution.
            if (routeWorkspaceID && routeWorkspaceID !== workspaceID && routeWorkspaceID !== routeWorkspaceIDFor(workspaceID)) return;
            socket?.close();
            void goto("/app", { invalidateAll: true, replaceState: true }).catch(handleAppLoadError);
            return;
          }
          if (error instanceof APIError && error.status === 401) {
            handleAppLoadError(error);
            return;
          }
          realtimeError = readableAPIError(error, "Could not process realtime event");
        }
      },
      onStatusChange: (next) => {
        connected = next;
      },
    });
  }

  async function reconcileRealtimeState(
    workspaceID: string,
    isCurrent: () => boolean = () => true,
    preserveScroll = true,
    authoritativeResync = true,
  ) {
    const serial = ++realtimeReconcileSerial;
    if (!workspaceID || workspaceID !== selectedWorkspaceID || !isCurrent()) return;
    const selectedThreadID = thread.root?.id || "";
    const selectedBotProfileID = selectedProfile?.kind === "bot" ? selectedProfile.id : "";
    typingEntries = [];
    agentProgressTurns = [];
    if (!authoritativeResync) {
      await Promise.all([loadSlashCommands(workspaceID), loadBotCommands(workspaceID, true)]);
      return;
    }
    reactionController.clear();
    await Promise.all([
      loadWorkspaces(),
      loadChannels(false, false, false),
      loadDirectConversations(workspaceID),
      loadModerationMembers(workspaceID),
      loadSlashCommands(workspaceID),
      loadBotCommands(workspaceID, true),
      loadTopics(workspaceID),
    ]);
    if (serial !== realtimeReconcileSerial || workspaceID !== selectedWorkspaceID || !isCurrent()) return;
    if (selectedChannelID && !channels.some((channel) => channel.id === selectedChannelID)) {
      selectedChannelID = "";
    }
    if (
      selectedDirectID &&
      !directConversations.some((conversation) => conversation.id === selectedDirectID)
    ) {
      selectedDirectID = "";
    }
    if (!selectedChannelID && !selectedDirectID) {
      const fallbackID = defaultTargetID(workspaceID);
      if (fallbackID) {
        const fallbackDirect = directConversations.some((conversation) => conversation.id === fallbackID);
        selectedDirectID = fallbackDirect ? fallbackID : "";
        selectedChannelID = fallbackDirect ? "" : fallbackID;
        if (!fallbackDirect) rememberLastChannel(workspaceID, fallbackID);
        await navigateToApp(workspaceID, fallbackID, true);
      }
    }
    if (serial !== realtimeReconcileSerial || workspaceID !== selectedWorkspaceID || !isCurrent()) return;
    await loadMessages(preserveScroll);
    if (serial !== realtimeReconcileSerial || workspaceID !== selectedWorkspaceID || !isCurrent()) return;
    await loadPinnedMessages();
    if (serial !== realtimeReconcileSerial || workspaceID !== selectedWorkspaceID || !isCurrent()) return;
    if (selectedThreadID && thread.root?.id === selectedThreadID) {
      await thread.refresh(isCurrent);
    }
    if (selectedBotProfileID && selectedProfile?.id === selectedBotProfileID) {
      const refreshed = lookupUser(selectedBotProfileID);
      selectedProfile = refreshed && !refreshed.deleted_at ? refreshed : null;
    }
  }

  async function handleEvent(event: RealtimeEvent, isCurrent: () => boolean) {
    if (
      (event.type === "pin.added" || event.type === "pin.removed") &&
      event.channel_id === selectedChannelID &&
      !selectedDirectID
    ) {
      await loadPinnedMessages();
      return;
    }
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
    if (event.type === "bot_command.updated") {
      // Cursorless command updates represent replaceable latest state. Detach the
      // fetch so a delayed response cannot block a newer invalidation; the loader
      // serial makes only the latest request authoritative.
      if (event.workspace_id === selectedWorkspaceID) {
        void loadBotCommands(event.workspace_id, true).catch((error) => {
          if (event.workspace_id !== selectedWorkspaceID) return;
          realtimeError = readableAPIError(error, "Could not refresh bot commands");
          connectRealtimeSocket();
        });
      }
      return;
    }
    if (event.type === "bot.deleted") {
      if (event.workspace_id === selectedWorkspaceID) await handleBotDeletedEvent(event);
      return;
    }
    if (event.type === "bot.membership_removed") {
      if (event.workspace_id === selectedWorkspaceID) await handleBotMembershipRemovedEvent(event);
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
          commitMessageWindow("", { messages: [], oldest_seq: 0, newest_seq: 0, has_older: false, has_newer: false }, "replace");
          return;
        }
        await loadMessages();
      }
      return;
    }
    if (
      event.workspace_id === selectedWorkspaceID &&
      event.payload.topic_id &&
      !topics.some((topic) => topic.id === event.payload.topic_id)
    ) {
      await loadTopics(event.workspace_id);
    }
    if (
      (event.type === "message.updated" || event.type === "message.deleted") &&
      pinnedMessageIDs.has(event.payload.message_id || "")
    ) {
      await loadPinnedMessages();
    }
    const affectsConversation =
      event.channel_id === selectedChannelID || event.payload.direct_conversation_id === selectedDirectID;
    const affectsActiveView =
      affectsConversation &&
      !(
        event.type === "message.created" &&
        activeTopicFilterID &&
        event.payload.topic_id !== activeTopicFilterID
      );
    if (
      affectsActiveView &&
      (event.type === "reaction.added" || event.type === "reaction.removed")
    ) {
      reactionController.applyEvent(event);
      return;
    }
    void maybeShowBrowserNotification(event, affectsActiveView);
    if (event.type === "message.created" && !affectsActiveView) {
      const loadedConversation = await loadUnknownDirectConversationFromEvent(event);
      if (!loadedConversation) handleUnreadBump(event);
    }
    if (
      affectsActiveView &&
      (event.type === "message.created" || event.type === "message.updated" || event.type === "message.deleted")
    ) {
      // Optimistic-send echo: if this is our own outgoing message, the HTTP
      // response will swap the placeholder; skip the reload to avoid a flicker.
      const echoNonce = event.payload.nonce;
      if (event.type === "message.created" && echoNonce && outgoingMessages.has(echoNonce)) {
        return;
      }
      // Snapshot stuck-to-bottom state BEFORE mutating messages. Once the
      // reload completes, virtua's scrollSize grows while offset is unchanged
      // and the cached atBottom flag flips to false — we'd lose the signal.
      const wasAtLiveEdge = isAtLiveEdge();
      const seq = eventMessageSeq(event);
      const missingMessage = event.type === "message.created" && (
        seq <= 0 || seq > (messageWindows.get(currentConversationKey())?.newest_seq || 0)
      );
      if (missingMessage && !wasAtLiveEdge) {
        suppressAutoReadUntil = Date.now() + 1200;
        markMessageWindowHasNewer(currentConversationKey());
      } else if (missingMessage) {
        await loadNewerMessagesFromRealtime(isCurrent);
      } else if (event.type !== "message.created") {
        await refreshActiveMessage(event.payload.message_id || "", isCurrent);
      }
      if (!isCurrent()) return;
      if (event.type === "message.created") {
        handleUnreadBump(event, wasAtLiveEdge, missingMessage);
      }
      // MessageList owns following and read receipts once layout settles.
      // Waiting for its frames here would stop durable ingestion in hidden tabs.
    }
    if (await thread.handleEvent(event, isCurrent)) {
      return;
    }
    const rootID = event.payload.root_message_id || event.payload.message_id;
    if (rootID && event.type === "thread.state_updated" && shouldRefreshThreadSummary(rootID, event)) {
      await refreshThreadSummary(rootID);
    }
  }

  async function handleBotDeletedEvent(event: RealtimeEvent) {
    const botUserID = event.payload.bot_user_id || "";
    if (!botUserID) return;
    const deletedAt = event.payload.deleted_at || event.created_at;
    const formerHandle = event.payload.former_handle || "";
    const deletedMember = (member: User): User =>
      member.id === botUserID
        ? {
            ...member,
            handle: "",
            former_handle: formerHandle,
            deleted_at: deletedAt,
          }
        : member;

    directConversations = directConversations.map((conversation) => ({
      ...conversation,
      members: conversation.members.map(deletedMember),
    }));
    moderationMembers = moderationMembers.filter((member) => member.user.id !== botUserID);
    slashCommands = slashCommands.filter((command) => command.bot_user_id !== botUserID);
    botCommands = botCommands.filter((command) => command.bot.id !== botUserID);
    typingEntries = typingEntries.filter((entry) => entry.userID !== botUserID);
    if (searchSession) {
      searchSession = {
        ...searchSession,
        results: searchSession.results.map((result) => ({
          ...result,
          author: deletedMember(result.author),
        })),
      };
    }
    if (selectedProfile?.id === botUserID) selectedProfile = null;

    const threadSelection = thread.selection;
    await Promise.all([
      loadDirectConversations(),
      loadModerationMembers(),
      loadSlashCommands(),
      loadBotCommands(),
    ]);
    void loadWorkspaceMembers();
    updateActiveAuthor({ id: botUserID, handle: "", former_handle: formerHandle, deleted_at: deletedAt });
    if (thread.isCurrent(threadSelection)) await thread.refresh();
  }

  async function handleBotMembershipRemovedEvent(event: RealtimeEvent) {
    const botUserID = event.payload.bot_user_id || "";
    if (!botUserID) return;
    moderationMembers = moderationMembers.filter((member) => member.user.id !== botUserID);
    slashCommands = slashCommands.filter((command) => command.bot_user_id !== botUserID);
    botCommands = botCommands.filter((command) => command.bot.id !== botUserID);
    if (selectedProfile?.id === botUserID) selectedProfile = null;
    await Promise.all([
      loadDirectConversations(event.workspace_id),
      loadModerationMembers(event.workspace_id),
      loadSlashCommands(event.workspace_id),
      loadBotCommands(event.workspace_id),
    ]);
    void loadWorkspaceMembers(event.workspace_id);
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

  function handleUnreadBump(event: RealtimeEvent, activeWasAtBottom?: boolean, newToView = false) {
    const payload = event.payload as Record<string, unknown>;
    // Durable agent activity messages never bump unread counts, mirroring the
    // server-side accounting (their rows are excluded from unread subqueries).
    const kind = typeof payload.kind === "string" ? payload.kind : "";
    if (kind === "agent_commentary" || kind === "agent_tool") return;
    // Don't bump for own messages.
    const authorID = typeof payload.author_id === "string" ? payload.author_id : "";
    if (authorID && authorID === user?.id) return;
    // Threaded replies don't affect channel unread (channel_seq isn't assigned).
    if (payload.parent_message_id) return;
    const seq = eventMessageSeq(event);
    const { channelID, dmID } = messageEventScope(event);
    const key = channelID || dmID;
    if (!key) return;
    const state = unreadStateForKey(key);
    const isActive = channelID ? channelID === selectedChannelID && !selectedDirectID : dmID === selectedDirectID;
    const activeAtBottom = isActive && (!channelID || !activeTopicFilterID) && (activeWasAtBottom ?? isAtLiveEdge());
    // A snapshot can count a live message before its row reaches the following view.
    if (seq > 0 && seq <= (state.last_seq || 0) && !(newToView && activeAtBottom)) return;
    const incomingSeq = seq > 0 ? seq : (state.last_seq || 0) + 1;
    const startsUnread = isActive && !activeAtBottom && (state.unread_count || 0) === 0;
    if (startsUnread) rememberUnreadMarkerFromEvent(key, state.last_read_seq || 0, event.created_at);
    const next = {
      last_seq: Math.max(state.last_seq || 0, incomingSeq),
      last_read_seq: startsUnread ? Math.max(state.last_read_seq || 0, incomingSeq - 1) : state.last_read_seq || 0,
      unread_count: activeAtBottom ? 0 : (state.unread_count || 0) + 1,
    };
    if (channelID) channels = channels.map((channel) => channel.id === key ? { ...channel, ...next } : channel);
    else directConversations = directConversations.map((conversation) => conversation.id === key ? { ...conversation, ...next } : conversation);
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
    const userId = typeof payload.user_id === "string" ? payload.user_id : "";
    const turnKey = agentProgressTurnKey(userId, turnId);
    if (op === "clear") {
      agentProgressTurns = agentProgressTurns.filter((turn) => {
        if (turn.turnId !== turnId) return true;
        return userId && turn.userId ? turn.key !== turnKey : false;
      });
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
    const existing = agentProgressTurns.find((turn) => turn.key === turnKey);
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
    const expiresAt = Date.now() + AGENT_PROGRESS_TTL_MS;
    if (!existing) {
      agentProgressTurns = [...agentProgressTurns, { key: turnKey, turnId, userId, lines: [view], expiresAt }];
    } else {
      const lines = existing.lines.some((l) => l.id === lineId)
        ? existing.lines.map((l) => (l.id === lineId ? view : l))
        : [...existing.lines, view];
      agentProgressTurns = agentProgressTurns.map((turn) =>
        turn.key === turnKey ? { ...turn, lines, expiresAt } : turn,
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
    const fromWorkspace = workspaceMemberUsers.find((member) => member.id === userID);
    if (fromWorkspace) return fromWorkspace;
    const fromMessages = messages.find((msg) => msg.author?.id === userID)?.author;
    if (fromMessages) return fromMessages;
    const fromReplies = replies.find((msg) => msg.author?.id === userID)?.author;
    if (fromReplies) return fromReplies;
    if (thread.root?.author?.id === userID) return thread.root.author;
    const fromModeration = moderationMembers.find((member) => member.user.id === userID)?.user;
    if (fromModeration) return fromModeration;
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
    if (!profile || profile.deleted_at) return;
    resetCreateActions();
    resetSearch();
    selectedArtifact = null;
    artifactConversationKey = "";
    clearRoutePanelState();
    selectedProfile = profile;
    commitSelectedRoute();
    if (
      (currentWorkspaceRole === "owner" || currentWorkspaceRole === "moderator") &&
      !moderationMembers.some((member) => member.user.id === profile.id)
    ) {
      void loadModerationMembers();
    }
  }

  function handleComposerKey(event: KeyboardEvent) {
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      void sendMessage();
    }
  }

  function handleReplyKey(event: KeyboardEvent) {
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      void sendReply();
    }
  }

  function openImageViewer(url: string, title: string) {
    if (isModalOpen()) return;
    selectedImage = { url, title };
  }

  function openArtifactViewer(upload: Upload) {
    artifactTrigger = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    artifactConversationKey = activeConversationKey;
    selectedArtifact = upload;
    void tick().then(() => {
      document.querySelector<HTMLElement>(".artifact-viewer__actions > button:last-child")?.focus();
    });
  }

  function closeArtifactViewer() {
    const trigger = artifactTrigger;
    const uploadID = selectedArtifact?.id || "";
    selectedArtifact = null;
    artifactConversationKey = "";
    artifactTrigger = null;
    void tick().then(() => {
      if (trigger?.isConnected) {
        trigger.focus({ preventScroll: true });
        return;
      }
      const scope = thread.root ? document.querySelector<HTMLElement>(".thread") : document;
      scope
        ?.querySelector<HTMLElement>(`[data-artifact-upload-id="${CSS.escape(uploadID)}"]`)
        ?.focus({ preventScroll: true });
    });
  }

  function syncArtifactModalInert(active: boolean, viewer: HTMLElement | null) {
    for (const element of artifactModalInertElements) element.inert = false;
    artifactModalInertElements.clear();
    if (!active || !shellElement || !viewer) return;
    for (const child of shellElement.children) {
      if (!(child instanceof HTMLElement) || child === viewer || child.inert) continue;
      child.inert = true;
      artifactModalInertElements.add(child);
    }
  }

  function containArtifactModalFocus(event: KeyboardEvent) {
    if (!selectedArtifact || !mobileNavViewport || event.key !== "Tab" || !artifactViewerElement) return;
    const focusable = Array.from(
      artifactViewerElement.querySelectorAll<HTMLElement>(
        'a[href], button:not([disabled]), [tabindex]:not([tabindex="-1"])',
      ),
    ).filter((element) => !element.inert && element.getClientRects().length > 0);
    if (focusable.length === 0) {
      event.preventDefault();
      artifactViewerElement.focus();
      return;
    }
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    if (event.shiftKey && (document.activeElement === first || !artifactViewerElement.contains(document.activeElement))) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && (document.activeElement === last || !artifactViewerElement.contains(document.activeElement))) {
      event.preventDefault();
      first.focus();
    }
  }

  function handleInlineImagePointerUp(event: PointerEvent) {
    const target = event.target;
    if (!(target instanceof HTMLImageElement)) return;
    if (!target.closest(".markdown")) return;
    event.preventDefault();
    openImageViewer(markdownImageViewerURL(target), target.alt || "Image");
  }

  function closeSidePanel() {
    if (selectedArtifact) {
      closeArtifactViewer();
      return;
    }
    if (pinnedPanelOpen) {
      pinnedPanelOpen = false;
      commitSelectedRoute();
      return;
    }
    resetCreateActions();
    editController.cancel(currentConversationKey(), "thread");
    clearRoutePanelState();
    commitSelectedRoute();
  }

  function toggleSidePanelFromTopbar() {
    if (thread.root) {
      closeSidePanel();
      return;
    }
    if (sidePanelOpen) closeSidePanel();
    composerNotice = { kind: "ephemeral", text: "Pick a message to open its thread." };
  }

  async function loadPinnedMessages() {
    const channelID = selectedChannelID;
    const serial = ++pinnedMessagesLoadSerial;
    const isCurrent = () => serial === pinnedMessagesLoadSerial && channelID === selectedChannelID && !selectedDirectID;
    if (!channelID || selectedDirectID) {
      pinnedMessages = [];
      pinnedMessagesError = "";
      pinnedMessagesLoading = false;
      return;
    }
    pinnedMessagesLoading = true;
    pinnedMessagesError = "";
    try {
      await messageRequests.run(
        () => api<{ messages: Message[] }>(`/api/channels/${channelID}/pins?limit=100`),
        (data) => { pinnedMessages = data.messages.filter((message) => !message.deleted_at); },
        isCurrent,
      );
    } catch (error) {
      if (!isCurrent()) return;
      pinnedMessagesError = error instanceof Error ? error.message : "Could not load pinned messages";
    } finally {
      if (isCurrent()) pinnedMessagesLoading = false;
    }
  }

  async function toggleMessagePin(message: Message, pinned: boolean) {
    const channelID = selectedChannelID;
    if (!channelID || selectedDirectID) throw new Error("Pins are available in channels only");
    if (pinned) {
      await api(`/api/channels/${channelID}/pins/${message.id}`, { method: "DELETE" });
    } else {
      await api(`/api/channels/${channelID}/pins`, {
        method: "POST",
        body: JSON.stringify({ message_id: message.id }),
      });
    }
    if (channelID === selectedChannelID && !selectedDirectID) await loadPinnedMessages();
  }

  function togglePinnedPanel() {
    if (!selectedChannelID || selectedDirectID) return;
    const opening = !pinnedPanelOpen;
    resetSearch();
    clearRoutePanelState();
    pinnedPanelOpen = opening;
    commitSelectedRoute();
    if (opening) void loadPinnedMessages();
  }

  async function openPinnedMessageThread(message: Message) {
    routeApplySerial++;
    pinnedPanelOpen = false;
    const loaded = await selectThread(message.thread_root_id, message.parent_message_id ? undefined : message);
    if (
      loaded && selectedWorkspaceID &&
      thread.root?.route_id &&
      window.location.pathname !== appHref(selectedWorkspaceID, thread.root.id)
    ) {
      await navigateToApp(selectedWorkspaceID, thread.root.id);
    }
  }

  function handleWindowKeydown(event: KeyboardEvent) {
    if (event.isComposing || event.keyCode === 229) return;
    containArtifactModalFocus(event);
    if (event.defaultPrevented) return;
    if (event.key === "Escape") {
      if (
        event.target instanceof Element &&
        event.target.closest("[data-handles-escape]")
      ) {
        return;
      }
      if (selectedImage) return;
      if (isModalOpen()) {
        closeModal();
      } else if (mobileNavOpen) {
        event.preventDefault();
        closeMobileNav();
        return;
      } else if (selectedArtifact) {
        event.preventDefault();
        closeSidePanel();
        return;
      } else if (searchThreadDetour) {
        event.preventDefault();
        closeSidePanel();
        return;
      } else if (searchPaneVisible) {
        event.preventDefault();
        resetSearch();
        return;
      } else if (replyTarget || thread.draft?.quote) {
        event.preventDefault();
        if (thread.draft?.quote && (activeComposerContext === "thread" || !replyTarget)) {
          thread.setQuote(null);
        } else {
          clearReplyTarget();
        }
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
      !event.altKey
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
    if (pendingDeleteMessage && deletingMessageIDs.has(pendingDeleteMessage.id)) return;
    if (channelSettingsSaving) return;
    resetCreateActions();
    pendingDeleteMessage = null;
    deleteMessageError = "";
    selectedImage = null;
    settingsModalOpen = false;
    channelSettingsOpen = false;
    channelSettingsError = "";
  }

  function closeMobileNav() {
    mobileNavOpen = false;
  }

  function handleSidebarCollapse() {
    if (mobileNavViewport) {
      mobileNavOpen = !mobileNavOpen;
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
  {#if integratedTitleBar && desktop}
    <div class="desktop-auth-titlebar" data-platform={desktop.platform} aria-hidden="true"></div>
  {/if}
  <main class="auth-shell">
    <section class="auth-panel" aria-label="Sign in">
      <div class="auth-brand">
        <KeystrokeMark class="mark" size={44} />
        <div class="brand-text">
          <strong>ClickClack</strong>
          <span>OpenClaw workspace chat</span>
        </div>
      </div>
      <div class="auth-copy">
        <h1>Welcome.</h1>
        <p>{authLead}</p>
      </div>
      {#if passwordAuthEnabled}
        <form class="auth-form" onsubmit={submitPasswordLogin}>
          <label class="field">
            <span>Email or username</span>
            <input
              bind:value={passwordIdentifier}
              autocomplete="username"
              name="identifier"
              required
              type="text"
            />
          </label>
          <label class="field">
            <span>Password</span>
            <input
              bind:value={passwordSecret}
              autocomplete="current-password"
              name="password"
              required
              type="password"
            />
          </label>
          <button class="auth-submit" type="submit" disabled={authSubmitting}>
            {authSubmitting ? "Signing in..." : "Sign in"}
          </button>
        </form>
      {/if}
      {#if githubAuthEnabled}
        {#if passwordAuthEnabled}
          <p class="auth-divider"><span>or</span></p>
        {/if}
        <a class="github-login" href={apiURL("/api/auth/github/start")} onclick={signInWithGitHub}>
          <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">
            <path fill="currentColor" d="M12 .5C5.65.5.5 5.65.5 12c0 5.08 3.29 9.39 7.86 10.91.58.1.79-.25.79-.56v-2c-3.2.69-3.87-1.37-3.87-1.37-.52-1.32-1.27-1.67-1.27-1.67-1.04-.71.08-.7.08-.7 1.15.08 1.76 1.18 1.76 1.18 1.02 1.75 2.68 1.25 3.34.96.1-.74.4-1.25.73-1.54-2.55-.29-5.24-1.28-5.24-5.69 0-1.26.45-2.29 1.18-3.1-.12-.29-.51-1.46.11-3.05 0 0 .96-.31 3.15 1.18a10.94 10.94 0 0 1 5.74 0c2.19-1.49 3.15-1.18 3.15-1.18.62 1.59.23 2.76.12 3.05.74.81 1.18 1.84 1.18 3.1 0 4.42-2.69 5.39-5.25 5.68.41.36.78 1.06.78 2.13v3.16c0 .31.21.67.8.56 4.56-1.52 7.85-5.83 7.85-10.91C23.5 5.65 18.35.5 12 .5z"/>
          </svg>
          Continue with GitHub
        </a>
      {/if}
      {#if !desktop}
        <a class="openclaw-login" href={apiURL("/api/auth/openclaw/start")}>
          <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">
            <path
              fill="currentColor"
              d="M12 2a10 10 0 1 0 10 10A10 10 0 0 0 12 2Zm0 3.5A6.5 6.5 0 1 1 5.5 12 6.51 6.51 0 0 1 12 5.5Zm0 3A3.5 3.5 0 1 0 15.5 12 3.5 3.5 0 0 0 12 8.5Z"
            />
          </svg>
          Sign in with OpenClaw ID
        </a>
      {/if}
      {#if authError}
        <p class="auth-error" role="alert">{authError}</p>
      {/if}
      <details class="auth-magic" open={!githubAuthEnabled && !passwordAuthEnabled}>
        <summary>Have a sign-in token?</summary>
        <form class="auth-form" onsubmit={submitMagicToken}>
          <label class="field">
            <span>Sign-in token</span>
            <input
              bind:value={magicToken}
              autocomplete="one-time-code"
              name="token"
              required
              type="text"
            />
          </label>
          <button class="auth-submit auth-submit-quiet" type="submit" disabled={authSubmitting}>
            Use token
          </button>
        </form>
      </details>
      {#if desktopAuthStatus || authFoot}
        <p class="auth-foot">{desktopAuthStatus || authFoot}</p>
      {/if}
    </section>
  </main>
{:else}
<div
  bind:this={shellElement}
  class="shell"
  class:desktop-shell={integratedTitleBar}
  class:nav-open={mobileNavOpen}
  class:sidebar-collapsed={sidebarCollapsed}
  class:thread-open={sidePanelOpen && !searchPaneVisible}
  class:search-open={searchPaneVisible}
  class:artifact-open={selectedArtifact !== null}
  data-connected={connected}
  data-app-ready={connected && appReady}
>
  {#if integratedTitleBar && desktop}
    <DesktopTitlebar
      channelNotifPreference={selectedChannel ? channelNotifPreference : null}
      {channelNotifSaving}
      pinnedOpen={pinnedPanelOpen}
      channelTitle={selectedDirect
        ? `@${dmTitle(selectedDirect, user?.id)}`
        : selectedChannel
          ? `#${channelDisplayTitle(selectedChannel)}`
          : undefined}
      externalURL={selectedDirect ? undefined : selectedChannel?.external_url}
      pinsAvailable={Boolean(selectedChannel)}
      channelSettingsAvailable={canManageSelectedChannel}
      {connected}
      platform={desktop.platform}
      {searchQuery}
      {sidebarCollapsed}
      {mobileNavOpen}
      mobileNavigation={mobileNavViewport}
      workspaceName={selectedWorkspace?.name}
      onOpenChannelSettings={openChannelSettings}
      onOpenWorkspaceSettings={openWorkspaceSettings}
      onResetSearch={resetSearch}
      onSearch={() => void searchMessages()}
      onSearchQuery={(value) => (searchQuery = value)}
      onToggleSidebar={handleSidebarCollapse}
      onToggleChannelNotifications={() => void cycleChannelNotifPreference()}
      onPinnedItems={togglePinnedPanel}
    />
  {/if}

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
    homeHref={integratedTitleBar && homeLink.url === "/" ? "/app" : homeLink.url}
    homeLabel={homeLink.label}
    homeTitle={homeLinkTitle(homeLink)}
    {selectedWorkspaceID}
    {workspaceName}
    {showWorkspaceCreate}
    createPending={createPending === "workspace"}
    createError={workspaceCreateError}
    hrefForWorkspace={(workspaceID) => appHref(workspaceID)}
    onSelectWorkspace={(workspaceID) => void selectWorkspace(workspaceID)}
    onToggleWorkspaceCreate={toggleWorkspaceCreate}
    onWorkspaceName={(value) => (workspaceName = value)}
    onCreateWorkspace={() => void createWorkspace()}
  />

  <Sidebar
    workspaceID={selectedWorkspaceID}
    workspaceName={selectedWorkspace?.name}
    workspaceIconURL={selectedWorkspace?.icon_url ? apiResourceURL(selectedWorkspace.icon_url) : ""}
    {connected}
    {sidebarCollapsed}
    showHeader={!integratedTitleBar}
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
    onCreateChannel={openCreateChannel}
    onSelectDirect={(conversationID) => void selectDirectConversation(conversationID)}
    onCreateDirect={openCreateDirect}
    onHideDirect={(conversationID) => void hideDirectConversation(conversationID)}
    hiddenDirectTitle={hiddenDirectUndo?.title}
    onUndoHideDirect={() => void undoHideDirectConversation()}
    onOpenProfile={openUserProfile}
    onOpenSettings={openProfileSettings}
    onOpenWorkspaceSettings={openWorkspaceSettings}
  />

  <main class="timeline" inert={mobileNavOpen}>
    <!-- The integrated title bar owns the conversation title, so desktop drops
         this header row entirely. -->
    {#if !integratedTitleBar}
      <Topbar
        {selectedDirect}
        {selectedChannel}
        workspaceName={selectedWorkspace?.name}
        currentUserID={user?.id}
        {searchQuery}
        threadOpen={$threadView.root !== null}
        pinnedOpen={pinnedPanelOpen}
        {channelNotifPreference}
        {channelNotifSaving}
        channelSettingsAvailable={canManageSelectedChannel}
        onSearchQuery={(value) => (searchQuery = value)}
        onSearch={() => void searchMessages()}
        onResetSearch={resetSearch}
        onToggleThread={toggleSidePanelFromTopbar}
        onToggleChannelNotifications={() => void cycleChannelNotifPreference()}
        onPinnedItems={togglePinnedPanel}
        onOpenChannelSettings={openChannelSettings}
      />
    {/if}

    {#if activeTopic}
      <div class="topic-filter" role="status">
        <span>Showing topic <strong>{activeTopic.name}</strong></span>
        <button type="button" onclick={() => void setTopicFilter("")}>Clear filter</button>
      </div>
    {/if}

    <MessageList
      channelID={selectedChannelID}
      {pinnedMessageIDs}
      onTogglePin={toggleMessagePin}
      onCopyLink={ensureMessageLink}
      messages={visibleMessages}
      {selectedDirect}
      {selectedChannel}
      {mentionPeople}
      {mentionAttentionUserID}
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
      selectedThreadID={$threadView.root?.id}
      currentUserID={user?.id}
      {reactionController}
      reactionsDisabled={Boolean(selectedDirect && !selectedDirectWritable)}
      canDeleteAnyMessage={canDeleteAnyMessage && !selectedDirectID}
      {deletingMessageIDs}
      onListRef={(handle) => (messageList = handle)}
      onActivateMessageComposer={activateMessageComposer}
      onInlineImagePointerUp={handleInlineImagePointerUp}
      onOpenProfile={openUserProfile}
      onReply={setReplyTarget}
      onOpenThread={openThread}
      onJumpToQuote={(message) => void jumpToQuotedMessage(message)}
      onOpenImage={openImageViewer}
      onOpenArtifact={openArtifactViewer}
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
      onDeleteMessage={requestMessageDelete}
      {editController}
      editScope={activeConversationKey}
      onMessageEdited={applyEditedMessage}
      {topics}
      onSelectTopic={(topicID) => void setTopicFilter(topicID)}
    />

    <AgentProgress turns={agentProgressTurns} />

    <TypingIndicator entries={typingEntries} currentUserID={user?.id} />

    <div class="composer-dock">
    <AgentResponding
      active={agentResponding && $threadView.root === null}
      agentNames={activeRespondingAgentNames}
    />

    {#if realtimeError}
      <p class="composer-notice composer-notice--error" role="status">Live updates: {realtimeError}</p>
    {/if}

    {#if workspaceMembersError}
      <p class="composer-notice composer-notice--error" role="status">Mentions unavailable: {workspaceMembersError}</p>
    {/if}

    {#if composerNotice}
      <div
        class="composer-notice"
        class:composer-notice--error={composerNotice.kind === "error"}
        role="status"
        aria-live="polite"
      >
        <span class="composer-notice__label">
          {composerNotice.kind === "ephemeral" ? "Only visible to you" : "Action failed"}
        </span>
        <span class="composer-notice__text">{composerNotice.text}</span>
        <button
          type="button"
          class="composer-notice__dismiss"
          aria-label="Dismiss notice"
          onclick={() => (composerNotice = null)}
        >×</button>
      </div>
    {/if}

    {#if selectedChannelID && eligibleTopics.length > 0}
      <label class="topic-picker">
        <span>Topic</span>
        <select
          aria-label="Message topic"
          value={selectedComposerTopicID}
          onchange={(event) => (selectedComposerTopicID = event.currentTarget.value)}
        >
          <option value="">No topic</option>
          {#each eligibleTopics as topic (topic.id)}
            <option value={topic.id}>{topic.name}</option>
          {/each}
        </select>
      </label>
    {/if}

    <ChatComposer
      value={messageBody}
      placeholder={selectedDirect && !selectedDirectWritable ? "No active recipient" : selectedDirect ? `Message ${dmTitle(selectedDirect, user?.id)}` : selectedChannel ? `Message #${channelDisplayTitle(selectedChannel)}` : "Pick a channel to start"}
      ariaLabel="Message body"
      submitLabel="Send"
      disabled={!!selectedDirect && !selectedDirectWritable}
      pendingUpload={pendingUpload}
      replyTarget={replyTarget && replyContext === (selectedDirectID ? "dm" : "channel") ? replyTarget : null}
      showUpload
      showToolbar
      slashCommands={selectedChannelID ? slashCommands : []}
      botCommands={composerBotCommands}
      {mentionPeople}
      onValue={(value) => {
        const previous = messageBody;
        messageBody = value;
        if (value.trim() && value !== previous) notifyComposerTyping();
        else if (!value.trim()) stopTyping();
      }}
      onSubmit={() => void sendMessage()}
      onKeydown={handleComposerKey}
      onFocus={activateMessageComposer}
      onInputRef={(node) => (messageInput = node)}
      onUploadFile={uploadFile}
      onRemoveUpload={clearPendingUpload}
      onClearReply={clearReplyTarget}
    />
    </div>
  </main>

  {#if selectedArtifact}
    <aside
      bind:this={artifactViewerElement}
      class="artifact-viewer open"
      inert={mobileNavOpen}
      role={mobileNavViewport ? "dialog" : "complementary"}
      aria-modal={mobileNavViewport ? "true" : undefined}
      aria-label="Artifact viewer"
      tabindex="-1"
    >
      <ArtifactViewer upload={selectedArtifact} onClose={closeArtifactViewer} />
    </aside>
  {/if}
  {#if searchPaneVisible && searchSession}
    <SearchResults
      session={searchSession}
      covered={selectedArtifact !== null}
      inert={mobileNavOpen || selectedArtifact !== null}
      contextFor={searchResultContext}
      onClose={resetSearch}
      onOpenResult={(result) => void openSearchResult(result)}
      onLoadMore={() => void loadMoreSearchResults()}
    />
  {:else}
  <aside
    class="thread"
    class:open={sidePanelOpen}
    class:covered={selectedArtifact !== null}
    inert={mobileNavOpen || selectedArtifact !== null}
    aria-hidden={selectedArtifact ? "true" : undefined}
    aria-label={pinnedPanelOpen ? "Pinned messages pane" : selectedProfile ? "Profile pane" : "Thread pane"}
  >
    {#if pinnedPanelOpen}
      <PinnedPanel
        messages={pinnedMessages}
        loading={pinnedMessagesLoading}
        error={pinnedMessagesError}
        {topics}
        {mentionPeople}
        {mentionAttentionUserID}
        onClose={closeSidePanel}
        onOpenThread={(message) => void openPinnedMessageThread(message)}
        onOpenImage={openImageViewer}
        onOpenArtifact={openArtifactViewer}
        onUnpin={(message) => toggleMessagePin(message, true)}
        onSelectTopic={(topicID) => {
          pinnedPanelOpen = false;
          void setTopicFilter(topicID);
        }}
      />
    {:else if $threadView.root}
      <ThreadPanel
        history={thread}
        root={$threadView.root}
        {replies}
        threadState={$threadView.state}
        replyBody={$threadView.draft?.body ?? ""}
        replyTarget={$threadView.draft?.quote ?? null}
        replyError={$threadView.draft?.error || $threadView.error}
        replySending={$threadView.draft?.sending ?? false}
        {mentionPeople}
        {mentionAttentionUserID}
        {agentResponding}
        respondingAgentNames={activeRespondingAgentNames}
        replyDisabled={Boolean(selectedDirect && !selectedDirectWritable)}
        onClose={closeSidePanel}
        onBack={searchThreadDetour && searchSession ? () => void returnToSearchFromThread() : undefined}
        onReplyBody={(value) => thread.updateDraft(value)}
        onSubmitReply={() => void sendReply()}
        onReplyKeydown={handleReplyKey}
        onReplyFocus={() => (activeComposerContext = "thread")}
        onReplyInputRef={(node) => (replyInput = node)}
        currentUserID={user?.id}
        {reactionController}
        reactionsDisabled={Boolean(selectedDirect && !selectedDirectWritable)}
        onSetReplyTarget={setReplyTarget}
        onClearReply={() => thread.setQuote(null)}
        canDeleteAnyMessage={canDeleteAnyMessage && !selectedDirectID}
        {deletingMessageIDs}
        onDeleteMessage={requestMessageDelete}
        channelID={selectedChannelID}
        {pinnedMessageIDs}
        onTogglePin={toggleMessagePin}
        onCopyLink={ensureMessageLink}
        {editController}
        editScope={activeConversationKey}
        onMessageEdited={applyEditedMessage}
        onActivateThreadComposer={() => (activeComposerContext = "thread")}
        onInlineImagePointerUp={handleInlineImagePointerUp}
        onJumpToQuote={(message) => void jumpToQuotedMessage(message)}
        onOpenImage={openImageViewer}
        onOpenArtifact={openArtifactViewer}
      />
    {:else if $threadView.selection}
      <header>
        <strong>Thread</strong>
        <button class="close" aria-label="Close thread" onclick={closeSidePanel}>×</button>
      </header>
      {#if $threadView.error}
        <p class="composer-notice" role="alert">{$threadView.error}</p>
        <button type="button" class="ghost-action" onclick={() => $threadView.selection && void selectThread($threadView.selection.messageID)}>Retry loading thread</button>
      {:else}
        <p class="composer-notice" role="status">Loading thread…</p>
      {/if}
    {:else if selectedProfile}
      <ProfilePane
        profile={selectedProfile}
        currentUser={user}
        workspaceName={selectedWorkspace?.name}
        currentUserRole={currentWorkspaceRole}
        moderation={selectedProfileModeration}
        onClose={closeSidePanel}
        onEdit={openProfileSettings}
        messagePending={createPending === "direct"}
        messageError={directCreateError}
        onMessage={(memberID) => void startDirectWithUser(memberID)}
        onApprove={(memberID) => void updateMemberModeration(memberID, { role: "member", clear_timeout: true, blocked: false })}
        onTimeout={(memberID) => void updateMemberModeration(memberID, { timeout_minutes: 60 })}
        onBlock={(memberID) => void updateMemberModeration(memberID, { blocked: true })}
        onUnblock={(memberID) => void updateMemberModeration(memberID, { blocked: false, clear_timeout: true })}
      />
    {:else}
      <ThreadEmptyState />
    {/if}
  </aside>
  {/if}
</div>
{#if settingsModalOpen && user}
  <SettingsModal
    {user}
    {workspaces}
    initialSection={settingsModalSection}
    {hideCommentary}
    {hideToolCalls}
    {userAlign}
    {otherAlign}
    isDesktop={desktop != null}
    onUserUpdated={handleSettingsUserUpdated}
    onHideCommentary={setHideCommentary}
    onHideToolCalls={setHideToolCalls}
    onUserAlign={setUserAlign}
    onOtherAlign={setOtherAlign}
    onBrowserNotificationsChanged={(value) => (browserNotificationsEnabled = value)}
    onClose={closeModal}
  />
{/if}
{#if channelSettingsOpen && selectedChannel}
  <ChannelSettingsModal
    channel={selectedChannel}
    saving={channelSettingsSaving}
    error={channelSettingsError}
    onClose={closeModal}
    onArchivedChange={(archived) => void setSelectedChannelArchived(archived)}
  />
{/if}
{#if showCreateChannel}
  <CreateChannelModal
    {channelName}
    pending={createPending === "channel"}
    error={channelCreateError}
    onChannelName={(value) => (channelName = value)}
    onClose={closeModal}
    onCreate={() => void createChannel()}
  />
{/if}
{#if showCreateDirect}
  <CreateDirectModal
    people={mentionPeople}
    currentUserID={user?.id}
    memberID={directMemberID}
    pending={createPending === "direct"}
    error={directCreateError || workspaceMembersError}
    onMemberID={(value) => (directMemberID = value)}
    onClose={closeModal}
    onStart={(memberID) => void startDirectWithUser(memberID)}
  />
{/if}
{#if pendingDeleteMessage}
  <DeleteMessageModal
    message={pendingDeleteMessage}
    deleting={deletingMessageIDs.has(pendingDeleteMessage.id)}
    error={deleteMessageError}
    onClose={closeModal}
    onConfirm={() => void confirmMessageDelete()}
  />
{/if}
{#if selectedImage}
  <ImageViewer url={selectedImage.url} title={selectedImage.title} onClose={closeModal} />
{/if}
{/if}
