-- name: InsertMagicLink :exec
INSERT INTO auth_magic_links (id, token, token_hash, email, display_name, created_at, expires_at)
VALUES (sqlc.arg(id), sqlc.arg(token), sqlc.arg(token_hash), sqlc.arg(email), sqlc.arg(display_name), sqlc.arg(created_at), sqlc.arg(expires_at));

-- name: GetMagicLinkByToken :one
SELECT id, token, token_hash, email, display_name, created_at, expires_at, used_at
FROM auth_magic_links
WHERE token_hash = sqlc.arg(token_hash);

-- name: MarkMagicLinkUsed :exec
UPDATE auth_magic_links
SET used_at = sqlc.arg(used_at)
WHERE id = sqlc.arg(id);

-- name: GetSessionUser :one
SELECT u.id, u.kind, u.owner_user_id, u.display_name, u.handle, u.avatar_url, u.created_at
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.token_hash = sqlc.arg(token_hash)
  AND s.revoked_at IS NULL
  AND s.expires_at > sqlc.arg(now);

-- name: InsertSession :exec
INSERT INTO sessions (id, token, token_hash, user_id, created_at, expires_at)
VALUES (sqlc.arg(id), sqlc.arg(token), sqlc.arg(token_hash), sqlc.arg(user_id), sqlc.arg(created_at), sqlc.arg(expires_at));

-- name: GetUserByIdentityEmail :one
SELECT u.id, u.kind, u.owner_user_id, u.display_name, u.handle, u.avatar_url, u.created_at
FROM identities i
JOIN users u ON u.id = i.user_id
WHERE i.email = sqlc.arg(email)
ORDER BY u.created_at
LIMIT 1;

-- name: GetUserByIdentityProviderSubject :one
SELECT u.id, u.kind, u.owner_user_id, u.display_name, u.handle, u.avatar_url, u.created_at
FROM identities i
JOIN users u ON u.id = i.user_id
WHERE i.provider = sqlc.arg(provider)
  AND i.provider_subject = sqlc.arg(provider_subject);

-- name: InsertHumanUser :exec
INSERT INTO users (id, display_name, avatar_url, created_at)
VALUES (sqlc.arg(id), sqlc.arg(display_name), sqlc.arg(avatar_url), sqlc.arg(created_at));

-- name: InsertIdentity :exec
INSERT INTO identities (id, user_id, provider, provider_subject, email, created_at)
VALUES (sqlc.arg(id), sqlc.arg(user_id), sqlc.arg(provider), sqlc.arg(provider_subject), sqlc.arg(email), sqlc.arg(created_at));

-- name: FirstUser :one
SELECT id, kind, owner_user_id, display_name, handle, avatar_url, created_at
FROM users
ORDER BY created_at
LIMIT 1;

-- name: GetUser :one
SELECT id, kind, owner_user_id, display_name, handle, avatar_url, created_at
FROM users
WHERE id = sqlc.arg(id);

-- name: UpdateUserProfile :exec
UPDATE users
SET display_name = sqlc.arg(display_name),
    handle = sqlc.arg(handle),
    avatar_url = sqlc.arg(avatar_url)
WHERE id = sqlc.arg(id);

-- name: UpsertNotificationSettings :exec
INSERT INTO user_notification_settings (user_id, pushover_enabled, pushover_user_key)
VALUES (sqlc.arg(user_id), sqlc.arg(pushover_enabled), sqlc.arg(pushover_user_key))
ON CONFLICT(user_id) DO UPDATE SET
  pushover_enabled = excluded.pushover_enabled,
  pushover_user_key = excluded.pushover_user_key;

-- name: GetNotificationSettings :one
SELECT pushover_enabled, pushover_user_key
FROM user_notification_settings
WHERE user_id = sqlc.arg(user_id);

