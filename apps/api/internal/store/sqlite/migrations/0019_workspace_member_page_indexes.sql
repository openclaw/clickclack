ALTER TABLE workspace_members ADD COLUMN role_sort INTEGER NOT NULL DEFAULT 9;
ALTER TABLE workspace_members ADD COLUMN sort_name TEXT NOT NULL DEFAULT '';
ALTER TABLE workspace_members ADD COLUMN sort_handle TEXT NOT NULL DEFAULT '';

UPDATE workspace_members
SET role_sort = CASE role
      WHEN 'owner' THEN 0
      WHEN 'moderator' THEN 1
      WHEN 'member' THEN 2
      WHEN 'bot' THEN 3
      WHEN 'guest' THEN 4
      ELSE 9
    END,
    sort_name = COALESCE(
      (SELECT lower(COALESCE(NULLIF(u.display_name, ''), NULLIF(u.handle, ''), u.id))
       FROM users u
       WHERE u.id = workspace_members.user_id),
      user_id
    ),
    sort_handle = COALESCE(
      (SELECT lower(COALESCE(NULLIF(u.handle, ''), u.id))
       FROM users u
       WHERE u.id = workspace_members.user_id),
      user_id
    );

CREATE INDEX IF NOT EXISTS idx_workspace_members_workspace_role_user
ON workspace_members(workspace_id, role, user_id);

CREATE INDEX IF NOT EXISTS idx_workspace_members_page
ON workspace_members(workspace_id, role_sort, sort_name, sort_handle, user_id);

CREATE INDEX IF NOT EXISTS idx_bot_tokens_workspace_bot_revoked
ON bot_tokens(workspace_id, bot_user_id, revoked_at);
