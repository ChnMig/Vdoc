CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS schema_migrations (
  version text PRIMARY KEY,
  name text NOT NULL,
  applied_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS users (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  email text NOT NULL,
  password_hash text NOT NULL,
  display_name text NOT NULL,
  is_super_admin boolean NOT NULL DEFAULT false,
  status smallint NOT NULL DEFAULT 1 CONSTRAINT users_status_check CHECK (status IN (1, 2)),
  last_login_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);
CREATE UNIQUE INDEX IF NOT EXISTS users_email_active_uidx ON users (lower(email)) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS users_status_idx ON users (status);
CREATE INDEX IF NOT EXISTS users_is_super_admin_idx ON users (is_super_admin);

CREATE TABLE IF NOT EXISTS teams (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name text NOT NULL,
  slug text NOT NULL,
  description text,
  created_by uuid NOT NULL REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);
CREATE UNIQUE INDEX IF NOT EXISTS teams_slug_active_uidx ON teams (lower(slug)) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS teams_created_by_idx ON teams (created_by);

CREATE TABLE IF NOT EXISTS projects (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  team_id uuid NOT NULL REFERENCES teams(id),
  name text NOT NULL,
  slug text NOT NULL,
  description text,
  status smallint NOT NULL DEFAULT 1 CONSTRAINT projects_status_check CHECK (status IN (1, 2)),
  created_by uuid NOT NULL REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);
CREATE UNIQUE INDEX IF NOT EXISTS projects_team_slug_active_uidx ON projects (team_id, lower(slug)) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS projects_team_status_idx ON projects (team_id, status);
CREATE INDEX IF NOT EXISTS projects_created_by_idx ON projects (created_by);

CREATE TABLE IF NOT EXISTS project_members (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id uuid NOT NULL REFERENCES projects(id),
  user_id uuid NOT NULL REFERENCES users(id),
  role smallint NOT NULL CONSTRAINT project_members_role_check CHECK (role IN (1, 2, 3)),
  status smallint NOT NULL DEFAULT 1 CONSTRAINT project_members_status_check CHECK (status IN (1, 2)),
  added_by uuid NOT NULL REFERENCES users(id),
  added_at timestamptz NOT NULL DEFAULT now(),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);
CREATE UNIQUE INDEX IF NOT EXISTS project_members_project_user_active_uidx ON project_members (project_id, user_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS project_members_user_status_idx ON project_members (user_id, status);
CREATE INDEX IF NOT EXISTS project_members_project_role_idx ON project_members (project_id, role);

CREATE TABLE IF NOT EXISTS documents (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id uuid NOT NULL REFERENCES projects(id),
  name text NOT NULL,
  document_type smallint NOT NULL CONSTRAINT documents_document_type_check CHECK (document_type IN (1, 2)),
  relative_path text NOT NULL,
  description text,
  status smallint NOT NULL DEFAULT 1 CONSTRAINT documents_status_check CHECK (status IN (1, 2)),
  created_by uuid NOT NULL REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);
CREATE UNIQUE INDEX IF NOT EXISTS documents_project_name_active_uidx ON documents (project_id, lower(name)) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS documents_project_relative_path_active_uidx ON documents (project_id, lower(relative_path)) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS documents_project_status_idx ON documents (project_id, status);
CREATE INDEX IF NOT EXISTS documents_project_type_idx ON documents (project_id, document_type);

CREATE TABLE IF NOT EXISTS document_branches (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  document_id uuid NOT NULL REFERENCES documents(id),
  name text NOT NULL,
  kind smallint NOT NULL CONSTRAINT document_branches_kind_check CHECK (kind IN (1, 2)),
  description text,
  is_default boolean NOT NULL DEFAULT false,
  is_protected boolean NOT NULL DEFAULT false,
  status smallint NOT NULL DEFAULT 1 CONSTRAINT document_branches_status_check CHECK (status IN (1, 2)),
  created_by uuid NOT NULL REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz,
  CONSTRAINT document_branches_feature_name_check CHECK (kind <> 2 OR name LIKE 'feature/%')
);
CREATE UNIQUE INDEX IF NOT EXISTS document_branches_document_name_uidx ON document_branches (document_id, name);
CREATE UNIQUE INDEX IF NOT EXISTS document_branches_default_uidx ON document_branches (document_id) WHERE is_default = true AND deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS document_branches_document_status_idx ON document_branches (document_id, status);
CREATE INDEX IF NOT EXISTS document_branches_document_protected_idx ON document_branches (document_id, is_protected);

CREATE TABLE IF NOT EXISTS mcp_tokens (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id),
  name text NOT NULL,
  token_hash text NOT NULL,
  token_ciphertext bytea NOT NULL,
  cipher_kid text NOT NULL,
  scopes smallint[] NOT NULL DEFAULT '{}'::smallint[] CONSTRAINT mcp_tokens_scopes_check CHECK (scopes <@ ARRAY[1,2,3,4]::smallint[]),
  status smallint NOT NULL DEFAULT 1 CONSTRAINT mcp_tokens_status_check CHECK (status IN (1, 2, 3)),
  expires_at timestamptz,
  last_used_at timestamptz,
  revoked_at timestamptz,
  revoked_by uuid REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz,
  CONSTRAINT mcp_tokens_revoked_fields_check CHECK (status <> 2 OR revoked_at IS NOT NULL)
);
CREATE UNIQUE INDEX IF NOT EXISTS mcp_tokens_hash_uidx ON mcp_tokens (token_hash);
CREATE INDEX IF NOT EXISTS mcp_tokens_user_status_idx ON mcp_tokens (user_id, status);
CREATE INDEX IF NOT EXISTS mcp_tokens_expires_at_idx ON mcp_tokens (expires_at);
CREATE INDEX IF NOT EXISTS mcp_tokens_last_used_at_idx ON mcp_tokens (last_used_at DESC);

CREATE TABLE IF NOT EXISTS document_drafts (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id uuid NOT NULL REFERENCES projects(id),
  document_id uuid NOT NULL REFERENCES documents(id),
  branch_id uuid NOT NULL REFERENCES document_branches(id),
  version_name text NOT NULL,
  relative_path text NOT NULL,
  status smallint NOT NULL DEFAULT 1 CONSTRAINT document_drafts_status_check CHECK (status IN (1, 2, 3, 4, 5)),
  document_format smallint NOT NULL CONSTRAINT document_drafts_document_format_check CHECK (document_format IN (1, 2, 3)),
  raw_schema_object_key text NOT NULL,
  normalized_schema_object_key text NOT NULL,
  stable_schema_object_key text,
  raw_schema_hash text NOT NULL,
  normalized_schema_hash text NOT NULL,
  stable_schema_hash text,
  schema_size_bytes bigint NOT NULL,
  schema_metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  changelog text,
  source_git_commit_id text,
  source_type smallint NOT NULL DEFAULT 1 CONSTRAINT document_drafts_source_type_check CHECK (source_type IN (1, 2, 3, 4)),
  source_branch_id uuid REFERENCES document_branches(id),
  source_version_id uuid,
  base_version_id uuid,
  diff_preview_json jsonb,
  diff_preview_object_key text,
  review_comment text,
  created_by_actor_type smallint NOT NULL CONSTRAINT document_drafts_actor_type_check CHECK (created_by_actor_type IN (1, 2, 3)),
  created_by_user_id uuid NOT NULL REFERENCES users(id),
  created_by_token_id uuid REFERENCES mcp_tokens(id),
  submitted_at timestamptz,
  reviewed_by uuid REFERENCES users(id),
  reviewed_at timestamptz,
  published_version_id uuid,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz,
  CONSTRAINT document_drafts_promote_fields_check CHECK (source_type <> 3 OR (source_branch_id IS NOT NULL AND source_version_id IS NOT NULL))
);
CREATE UNIQUE INDEX IF NOT EXISTS document_drafts_active_version_uidx ON document_drafts (document_id, branch_id, version_name) WHERE status IN (1, 2, 3) AND deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS document_drafts_document_branch_status_idx ON document_drafts (document_id, branch_id, status);
CREATE INDEX IF NOT EXISTS document_drafts_project_path_idx ON document_drafts (project_id, relative_path);
CREATE INDEX IF NOT EXISTS document_drafts_created_by_status_idx ON document_drafts (created_by_user_id, status);
CREATE INDEX IF NOT EXISTS document_drafts_base_version_idx ON document_drafts (base_version_id);

CREATE TABLE IF NOT EXISTS document_versions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id uuid NOT NULL REFERENCES projects(id),
  document_id uuid NOT NULL REFERENCES documents(id),
  branch_id uuid NOT NULL REFERENCES document_branches(id),
  version_name text NOT NULL,
  version_no integer NOT NULL,
  relative_path text NOT NULL,
  status smallint NOT NULL DEFAULT 1 CONSTRAINT document_versions_status_check CHECK (status = 1),
  source_draft_id uuid NOT NULL REFERENCES document_drafts(id),
  source_type smallint NOT NULL DEFAULT 1 CONSTRAINT document_versions_source_type_check CHECK (source_type IN (1, 2, 3, 4)),
  source_branch_id uuid REFERENCES document_branches(id),
  source_version_id uuid REFERENCES document_versions(id),
  base_version_id uuid REFERENCES document_versions(id),
  document_format smallint NOT NULL CONSTRAINT document_versions_document_format_check CHECK (document_format IN (1, 2, 3)),
  raw_schema_object_key text NOT NULL,
  normalized_schema_object_key text NOT NULL,
  stable_schema_object_key text,
  raw_schema_hash text NOT NULL,
  normalized_schema_hash text NOT NULL,
  stable_schema_hash text,
  schema_size_bytes bigint NOT NULL,
  schema_metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  changelog text,
  source_git_commit_id text,
  endpoint_count integer NOT NULL DEFAULT 0,
  published_by uuid NOT NULL REFERENCES users(id),
  published_at timestamptz NOT NULL DEFAULT now(),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT document_versions_source_draft_uidx UNIQUE (source_draft_id)
);
CREATE UNIQUE INDEX IF NOT EXISTS document_versions_document_branch_version_name_uidx ON document_versions (document_id, branch_id, version_name);
CREATE UNIQUE INDEX IF NOT EXISTS document_versions_document_branch_version_no_uidx ON document_versions (document_id, branch_id, version_no);
CREATE INDEX IF NOT EXISTS document_versions_hash_idx ON document_versions (document_id, branch_id, normalized_schema_hash);
CREATE INDEX IF NOT EXISTS document_versions_published_at_idx ON document_versions (document_id, branch_id, published_at DESC);
CREATE INDEX IF NOT EXISTS document_versions_project_path_idx ON document_versions (project_id, relative_path);

