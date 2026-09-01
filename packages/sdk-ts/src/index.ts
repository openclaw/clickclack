import type { components } from "./generated/openapi";

export type { components, paths } from "./generated/openapi";

export type HomeLink = components["schemas"]["HomeLink"];

export type User = components["schemas"]["User"];

export type DeletedBot = components["schemas"]["DeletedBot"];

export type BotToken = components["schemas"]["BotToken"];

export type BotWithTokens = components["schemas"]["BotWithTokens"];

export type BotSetupCode = components["schemas"]["BotSetupCode"];

export type BotSetupCodeDefaults = components["schemas"]["BotSetupCodeClaimDefaults"];

export type BotSetupCodeClaim = components["schemas"]["BotSetupCodeClaimResponse"];

export type BotCommandInput = components["schemas"]["BotCommandInput"];

export type BotCommand = components["schemas"]["BotCommand"];

export type BotCommandBot = components["schemas"]["BotCommandBot"];

export type WorkspaceBotCommand = components["schemas"]["WorkspaceBotCommand"];

export type OwnedBotWorkspace = components["schemas"]["OwnedBotWorkspace"];

export type OwnedBotEntry = components["schemas"]["OwnedBotEntry"];

export type AppInstallation = components["schemas"]["AppInstallation"];

export type RevokeAppInstallationOptions = components["schemas"]["RevokeAppInstallationRequest"];

export type AppInstallationRevokedCounts = components["schemas"]["AppInstallationRevokedCounts"];

export type RevokeAppInstallationResult = components["schemas"]["RevokeAppInstallationResponse"];

export type SlashCommand = components["schemas"]["SlashCommand"];

export type EventSubscription = components["schemas"]["EventSubscription"];

export type EventDeliveryAttempt = components["schemas"]["EventDeliveryAttempt"];

export type EventDeliveryAttemptsOptions = {
  limit?: number;
  before?: string;
};

export type EventDeliveryAttemptsPage = components["schemas"]["EventDeliveryAttemptsResponse"];

export type AuditLogEntry = components["schemas"]["AuditLogEntry"];

export type ConnectedAccount = components["schemas"]["ConnectedAccount"];

export type BotEventHandler = (
  event: RealtimeEvent,
  client: ClickClackClient,
) => void | Promise<void>;

export type ClickClackBotOptions = ClickClackClientOptions & {
  workspaceId: string;
  afterCursor?: string;
  onEvent: BotEventHandler;
  onClose?: () => void;
};

export type Workspace = components["schemas"]["Workspace"];

export type Channel = components["schemas"]["Channel"];

export type Topic = components["schemas"]["Topic"];

export type MessageKind = "message" | "agent_commentary" | "agent_tool";

type MessageInputBase = {
  body: string;
  quoted_message_id?: string;
  nonce?: string;
};

export type MessageInput = MessageInputBase &
  (
    | { kind?: "message"; turn_id?: never }
    | { kind: "agent_commentary" | "agent_tool"; turn_id?: string }
  );

export type ReactionSummary = components["schemas"]["ReactionSummary"];
export type ReactionMutationResponse = components["schemas"]["ReactionMutationResponse"];
export type PinnedMessage = components["schemas"]["PinnedMessage"];
export type PinMessageResponse = components["schemas"]["PinMessageResponse"];

export type Message = components["schemas"]["Message"];

export type MessagePage = components["schemas"]["MessagePage"];

export type MessagePageOptions = {
  mode?: "latest";
  limit?: number;
  before_seq?: number;
  after_seq?: number;
  around_seq?: number;
  topic_id?: string;
};

export type SearchHighlight = components["schemas"]["SearchHighlight"];

export type SearchResult = components["schemas"]["SearchResult"];

export type SearchResponse = components["schemas"]["SearchResponse"];

export type Upload = components["schemas"]["Upload"];

export type DirectConversation = components["schemas"]["DirectConversation"];

export type ReadReceipt = components["schemas"]["ReadReceipt"];

export type RouteTarget = components["schemas"]["RouteTarget"];

export type RealtimeEventPayload = {
  correlation_id?: string;
  [key: string]: unknown;
};

export type RealtimeEvent = {
  id: string;
  cursor: string;
  type: string;
  workspace_id: string;
  channel_id?: string;
  seq?: number;
  created_at: string;
  payload: RealtimeEventPayload;
  mentioned_user_ids?: string[];
};

export type ChannelNotificationPreference = "all" | "mentions" | "muted";

export type AgentProgressLine = {
  id: string;
  kind: "commentary" | "lifecycle" | "thinking" | "tool";
  text?: string;
  title?: string;
  tool_name?: string;
  status?: string;
};

