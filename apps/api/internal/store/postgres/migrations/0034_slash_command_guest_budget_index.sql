CREATE INDEX idx_slash_command_invocations_guest_budget
  ON slash_command_invocations(workspace_id, user_id, created_at);
