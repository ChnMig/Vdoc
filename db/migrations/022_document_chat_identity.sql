-- 文档身份独立于内容存储。兼容旧 documents 和新的文档提交仓库，保留原聊天及审计外键。
CREATE TABLE document_identity_index (
  id uuid PRIMARY KEY,
  project_id uuid NOT NULL REFERENCES projects(id)
);
INSERT INTO document_identity_index(id, project_id)
  SELECT id, project_id FROM documents
  UNION
  SELECT id::uuid, project_id FROM repository_records WHERE kind = 'document';

CREATE FUNCTION maintain_document_identity() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  document_id uuid;
BEGIN
  IF TG_TABLE_NAME = 'repository_records' THEN
    IF NEW.kind <> 'document' THEN RETURN NEW; END IF;
  END IF;
  document_id := NEW.id::uuid;
  INSERT INTO document_identity_index(id, project_id) VALUES(document_id, NEW.project_id)
    ON CONFLICT(id) DO UPDATE SET project_id = EXCLUDED.project_id
      WHERE document_identity_index.project_id = EXCLUDED.project_id;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'document identity belongs to another project' USING ERRCODE = '23505';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER documents_identity BEFORE INSERT OR UPDATE OF id, project_id ON documents
  FOR EACH ROW EXECUTE FUNCTION maintain_document_identity();
CREATE TRIGGER repository_documents_identity BEFORE INSERT OR UPDATE OF id, project_id, kind ON repository_records
  FOR EACH ROW EXECUTE FUNCTION maintain_document_identity();

ALTER TABLE ai_chat_sessions DROP CONSTRAINT IF EXISTS ai_chat_sessions_document_id_fkey;
ALTER TABLE ai_chat_sessions ADD CONSTRAINT ai_chat_sessions_document_id_fkey
  FOREIGN KEY(document_id) REFERENCES document_identity_index(id);
ALTER TABLE audit_logs DROP CONSTRAINT IF EXISTS audit_logs_document_id_fkey;
ALTER TABLE audit_logs DROP CONSTRAINT IF EXISTS audit_logs_service_id_fkey;
ALTER TABLE audit_logs ADD CONSTRAINT audit_logs_document_id_fkey
  FOREIGN KEY(document_id) REFERENCES document_identity_index(id);
