-- name: InsertMagicLink :exec
INSERT INTO auth_magic_links (id, token, token_hash, email, display_name, created_at, expires_at)
VALUES (sqlc.arg(id), sqlc.arg(token), sqlc.arg(token_hash), sqlc.arg(email), sqlc.arg(display_name), sqlc.arg(created_at), sqlc.arg(expires_at));

-- name: GetMagicLinkByToken :one
SELECT id, token, token_hash, email, display_name, created_at, expires_at, used_at
FROM auth_magic_links
WHERE token_hash = sqlc.arg(token_hash);

-- name: MarkMagicLinkUsed :execrows
UPDATE auth_magic_links
SET used_at = sqlc.arg(used_at)
WHERE id = sqlc.arg(id)
  AND used_at IS NULL
  AND (
    CASE
      WHEN instr(expires_at, '.') = 0 THEN replace(expires_at, 'Z', '.000000000Z')
      ELSE substr(expires_at, 1, instr(expires_at, '.')) ||
           substr(substr(expires_at, instr(expires_at, '.') + 1, instr(expires_at, 'Z') - instr(expires_at, '.') - 1) || '000000000', 1, 9) ||
           'Z'
    END
  ) > (
    CASE
      WHEN instr(CAST(sqlc.arg(now) AS TEXT), '.') = 0 THEN replace(CAST(sqlc.arg(now) AS TEXT), 'Z', '.000000000Z')
      ELSE substr(CAST(sqlc.arg(now) AS TEXT), 1, instr(CAST(sqlc.arg(now) AS TEXT), '.')) ||
           substr(substr(CAST(sqlc.arg(now) AS TEXT), instr(CAST(sqlc.arg(now) AS TEXT), '.') + 1, instr(CAST(sqlc.arg(now) AS TEXT), 'Z') - instr(CAST(sqlc.arg(now) AS TEXT), '.') - 1) || '000000000', 1, 9) ||
           'Z'
    END
  );

-- name: GetSessionUser :one
SELECT u.id, u.kind, u.owner_user_id, u.display_name, u.handle, u.avatar_url, u.created_at, s.expires_at AS session_expires_at,
       COALESCE(uns.pushover_enabled, 0) AS pushover_enabled,
       COALESCE(uns.pushover_user_key, '') AS pushover_user_key
FROM sessions s
JOIN users u ON u.id = s.user_id
LEFT JOIN user_notification_settings uns ON uns.user_id = u.id
WHERE s.token_hash = sqlc.arg(token_hash)
  AND s.revoked_at IS NULL;

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
INSERT OR IGNORE INTO workspace_members (workspace_id, user_id, role, created_at)
VALUES (sqlc.arg(workspace_id), sqlc.arg(user_id), sqlc.arg(role), sqlc.arg(created_at));

-- name: UpsertGuestWorkspaceMemberRole :exec
INSERT INTO workspace_members (workspace_id, user_id, role, created_at)
VALUES (sqlc.arg(workspace_id), sqlc.arg(user_id), sqlc.arg(role), sqlc.arg(created_at))
ON CONFLICT(workspace_id, user_id) DO UPDATE SET
  role = CASE
    WHEN workspace_members.role = 'owner' THEN workspace_members.role
    WHEN excluded.role = 'moderator' THEN excluded.role
    WHEN workspace_members.role = 'moderator' THEN excluded.role
    WHEN workspace_members.role = 'member' AND excluded.role = 'moderator' THEN excluded.role
    WHEN workspace_members.role = 'guest' AND excluded.role IN ('member', 'moderator') THEN excluded.role
    ELSE workspace_members.role
  END;

-- name: InsertDefaultWorkspaceMember :exec
INSERT OR IGNORE INTO workspace_members (workspace_id, user_id, role, created_at)
VALUES (sqlc.arg(workspace_id), sqlc.arg(user_id), sqlc.arg(role), sqlc.arg(created_at));

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
SELECT w.id, COALESCE(w.route_id, '') AS route_id, w.name, w.slug, w.created_at, wm.role
FROM workspaces w
JOIN workspace_members wm ON wm.workspace_id = w.id
WHERE wm.user_id = sqlc.arg(user_id)
ORDER BY w.created_at;

-- name: InsertWorkspace :exec
INSERT INTO workspaces (id, route_id, name, slug, created_at)
VALUES (sqlc.arg(id), sqlc.arg(route_id), sqlc.arg(name), sqlc.arg(slug), sqlc.arg(created_at));