export type AgentProgressPayload = {
  turn_id: string;
  seq?: number;
} & (
  | { op: "append" | "update" | "finalize"; line: AgentProgressLine }
  | { op: "clear"; line?: never }
);

type EphemeralEventTarget =
  | { channelId: string; directConversationId?: never }
  | { channelId?: never; directConversationId: string };

type OptionalEphemeralEventTarget =
  | EphemeralEventTarget
  | { channelId?: never; directConversationId?: never };

type TargetedEphemeralEventInput = {
  workspaceId: string;
} & EphemeralEventTarget &
  (
    | {
        type: "agent.progress";
        payload: AgentProgressPayload;
      }
    | {
        type: "typing.started" | "typing.stopped";
        payload?: Record<string, unknown>;
      }
  );

type PresenceEventInput = {
  workspaceId: string;
  type: "presence.changed";
  payload?: Record<string, unknown>;
} & OptionalEphemeralEventTarget;

export type EphemeralEventInput = TargetedEphemeralEventInput | PresenceEventInput;

export type RealtimeEventPage = {
  events: RealtimeEvent[];
  tailCursor?: string;
};

export type ThreadState = components["schemas"]["ThreadState"];

export type Thread = {
  root: Message;
  replies: Message[];
  thread_state: ThreadState;
};

export type ThreadPage = components["schemas"]["ThreadPage"];

export type ClickClackClientOptions = {
  baseUrl: string;
  userId?: string;
  token?: string;
  fetch?: typeof fetch;
  webSocket?: WebSocketConstructor;
};

export type WebSocketConstructor = new (
  url: string | URL,
  protocols?: string | string[],
) => WebSocket;

export class ClickClackClient {
  private readonly baseUrl: string;
  private readonly userId?: string;
  private token?: string;
  private readonly fetcher: typeof fetch;
  private readonly WebSocket?: WebSocketConstructor;

  constructor(options: ClickClackClientOptions) {
    this.baseUrl = options.baseUrl.replace(/\/$/, "");
    this.userId = options.userId;
    this.token = options.token;
    this.fetcher = options.fetch ?? fetch;
    this.WebSocket = options.webSocket ?? globalThis.WebSocket;
  }

  auth = {
    requestMagicLink: async (input: { email: string; display_name?: string }) => {
      return this.request("/api/auth/magic/request", {
        method: "POST",
        body: JSON.stringify(input),
      });
    },
    consumeMagicLink: async (
      token: string,
    ): Promise<{ user: User; session: { token: string } }> => {
      const data = await this.request<{ user: User; session: { token: string } }>(
        "/api/auth/magic/consume",
        {
          method: "POST",
          body: JSON.stringify({ token }),
        },
      );
      this.token = data.session.token;
      return data;
    },
    setToken: (token: string) => {
      this.token = token;
    },
    githubStartUrl: (): string => {
      return `${this.baseUrl}/api/auth/github/start`;
    },
  };

  homeLink(): Promise<HomeLink> {
    return this.request<HomeLink>("/api/home-link");
  }

  async me(): Promise<User> {
    const data = await this.request<{ user: User }>("/api/me");
    return data.user;
  }

  async updateMe(input: components["schemas"]["UpdateMeRequest"]): Promise<User> {
    const data = await this.request<{ user: User }>("/api/me", {
      method: "PATCH",
      body: JSON.stringify(input),
    });
    return data.user;
  }

  workspaces = {
    list: async (): Promise<Workspace[]> => {
      const data = await this.request<{ workspaces: Workspace[] }>("/api/workspaces");
      return data.workspaces;
    },
    create: async (input: { name: string; slug?: string }): Promise<Workspace> => {
      const data = await this.request<{ workspace: Workspace }>("/api/workspaces", {
        method: "POST",
        body: JSON.stringify(input),
      });
      return data.workspace;
    },
    get: async (workspaceId: string): Promise<Workspace> => {
      const data = await this.request<{ workspace: Workspace }>(`/api/workspaces/${workspaceId}`);
      return data.workspace;
    },
    update: async (
      workspaceId: string,
      input: { name?: string; slug?: string; icon_url?: string },
    ): Promise<Workspace> => {
      const data = await this.request<{ workspace: Workspace }>(`/api/workspaces/${workspaceId}`, {
        method: "PATCH",
        body: JSON.stringify(input),
      });
      return data.workspace;
    },
    transferOwnership: async (workspaceId: string, userId: string): Promise<Workspace> => {
      const data = await this.request<{ workspace: Workspace }>(
        `/api/workspaces/${workspaceId}/transfer-ownership`,
        {
          method: "POST",
          body: JSON.stringify({ user_id: userId }),
        },
      );
      return data.workspace;
    },
    delete: async (workspaceId: string): Promise<void> => {
      await this.request(`/api/workspaces/${workspaceId}`, { method: "DELETE" });
    },
  };

