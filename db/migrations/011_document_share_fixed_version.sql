ALTER TABLE document_shares
  ADD COLUMN pinned_version_id uuid CONSTRAINT document_shares_pinned_version_fk REFERENCES document_versions(id);

ALTER TABLE document_shares DROP CONSTRAINT document_shares_version_scope_check;
ALTER TABLE document_shares
  ADD CONSTRAINT document_shares_version_scope_check CHECK (version_scope IN (1, 2, 3)),
  ADD CONSTRAINT document_shares_pinned_version_scope_check CHECK (
    (version_scope IN (1, 2) AND pinned_version_id IS NULL) OR
    (version_scope = 3 AND pinned_version_id IS NOT NULL)
  );
