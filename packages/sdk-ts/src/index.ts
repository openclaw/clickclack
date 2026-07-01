export type { components, paths } from "./generated/openapi";

export type User = {
  id: string;
  kind: "human" | "bot";
  owner_user_id?: string;
  display_name: string;
  handle: string;
  avatar_url: string;
  created_at: string;
};

export type BotToken = {
  id: string;
  bot_user_id: string;
  workspace_id: string;
  owner_user_id?: string;
  name: string;
  scopes: string[];
  created_by?: string;
  created_at: string;
  last_used_at?: string;
  revoked_at?: string;
  token?: string;
};

export type BotWithTokens = {
  bot: User;
  tokens: BotToken[];
};

export type AppInstallation = {
  id: string;
  workspace_id: string;
  app_slug: string;
  display_name: string;
  bot_user_id: string;
  config: Record<string, unknown>;
  created_by?: string;
  created_at: string;
  revoked_at?: string;
};

export type SlashCommand = {
  id: string;
  workspace_id: string;
  app_installation_id?: string;
  command: string;
  description: string;
  callback_url: string;
  signing_secret?: string;
  bot_user_id: string;
  created_by?: string;
  created_at: string;
  revoked_at?: string;
};

export type EventSubscription = {
  id: string;
  workspace_id: string;
  app_installation_id?: string;
  event_types: string[];
  callback_url: string;
  signing_secret?: string;
  created_by?: string;
  created_at: string;
  revoked_at?: string;
};

export type EventDeliveryAttempt = {
  id: string;
  subscription_id: string;
  event_id: string;
  workspace_id: string;
  event_type: string;
  attempt: number;
  request_json?: string;
  response_status: number;
  response_body?: string;
  error?: string;
  created_at: string;
  completed_at: string;
};

export type AuditLogEntry = {
  id: string;
  workspace_id: string;
  actor_user_id: string;
  action: string;
  target_type: string;
  target_id: string;
  metadata: Record<string, unknown>;
  created_at: string;
};

export type ConnectedAccount = {
  id: string;
  workspace_id: string;
  user_id: string;
  provider: string;
  provider_account_id: string;
  display_name: string;
  scopes: string[];
  metadata: Record<string, unknown>;
  created_at: string;
  revoked_at?: string;
};

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

export type Workspace = {
  id: string;
  route_id: string;
  name: string;
  slug: string;
  created_at: string;
};

export type Channel = {
  id: string;
  route_id: string;
  workspace_id: string;
  name: string;
  kind: string;
  created_at: string;
  archived_at?: string;
  last_seq?: number;
  last_read_seq?: number;
  unread_count?: number;
};

export type Topic = {
  id: string;
  workspace_id: string;
  channel_id?: string;
  name: string;
  created_by?: string;
  created_at: string;
  archived_at?: string;
};

export type Message = {
  id: string;
  route_id?: string;
  workspace_id: string;
  channel_id?: string;
  direct_conversation_id?: string;
  author_id: string;
  parent_message_id?: string;
  thread_root_id: string;
  topic_id?: string;
  channel_seq?: number;
  thread_seq?: number;
  body: string;
  body_format: "markdown";
  created_at: string;
  edited_at?: string;
  deleted_at?: string;
  author?: User;
  attachments?: Upload[];
  quoted_message_id?: string;
  quoted_body_snapshot?: string;
  quoted_author_id?: string;
  quoted_author?: User;
  nonce?: string;
};

export type Upload = {
  id: string;
  workspace_id: string;
  owner_id: string;
  filename: string;
  content_type: string;
  byte_size: number;
  width?: number;
  height?: number;
  duration_ms?: number;
  created_at: string;
};

export type DirectConversation = {
  id: string;
  route_id: string;
  workspace_id: string;
  created_at: string;
  members: User[];
  last_seq?: number;
  last_read_seq?: number;
  unread_count?: number;
};

export type ReadReceipt = {
  scope_id: string;
  user_id: string;
  last_read_seq: number;
  last_read_at: string;
};

export type RouteTarget = {
  workspace_id: string;
  workspace_route_id: string;
  target_type: "channel" | "direct" | "thread";
  target_id: string;
  target_route_id: string;
  parent_type?: "channel" | "direct";
  parent_id?: string;
  parent_route_id?: string;
  canonical_path: string;
};