  routes = {
    resolve: async (workspaceRouteId: string, targetRouteId: string): Promise<RouteTarget> => {
      const data = await this.request<{ route: RouteTarget }>(
        `/api/routes/${encodeURIComponent(workspaceRouteId)}/${encodeURIComponent(targetRouteId)}`,
      );
      return data.route;
    },
  };

  topics = {
    list: async (workspaceId: string): Promise<Topic[]> => {
      const data = await this.request<{ topics: Topic[] }>(`/api/workspaces/${workspaceId}/topics`);
      return data.topics;
    },
    create: async (
      workspaceId: string,
      input: { name: string; channel_id?: string },
    ): Promise<Topic> => {
      const data = await this.request<{ topic: Topic }>(`/api/workspaces/${workspaceId}/topics`, {
        method: "POST",
        body: JSON.stringify(input),
      });
      return data.topic;
    },
  };

  bots = {
    listMine: async (): Promise<OwnedBotEntry[]> => {
      const data = await this.request<{ bots: OwnedBotEntry[] }>("/api/me/bots");
      return data.bots;
    },
    list: async (workspaceId: string): Promise<BotWithTokens[]> => {
      const data = await this.request<{ bots: BotWithTokens[] }>(
        `/api/workspaces/${workspaceId}/bots`,
      );
      return data.bots;
    },
    listCommands: async (workspaceId: string): Promise<WorkspaceBotCommand[]> => {
      const data = await this.request<{ bot_commands: WorkspaceBotCommand[] }>(
        `/api/workspaces/${workspaceId}/bot-commands`,
      );
      return data.bot_commands;
    },
    setCommands: async (commands: BotCommandInput[]): Promise<BotCommand[]> => {
      const data = await this.request<{ bot_commands: BotCommand[] }>("/api/bots/self/commands", {
        method: "PUT",
        body: JSON.stringify({ commands }),
      });
      return data.bot_commands;
    },
    create: async (
      workspaceId: string,
      input: {
        display_name: string;
        owner_user_id?: string;
        handle?: string;
        avatar_url?: string;
        token_name?: string;
        scopes?: string[];
        setup_nonce?: string;
        initial_token?: boolean;
      },
    ): Promise<{ bot: User; bot_token?: BotToken }> => {
      return this.request(`/api/workspaces/${workspaceId}/bots`, {
        method: "POST",
        body: JSON.stringify(input),
      });
    },
    removeMembership: async (workspaceId: string, botUserId: string): Promise<void> => {
      await this.request(`/api/workspaces/${workspaceId}/bots/${botUserId}/membership`, {
        method: "DELETE",
      });
    },
    delete: async (botUserId: string): Promise<DeletedBot> => {
      const data = await this.request<{ deleted_bot: DeletedBot }>(`/api/bots/${botUserId}`, {
        method: "DELETE",
      });
      return data.deleted_bot;
    },
    listWorkspaceTokens: async (workspaceId: string, botUserId: string): Promise<BotToken[]> => {
      const data = await this.request<{ bot_tokens: BotToken[] }>(
        `/api/workspaces/${workspaceId}/bots/${botUserId}/tokens`,
      );
      return data.bot_tokens;
    },
    createWorkspaceToken: async (
      workspaceId: string,
      botUserId: string,
      input: { name?: string; scopes?: string[]; setup_nonce?: string },
    ): Promise<BotToken> => {
      const data = await this.request<{ bot_token: BotToken }>(
        `/api/workspaces/${workspaceId}/bots/${botUserId}/tokens`,
        {
          method: "POST",
          body: JSON.stringify(input),
        },
      );
      return data.bot_token;
    },
    listTokens: async (botUserId: string): Promise<BotToken[]> => {
      const data = await this.request<{ bot_tokens: BotToken[] }>(`/api/bots/${botUserId}/tokens`);
      return data.bot_tokens;
    },
    createToken: async (
      botUserId: string,
      input: { name?: string; scopes?: string[]; setup_nonce?: string },
    ): Promise<BotToken> => {
      const data = await this.request<{ bot_token: BotToken }>(`/api/bots/${botUserId}/tokens`, {
        method: "POST",
        body: JSON.stringify(input),
      });
      return data.bot_token;
    },
    revokeToken: async (tokenId: string): Promise<BotToken> => {
      const data = await this.request<{ bot_token: BotToken }>(
        `/api/bot-tokens/${tokenId}/revoke`,
        {
          method: "POST",
          body: JSON.stringify({}),
        },
      );
      return data.bot_token;
    },
    createSetupCode: async (
      workspaceId: string,
      botUserId: string,
      input: { name?: string; scopes?: string[]; defaults?: BotSetupCodeDefaults } = {},
    ): Promise<BotSetupCode> => {
      const data = await this.request<{ setup_code: BotSetupCode }>(
        `/api/workspaces/${workspaceId}/bots/${botUserId}/setup-codes`,
        {
          method: "POST",
          body: JSON.stringify(input),
        },
      );
      return data.setup_code;
    },
    claimSetupCode: async (code: string): Promise<BotSetupCodeClaim> => {
      return this.request(`/api/bot-setup-codes/claim`, {
        method: "POST",
        body: JSON.stringify({ code }),
      });
    },
  };

