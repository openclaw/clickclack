CREATE INDEX IF NOT EXISTS idx_workspace_members_workspace_role_user
ON workspace_members(workspace_id, role, user_id);
