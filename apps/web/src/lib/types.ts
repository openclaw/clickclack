export type User = {
  id: string;
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
  name: string;
  slug: string;
  created_at: string;
};

export type Channel = {
  id: string;
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
  // Optimistic-send: client-supplied id, echoed by server. Used to swap
  // pending placeholder with the real message on response/WS event.
  nonce?: string;
  // Client-only status. Absent for sent messages.
  status?: "pending" | "failed";
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
  workspace_id: string;
  created_at: string;
  members: User[];
  last_seq?: number;
  last_read_seq?: number;
  unread_count?: number;
};

export type ThreadState = {
  root_message_id: string;
  reply_count: number;
  last_reply_at?: string;
  last_reply_author_ids: string[];
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