ALTER TABLE document_drafts
  ADD CONSTRAINT document_drafts_source_version_fk FOREIGN KEY (source_version_id) REFERENCES document_versions(id),
  ADD CONSTRAINT document_drafts_base_version_fk FOREIGN KEY (base_version_id) REFERENCES document_versions(id),
  ADD CONSTRAINT document_drafts_published_version_fk FOREIGN KEY (published_version_id) REFERENCES document_versions(id);

CREATE TABLE IF NOT EXISTS api_endpoints (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  document_version_id uuid NOT NULL REFERENCES document_versions(id),
  document_id uuid NOT NULL REFERENCES documents(id),
  branch_id uuid NOT NULL REFERENCES document_branches(id),
  method smallint NOT NULL CONSTRAINT api_endpoints_method_check CHECK (method IN (1, 2, 3, 4, 5, 6, 7, 8)),
  path text NOT NULL,
  operation_id text,
  summary text,
  description text,
  tags text[] NOT NULL DEFAULT '{}'::text[],
  deprecated boolean NOT NULL DEFAULT false,
  request_hash text NOT NULL,
  response_hash text NOT NULL,
  security_hash text,
  endpoint_hash text NOT NULL,
  sort_order integer NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT api_endpoints_version_method_path_uidx UNIQUE (document_version_id, method, path)
);
CREATE INDEX IF NOT EXISTS api_endpoints_version_sort_idx ON api_endpoints (document_version_id, sort_order);
CREATE INDEX IF NOT EXISTS api_endpoints_path_idx ON api_endpoints (document_id, branch_id, method, path);
CREATE INDEX IF NOT EXISTS api_endpoints_operation_idx ON api_endpoints (document_version_id, operation_id);
CREATE INDEX IF NOT EXISTS api_endpoints_tags_gidx ON api_endpoints USING gin (tags);
CREATE INDEX IF NOT EXISTS api_endpoints_hash_idx ON api_endpoints (document_version_id, endpoint_hash);