  apps = {
    list: async (workspaceId: string): Promise<AppInstallation[]> => {
      const data = await this.request<{ app_installations: AppInstallation[] }>(
        `/api/workspaces/${workspaceId}/app-installations`,
      );
      return data.app_installations;
    },
    install: async (
      workspaceId: string,
      input: {
        app_slug: string;
        display_name?: string;
        bot_user_id: string;
        config?: Record<string, unknown>;
        setup_nonce?: string;
      },
    ): Promise<AppInstallation> => {
      const data = await this.request<{ app_installation: AppInstallation }>(
        `/api/workspaces/${workspaceId}/app-installations`,
        {
          method: "POST",
          body: JSON.stringify(input),
        },
      );
      return data.app_installation;
    },
    revoke: async (
      installationId: string,
      options: RevokeAppInstallationOptions = {},
    ): Promise<RevokeAppInstallationResult> => {
      return this.request<RevokeAppInstallationResult>(
        `/api/app-installations/${installationId}/revoke`,
        {
          method: "POST",
          body: JSON.stringify(options),
        },
      );
    },
  };

  slashCommands = {
    list: async (workspaceId: string): Promise<SlashCommand[]> => {
      const data = await this.request<{ slash_commands: SlashCommand[] }>(
        `/api/workspaces/${workspaceId}/slash-commands`,
      );
      return data.slash_commands;
    },
    create: async (
      workspaceId: string,
      input: {
        command: string;
        callback_url: string;
        bot_user_id: string;
        app_installation_id?: string;
        description?: string;
      },
    ): Promise<SlashCommand> => {
      const data = await this.request<{ slash_command: SlashCommand }>(
        `/api/workspaces/${workspaceId}/slash-commands`,
        {
          method: "POST",
          body: JSON.stringify(input),
        },
      );
      return data.slash_command;
    },
    revoke: async (commandId: string): Promise<SlashCommand> => {
      const data = await this.request<{ slash_command: SlashCommand }>(
        `/api/slash-commands/${commandId}/revoke`,
        {
          method: "POST",
          body: JSON.stringify({}),
        },
      );
      return data.slash_command;
    },
    rotateSecret: async (commandId: string): Promise<SlashCommand> => {
      const data = await this.request<{ slash_command: SlashCommand }>(
        `/api/slash-commands/${commandId}/rotate-secret`,
        {
          method: "POST",
          body: JSON.stringify({}),
        },
      );
      return data.slash_command;
    },
  };

  eventSubscriptions = {
    list: async (workspaceId: string): Promise<EventSubscription[]> => {
      const data = await this.request<{ event_subscriptions: EventSubscription[] }>(
        `/api/workspaces/${workspaceId}/event-subscriptions`,
      );
      return data.event_subscriptions;
    },
    create: async (
      workspaceId: string,
      input: {
        event_types: string[];
        callback_url: string;
        app_installation_id?: string;
      },
    ): Promise<EventSubscription> => {
      const data = await this.request<{ event_subscription: EventSubscription }>(
        `/api/workspaces/${workspaceId}/event-subscriptions`,
        {
          method: "POST",
          body: JSON.stringify(input),
        },
      );
      return data.event_subscription;
    },
    revoke: async (subscriptionId: string): Promise<EventSubscription> => {
      const data = await this.request<{ event_subscription: EventSubscription }>(
        `/api/event-subscriptions/${subscriptionId}/revoke`,
        {
          method: "POST",
          body: JSON.stringify({}),
        },
      );
      return data.event_subscription;
    },
    rotateSecret: async (subscriptionId: string): Promise<EventSubscription> => {
      const data = await this.request<{ event_subscription: EventSubscription }>(
        `/api/event-subscriptions/${subscriptionId}/rotate-secret`,
        {
          method: "POST",
          body: JSON.stringify({}),
        },
      );
      return data.event_subscription;
    },
    deliveries: async (
      subscriptionId: string,
      options: EventDeliveryAttemptsOptions = {},
    ): Promise<EventDeliveryAttemptsPage> => {
      const query = new URLSearchParams();
      if (options.limit !== undefined) {
        query.set("limit", String(options.limit));
      }
      if (options.before) {
        query.set("before", options.before);
      }
      const suffix = query.toString();
      return this.request<EventDeliveryAttemptsPage>(
        `/api/event-subscriptions/${subscriptionId}/deliveries${suffix ? `?${suffix}` : ""}`,
      );
    },
  };

