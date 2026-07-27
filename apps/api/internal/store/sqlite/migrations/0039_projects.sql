CREATE TABLE projects (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  slug TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  channel_id TEXT NOT NULL UNIQUE REFERENCES channels(id) ON DELETE CASCADE,
  integration_user_id TEXT NOT NULL REFERENCES users(id),
  webhook_secret TEXT NOT NULL,
  created_by TEXT NOT NULL REFERENCES users(id),
  created_at TEXT NOT NULL,
  UNIQUE(workspace_id, slug)
);

CREATE TABLE project_repositories (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  provider TEXT NOT NULL CHECK (provider = 'github'),
  owner TEXT NOT NULL,
  name TEXT NOT NULL,
  full_name TEXT NOT NULL,
  url TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(project_id, provider, full_name)
);

CREATE INDEX idx_project_repositories_full_name
  ON project_repositories(provider, full_name);

CREATE TABLE project_members (
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role TEXT NOT NULL CHECK (role IN ('admin', 'member')),
  created_at TEXT NOT NULL,
  PRIMARY KEY (project_id, user_id)
);

CREATE TABLE github_deliveries (
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  delivery_id TEXT NOT NULL,
  event_type TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('processing', 'complete')),
  created_at TEXT NOT NULL,
  completed_at TEXT,
  PRIMARY KEY (project_id, delivery_id)
);

CREATE TABLE github_pull_request_threads (
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  repository_id TEXT NOT NULL REFERENCES project_repositories(id) ON DELETE CASCADE,
  pull_number INTEGER NOT NULL,
  root_message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (project_id, repository_id, pull_number)
);