CREATE TABLE IF NOT EXISTS api_endpoint_details (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  endpoint_id uuid NOT NULL REFERENCES api_endpoints(id),
  parameters_json jsonb NOT NULL DEFAULT '[]'::jsonb,
  request_body_json jsonb,
  responses_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  security_json jsonb,
  servers_json jsonb,
  normalized_operation_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  schema_refs_json jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT api_endpoint_details_endpoint_uidx UNIQUE (endpoint_id)
);
CREATE INDEX IF NOT EXISTS api_endpoint_details_normalized_operation_gidx ON api_endpoint_details USING gin (normalized_operation_json);

CREATE TABLE IF NOT EXISTS document_version_diffs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  document_id uuid NOT NULL REFERENCES documents(id),
  from_branch_id uuid NOT NULL REFERENCES document_branches(id),
  to_branch_id uuid NOT NULL REFERENCES document_branches(id),
  from_version_id uuid NOT NULL REFERENCES document_versions(id),
  to_version_id uuid NOT NULL REFERENCES document_versions(id),
  diff_status smallint NOT NULL DEFAULT 1 CONSTRAINT document_version_diffs_status_check CHECK (diff_status IN (1, 2, 3, 4)),
  diff_object_key text,
  diff_hash text,
  diff_summary_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  breaking_changes_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  added_count integer NOT NULL DEFAULT 0,
  modified_count integer NOT NULL DEFAULT 0,
  removed_count integer NOT NULL DEFAULT 0,
  breaking_count integer NOT NULL DEFAULT 0,
  summary_text text,
  error_message text,
  generated_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT document_version_diffs_versions_different_check CHECK (from_version_id <> to_version_id),
  CONSTRAINT document_version_diffs_versions_uidx UNIQUE (from_version_id, to_version_id)
);
CREATE INDEX IF NOT EXISTS document_version_diffs_document_to_branch_created_idx ON document_version_diffs (document_id, to_branch_id, created_at DESC);
CREATE INDEX IF NOT EXISTS document_version_diffs_status_idx ON document_version_diffs (diff_status);