-- name: GetWorkspace :one
SELECT w.id, COALESCE(w.route_id, '') AS route_id, w.name, w.slug, w.created_at, wm.role
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
       CAST(COALESCE((SELECT MAX(channel_seq) FROM messages WHERE channel_id = c.id AND parent_message_id IS NULL), 0) AS INTEGER) AS last_seq,
       CAST(COALESCE((SELECT cr.last_read_seq FROM channel_reads cr WHERE cr.channel_id = c.id AND cr.user_id = sqlc.arg(reader_user_id)), 0) AS INTEGER) AS last_read_seq,
       CAST(COALESCE((
         SELECT COUNT(*)
         FROM messages m
         WHERE m.channel_id = c.id
           AND m.parent_message_id IS NULL
           AND m.author_id <> sqlc.arg(reader_user_id)
           AND m.channel_seq > COALESCE((SELECT cr2.last_read_seq FROM channel_reads cr2 WHERE cr2.channel_id = c.id AND cr2.user_id = sqlc.arg(reader_user_id)), 0)
       ), 0) AS INTEGER) AS unread_count
FROM channels c
WHERE c.workspace_id = sqlc.arg(workspace_id)
ORDER BY c.name;

-- name: RequireMembership :one
SELECT 1
FROM workspace_members
WHERE workspace_id = sqlc.arg(workspace_id)
  AND user_id = sqlc.arg(user_id);

-- name: RequireMembershipRole :one
SELECT role
FROM workspace_members
WHERE workspace_id = sqlc.arg(workspace_id)
  AND user_id = sqlc.arg(user_id);

-- name: UserHasNonGuestMembership :one
SELECT workspace_id
FROM workspace_members
WHERE user_id = sqlc.arg(user_id)
  AND role <> 'guest'
LIMIT 1;

-- name: ChannelNameForAccess :one
SELECT name
FROM channels
WHERE id = sqlc.arg(id)
  AND workspace_id = sqlc.arg(workspace_id);

-- name: RequireChannelAdmin :one
SELECT 1
FROM workspace_members
WHERE workspace_id = sqlc.arg(workspace_id)
  AND user_id = sqlc.arg(user_id)
  AND role IN ('owner', 'bot');

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

-- name: CountRecentWorkspaceMessagesByAuthor :one
SELECT COUNT(*)
FROM messages m
WHERE m.workspace_id = sqlc.arg(workspace_id)
  AND m.author_id = sqlc.arg(author_id)
  AND m.direct_conversation_id IS NULL
  AND m.channel_id IN (SELECT c.id FROM channels c WHERE c.workspace_id = sqlc.arg(workspace_id) AND c.name = 'guest')
  AND m.created_at >= sqlc.arg(cutoff);

-- name: ListWorkspaceMembersForModeration :many
SELECT u.id, u.kind, COALESCE(u.owner_user_id, '') AS owner_user_id, u.display_name, u.handle, u.avatar_url, u.created_at,
       wm.role, COALESCE(m.timeout_until, '') AS timeout_until, COALESCE(m.blocked_at, '') AS blocked_at,
       COALESCE(m.moderation_note, '') AS moderation_note, COALESCE(m.moderation_by, '') AS moderation_by,
       COALESCE(m.moderation_at, '') AS moderation_at
FROM workspace_members wm
JOIN users u ON u.id = wm.user_id
LEFT JOIN workspace_member_moderation m ON m.workspace_id = wm.workspace_id AND m.user_id = wm.user_id
WHERE wm.workspace_id = sqlc.arg(workspace_id)
ORDER BY CASE wm.role WHEN 'owner' THEN 4 WHEN 'moderator' THEN 3 WHEN 'member' THEN 2 ELSE 1 END DESC,
         u.display_name COLLATE NOCASE;

-- name: UpdateWorkspaceMemberRole :exec
UPDATE workspace_members
SET role = sqlc.arg(role)
WHERE workspace_id = sqlc.arg(workspace_id)
  AND user_id = sqlc.arg(user_id);

-- name: GetMemberModerationState :one
SELECT timeout_until, blocked_at
FROM workspace_member_moderation
WHERE workspace_id = sqlc.arg(workspace_id)
  AND user_id = sqlc.arg(user_id);

