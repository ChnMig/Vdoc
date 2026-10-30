CREATE TABLE IF NOT EXISTS repository_share_index (
  id uuid PRIMARY KEY,
  project_id uuid NOT NULL REFERENCES projects(id)
);
CREATE INDEX IF NOT EXISTS repository_share_project_idx ON repository_share_index(project_id);

-- 两种链接共享公开路由，跨表约束保证任何 id 只对应一种分享。
CREATE OR REPLACE FUNCTION vdoc_guard_share_identity() RETURNS trigger AS $$
BEGIN
  PERFORM pg_advisory_xact_lock(hashtextextended('vdoc-share:' || NEW.id::text, 0));
  IF TG_TABLE_NAME = 'repository_share_index' THEN
    IF EXISTS(SELECT 1 FROM document_shares WHERE id = NEW.id) THEN
      RAISE EXCEPTION 'share identity already exists' USING ERRCODE = '23505';
    END IF;
  ELSE
    IF EXISTS(SELECT 1 FROM repository_share_index WHERE id = NEW.id) THEN
      RAISE EXCEPTION 'share identity already exists' USING ERRCODE = '23505';
    END IF;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER repository_share_identity BEFORE INSERT OR UPDATE OF id ON repository_share_index
FOR EACH ROW EXECUTE FUNCTION vdoc_guard_share_identity();
CREATE TRIGGER document_share_identity BEFORE INSERT OR UPDATE OF id ON document_shares
FOR EACH ROW EXECUTE FUNCTION vdoc_guard_share_identity();