CREATE TABLE IF NOT EXISTS document_diff_items (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  diff_id uuid NOT NULL REFERENCES document_version_diffs(id),
  endpoint_id uuid REFERENCES api_endpoints(id),
  change_type smallint NOT NULL,
  severity smallint NOT NULL CONSTRAINT document_diff_items_severity_check CHECK (severity IN (1, 2, 3)),
  method smallint CONSTRAINT document_diff_items_method_check CHECK (method IS NULL OR method IN (1, 2, 3, 4, 5, 6, 7, 8)),
  path text,
  operation_id text,
  location text,
  old_value jsonb,
  new_value jsonb,
  message text NOT NULL,
  frontend_impact text,
  is_breaking boolean NOT NULL DEFAULT false,
  sort_order integer NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT document_diff_items_breaking_consistency_check CHECK (severity <> 3 OR is_breaking = true)
);
CREATE INDEX IF NOT EXISTS document_diff_items_diff_sort_idx ON document_diff_items (diff_id, sort_order);
CREATE INDEX IF NOT EXISTS document_diff_items_diff_severity_idx ON document_diff_items (diff_id, severity);
CREATE INDEX IF NOT EXISTS document_diff_items_endpoint_idx ON document_diff_items (diff_id, method, path);
CREATE INDEX IF NOT EXISTS document_diff_items_change_type_idx ON document_diff_items (change_type);

CREATE TABLE IF NOT EXISTS ai_providers (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  scope text NOT NULL CHECK (scope IN ('system', 'project')),
  project_id uuid REFERENCES projects(id),
  name text NOT NULL,
  base_url text NOT NULL,
  model text NOT NULL,
  api_mode text NOT NULL CHECK (api_mode IN ('chat_completions', 'responses')),
  api_key_ciphertext bytea NOT NULL,
  cipher_kid text NOT NULL,
  api_key_last4 text,
  enabled boolean NOT NULL DEFAULT true,
  temperature double precision NOT NULL DEFAULT 0.2 CHECK (temperature >= 0 AND temperature <= 2),
  timeout_ms integer NOT NULL DEFAULT 30000 CHECK (timeout_ms >= 1000 AND timeout_ms <= 120000),
  max_output_tokens integer NOT NULL DEFAULT 1000 CHECK (max_output_tokens >= 1 AND max_output_tokens <= 32000),
  created_by uuid NOT NULL REFERENCES users(id),
  updated_by uuid NOT NULL REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT ai_providers_scope_project_check CHECK ((scope = 'system' AND project_id IS NULL) OR (scope = 'project' AND project_id IS NOT NULL))
);
CREATE UNIQUE INDEX IF NOT EXISTS ai_providers_system_uidx ON ai_providers (scope) WHERE scope = 'system';
CREATE UNIQUE INDEX IF NOT EXISTS ai_providers_project_uidx ON ai_providers (project_id) WHERE scope = 'project';

