ALTER TABLE documents DROP CONSTRAINT documents_document_type_check;
ALTER TABLE documents ADD CONSTRAINT documents_document_type_check CHECK (document_type IN (1, 2, 3));

ALTER TABLE document_drafts DROP CONSTRAINT document_drafts_document_format_check;
ALTER TABLE document_drafts ADD CONSTRAINT document_drafts_document_format_check CHECK (document_format IN (1, 2, 3, 4));

ALTER TABLE document_versions DROP CONSTRAINT document_versions_document_format_check;
ALTER TABLE document_versions ADD CONSTRAINT document_versions_document_format_check CHECK (document_format IN (1, 2, 3, 4));