-- name: UpsertMemberModeration :exec
INSERT INTO workspace_member_moderation (workspace_id, user_id, timeout_until, blocked_at, moderation_note, moderation_by, moderation_at)
VALUES (sqlc.arg(workspace_id), sqlc.arg(user_id), sqlc.arg(timeout_until), sqlc.arg(blocked_at), '', sqlc.arg(moderation_by), sqlc.arg(moderation_at))
ON CONFLICT(workspace_id, user_id) DO UPDATE SET
  timeout_until = CASE WHEN excluded.timeout_until <> '' THEN excluded.timeout_until ELSE workspace_member_moderation.timeout_until END,
  blocked_at = CASE WHEN excluded.blocked_at <> '' THEN excluded.blocked_at ELSE workspace_member_moderation.blocked_at END,
  moderation_by = excluded.moderation_by,
  moderation_at = excluded.moderation_at;

-- name: UpsertMemberModerationWithNote :exec
INSERT INTO workspace_member_moderation (workspace_id, user_id, timeout_until, blocked_at, moderation_note, moderation_by, moderation_at)
VALUES (sqlc.arg(workspace_id), sqlc.arg(user_id), sqlc.arg(timeout_until), sqlc.arg(blocked_at), sqlc.arg(moderation_note), sqlc.arg(moderation_by), sqlc.arg(moderation_at))
ON CONFLICT(workspace_id, user_id) DO UPDATE SET
  timeout_until = CASE WHEN excluded.timeout_until <> '' THEN excluded.timeout_until ELSE workspace_member_moderation.timeout_until END,
  blocked_at = CASE WHEN excluded.blocked_at <> '' THEN excluded.blocked_at ELSE workspace_member_moderation.blocked_at END,
  moderation_note = excluded.moderation_note,
  moderation_by = excluded.moderation_by,
  moderation_at = excluded.moderation_at;

-- name: ClearMemberTimeout :exec
UPDATE workspace_member_moderation
SET timeout_until = NULL,
    moderation_by = sqlc.arg(moderation_by),
    moderation_at = sqlc.arg(moderation_at)
WHERE workspace_id = sqlc.arg(workspace_id)
  AND user_id = sqlc.arg(user_id);

-- name: ClearMemberBlocked :exec
UPDATE workspace_member_moderation
SET blocked_at = NULL,
    moderation_by = sqlc.arg(moderation_by),
    moderation_at = sqlc.arg(moderation_at)
WHERE workspace_id = sqlc.arg(workspace_id)
  AND user_id = sqlc.arg(user_id);

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

-- name: UploadHasDirectMessageAttachment :one
SELECT EXISTS (
  SELECT 1
  FROM message_attachments ma
  JOIN messages m ON m.id = ma.message_id
  WHERE ma.upload_id = sqlc.arg(upload_id)
    AND m.deleted_at IS NULL
    AND m.direct_conversation_id IS NOT NULL
    AND m.direct_conversation_id <> ''
) AS has_direct_message_attachment;

-- name: UploadHasOtherDirectMessageAttachment :one
SELECT EXISTS (
  SELECT 1
  FROM message_attachments ma
  JOIN messages m ON m.id = ma.message_id
  WHERE ma.upload_id = sqlc.arg(upload_id)
    AND ma.message_id <> sqlc.arg(message_id)
    AND m.deleted_at IS NULL
    AND m.direct_conversation_id IS NOT NULL
    AND m.direct_conversation_id <> ''
) AS has_other_direct_message_attachment;

-- name: InsertUploadQuotaReservation :exec
INSERT INTO upload_quota_reservations (id, workspace_id, owner_id, byte_size, created_at, expires_at)
VALUES (sqlc.arg(id), sqlc.arg(workspace_id), sqlc.arg(owner_id), sqlc.arg(byte_size), sqlc.arg(created_at), sqlc.arg(expires_at));

-- name: GetUploadQuotaReservation :one
SELECT id, workspace_id, owner_id, byte_size, created_at, expires_at
FROM upload_quota_reservations
WHERE id = sqlc.arg(id);

-- name: DeleteUploadQuotaReservation :execrows
DELETE FROM upload_quota_reservations
WHERE id = sqlc.arg(id) AND owner_id = sqlc.arg(owner_id);

-- name: DeleteExpiredUploadQuotaReservations :exec
DELETE FROM upload_quota_reservations
WHERE expires_at <= sqlc.arg(now);