CREATE TABLE IF NOT EXISTS ai_prompt_overrides (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  scope text NOT NULL CHECK (scope IN ('system', 'project')),
  project_id uuid REFERENCES projects(id),
  prompt_key text NOT NULL,
  system_prompt text NOT NULL,
  user_prompt_template text NOT NULL,
  enabled boolean NOT NULL DEFAULT true,
  created_by uuid NOT NULL REFERENCES users(id),
  updated_by uuid NOT NULL REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT ai_prompt_scope_project_check CHECK ((scope = 'system' AND project_id IS NULL) OR (scope = 'project' AND project_id IS NOT NULL))
);
CREATE UNIQUE INDEX IF NOT EXISTS ai_prompt_system_uidx ON ai_prompt_overrides (prompt_key) WHERE scope = 'system';
CREATE UNIQUE INDEX IF NOT EXISTS ai_prompt_project_uidx ON ai_prompt_overrides (project_id, prompt_key) WHERE scope = 'project';

CREATE TABLE IF NOT EXISTS ai_summaries (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id uuid NOT NULL REFERENCES projects(id),
  document_id uuid NOT NULL REFERENCES documents(id),
  owner_type text NOT NULL CHECK (owner_type IN ('draft', 'version', 'diff')),
  owner_id uuid NOT NULL,
  prompt_key text NOT NULL,
  provider_id uuid REFERENCES ai_providers(id),
  status text NOT NULL CHECK (status IN ('skipped', 'succeeded', 'failed')),
  content text,
  error_message text,
  generated_by uuid NOT NULL REFERENCES users(id),
  generated_at timestamptz NOT NULL DEFAULT now(),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS ai_summaries_owner_uidx ON ai_summaries (project_id, document_id, owner_type, owner_id);

CREATE TABLE IF NOT EXISTS ai_chat_sessions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id uuid NOT NULL REFERENCES projects(id),
  document_id uuid REFERENCES documents(id),
  context_type text NOT NULL CHECK (context_type IN ('draft', 'version', 'diff')),
  context_id uuid NOT NULL,
  title text NOT NULL,
  created_by uuid NOT NULL REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS ai_chat_sessions_project_idx ON ai_chat_sessions (project_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS ai_chat_messages (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  session_id uuid NOT NULL REFERENCES ai_chat_sessions(id),
  role text NOT NULL CHECK (role IN ('user', 'assistant')),
  content text NOT NULL,
  provider_id uuid REFERENCES ai_providers(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS ai_chat_messages_session_idx ON ai_chat_messages (session_id, created_at);

CREATE TABLE IF NOT EXISTS audit_logs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  actor_type smallint NOT NULL CONSTRAINT audit_logs_actor_type_check CHECK (actor_type IN (1, 2, 3)),
  actor_user_id uuid REFERENCES users(id),
  actor_token_id uuid REFERENCES mcp_tokens(id),
  action text NOT NULL,
  resource_type text NOT NULL,
  resource_id uuid,
  project_id uuid REFERENCES projects(id),
  document_id uuid REFERENCES documents(id),
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  ip_address inet,
  user_agent text,
  request_id text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS audit_logs_actor_idx ON audit_logs (actor_type, actor_user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS audit_logs_resource_idx ON audit_logs (resource_type, resource_id, created_at DESC);
CREATE INDEX IF NOT EXISTS audit_logs_project_idx ON audit_logs (project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS audit_logs_document_idx ON audit_logs (document_id, created_at DESC);
CREATE INDEX IF NOT EXISTS audit_logs_action_idx ON audit_logs (action, created_at DESC);
CREATE INDEX IF NOT EXISTS audit_logs_request_idx ON audit_logs (request_id);

CREATE TABLE IF NOT EXISTS vdoc_schema_objects (
  object_key text PRIMARY KEY,
  kind text NOT NULL,
  owner_type text NOT NULL,
  owner_id uuid,
  sha256 text NOT NULL,
  content_type text NOT NULL DEFAULT 'application/json',
  size_bytes bigint NOT NULL,
  etag text,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS vdoc_schema_objects_owner_idx ON vdoc_schema_objects (owner_type, owner_id, kind);
CREATE INDEX IF NOT EXISTS vdoc_schema_objects_hash_idx ON vdoc_schema_objects (sha256);
