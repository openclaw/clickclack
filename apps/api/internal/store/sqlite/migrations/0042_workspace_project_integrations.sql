CREATE TABLE workspace_project_integrations (
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  provider TEXT NOT NULL CHECK (provider = 'github'),
  user_id TEXT NOT NULL REFERENCES users(id),
  created_at TEXT NOT NULL,
  PRIMARY KEY (workspace_id, provider)
);

INSERT INTO workspace_project_integrations (workspace_id, provider, user_id, created_at)
SELECT p.workspace_id, 'github', p.integration_user_id, p.created_at
FROM projects p
WHERE NOT EXISTS (
  SELECT 1
  FROM projects earlier
  WHERE earlier.workspace_id = p.workspace_id
    AND (
      earlier.created_at < p.created_at
      OR (earlier.created_at = p.created_at AND earlier.id < p.id)
    )
);

DELETE FROM workspace_members
WHERE EXISTS (
  SELECT 1
  FROM users
  WHERE users.id = workspace_members.user_id
    AND users.kind = 'bot'
    AND users.owner_user_id IS NULL
    AND users.display_name = 'GitHub'
    AND users.handle = ''
)
AND EXISTS (
  SELECT 1
  FROM projects
  WHERE projects.workspace_id = workspace_members.workspace_id
    AND projects.integration_user_id = workspace_members.user_id
)
AND NOT EXISTS (
  SELECT 1
  FROM workspace_project_integrations integration
  WHERE integration.workspace_id = workspace_members.workspace_id
    AND integration.user_id = workspace_members.user_id
);

UPDATE projects
SET integration_user_id = (
  SELECT integration.user_id
  FROM workspace_project_integrations integration
  WHERE integration.workspace_id = projects.workspace_id
    AND integration.provider = 'github'
);