  eventTypes = {
    list: async (): Promise<string[]> => {
      const data = await this.request<{ event_types: string[] }>("/api/event-types");
      return data.event_types;
    },
  };

  auditLog = {
    list: async (workspaceId: string, limit = 100): Promise<AuditLogEntry[]> => {
      const data = await this.request<{ audit_log_entries: AuditLogEntry[] }>(
        `/api/workspaces/${workspaceId}/audit-log?limit=${limit}`,
      );
      return data.audit_log_entries;
    },
  };

  connectedAccounts = {
    list: async (workspaceId: string): Promise<ConnectedAccount[]> => {
      const data = await this.request<{ connected_accounts: ConnectedAccount[] }>(
        `/api/workspaces/${workspaceId}/connected-accounts`,
      );
      return data.connected_accounts;
    },
    create: async (
      workspaceId: string,
      input: {
        user_id: string;
        provider: string;
        provider_account_id: string;
        display_name?: string;
        scopes?: string[];
        metadata?: Record<string, unknown>;
      },
    ): Promise<ConnectedAccount> => {
      const data = await this.request<{ connected_account: ConnectedAccount }>(
        `/api/workspaces/${workspaceId}/connected-accounts`,
        {
          method: "POST",
          body: JSON.stringify(input),
        },
      );
      return data.connected_account;
    },
    revoke: async (accountId: string): Promise<ConnectedAccount> => {
      const data = await this.request<{ connected_account: ConnectedAccount }>(
        `/api/connected-accounts/${accountId}/revoke`,
        {
          method: "POST",
          body: JSON.stringify({}),
        },
      );
      return data.connected_account;
    },
  };

  channels = {
    list: async (workspaceId: string): Promise<Channel[]> => {
      const data = await this.request<{ channels: Channel[] }>(
        `/api/workspaces/${workspaceId}/channels`,
      );
      return data.channels;
    },
    create: async (
      workspaceId: string,
      input: {
        name: string;
        display_title?: string;
        kind?: string;
        external_managed?: boolean;
        external_ref?: string;
        external_url?: string;
        sidebar_section?: string;
      },
    ): Promise<Channel> => {
      const data = await this.request<{ channel: Channel }>(
        `/api/workspaces/${workspaceId}/channels`,
        {
          method: "POST",
          body: JSON.stringify(input),
        },
      );
      return data.channel;
    },
    update: async (
      channelId: string,
      input: {
        name?: string;
        display_title?: string;
        kind?: string;
        archived?: boolean;
        external_managed?: boolean;
        external_ref?: string;
        external_url?: string;
        sidebar_section?: string;
      },
    ): Promise<Channel> => {
      const data = await this.request<{ channel: Channel }>(`/api/channels/${channelId}`, {
        method: "PATCH",
        body: JSON.stringify(input),
      });
      return data.channel;
    },
    messages: async (
      channelId: string,
      afterSeqOrOptions: number | MessagePageOptions = 0,
    ): Promise<Message[]> => {
      const options =
        typeof afterSeqOrOptions === "number"
          ? { after_seq: afterSeqOrOptions }
          : afterSeqOrOptions;
      const data = await this.channels.messagePage(channelId, options);
      return data.messages;
    },
    messagePage: async (
      channelId: string,
      options: MessagePageOptions = {},
    ): Promise<MessagePage> => {
      const params = new URLSearchParams();
      for (const [key, value] of Object.entries(options)) {
        if (value !== undefined) params.set(key, String(value));
      }
      const query = params.toString();
      return this.request<MessagePage>(
        `/api/channels/${channelId}/messages${query ? `?${query}` : ""}`,
      );
    },
    sendMessage: async (
      channelId: string,
      input: MessageInput & { topic_id?: string },
    ): Promise<Message> => {
      const data = await this.request<{ message: Message }>(`/api/channels/${channelId}/messages`, {
        method: "POST",
        body: JSON.stringify(input),
      });
      return data.message;
    },
    markRead: async (channelId: string, seq: number): Promise<ReadReceipt> => {
      const data = await this.request<{ receipt: ReadReceipt }>(`/api/channels/${channelId}/read`, {
        method: "POST",
        body: JSON.stringify({ seq }),
      });
      return data.receipt;
    },
    getNotificationPreference: async (
      channelId: string,
    ): Promise<ChannelNotificationPreference> => {
      const data = await this.request<{ preference: ChannelNotificationPreference }>(
        `/api/channels/${channelId}/notification-settings`,
      );
      return data.preference;
    },
    updateNotificationPreference: async (
      channelId: string,
      preference: ChannelNotificationPreference,
    ): Promise<ChannelNotificationPreference> => {
      const data = await this.request<{ preference: ChannelNotificationPreference }>(
        `/api/channels/${channelId}/notification-settings`,
        { method: "PATCH", body: JSON.stringify({ preference }) },
      );
      return data.preference;
    },
    pinnedMessages: async (channelId: string, limit = 100): Promise<Message[]> => {
      const data = await this.request<{ messages: Message[] }>(
        `/api/channels/${channelId}/pins?limit=${limit}`,
      );
      return data.messages;
    },
    pinMessage: async (channelId: string, messageId: string): Promise<PinMessageResponse> =>
      this.request<PinMessageResponse>(`/api/channels/${channelId}/pins`, {
        method: "POST",
        body: JSON.stringify({ message_id: messageId }),
      }),
    unpinMessage: async (channelId: string, messageId: string): Promise<RealtimeEvent> => {
      const data = await this.request<{ event: RealtimeEvent }>(
        `/api/channels/${channelId}/pins/${messageId}`,
        { method: "DELETE" },
      );
      return data.event;
    },
  };