-- name: AttachUpload :execrows
INSERT OR IGNORE INTO message_attachments (message_id, upload_id, created_at)
VALUES (sqlc.arg(message_id), sqlc.arg(upload_id), sqlc.arg(created_at));

-- name: GetChannelWorkspace :one
SELECT workspace_id
FROM channels
WHERE id = sqlc.arg(id);

-- name: GetDirectConversationWorkspace :one
SELECT workspace_id
FROM direct_conversations
WHERE id = sqlc.arg(id);

-- name: ChannelLastSeq :one
SELECT CAST(COALESCE(MAX(channel_seq), 0) AS INTEGER) AS last_seq
FROM messages
WHERE channel_id = CAST(sqlc.arg(channel_id) AS TEXT)
  AND parent_message_id IS NULL;

-- name: ChannelNextSeq :one
SELECT CAST(COALESCE(MAX(channel_seq), 0) + 1 AS INTEGER) AS next_seq
FROM messages
WHERE channel_id = CAST(sqlc.arg(channel_id) AS TEXT)
  AND parent_message_id IS NULL;

-- name: DirectLastSeq :one
SELECT CAST(COALESCE(MAX(channel_seq), 0) AS INTEGER) AS last_seq
FROM messages
WHERE direct_conversation_id = CAST(sqlc.arg(conversation_id) AS TEXT)
  AND parent_message_id IS NULL;

-- name: DirectNextSeq :one
SELECT CAST(COALESCE(MAX(channel_seq), 0) + 1 AS INTEGER) AS next_seq
FROM messages
WHERE direct_conversation_id = CAST(sqlc.arg(conversation_id) AS TEXT);

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

-- name: UpsertChannelRead :execrows
INSERT INTO channel_reads (channel_id, user_id, last_read_seq, last_read_at)
VALUES (sqlc.arg(channel_id), sqlc.arg(user_id), sqlc.arg(last_read_seq), sqlc.arg(last_read_at))
ON CONFLICT(channel_id, user_id) DO UPDATE SET
  last_read_seq = excluded.last_read_seq,
  last_read_at = excluded.last_read_at
WHERE excluded.last_read_seq > channel_reads.last_read_seq;

-- name: UpsertDirectRead :execrows
INSERT INTO direct_reads (conversation_id, user_id, last_read_seq, last_read_at)
VALUES (sqlc.arg(conversation_id), sqlc.arg(user_id), sqlc.arg(last_read_seq), sqlc.arg(last_read_at))
ON CONFLICT(conversation_id, user_id) DO UPDATE SET
  last_read_seq = excluded.last_read_seq,
  last_read_at = excluded.last_read_at
WHERE excluded.last_read_seq > direct_reads.last_read_seq;

-- name: ListDirectConversations :many
SELECT dc.id, COALESCE(dc.route_id, '') AS route_id, dc.workspace_id, dc.created_at,
       CAST(COALESCE((SELECT MAX(channel_seq) FROM messages WHERE direct_conversation_id = dc.id AND parent_message_id IS NULL), 0) AS INTEGER) AS last_seq,
       CAST(COALESCE((SELECT dr.last_read_seq FROM direct_reads dr WHERE dr.conversation_id = dc.id AND dr.user_id = sqlc.arg(reader_user_id)), 0) AS INTEGER) AS last_read_seq,
       CAST(COALESCE((
         SELECT COUNT(*)
         FROM messages m
         WHERE m.direct_conversation_id = dc.id
           AND m.parent_message_id IS NULL
           AND m.author_id <> sqlc.arg(reader_user_id)
           AND m.channel_seq > COALESCE((SELECT dr2.last_read_seq FROM direct_reads dr2 WHERE dr2.conversation_id = dc.id AND dr2.user_id = sqlc.arg(reader_user_id)), 0)
       ), 0) AS INTEGER) AS unread_count
FROM direct_conversations dc
JOIN direct_conversation_members dcm ON dcm.conversation_id = dc.id
WHERE dc.workspace_id = sqlc.arg(workspace_id)
  AND dcm.user_id = sqlc.arg(reader_user_id)
  AND NOT EXISTS (
    SELECT 1
    FROM direct_conversation_hidden dch
    WHERE dch.conversation_id = dc.id
      AND dch.user_id = sqlc.arg(reader_user_id)
  )
ORDER BY dc.created_at;

