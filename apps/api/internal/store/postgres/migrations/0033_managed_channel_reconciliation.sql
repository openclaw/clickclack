ALTER TABLE channels ADD COLUMN external_provider TEXT;

CREATE UNIQUE INDEX idx_channels_managed_identity
  ON channels(workspace_id, external_provider, external_ref)
  WHERE external_provider IS NOT NULL AND external_ref IS NOT NULL;
