CREATE TABLE users (
  id TEXT PRIMARY KEY,
  display_name TEXT NOT NULL,
  avatar_url TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  handle TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL DEFAULT 'human',
  owner_user_id TEXT REFERENCES users(id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX idx_users_handle ON users(handle) WHERE handle <> '';

CREATE TABLE identities (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  provider TEXT NOT NULL,
  provider_subject TEXT NOT NULL,
  email TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  UNIQUE(provider, provider_subject)
);

CREATE TABLE workspaces (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  slug TEXT NOT NULL UNIQUE,
  created_at TEXT NOT NULL,
  route_id TEXT
);

CREATE UNIQUE INDEX idx_workspaces_route_id ON workspaces(route_id) WHERE route_id IS NOT NULL;

CREATE TABLE workspace_members (
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role TEXT NOT NULL,
  created_at TEXT NOT NULL,
  role_sort SMALLINT NOT NULL DEFAULT 9,
  sort_name TEXT NOT NULL DEFAULT '',
  sort_handle TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (workspace_id, user_id)
);

CREATE INDEX idx_workspace_members_workspace_role_user ON workspace_members(workspace_id, role, user_id);
CREATE INDEX idx_workspace_members_page ON workspace_members(workspace_id, role_sort, sort_name, sort_handle, user_id);

CREATE TABLE workspace_member_moderation (
  workspace_id TEXT NOT NULL,
  user_id TEXT NOT NULL,
  timeout_until TEXT,
  blocked_at TEXT,
  moderation_note TEXT NOT NULL DEFAULT '',
  moderation_by TEXT REFERENCES users(id) ON DELETE SET NULL,
  moderation_at TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (workspace_id, user_id),
  FOREIGN KEY (workspace_id, user_id) REFERENCES workspace_members(workspace_id, user_id) ON DELETE CASCADE
);

CREATE INDEX idx_workspace_member_moderation_timeout ON workspace_member_moderation(workspace_id, timeout_until);

CREATE TABLE channels (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  kind TEXT NOT NULL,
  created_at TEXT NOT NULL,
  archived_at TEXT,
  route_id TEXT,
  UNIQUE(workspace_id, name)
);

CREATE UNIQUE INDEX idx_channels_workspace_route_id ON channels(workspace_id, route_id) WHERE route_id IS NOT NULL;

CREATE TABLE direct_conversations (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL,
  route_id TEXT,
  member_set_key TEXT
);

CREATE UNIQUE INDEX idx_direct_conversations_workspace_route_id ON direct_conversations(workspace_id, route_id) WHERE route_id IS NOT NULL;
CREATE UNIQUE INDEX idx_direct_conversations_workspace_member_set ON direct_conversations(workspace_id, member_set_key) WHERE member_set_key IS NOT NULL;

CREATE TABLE direct_conversation_members (
  conversation_id TEXT NOT NULL REFERENCES direct_conversations(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL,
  PRIMARY KEY (conversation_id, user_id)
);

CREATE INDEX idx_direct_conversation_members_user ON direct_conversation_members(user_id, conversation_id);

CREATE TABLE direct_conversation_hidden (
  conversation_id TEXT NOT NULL REFERENCES direct_conversations(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  hidden_at TEXT NOT NULL,
  PRIMARY KEY (conversation_id, user_id)
);

CREATE INDEX idx_direct_conversation_hidden_user ON direct_conversation_hidden(user_id, conversation_id);

CREATE TABLE topics (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  channel_id TEXT REFERENCES channels(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  created_by TEXT REFERENCES users(id),
  created_at TEXT NOT NULL,
  archived_at TEXT,
  UNIQUE(workspace_id, channel_id, name)
);

CREATE INDEX idx_topics_workspace ON topics(workspace_id, archived_at, name);

CREATE TABLE messages (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  channel_id TEXT REFERENCES channels(id) ON DELETE CASCADE,
  direct_conversation_id TEXT,
  author_id TEXT NOT NULL REFERENCES users(id),
  parent_message_id TEXT REFERENCES messages(id) ON DELETE CASCADE,
  thread_root_id TEXT NOT NULL,
  topic_id TEXT REFERENCES topics(id) ON DELETE SET NULL,
  channel_seq BIGINT,
  thread_seq BIGINT,
  body TEXT NOT NULL,
  body_format TEXT NOT NULL,
  created_at TEXT NOT NULL,
  edited_at TEXT,
  deleted_at TEXT,
  quoted_message_id TEXT REFERENCES messages(id) ON DELETE SET NULL,
  quoted_body_snapshot TEXT NOT NULL DEFAULT '',
  quoted_author_id TEXT REFERENCES users(id) ON DELETE SET NULL,
  client_nonce TEXT NOT NULL DEFAULT '',
  route_id TEXT,
  kind TEXT NOT NULL DEFAULT 'message',
  turn_id TEXT
);

CREATE INDEX idx_messages_channel_seq ON messages(channel_id, channel_seq);
CREATE INDEX idx_messages_thread_seq ON messages(thread_root_id, thread_seq);
CREATE INDEX idx_messages_topic ON messages(topic_id, channel_seq);
CREATE INDEX idx_messages_channel_root_page ON messages(channel_id, parent_message_id, channel_seq)
  WHERE channel_id IS NOT NULL AND parent_message_id IS NULL;
CREATE INDEX idx_messages_direct_page ON messages(direct_conversation_id, parent_message_id, channel_seq)
  WHERE direct_conversation_id IS NOT NULL AND parent_message_id IS NULL;
CREATE UNIQUE INDEX idx_messages_author_client_nonce ON messages(author_id, client_nonce) WHERE client_nonce <> '';
CREATE UNIQUE INDEX idx_messages_workspace_route_id ON messages(workspace_id, route_id) WHERE route_id IS NOT NULL;
CREATE UNIQUE INDEX idx_messages_channel_root_unique_seq ON messages(channel_id, channel_seq)
  WHERE channel_id IS NOT NULL AND parent_message_id IS NULL AND channel_seq IS NOT NULL;
CREATE UNIQUE INDEX idx_messages_direct_unique_seq ON messages(direct_conversation_id, channel_seq)
  WHERE direct_conversation_id IS NOT NULL AND parent_message_id IS NULL AND channel_seq IS NOT NULL;
CREATE UNIQUE INDEX idx_messages_thread_unique_seq ON messages(thread_root_id, thread_seq)
  WHERE parent_message_id IS NOT NULL AND thread_seq IS NOT NULL;
CREATE INDEX idx_messages_search_fts ON messages
  USING GIN (to_tsvector('simple', body))
  WHERE direct_conversation_id IS NULL AND channel_id IS NOT NULL AND deleted_at IS NULL AND kind = 'message';

CREATE TABLE thread_state (
  root_message_id TEXT PRIMARY KEY REFERENCES messages(id) ON DELETE CASCADE,
  reply_count BIGINT NOT NULL DEFAULT 0,
  last_reply_at TEXT,
  last_reply_author_ids_json TEXT NOT NULL DEFAULT '[]'
);

CREATE TABLE reactions (
  message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  emoji TEXT NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY (message_id, user_id, emoji)
);

CREATE TABLE events (
  id TEXT PRIMARY KEY,
  cursor TEXT NOT NULL UNIQUE,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  channel_id TEXT,
  type TEXT NOT NULL,
  seq BIGINT,
  payload_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  is_private BIGINT NOT NULL DEFAULT 0 CHECK (is_private IN (0, 1))
);

CREATE INDEX idx_events_workspace_cursor ON events(workspace_id, cursor);

CREATE TABLE event_recipients (
  event_id TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  PRIMARY KEY (event_id, user_id)
);

CREATE INDEX idx_event_recipients_user_event ON event_recipients(user_id, event_id);

CREATE TABLE uploads (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  owner_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  filename TEXT NOT NULL,
  content_type TEXT NOT NULL,
  byte_size BIGINT NOT NULL,
  storage_path TEXT NOT NULL,
  created_at TEXT NOT NULL,
  width BIGINT NOT NULL DEFAULT 0,
  height BIGINT NOT NULL DEFAULT 0,
  duration_ms BIGINT NOT NULL DEFAULT 0
);

CREATE INDEX idx_uploads_workspace_owner ON uploads(workspace_id, owner_id);

CREATE TABLE upload_quota_reservations (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  owner_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  byte_size BIGINT NOT NULL,
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL
);

CREATE INDEX idx_upload_quota_reservations_workspace_owner
  ON upload_quota_reservations(workspace_id, owner_id, expires_at);

CREATE TABLE message_attachments (
  message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  upload_id TEXT NOT NULL REFERENCES uploads(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL,
  PRIMARY KEY (message_id, upload_id)
);

CREATE INDEX idx_message_attachments_message_created ON message_attachments(message_id, created_at, upload_id);
CREATE INDEX idx_message_attachments_upload_message ON message_attachments(upload_id, message_id);

CREATE TABLE invites (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  token TEXT NOT NULL UNIQUE,
  created_by TEXT NOT NULL REFERENCES users(id),
  created_at TEXT NOT NULL,
  accepted_at TEXT
);

CREATE TABLE auth_magic_links (
  id TEXT PRIMARY KEY,
  token TEXT NOT NULL UNIQUE,
  token_hash TEXT NOT NULL DEFAULT '',
  email TEXT NOT NULL,
  display_name TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  used_at TEXT
);

CREATE INDEX idx_auth_magic_links_token ON auth_magic_links(token);
CREATE UNIQUE INDEX idx_auth_magic_links_token_hash ON auth_magic_links(token_hash) WHERE token_hash <> '';

CREATE TABLE sessions (
  id TEXT PRIMARY KEY,
  token TEXT NOT NULL UNIQUE,
  token_hash TEXT NOT NULL DEFAULT '',
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  revoked_at TEXT
);

CREATE INDEX idx_sessions_token ON sessions(token);
CREATE UNIQUE INDEX idx_sessions_token_hash ON sessions(token_hash) WHERE token_hash <> '';

CREATE TABLE channel_reads (
  channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  last_read_seq BIGINT NOT NULL DEFAULT 0,
  last_read_at TEXT NOT NULL,
  PRIMARY KEY (channel_id, user_id)
);

CREATE TABLE direct_reads (
  conversation_id TEXT NOT NULL REFERENCES direct_conversations(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  last_read_seq BIGINT NOT NULL DEFAULT 0,
  last_read_at TEXT NOT NULL,
  PRIMARY KEY (conversation_id, user_id)
);

CREATE TABLE user_notification_settings (
  user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  pushover_enabled BIGINT NOT NULL DEFAULT 0,
  pushover_user_key TEXT NOT NULL DEFAULT ''
);

CREATE TABLE bot_tokens (
  id TEXT PRIMARY KEY,
  token_hash TEXT NOT NULL UNIQUE,
  bot_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  owner_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
  name TEXT NOT NULL,
  scopes_json TEXT NOT NULL,
  created_by TEXT REFERENCES users(id),
  created_at TEXT NOT NULL,
  last_used_at TEXT,
  revoked_at TEXT
);

CREATE INDEX idx_bot_tokens_hash ON bot_tokens(token_hash);
CREATE INDEX idx_bot_tokens_bot ON bot_tokens(bot_user_id);
CREATE INDEX idx_bot_tokens_workspace_bot_revoked ON bot_tokens(workspace_id, bot_user_id, revoked_at);

CREATE TABLE app_installations (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  app_slug TEXT NOT NULL,
  display_name TEXT NOT NULL,
  bot_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  config_json TEXT NOT NULL DEFAULT '{}',
  created_by TEXT REFERENCES users(id),
  created_at TEXT NOT NULL,
  revoked_at TEXT
);

CREATE UNIQUE INDEX idx_app_installations_active_slug
  ON app_installations(workspace_id, app_slug)
  WHERE revoked_at IS NULL;

CREATE INDEX idx_app_installations_workspace
  ON app_installations(workspace_id, revoked_at, app_slug);

CREATE INDEX idx_app_installations_bot
  ON app_installations(bot_user_id);

CREATE TABLE slash_commands (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  app_installation_id TEXT REFERENCES app_installations(id) ON DELETE SET NULL,
  command TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  callback_url TEXT NOT NULL,
  signing_secret TEXT NOT NULL,
  bot_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_by TEXT REFERENCES users(id),
  created_at TEXT NOT NULL,
  revoked_at TEXT
);

CREATE UNIQUE INDEX idx_slash_commands_active_command
  ON slash_commands(workspace_id, command)
  WHERE revoked_at IS NULL;

CREATE INDEX idx_slash_commands_workspace
  ON slash_commands(workspace_id, revoked_at, command);

CREATE TABLE slash_command_invocations (
  id TEXT PRIMARY KEY,
  command_id TEXT NOT NULL REFERENCES slash_commands(id) ON DELETE CASCADE,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  text TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  response_status BIGINT NOT NULL DEFAULT 0,
  response_body TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  completed_at TEXT
);

CREATE INDEX idx_slash_command_invocations_command
  ON slash_command_invocations(command_id, created_at);

CREATE TABLE event_subscriptions (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  app_installation_id TEXT REFERENCES app_installations(id) ON DELETE SET NULL,
  event_types_json TEXT NOT NULL,
  callback_url TEXT NOT NULL,
  signing_secret TEXT NOT NULL,
  created_by TEXT REFERENCES users(id),
  created_at TEXT NOT NULL,
  revoked_at TEXT
);

CREATE INDEX idx_event_subscriptions_workspace
  ON event_subscriptions(workspace_id, revoked_at);

CREATE TABLE event_delivery_attempts (
  id TEXT PRIMARY KEY,
  subscription_id TEXT NOT NULL REFERENCES event_subscriptions(id) ON DELETE CASCADE,
  event_id TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  event_type TEXT NOT NULL,
  attempt BIGINT NOT NULL DEFAULT 1,
  request_json TEXT NOT NULL,
  response_status BIGINT NOT NULL DEFAULT 0,
  response_body TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  completed_at TEXT NOT NULL
);

CREATE INDEX idx_event_delivery_attempts_subscription
  ON event_delivery_attempts(subscription_id, created_at);

CREATE UNIQUE INDEX idx_event_delivery_attempts_once
  ON event_delivery_attempts(subscription_id, event_id, attempt);

CREATE TABLE audit_log_entries (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  actor_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  action TEXT NOT NULL,
  target_type TEXT NOT NULL,
  target_id TEXT NOT NULL,
  metadata_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL
);

CREATE INDEX idx_audit_log_entries_workspace
  ON audit_log_entries(workspace_id, created_at);

CREATE TABLE connected_accounts (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  provider TEXT NOT NULL,
  provider_account_id TEXT NOT NULL,
  display_name TEXT NOT NULL DEFAULT '',
  scopes_json TEXT NOT NULL DEFAULT '[]',
  metadata_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  revoked_at TEXT,
  UNIQUE(workspace_id, provider, provider_account_id)
);

CREATE INDEX idx_connected_accounts_user
  ON connected_accounts(workspace_id, user_id, revoked_at);