-- name: ListWorkspacePushNotificationRecipients :many
SELECT u.id AS user_id, u.display_name, uns.pushover_user_key
FROM workspace_members wm
JOIN users u ON u.id = wm.user_id
JOIN user_notification_settings uns ON uns.user_id = u.id
WHERE wm.workspace_id = sqlc.arg(workspace_id)
  AND u.id <> sqlc.arg(author_id)
  AND uns.pushover_enabled = 1
  AND uns.pushover_user_key <> ''
ORDER BY u.id;

-- name: ListDirectPushNotificationRecipients :many
SELECT u.id AS user_id, u.display_name, uns.pushover_user_key
FROM direct_conversation_members dcm
JOIN direct_conversations dc ON dc.id = dcm.conversation_id
JOIN workspace_members wm ON wm.workspace_id = dc.workspace_id AND wm.user_id = dcm.user_id
JOIN users u ON u.id = dcm.user_id
JOIN user_notification_settings uns ON uns.user_id = u.id
WHERE dcm.conversation_id = sqlc.arg(conversation_id)
  AND u.id <> sqlc.arg(author_id)
  AND uns.pushover_enabled = 1
  AND uns.pushover_user_key <> ''
ORDER BY u.id;

-- name: InsertInvite :exec
INSERT INTO invites (id, workspace_id, token, created_by, created_at)
VALUES (sqlc.arg(id), sqlc.arg(workspace_id), sqlc.arg(token), sqlc.arg(created_by), sqlc.arg(created_at));

-- name: InsertWorkspaceMember :exec
INSERT INTO workspace_members (workspace_id, user_id, role, created_at)
VALUES (sqlc.arg(workspace_id), sqlc.arg(user_id), sqlc.arg(role), sqlc.arg(created_at))
ON CONFLICT DO NOTHING;

-- name: InsertDefaultWorkspaceMember :exec
INSERT INTO workspace_members (workspace_id, user_id, role, created_at)
VALUES (sqlc.arg(workspace_id), sqlc.arg(user_id), 'member', sqlc.arg(created_at))
ON CONFLICT DO NOTHING;

-- name: FirstWorkspace :one
SELECT id, COALESCE(route_id, '') AS route_id, name, slug, created_at
FROM workspaces
ORDER BY created_at
LIMIT 1;

-- name: GetWorkspaceByRouteID :one
SELECT id, COALESCE(route_id, '') AS route_id, name, slug, created_at
FROM workspaces
WHERE route_id = sqlc.arg(route_id);

-- name: ListWorkspaces :many
SELECT w.id, COALESCE(w.route_id, '') AS route_id, w.name, w.slug, w.created_at
FROM workspaces w
JOIN workspace_members wm ON wm.workspace_id = w.id
WHERE wm.user_id = sqlc.arg(user_id)
ORDER BY w.created_at;

-- name: InsertWorkspace :exec
INSERT INTO workspaces (id, route_id, name, slug, created_at)
VALUES (sqlc.arg(id), sqlc.arg(route_id), sqlc.arg(name), sqlc.arg(slug), sqlc.arg(created_at));

-- name: GetWorkspace :one
SELECT w.id, COALESCE(w.route_id, '') AS route_id, w.name, w.slug, w.created_at
FROM workspaces w
JOIN workspace_members wm ON wm.workspace_id = w.id
WHERE w.id = sqlc.arg(workspace_id)
  AND wm.user_id = sqlc.arg(user_id);

-- name: InsertDefaultChannel :exec
INSERT INTO channels (id, route_id, workspace_id, name, kind, created_at)
VALUES (sqlc.arg(id), sqlc.arg(route_id), sqlc.arg(workspace_id), 'general', 'public', sqlc.arg(created_at));

-- name: InsertChannel :exec
INSERT INTO channels (id, route_id, workspace_id, name, kind, created_at)
VALUES (sqlc.arg(id), sqlc.arg(route_id), sqlc.arg(workspace_id), sqlc.arg(name), sqlc.arg(kind), sqlc.arg(created_at));