  messages = {
    get: async (messageId: string): Promise<Message> => {
      const data = await this.request<{ message: Message }>(`/api/messages/${messageId}`);
      return data.message;
    },
    findByNonce: async (workspaceId: string, nonce: string): Promise<Message | undefined> => {
      const params = new URLSearchParams({ workspace_id: workspaceId, nonce });
      const response = await this.fetchResponse(`/api/messages/by-nonce?${params.toString()}`);
      if (
        response.status === 404 &&
        response.headers.get("X-ClickClack-Message-Nonce") === "supported"
      ) {
        await response.text();
        return undefined;
      }
      const data = await this.readResponse<{ message: Message }>(response);
      return data.message;
    },
    update: async (messageId: string, input: { body: string }): Promise<Message> => {
      const data = await this.request<{ message: Message }>(`/api/messages/${messageId}`, {
        method: "PATCH",
        body: JSON.stringify(input),
      });
      return data.message;
    },
    delete: async (messageId: string): Promise<Message> => {
      const data = await this.request<{ message: Message }>(`/api/messages/${messageId}`, {
        method: "DELETE",
      });
      return data.message;
    },
    ensureRoute: async (messageId: string): Promise<Message> => {
      const data = await this.request<{ message: Message }>(`/api/messages/${messageId}/route`, {
        method: "POST",
      });
      return data.message;
    },
    addReaction: async (messageId: string, emoji: string): Promise<ReactionMutationResponse> =>
      this.request<ReactionMutationResponse>(`/api/messages/${messageId}/reactions`, {
        method: "POST",
        body: JSON.stringify({ emoji }),
      }),
    removeReaction: async (messageId: string, emoji: string): Promise<ReactionMutationResponse> =>
      this.request<ReactionMutationResponse>(
        `/api/messages/${messageId}/reactions/${encodeURIComponent(emoji)}`,
        { method: "DELETE" },
      ),
  };

  threads = {
    get: async (
      messageId: string,
      options: {
        limit?: number;
        latest?: boolean;
        before_seq?: number;
        after_seq?: number;
        around_seq?: number;
      } = {},
    ): Promise<ThreadPage> => {
      const params = new URLSearchParams();
      if (options.limit !== undefined) params.set("limit", String(options.limit));
      if (options.latest !== undefined) params.set("latest", String(options.latest));
      for (const key of ["before_seq", "after_seq", "around_seq"] as const) {
        if (options[key] !== undefined) params.set(key, String(options[key]));
      }
      const query = params.size > 0 ? `?${params.toString()}` : "";
      return this.request<ThreadPage>(`/api/messages/${messageId}/thread${query}`);
    },
    reply: async (
      messageId: string,
      input: { body: string; quoted_message_id?: string; nonce?: string },
    ): Promise<Message> => {
      const data = await this.request<{ message: Message }>(
        `/api/messages/${messageId}/thread/replies`,
        {
          method: "POST",
          body: JSON.stringify(input),
        },
      );
      return data.message;
    },
  };

