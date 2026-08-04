DROP INDEX IF EXISTS idx_slash_command_invocations_guest_budget;

CREATE INDEX IF NOT EXISTS idx_slash_command_invocations_guest_budget
  ON slash_command_invocations(workspace_id, user_id, channel_id, created_at);