export type RealtimeEvent = {
  id: string;
  cursor: string;
  type: string;
  workspace_id: string;
  channel_id?: string;
  seq?: number;
  created_at: string;
  payload: unknown;
};

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

  async me(): Promise<User> {
    const data = await this.request<{ user: User }>("/api/me");
    return data.user;
  }

  async updateMe(input: {
    display_name: string;
    handle?: string;
    avatar_url?: string;
  }): Promise<User> {
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
    list: async (workspaceId: string): Promise<BotWithTokens[]> => {
      const data = await this.request<{ bots: BotWithTokens[] }>(
        `/api/workspaces/${workspaceId}/bots`,
      );
      return data.bots;
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
      },
    ): Promise<{ bot: User; bot_token: BotToken }> => {
      return this.request(`/api/workspaces/${workspaceId}/bots`, {
        method: "POST",
        body: JSON.stringify(input),
      });
    },
    listTokens: async (botUserId: string): Promise<BotToken[]> => {
      const data = await this.request<{ bot_tokens: BotToken[] }>(`/api/bots/${botUserId}/tokens`);
      return data.bot_tokens;
    },
    createToken: async (
      botUserId: string,
      input: { name?: string; scopes?: string[] },
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
    revoke: async (installationId: string): Promise<AppInstallation> => {
      const data = await this.request<{ app_installation: AppInstallation }>(
        `/api/app-installations/${installationId}/revoke`,
        {
          method: "POST",
          body: JSON.stringify({}),
        },
      );
      return data.app_installation;
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
    deliveries: async (subscriptionId: string): Promise<EventDeliveryAttempt[]> => {
      const data = await this.request<{ event_delivery_attempts: EventDeliveryAttempt[] }>(
        `/api/event-subscriptions/${subscriptionId}/deliveries`,
      );
      return data.event_delivery_attempts;
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
      input: { name: string; kind?: string },
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
      input: { name?: string; kind?: string; archived?: boolean },
    ): Promise<Channel> => {
      const data = await this.request<{ channel: Channel }>(`/api/channels/${channelId}`, {
        method: "PATCH",
        body: JSON.stringify(input),
      });
      return data.channel;
    },
    messages: async (channelId: string, afterSeq = 0): Promise<Message[]> => {
      const data = await this.request<{ messages: Message[] }>(
        `/api/channels/${channelId}/messages?after_seq=${afterSeq}`,
      );
      return data.messages;
    },
    sendMessage: async (
      channelId: string,
      input: { body: string; quoted_message_id?: string; nonce?: string; topic_id?: string },
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
  };

  messages = {
    get: async (messageId: string): Promise<Message> => {
      const data = await this.request<{ message: Message }>(`/api/messages/${messageId}`);
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
  };

  threads = {
    get: async (messageId: string) => {
      return this.request(`/api/messages/${messageId}/thread`);
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

  search = async (workspaceId: string, query: string, options: { channelId?: string } = {}) => {
    const params = new URLSearchParams({ workspace_id: workspaceId, q: query });
    if (options.channelId) params.set("channel_id", options.channelId);
    return this.request(`/api/search?${params.toString()}`);
  };

  uploads = {
    create: async (
      workspaceId: string,
      file: File | Blob,
      filename = "upload.bin",
    ): Promise<Upload> => {
      const form = new FormData();
      form.set("file", file, filename);
      const params = new URLSearchParams({ workspace_id: workspaceId });
      const data = await this.request<{ upload: Upload }>(`/api/uploads?${params.toString()}`, {
        method: "POST",
        body: form,
      });
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
    sendMessage: async (
      conversationId: string,
      input: { body: string; quoted_message_id?: string; nonce?: string },
    ): Promise<Message> => {
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
    publishEphemeral: async (input: {
      workspaceId: string;
      channelId?: string;
      directConversationId?: string;
      type: "typing.started" | "typing.stopped" | "presence.changed";
      payload?: Record<string, unknown>;
    }): Promise<RealtimeEvent> => {
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

  private async request<T>(path: string, init: RequestInit = {}): Promise<T> {
    const headers = new Headers(init.headers);
    const method = (init.method ?? "GET").toUpperCase();
    headers.set("Accept", "application/json");
    if (init.body && !(init.body instanceof FormData))
      headers.set("Content-Type", "application/json");
    if (!this.token && !["GET", "HEAD", "OPTIONS", "TRACE"].includes(method))
      headers.set("X-ClickClack-CSRF", "1");
    if (this.token) headers.set("Authorization", `Bearer ${this.token}`);
    if (this.userId) headers.set("X-ClickClack-User", this.userId);
    const response = await this.fetcher(`${this.baseUrl}${path}`, { ...init, headers });
    if (!response.ok) {
      throw new Error(await response.text());
    }
    if (response.status === 204 || response.status === 205) {
      return undefined as T;
    }
    const text = await response.text();
    return text ? (JSON.parse(text) as T) : (undefined as T);
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