-- name: GetDirectConversation :one
SELECT dc.id, COALESCE(dc.route_id, '') AS route_id, dc.workspace_id, dc.created_at,
       CAST(COALESCE((SELECT MAX(channel_seq) FROM messages WHERE direct_conversation_id = dc.id AND parent_message_id IS NULL), 0) AS INTEGER) AS last_seq,
       CAST(COALESCE((SELECT dr.last_read_seq FROM direct_reads dr WHERE dr.conversation_id = dc.id AND dr.user_id = sqlc.arg(reader_user_id)), 0) AS INTEGER) AS last_read_seq,
       CAST(COALESCE((
         SELECT COUNT(*)
         FROM messages m
         WHERE m.direct_conversation_id = dc.id
           AND m.parent_message_id IS NULL
           AND m.author_id <> sqlc.arg(reader_user_id)
           AND m.channel_seq > COALESCE((SELECT dr2.last_read_seq FROM direct_reads dr2 WHERE dr2.conversation_id = dc.id AND dr2.user_id = sqlc.arg(reader_user_id)), 0)
       ), 0) AS INTEGER) AS unread_count
FROM direct_conversations dc
JOIN direct_conversation_members dcm ON dcm.conversation_id = dc.id
JOIN workspace_members wm ON wm.workspace_id = dc.workspace_id AND wm.user_id = dcm.user_id
WHERE dc.id = sqlc.arg(conversation_id)
  AND dcm.user_id = sqlc.arg(reader_user_id);

-- name: FindOneToOneDirectConversation :one
SELECT dc.id, COALESCE(dc.route_id, '') AS route_id, dc.workspace_id, dc.created_at
FROM direct_conversations dc
WHERE dc.workspace_id = sqlc.arg(workspace_id)
  AND EXISTS (
    SELECT 1 FROM direct_conversation_members first_member
    WHERE first_member.conversation_id = dc.id
      AND first_member.user_id = sqlc.arg(first_user_id)
  )
  AND EXISTS (
    SELECT 1 FROM direct_conversation_members second_member
    WHERE second_member.conversation_id = dc.id
      AND second_member.user_id = sqlc.arg(second_user_id)
  )
  AND (
    SELECT COUNT(*) FROM direct_conversation_members member_count
    WHERE member_count.conversation_id = dc.id
  ) = 2
ORDER BY dc.created_at, dc.id
LIMIT 1;

-- name: GetDirectConversationByMemberSetKey :one
SELECT id, COALESCE(route_id, '') AS route_id, workspace_id, created_at
FROM direct_conversations
WHERE workspace_id = sqlc.arg(workspace_id)
  AND member_set_key = sqlc.arg(member_set_key);

-- name: InsertDirectConversation :execrows
INSERT INTO direct_conversations (id, route_id, workspace_id, created_at, member_set_key)
VALUES (sqlc.arg(id), sqlc.arg(route_id), sqlc.arg(workspace_id), sqlc.arg(created_at), sqlc.arg(member_set_key))
ON CONFLICT DO NOTHING;

-- name: InsertDirectConversationMember :exec
INSERT INTO direct_conversation_members (conversation_id, user_id, created_at)
VALUES (sqlc.arg(conversation_id), sqlc.arg(user_id), sqlc.arg(created_at));

-- name: HideDirectConversation :exec
INSERT INTO direct_conversation_hidden (conversation_id, user_id, hidden_at)
VALUES (sqlc.arg(conversation_id), sqlc.arg(user_id), sqlc.arg(hidden_at))
ON CONFLICT(conversation_id, user_id) DO UPDATE SET
  hidden_at = excluded.hidden_at;

-- name: UnhideDirectConversation :exec
DELETE FROM direct_conversation_hidden
WHERE conversation_id = sqlc.arg(conversation_id)
  AND user_id = sqlc.arg(user_id);

-- name: UnhideDirectConversationForMembers :exec
DELETE FROM direct_conversation_hidden
WHERE conversation_id = sqlc.arg(conversation_id);

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
INSERT INTO messages (id, workspace_id, channel_id, direct_conversation_id, author_id, parent_message_id, thread_root_id, topic_id, channel_seq, thread_seq, body, body_format, created_at, quoted_message_id, quoted_body_snapshot, quoted_author_id, client_nonce)
VALUES (sqlc.arg(id), sqlc.arg(workspace_id), sqlc.arg(channel_id), NULL, sqlc.arg(author_id), NULL, sqlc.arg(thread_root_id), sqlc.arg(topic_id), sqlc.arg(channel_seq), NULL, sqlc.arg(body), 'markdown', sqlc.arg(created_at), sqlc.arg(quoted_message_id), sqlc.arg(quoted_body_snapshot), sqlc.arg(quoted_author_id), sqlc.arg(client_nonce));

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