-- name: ListChannels :many
SELECT c.id, COALESCE(c.route_id, '') AS route_id, c.workspace_id, c.name, c.kind, c.created_at, c.archived_at,
       CAST(COALESCE((SELECT MAX(channel_seq) FROM messages WHERE channel_id = c.id AND parent_message_id IS NULL), 0) AS BIGINT) AS last_seq,
       CAST(COALESCE((SELECT cr.last_read_seq FROM channel_reads cr WHERE cr.channel_id = c.id AND cr.user_id = sqlc.arg(reader_user_id)), 0) AS BIGINT) AS last_read_seq,
       CAST(COALESCE((
         SELECT COUNT(*)
         FROM messages m
         WHERE m.channel_id = c.id
           AND m.parent_message_id IS NULL
           AND m.author_id <> sqlc.arg(reader_user_id)
           AND m.channel_seq > COALESCE((SELECT cr2.last_read_seq FROM channel_reads cr2 WHERE cr2.channel_id = c.id AND cr2.user_id = sqlc.arg(reader_user_id)), 0)
       ), 0) AS BIGINT) AS unread_count
FROM channels c
WHERE c.workspace_id = sqlc.arg(workspace_id)
ORDER BY c.name;

-- name: RequireMembership :one
SELECT 1
FROM workspace_members
WHERE workspace_id = sqlc.arg(workspace_id)
  AND user_id = sqlc.arg(user_id);

-- name: InsertBotUser :exec
INSERT INTO users (id, kind, owner_user_id, display_name, handle, avatar_url, created_at)
VALUES (sqlc.arg(id), 'bot', sqlc.arg(owner_user_id), sqlc.arg(display_name), sqlc.arg(handle), sqlc.arg(avatar_url), sqlc.arg(created_at));

-- name: InsertBotToken :exec
INSERT INTO bot_tokens (id, token_hash, bot_user_id, workspace_id, owner_user_id, name, scopes_json, created_by, created_at)
VALUES (sqlc.arg(id), sqlc.arg(token_hash), sqlc.arg(bot_user_id), sqlc.arg(workspace_id), sqlc.arg(owner_user_id), sqlc.arg(name), sqlc.arg(scopes_json), sqlc.arg(created_by), sqlc.arg(created_at));

-- name: GetBotTokenAuth :one
SELECT u.id, u.kind, u.owner_user_id, u.display_name, u.handle, u.avatar_url, u.created_at,
       bt.id AS token_id, bt.workspace_id, bt.scopes_json
FROM bot_tokens bt
JOIN users u ON u.id = bt.bot_user_id
WHERE bt.token_hash = sqlc.arg(token_hash)
  AND bt.revoked_at IS NULL;

-- name: TouchBotToken :exec
UPDATE bot_tokens
SET last_used_at = sqlc.arg(last_used_at)
WHERE id = sqlc.arg(id);

-- name: InsertUpload :exec
INSERT INTO uploads (id, workspace_id, owner_id, filename, content_type, byte_size, width, height, duration_ms, storage_path, created_at)
VALUES (sqlc.arg(id), sqlc.arg(workspace_id), sqlc.arg(owner_id), sqlc.arg(filename), sqlc.arg(content_type), sqlc.arg(byte_size), sqlc.arg(width), sqlc.arg(height), sqlc.arg(duration_ms), sqlc.arg(storage_path), sqlc.arg(created_at));

-- name: GetUpload :one
SELECT id, workspace_id, owner_id, filename, content_type, byte_size, width, height, duration_ms, storage_path, created_at
FROM uploads
WHERE id = sqlc.arg(id);

-- name: GetUploadWorkspace :one
SELECT workspace_id
FROM uploads
WHERE id = sqlc.arg(id);

-- name: AttachUpload :exec
INSERT INTO message_attachments (message_id, upload_id, created_at)
VALUES (sqlc.arg(message_id), sqlc.arg(upload_id), sqlc.arg(created_at))
ON CONFLICT DO NOTHING;

-- name: GetChannelWorkspace :one
SELECT workspace_id
FROM channels
WHERE id = sqlc.arg(id);