  search = async (
    workspaceId: string,
    query: string,
    options: {
      channelId?: string;
      directConversationId?: string;
      sort?: "relevance" | "newest";
      limit?: number;
      cursor?: string;
    } = {},
  ): Promise<SearchResponse> => {
    const params = new URLSearchParams({ workspace_id: workspaceId, q: query });
    if (options.channelId) params.set("channel_id", options.channelId);
    if (options.directConversationId) {
      params.set("direct_conversation_id", options.directConversationId);
    }
    if (options.sort) params.set("sort", options.sort);
    if (options.limit !== undefined) params.set("limit", String(options.limit));
    if (options.cursor) params.set("cursor", options.cursor);
    return this.request<SearchResponse>(`/api/search?${params.toString()}`);
  };

  uploads = {
    create: async (
      workspaceId: string,
      file: File | Blob,
      filename = "upload.bin",
      options: { nonce?: string } = {},
    ): Promise<Upload> => {
      const form = new FormData();
      form.set("file", file, filename);
      const params = new URLSearchParams({ workspace_id: workspaceId });
      if (options.nonce) params.set("nonce", options.nonce);
      const data = await this.request<{ upload: Upload }>(`/api/uploads?${params.toString()}`, {
        method: "POST",
        body: form,
      });
      return data.upload;
    },
    findByNonce: async (workspaceId: string, nonce: string): Promise<Upload | undefined> => {
      const params = new URLSearchParams({ workspace_id: workspaceId, nonce });
      const response = await this.fetchResponse(`/api/uploads/by-nonce?${params.toString()}`);
      if (
        response.status === 404 &&
        response.headers.get("X-ClickClack-Upload-Nonce") === "supported"
      ) {
        await response.text();
        return undefined;
      }
      const data = await this.readResponse<{ upload: Upload }>(response);
      return data.upload;
    },
    attach: async (messageId: string, uploadId: string): Promise<void> => {
      await this.request(`/api/messages/${messageId}/attachments`, {
        method: "POST",
        body: JSON.stringify({ upload_id: uploadId }),
      });
    },
  };

  dms = {
    list: async (workspaceId: string): Promise<DirectConversation[]> => {
      const data = await this.request<{ conversations: DirectConversation[] }>(
        `/api/dms?workspace_id=${encodeURIComponent(workspaceId)}`,
      );
      return data.conversations;
    },
    create: async (workspaceId: string, memberIds: string[]): Promise<DirectConversation> => {
      const data = await this.request<{ conversation: DirectConversation }>("/api/dms", {
        method: "POST",
        body: JSON.stringify({ workspace_id: workspaceId, member_ids: memberIds }),
      });
      return data.conversation;
    },
    get: async (conversationId: string): Promise<DirectConversation> => {
      const data = await this.request<{ conversation: DirectConversation }>(
        `/api/dms/${conversationId}`,
      );
      return data.conversation;
    },
    close: async (conversationId: string): Promise<void> => {
      await this.request(`/api/dms/${conversationId}`, { method: "DELETE" });
    },
    open: async (conversationId: string): Promise<DirectConversation> => {
      const data = await this.request<{ conversation: DirectConversation }>(
        `/api/dms/${conversationId}/open`,
        { method: "POST" },
      );
      return data.conversation;
    },
    messages: async (conversationId: string, afterSeq = 0): Promise<Message[]> => {
      const data = await this.request<{ messages: Message[] }>(
        `/api/dms/${conversationId}/messages?after_seq=${afterSeq}`,
      );
      return data.messages;
    },
    sendMessage: async (conversationId: string, input: MessageInput): Promise<Message> => {
      const data = await this.request<{ message: Message }>(`/api/dms/${conversationId}/messages`, {
        method: "POST",
        body: JSON.stringify(input),
      });
      return data.message;
    },
    markRead: async (conversationId: string, seq: number): Promise<ReadReceipt> => {
      const data = await this.request<{ receipt: ReadReceipt }>(`/api/dms/${conversationId}/read`, {
        method: "POST",
        body: JSON.stringify({ seq }),
      });
      return data.receipt;
    },
  };

