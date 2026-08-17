-- This migration runs before the frozen v0.1 migrations. It is a no-op for a
-- fresh database. For databases created by an older, checksum-less 001, it
-- makes the historical schema safe to replay against the current frozen 001.
DO $migration$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM schema_migrations
    WHERE version = '001' AND checksum IS NULL
  ) THEN
    RETURN;
  END IF;

  -- The first public 001 used api_services/api_contract_* names. Rename the
  -- graph in place so PostgreSQL preserves rows and updates foreign keys.
  IF to_regclass('public.documents') IS NULL
     AND to_regclass('public.api_services') IS NOT NULL THEN
    EXECUTE 'ALTER TABLE api_services RENAME TO documents';
    EXECUTE 'ALTER TABLE api_contract_branches RENAME TO document_branches';
    EXECUTE 'ALTER TABLE api_contract_drafts RENAME TO document_drafts';
    EXECUTE 'ALTER TABLE api_contract_versions RENAME TO document_versions';
    EXECUTE 'ALTER TABLE api_version_diffs RENAME TO document_version_diffs';
    EXECUTE 'ALTER TABLE api_diff_items RENAME TO document_diff_items';

    EXECUTE 'ALTER TABLE document_branches RENAME COLUMN service_id TO document_id';
    EXECUTE 'ALTER TABLE document_drafts RENAME COLUMN service_id TO document_id';
    EXECUTE 'ALTER TABLE document_drafts RENAME COLUMN schema_format TO document_format';
    EXECUTE 'ALTER TABLE document_versions RENAME COLUMN service_id TO document_id';
    EXECUTE 'ALTER TABLE document_versions RENAME COLUMN schema_format TO document_format';
    EXECUTE 'ALTER TABLE api_endpoints RENAME COLUMN contract_version_id TO document_version_id';
    EXECUTE 'ALTER TABLE api_endpoints RENAME COLUMN service_id TO document_id';
    EXECUTE 'ALTER TABLE document_version_diffs RENAME COLUMN service_id TO document_id';
    EXECUTE 'ALTER TABLE audit_logs RENAME COLUMN service_id TO document_id';

    EXECUTE 'ALTER TABLE documents ADD COLUMN document_type smallint';
    EXECUTE 'ALTER TABLE documents ADD COLUMN relative_path text';
    EXECUTE 'UPDATE documents SET document_type = 1, relative_path = ''legacy/'' || id::text || ''.openapi''';
    EXECUTE 'ALTER TABLE documents ALTER COLUMN document_type SET NOT NULL';
    EXECUTE 'ALTER TABLE documents ALTER COLUMN relative_path SET NOT NULL';
    EXECUTE 'ALTER TABLE documents ADD CONSTRAINT documents_document_type_check CHECK (document_type IN (1, 2))';

    EXECUTE 'ALTER TABLE document_drafts ADD COLUMN project_id uuid';
    EXECUTE 'ALTER TABLE document_drafts ADD COLUMN relative_path text';
    EXECUTE 'ALTER TABLE document_drafts ADD COLUMN stable_schema_object_key text';
    EXECUTE 'ALTER TABLE document_drafts ADD COLUMN stable_schema_hash text';
    EXECUTE 'UPDATE document_drafts d SET project_id = doc.project_id, relative_path = doc.relative_path FROM documents doc WHERE d.document_id = doc.id';
    EXECUTE 'ALTER TABLE document_drafts ALTER COLUMN project_id SET NOT NULL';
    EXECUTE 'ALTER TABLE document_drafts ALTER COLUMN relative_path SET NOT NULL';
    EXECUTE 'ALTER TABLE document_drafts ADD CONSTRAINT document_drafts_project_id_fkey FOREIGN KEY (project_id) REFERENCES projects(id)';
    EXECUTE 'ALTER TABLE document_drafts DROP CONSTRAINT IF EXISTS api_contract_drafts_schema_format_check';
    EXECUTE 'ALTER TABLE document_drafts DROP CONSTRAINT IF EXISTS api_contract_drafts_source_type_check';
    EXECUTE 'ALTER TABLE document_drafts ADD CONSTRAINT document_drafts_document_format_check CHECK (document_format IN (1, 2, 3))';
    EXECUTE 'ALTER TABLE document_drafts ADD CONSTRAINT document_drafts_source_type_check CHECK (source_type IN (1, 2, 3, 4))';

    EXECUTE 'ALTER TABLE document_versions ADD COLUMN project_id uuid';
    EXECUTE 'ALTER TABLE document_versions ADD COLUMN relative_path text';
    EXECUTE 'ALTER TABLE document_versions ADD COLUMN stable_schema_object_key text';
    EXECUTE 'ALTER TABLE document_versions ADD COLUMN stable_schema_hash text';
    EXECUTE 'UPDATE document_versions v SET project_id = doc.project_id, relative_path = doc.relative_path FROM documents doc WHERE v.document_id = doc.id';
    EXECUTE 'ALTER TABLE document_versions ALTER COLUMN project_id SET NOT NULL';
    EXECUTE 'ALTER TABLE document_versions ALTER COLUMN relative_path SET NOT NULL';
    EXECUTE 'ALTER TABLE document_versions ADD CONSTRAINT document_versions_project_id_fkey FOREIGN KEY (project_id) REFERENCES projects(id)';
    EXECUTE 'ALTER TABLE document_versions DROP CONSTRAINT IF EXISTS api_contract_versions_schema_format_check';
    EXECUTE 'ALTER TABLE document_versions DROP CONSTRAINT IF EXISTS api_contract_versions_source_type_check';
    EXECUTE 'ALTER TABLE document_versions ADD CONSTRAINT document_versions_document_format_check CHECK (document_format IN (1, 2, 3))';
    EXECUTE 'ALTER TABLE document_versions ADD CONSTRAINT document_versions_source_type_check CHECK (source_type IN (1, 2, 3, 4))';

    -- Remove superseded index names before the replay creates the canonical
    -- names. Constraint-backed indexes are intentionally retained.
    EXECUTE 'DROP INDEX IF EXISTS api_services_project_name_active_uidx';
    EXECUTE 'DROP INDEX IF EXISTS api_services_project_status_idx';
    EXECUTE 'DROP INDEX IF EXISTS api_contract_branches_service_name_uidx';
    EXECUTE 'DROP INDEX IF EXISTS api_contract_branches_default_uidx';
    EXECUTE 'DROP INDEX IF EXISTS api_contract_branches_service_status_idx';
    EXECUTE 'DROP INDEX IF EXISTS api_contract_branches_service_protected_idx';
    EXECUTE 'DROP INDEX IF EXISTS api_contract_drafts_active_version_uidx';
    EXECUTE 'DROP INDEX IF EXISTS api_contract_drafts_service_branch_status_idx';
    EXECUTE 'DROP INDEX IF EXISTS api_contract_drafts_created_by_status_idx';
    EXECUTE 'DROP INDEX IF EXISTS api_contract_drafts_base_version_idx';
    EXECUTE 'DROP INDEX IF EXISTS api_contract_versions_branch_version_name_uidx';
    EXECUTE 'DROP INDEX IF EXISTS api_contract_versions_branch_version_no_uidx';
    EXECUTE 'DROP INDEX IF EXISTS api_contract_versions_hash_idx';
    EXECUTE 'DROP INDEX IF EXISTS api_contract_versions_published_at_idx';
    EXECUTE 'DROP INDEX IF EXISTS api_version_diffs_service_to_branch_created_idx';
    EXECUTE 'DROP INDEX IF EXISTS api_version_diffs_status_idx';
    EXECUTE 'DROP INDEX IF EXISTS api_diff_items_diff_sort_idx';
    EXECUTE 'DROP INDEX IF EXISTS api_diff_items_diff_severity_idx';
    EXECUTE 'DROP INDEX IF EXISTS api_diff_items_endpoint_idx';
    EXECUTE 'DROP INDEX IF EXISTS api_diff_items_change_type_idx';
  END IF;

  -- Current 001 has one non-idempotent ALTER TABLE block. Remove either the
  -- old or current names before the migration runner replays checksum-less 001.
  IF to_regclass('public.document_drafts') IS NOT NULL THEN
    EXECUTE 'ALTER TABLE document_drafts DROP CONSTRAINT IF EXISTS api_contract_drafts_source_version_fk';
    EXECUTE 'ALTER TABLE document_drafts DROP CONSTRAINT IF EXISTS api_contract_drafts_base_version_fk';
    EXECUTE 'ALTER TABLE document_drafts DROP CONSTRAINT IF EXISTS api_contract_drafts_published_version_fk';
    EXECUTE 'ALTER TABLE document_drafts DROP CONSTRAINT IF EXISTS document_drafts_source_version_fk';
    EXECUTE 'ALTER TABLE document_drafts DROP CONSTRAINT IF EXISTS document_drafts_base_version_fk';
    EXECUTE 'ALTER TABLE document_drafts DROP CONSTRAINT IF EXISTS document_drafts_published_version_fk';
  END IF;

  IF to_regclass('public.mcp_tokens') IS NOT NULL THEN
    EXECUTE 'ALTER TABLE mcp_tokens DROP CONSTRAINT IF EXISTS mcp_tokens_scopes_check';
    EXECUTE 'ALTER TABLE mcp_tokens ADD CONSTRAINT mcp_tokens_scopes_check CHECK (scopes <@ ARRAY[1,2,3,4]::smallint[])';
  END IF;
END
$migration$;