-- name: GetDirectConversationWorkspace :one
SELECT workspace_id
FROM direct_conversations
WHERE id = sqlc.arg(id);

-- name: ChannelLastSeq :one
SELECT CAST(COALESCE(MAX(channel_seq), 0) AS BIGINT) AS last_seq
FROM messages
WHERE channel_id = sqlc.arg(channel_id)::text
  AND parent_message_id IS NULL;

-- name: ChannelNextSeq :one
SELECT CAST(COALESCE(MAX(channel_seq), 0) + 1 AS BIGINT) AS next_seq
FROM messages
WHERE channel_id = sqlc.arg(channel_id)::text
  AND parent_message_id IS NULL;

-- name: DirectLastSeq :one
SELECT CAST(COALESCE(MAX(channel_seq), 0) AS BIGINT) AS last_seq
FROM messages
WHERE direct_conversation_id = sqlc.arg(conversation_id)::text
  AND parent_message_id IS NULL;

-- name: DirectNextSeq :one
SELECT CAST(COALESCE(MAX(channel_seq), 0) + 1 AS BIGINT) AS next_seq
FROM messages
WHERE direct_conversation_id = sqlc.arg(conversation_id)::text;

-- name: ReadChannelRead :one
SELECT last_read_seq, last_read_at
FROM channel_reads
WHERE channel_id = sqlc.arg(channel_id)
  AND user_id = sqlc.arg(user_id);

-- name: ReadDirectRead :one
SELECT last_read_seq, last_read_at
FROM direct_reads
WHERE conversation_id = sqlc.arg(conversation_id)
  AND user_id = sqlc.arg(user_id);

-- name: UpsertChannelRead :exec
INSERT INTO channel_reads (channel_id, user_id, last_read_seq, last_read_at)
VALUES (sqlc.arg(channel_id), sqlc.arg(user_id), sqlc.arg(last_read_seq), sqlc.arg(last_read_at))
ON CONFLICT(channel_id, user_id) DO UPDATE SET
  last_read_seq = excluded.last_read_seq,
  last_read_at = excluded.last_read_at;

-- name: UpsertDirectRead :exec
INSERT INTO direct_reads (conversation_id, user_id, last_read_seq, last_read_at)
VALUES (sqlc.arg(conversation_id), sqlc.arg(user_id), sqlc.arg(last_read_seq), sqlc.arg(last_read_at))
ON CONFLICT(conversation_id, user_id) DO UPDATE SET
  last_read_seq = excluded.last_read_seq,
  last_read_at = excluded.last_read_at;

-- name: ListDirectConversations :many
SELECT dc.id, COALESCE(dc.route_id, '') AS route_id, dc.workspace_id, dc.created_at,
       CAST(COALESCE((SELECT MAX(channel_seq) FROM messages WHERE direct_conversation_id = dc.id AND parent_message_id IS NULL), 0) AS BIGINT) AS last_seq,
       CAST(COALESCE((SELECT dr.last_read_seq FROM direct_reads dr WHERE dr.conversation_id = dc.id AND dr.user_id = sqlc.arg(reader_user_id)), 0) AS BIGINT) AS last_read_seq,
       CAST(COALESCE((
         SELECT COUNT(*)
         FROM messages m
         WHERE m.direct_conversation_id = dc.id
           AND m.parent_message_id IS NULL
           AND m.author_id <> sqlc.arg(reader_user_id)
           AND m.channel_seq > COALESCE((SELECT dr2.last_read_seq FROM direct_reads dr2 WHERE dr2.conversation_id = dc.id AND dr2.user_id = sqlc.arg(reader_user_id)), 0)
       ), 0) AS BIGINT) AS unread_count
FROM direct_conversations dc
JOIN direct_conversation_members dcm ON dcm.conversation_id = dc.id
WHERE dc.workspace_id = sqlc.arg(workspace_id)
  AND dcm.user_id = sqlc.arg(reader_user_id)
ORDER BY dc.created_at;

