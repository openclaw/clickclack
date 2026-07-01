ALTER TABLE direct_conversations ADD COLUMN member_set_key TEXT;

CREATE UNIQUE INDEX idx_direct_conversations_workspace_member_set
ON direct_conversations(workspace_id, member_set_key)
WHERE member_set_key IS NOT NULL;