  events = {
    list: async (input: {
      workspaceId: string;
      afterCursor?: string;
      limit?: number;
      includeTail?: boolean;
    }): Promise<RealtimeEventPage> => {
      const params = new URLSearchParams({ workspace_id: input.workspaceId });
      if (input.afterCursor) params.set("after_cursor", input.afterCursor);
      if (input.limit !== undefined) params.set("limit", String(input.limit));
      if (input.includeTail) params.set("include_tail", "true");
      const data = await this.request<{ events: RealtimeEvent[]; tail_cursor?: string }>(
        `/api/realtime/events?${params.toString()}`,
      );
      return {
        events: data.events,
        ...(data.tail_cursor !== undefined ? { tailCursor: data.tail_cursor } : {}),
      };
    },
    publishEphemeral: async (input: EphemeralEventInput): Promise<RealtimeEvent> => {
      const data = await this.request<{ event: RealtimeEvent }>("/api/realtime/ephemeral", {
        method: "POST",
        body: JSON.stringify({
          workspace_id: input.workspaceId,
          channel_id: input.channelId,
          direct_conversation_id: input.directConversationId,
          type: input.type,
          payload: input.payload,
        }),
      });
      return data.event;
    },
    subscribe: (options: {
      workspaceId: string;
      afterCursor?: string;
      onEvent: (event: RealtimeEvent) => void;
      onClose?: () => void;
    }): WebSocket => {
      const url = new URL(`${this.baseUrl}/api/realtime/ws`);
      url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
      url.searchParams.set("workspace_id", options.workspaceId);
      if (options.afterCursor) url.searchParams.set("after_cursor", options.afterCursor);
      const protocols = this.token ? [`clickclack.bearer.${this.token}`] : undefined;
      if (!this.WebSocket) {
        throw new Error("ClickClackClient events.subscribe requires a WebSocket implementation");
      }
      const socket = protocols ? new this.WebSocket(url, protocols) : new this.WebSocket(url);
      socket.addEventListener("message", (message) =>
        options.onEvent(JSON.parse(String(message.data))),
      );
      if (options.onClose) socket.addEventListener("close", options.onClose);
      return socket;
    },
  };

  private async fetchResponse(path: string, init: RequestInit = {}): Promise<Response> {
    const headers = new Headers(init.headers);
    const method = (init.method ?? "GET").toUpperCase();
    headers.set("Accept", "application/json");
    if (init.body && !(init.body instanceof FormData))
      headers.set("Content-Type", "application/json");
    if (!this.token && !["GET", "HEAD", "OPTIONS", "TRACE"].includes(method))
      headers.set("X-ClickClack-CSRF", "1");
    if (this.token) headers.set("Authorization", `Bearer ${this.token}`);
    if (this.userId) headers.set("X-ClickClack-User", this.userId);
    return this.fetcher(`${this.baseUrl}${path}`, { ...init, headers });
  }

  private async readResponse<T>(response: Response): Promise<T> {
    if (!response.ok) {
      throw new Error(await response.text());
    }
    if (response.status === 204 || response.status === 205) {
      return undefined as T;
    }
    const text = await response.text();
    return text ? (JSON.parse(text) as T) : (undefined as T);
  }

  private async request<T>(path: string, init: RequestInit = {}): Promise<T> {
    return this.readResponse<T>(await this.fetchResponse(path, init));
  }
}

export class ClickClackBot {
  readonly client: ClickClackClient;
  private readonly workspaceId: string;
  private readonly afterCursor?: string;
  private readonly onEvent: BotEventHandler;
  private readonly onClose?: () => void;
  private socket?: WebSocket;

  constructor(options: ClickClackBotOptions) {
    this.client = new ClickClackClient(options);
    this.workspaceId = options.workspaceId;
    this.afterCursor = options.afterCursor;
    this.onEvent = options.onEvent;
    this.onClose = options.onClose;
  }

  start(): WebSocket {
    if (this.socket && isActiveSocket(this.socket)) {
      return this.socket;
    }
    let socket: WebSocket;
    socket = this.client.events.subscribe({
      workspaceId: this.workspaceId,
      afterCursor: this.afterCursor,
      onEvent: (event) => void this.onEvent(event, this.client),
      onClose: () => {
        if (this.socket === socket) this.socket = undefined;
        this.onClose?.();
      },
    });
    this.socket = socket;
    return this.socket;
  }

  stop(): void {
    this.socket?.close();
    this.socket = undefined;
  }

  sendChannelMessage(channelId: string, body: string): Promise<Message> {
    return this.client.channels.sendMessage(channelId, { body });
  }

  sendDirectMessage(conversationId: string, body: string): Promise<Message> {
    return this.client.dms.sendMessage(conversationId, { body });
  }
}

function isActiveSocket(socket: WebSocket): boolean {
  return socket.readyState === 0 || socket.readyState === 1;
}