-- name: GetDirectConversation :one
SELECT dc.id, COALESCE(dc.route_id, '') AS route_id, dc.workspace_id, dc.created_at,
       CAST(COALESCE((SELECT MAX(channel_seq) FROM messages WHERE direct_conversation_id = dc.id AND parent_message_id IS NULL), 0) AS BIGINT) AS last_seq,
       CAST(COALESCE((SELECT dr.last_read_seq FROM direct_reads dr WHERE dr.conversation_id = dc.id AND dr.user_id = sqlc.arg(reader_user_id)), 0) AS BIGINT) AS last_read_seq,
       CAST(COALESCE((
         SELECT COUNT(*)
         FROM messages m
         WHERE m.direct_conversation_id = dc.id
           AND m.parent_message_id IS NULL
           AND m.author_id <> sqlc.arg(reader_user_id)
           AND m.channel_seq > COALESCE((SELECT dr2.last_read_seq FROM direct_reads dr2 WHERE dr2.conversation_id = dc.id AND dr2.user_id = sqlc.arg(reader_user_id)), 0)
       ), 0) AS BIGINT) AS unread_count
FROM direct_conversations dc
JOIN direct_conversation_members dcm ON dcm.conversation_id = dc.id
JOIN workspace_members wm ON wm.workspace_id = dc.workspace_id AND wm.user_id = dcm.user_id
WHERE dc.id = sqlc.arg(conversation_id)
  AND dcm.user_id = sqlc.arg(reader_user_id);

-- name: InsertDirectConversation :exec
INSERT INTO direct_conversations (id, route_id, workspace_id, created_at)
VALUES (sqlc.arg(id), sqlc.arg(route_id), sqlc.arg(workspace_id), sqlc.arg(created_at));

-- name: InsertDirectConversationMember :exec
INSERT INTO direct_conversation_members (conversation_id, user_id, created_at)
VALUES (sqlc.arg(conversation_id), sqlc.arg(user_id), sqlc.arg(created_at));

-- name: RequireDirectMembership :one
SELECT 1
FROM direct_conversation_members dcm
JOIN direct_conversations dc ON dc.id = dcm.conversation_id
JOIN workspace_members wm ON wm.workspace_id = dc.workspace_id AND wm.user_id = dcm.user_id
WHERE dcm.conversation_id = sqlc.arg(conversation_id)
  AND dcm.user_id = sqlc.arg(user_id);

-- name: DirectConversationMemberIDs :many
SELECT dcm.user_id
FROM direct_conversation_members dcm
JOIN direct_conversations dc ON dc.id = dcm.conversation_id
JOIN workspace_members wm ON wm.workspace_id = dc.workspace_id AND wm.user_id = dcm.user_id
WHERE dcm.conversation_id = sqlc.arg(conversation_id)
ORDER BY dcm.user_id;

-- name: DirectConversationMembers :many
SELECT u.id, u.kind, u.owner_user_id, u.display_name, u.handle, u.avatar_url, u.created_at
FROM users u
JOIN direct_conversation_members dcm ON dcm.user_id = u.id
WHERE dcm.conversation_id = sqlc.arg(conversation_id)
ORDER BY u.display_name;

-- name: InsertChannelMessage :exec
INSERT INTO messages (id, workspace_id, channel_id, direct_conversation_id, author_id, parent_message_id, thread_root_id, channel_seq, thread_seq, body, body_format, created_at, quoted_message_id, quoted_body_snapshot, quoted_author_id, client_nonce)
VALUES (sqlc.arg(id), sqlc.arg(workspace_id), sqlc.arg(channel_id), NULL, sqlc.arg(author_id), NULL, sqlc.arg(thread_root_id), sqlc.arg(channel_seq), NULL, sqlc.arg(body), 'markdown', sqlc.arg(created_at), sqlc.arg(quoted_message_id), sqlc.arg(quoted_body_snapshot), sqlc.arg(quoted_author_id), sqlc.arg(client_nonce));

