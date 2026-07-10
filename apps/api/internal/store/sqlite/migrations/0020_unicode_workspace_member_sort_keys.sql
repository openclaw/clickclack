UPDATE workspace_members
SET sort_name = COALESCE(
      (SELECT clickclack_lower(COALESCE(NULLIF(u.display_name, ''), NULLIF(u.handle, ''), u.id))
       FROM users u
       WHERE u.id = workspace_members.user_id),
      user_id
    ),
    sort_handle = COALESCE(
      (SELECT clickclack_lower(COALESCE(NULLIF(u.handle, ''), u.id))
       FROM users u
       WHERE u.id = workspace_members.user_id),
      user_id
    );
