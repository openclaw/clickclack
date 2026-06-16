export type User = {
  id: string;
  kind: "human" | "bot";
  owner_user_id?: string;
  display_name: string;
  handle: string;
  avatar_url: string;
  created_at: string;
  notification_settings?: NotificationSettings;
};

export type NotificationSettings = {
  pushover_enabled: boolean;
  pushover_user_key: string;
};

export type Workspace = {
  id: string;
  route_id: string;
  name: string;
  slug: string;
  created_at: string;
  role?: "owner" | "moderator" | "member" | "guest" | "bot";
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

export type Message = {
  id: string;
  route_id?: string;
  workspace_id: string;
  channel_id?: string;
  direct_conversation_id?: string;
  author_id: string;
  parent_message_id?: string;
  thread_root_id: string;
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
  thread_state?: ThreadState;
  // Optimistic-send: client-supplied id, echoed by server. Used to swap
  // pending placeholder with the real message on response/WS event.
  nonce?: string;
  // Client-only status. Absent for sent messages.
  status?: "pending" | "failed";
};

export type MessagePage = {
  messages: Message[];
  oldest_seq: number;
  newest_seq: number;
  has_older: boolean;
  has_newer: boolean;
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

export type SearchResult = {
  message: Message;
  rank: number;
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

export type MemberModeration = {
  workspace_id: string;
  user: User;
  role: "owner" | "moderator" | "member" | "guest" | "bot";
  posts_remaining: number;
  post_limit: number;
  timeout_until?: string;
  blocked_at?: string;
  moderation_note?: string;
  moderation_by?: string;
  moderation_at?: string;
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

export type ThreadState = {
  root_message_id: string;
  reply_count: number;
  last_reply_at?: string;
  last_reply_author_ids: string[];
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

export type EventPayload = {
  message_id?: string;
  root_message_id?: string;
  channel_id?: string;
  direct_conversation_id?: string;
  nonce?: string;
  user_id?: string;
  author_id?: string;
  last_read_seq?: number;
  seq?: number;
};

export type RealtimeEvent = {
  id: string;
  cursor: string;
  type: string;
  workspace_id: string;
  channel_id?: string;
  seq?: number;
  created_at: string;
  payload: EventPayload;
};