-- name: InsertDirectMessage :exec
INSERT INTO messages (id, workspace_id, channel_id, direct_conversation_id, author_id, parent_message_id, thread_root_id, channel_seq, thread_seq, body, body_format, created_at, quoted_message_id, quoted_body_snapshot, quoted_author_id, client_nonce)
VALUES (sqlc.arg(id), sqlc.arg(workspace_id), NULL, sqlc.arg(direct_conversation_id), sqlc.arg(author_id), NULL, sqlc.arg(thread_root_id), sqlc.arg(channel_seq), NULL, sqlc.arg(body), 'markdown', sqlc.arg(created_at), sqlc.arg(quoted_message_id), sqlc.arg(quoted_body_snapshot), sqlc.arg(quoted_author_id), sqlc.arg(client_nonce));

-- name: InsertThreadState :exec
INSERT INTO thread_state (root_message_id)
VALUES (sqlc.arg(root_message_id));

-- name: GetThreadState :one
SELECT root_message_id, reply_count, last_reply_at, last_reply_author_ids_json
FROM thread_state
WHERE root_message_id = sqlc.arg(root_message_id);

-- name: ThreadNextSeq :one
SELECT CAST(COALESCE(MAX(thread_seq), 0) + 1 AS BIGINT) AS next_seq
FROM messages
WHERE thread_root_id = sqlc.arg(thread_root_id)
  AND parent_message_id = sqlc.arg(parent_message_id);

-- name: UpdateThreadState :exec
UPDATE thread_state
SET reply_count = reply_count + 1,
    last_reply_at = sqlc.arg(last_reply_at),
    last_reply_author_ids_json = sqlc.arg(last_reply_author_ids_json)
WHERE root_message_id = sqlc.arg(root_message_id);

-- name: InsertThreadReply :exec
INSERT INTO messages (id, workspace_id, channel_id, direct_conversation_id, author_id, parent_message_id, thread_root_id, channel_seq, thread_seq, body, body_format, created_at, quoted_message_id, quoted_body_snapshot, quoted_author_id, client_nonce)
VALUES (sqlc.arg(id), sqlc.arg(workspace_id), sqlc.arg(channel_id), sqlc.arg(direct_conversation_id), sqlc.arg(author_id), sqlc.arg(parent_message_id), sqlc.arg(thread_root_id), NULL, sqlc.arg(thread_seq), sqlc.arg(body), 'markdown', sqlc.arg(created_at), sqlc.arg(quoted_message_id), sqlc.arg(quoted_body_snapshot), sqlc.arg(quoted_author_id), sqlc.arg(client_nonce));

-- name: GetChannel :one
SELECT id, COALESCE(route_id, '') AS route_id, workspace_id, name, kind, created_at, archived_at
FROM channels
WHERE id = sqlc.arg(id);

-- name: GetChannelByIDAndWorkspace :one
SELECT id, COALESCE(route_id, '') AS route_id, workspace_id, name, kind, created_at, archived_at
FROM channels
WHERE workspace_id = sqlc.arg(workspace_id)
  AND id = sqlc.arg(id);

-- name: GetChannelByRouteIDAndWorkspace :one
SELECT id, COALESCE(route_id, '') AS route_id, workspace_id, name, kind, created_at, archived_at
FROM channels
WHERE workspace_id = sqlc.arg(workspace_id)
  AND route_id = sqlc.arg(route_id);

-- name: GetDirectByIDAndWorkspace :one
SELECT dc.id, COALESCE(dc.route_id, '') AS route_id, dc.workspace_id, dc.created_at
FROM direct_conversations dc
JOIN direct_conversation_members dcm ON dcm.conversation_id = dc.id
JOIN workspace_members wm ON wm.workspace_id = dc.workspace_id AND wm.user_id = dcm.user_id
WHERE dc.workspace_id = sqlc.arg(workspace_id)
  AND dc.id = sqlc.arg(id)
  AND dcm.user_id = sqlc.arg(user_id);

