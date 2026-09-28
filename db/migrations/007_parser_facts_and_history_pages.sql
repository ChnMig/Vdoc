ALTER TABLE document_versions ADD COLUMN parser_version integer NOT NULL DEFAULT 0;
ALTER TABLE document_versions ADD COLUMN parsed_schema_hash text NOT NULL DEFAULT '';

CREATE INDEX document_drafts_history_page_idx ON document_drafts (document_id, created_at DESC, id DESC);
CREATE INDEX document_drafts_branch_page_idx ON document_drafts (document_id, branch_id, created_at DESC, id DESC);
CREATE INDEX document_diffs_history_page_idx ON document_version_diffs (document_id, created_at DESC, id DESC);

ALTER TABLE document_diff_items ADD COLUMN must_handle boolean NOT NULL DEFAULT false;
UPDATE document_diff_items SET must_handle = is_breaking;