-- name: ListThreadStates :many
SELECT root_message_id, reply_count, last_reply_at, last_reply_author_ids_json
FROM thread_state
WHERE root_message_id IN (sqlc.slice(root_message_ids));

-- name: ThreadNextSeq :one
SELECT CAST(COALESCE(MAX(thread_seq), 0) + 1 AS INTEGER) AS next_seq
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

-- name: UpdateMessageBody :execrows
UPDATE messages
SET body = sqlc.arg(body),
    edited_at = sqlc.arg(edited_at)
WHERE id = sqlc.arg(id)
  AND deleted_at IS NULL;

-- name: DeleteMessageBody :execrows
UPDATE messages
SET body = '',
    deleted_at = sqlc.arg(deleted_at)
WHERE id = sqlc.arg(id)
  AND deleted_at IS NULL;

-- name: AddReaction :execrows
INSERT OR IGNORE INTO reactions (message_id, user_id, emoji, created_at)
VALUES (sqlc.arg(message_id), sqlc.arg(user_id), sqlc.arg(emoji), sqlc.arg(created_at));

-- name: RemoveReaction :execrows
DELETE FROM reactions
WHERE message_id = sqlc.arg(message_id)
  AND user_id = sqlc.arg(user_id)
  AND emoji = sqlc.arg(emoji);

-- name: ListEventsAfter :many
SELECT e.id, e.cursor, e.workspace_id, COALESCE(e.channel_id, '') AS channel_id, e.type, e.seq, e.payload_json, e.created_at
FROM events e
JOIN workspace_members viewer ON viewer.workspace_id = e.workspace_id AND viewer.user_id = sqlc.arg(user_id)
LEFT JOIN channels event_channel ON event_channel.id = e.channel_id AND event_channel.workspace_id = e.workspace_id
WHERE e.workspace_id = sqlc.arg(workspace_id)
  AND e.cursor > sqlc.arg(cursor)
  AND (
    e.is_private = 0
    OR EXISTS (SELECT 1 FROM event_recipients er WHERE er.event_id = e.id AND er.user_id = sqlc.arg(user_id))
  )
  AND (
    e.channel_id IS NULL
    OR viewer.role <> 'guest'
    OR event_channel.name = 'guest'
  )
  AND (
    COALESCE(
      json_extract(e.payload_json, '$.direct_conversation_id'),
      (
        SELECT m.direct_conversation_id
        FROM messages m
        WHERE m.workspace_id = e.workspace_id
          AND m.id IN (
            COALESCE(json_extract(e.payload_json, '$.message_id'), ''),
            COALESCE(json_extract(e.payload_json, '$.root_message_id'), '')
          )
          AND m.direct_conversation_id IS NOT NULL
          AND m.direct_conversation_id <> ''
        LIMIT 1
      ),
      ''
    ) = ''
    OR (
      viewer.role <> 'guest'
      AND EXISTS (
        SELECT 1
        FROM direct_conversation_members dcm
        JOIN direct_conversations dc ON dc.id = dcm.conversation_id
        WHERE dc.workspace_id = e.workspace_id
          AND dcm.conversation_id = COALESCE(
            json_extract(e.payload_json, '$.direct_conversation_id'),
            (
              SELECT m.direct_conversation_id
              FROM messages m
              WHERE m.workspace_id = e.workspace_id
                AND m.id IN (
                  COALESCE(json_extract(e.payload_json, '$.message_id'), ''),
                  COALESCE(json_extract(e.payload_json, '$.root_message_id'), '')
                )
                AND m.direct_conversation_id IS NOT NULL
                AND m.direct_conversation_id <> ''
              LIMIT 1
            )
          )
          AND dcm.user_id = sqlc.arg(user_id)
      )
    )
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
  AND (CAST(sqlc.arg(before) AS TEXT) = '' OR julianday(e.created_at) < julianday(CAST(sqlc.arg(before) AS TEXT)))
  AND e.id NOT IN (
    SELECT kept.id
    FROM events AS kept
    WHERE kept.workspace_id = sqlc.arg(workspace_id_arg)
    ORDER BY kept.cursor DESC
    LIMIT sqlc.arg(keep_latest)
  );