-- name: GetDirectByRouteIDAndWorkspace :one
SELECT dc.id, COALESCE(dc.route_id, '') AS route_id, dc.workspace_id, dc.created_at
FROM direct_conversations dc
JOIN direct_conversation_members dcm ON dcm.conversation_id = dc.id
JOIN workspace_members wm ON wm.workspace_id = dc.workspace_id AND wm.user_id = dcm.user_id
WHERE dc.workspace_id = sqlc.arg(workspace_id)
  AND dc.route_id = sqlc.arg(route_id)
  AND dcm.user_id = sqlc.arg(user_id);

-- name: ChannelRouteID :one
SELECT COALESCE(route_id, '') AS route_id
FROM channels
WHERE workspace_id = sqlc.arg(workspace_id)
  AND id = sqlc.arg(id);

-- name: DirectRouteID :one
SELECT COALESCE(dc.route_id, '') AS route_id
FROM direct_conversations dc
JOIN direct_conversation_members dcm ON dcm.conversation_id = dc.id
JOIN workspace_members wm ON wm.workspace_id = dc.workspace_id AND wm.user_id = dcm.user_id
WHERE dc.workspace_id = sqlc.arg(workspace_id)
  AND dc.id = sqlc.arg(id)
  AND dcm.user_id = sqlc.arg(user_id);

-- name: UpdateChannel :exec
UPDATE channels
SET name = sqlc.arg(name),
    kind = sqlc.arg(kind),
    archived_at = sqlc.arg(archived_at)
WHERE id = sqlc.arg(id);

-- name: UpdateMessageBody :exec
UPDATE messages
SET body = sqlc.arg(body),
    edited_at = sqlc.arg(edited_at)
WHERE id = sqlc.arg(id);

-- name: DeleteMessageBody :exec
UPDATE messages
SET body = '',
    deleted_at = sqlc.arg(deleted_at)
WHERE id = sqlc.arg(id);

-- name: AddReaction :execrows
INSERT INTO reactions (message_id, user_id, emoji, created_at)
VALUES (sqlc.arg(message_id), sqlc.arg(user_id), sqlc.arg(emoji), sqlc.arg(created_at))
ON CONFLICT DO NOTHING;

-- name: RemoveReaction :execrows
DELETE FROM reactions
WHERE message_id = sqlc.arg(message_id)
  AND user_id = sqlc.arg(user_id)
  AND emoji = sqlc.arg(emoji);

-- name: ListEventsAfter :many
SELECT e.id, e.cursor, e.workspace_id, COALESCE(e.channel_id, '') AS channel_id, e.type, e.seq, e.payload_json, e.created_at
FROM events e
WHERE e.workspace_id = sqlc.arg(workspace_id)
  AND e.cursor > sqlc.arg(cursor)
  AND (
    e.is_private = 0
    OR EXISTS (SELECT 1 FROM event_recipients er WHERE er.event_id = e.id AND er.user_id = sqlc.arg(user_id))
  )
ORDER BY e.cursor
LIMIT sqlc.arg(limit_count);

-- name: InsertEvent :exec
INSERT INTO events (id, cursor, workspace_id, channel_id, type, seq, payload_json, created_at, is_private)
VALUES (sqlc.arg(id), sqlc.arg(cursor), sqlc.arg(workspace_id), sqlc.arg(channel_id), sqlc.arg(type), sqlc.arg(seq), sqlc.arg(payload_json), sqlc.arg(created_at), sqlc.arg(is_private));

-- name: InsertEventRecipient :exec
INSERT INTO event_recipients (event_id, user_id)
VALUES (sqlc.arg(event_id), sqlc.arg(user_id));

-- name: PruneEvents :execrows
DELETE FROM events AS e
WHERE e.workspace_id = sqlc.arg(workspace_id_arg)
  AND (sqlc.arg(before)::text = '' OR e.created_at < sqlc.arg(before)::text)
  AND e.id NOT IN (
    SELECT kept.id
    FROM events AS kept
    WHERE kept.workspace_id = sqlc.arg(workspace_id_arg)
    ORDER BY kept.cursor DESC
    LIMIT sqlc.arg(keep_latest)
  );
