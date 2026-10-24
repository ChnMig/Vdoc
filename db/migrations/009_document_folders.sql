CREATE TABLE IF NOT EXISTS document_folders (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id uuid NOT NULL REFERENCES projects(id),
  parent_id uuid,
  name text NOT NULL,
  path text NOT NULL,
  created_by uuid NOT NULL REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT document_folders_project_id_id_key UNIQUE (project_id, id),
  CONSTRAINT document_folders_parent_project_fk FOREIGN KEY (project_id, parent_id) REFERENCES document_folders(project_id, id),
  CONSTRAINT document_folders_parent_not_self_check CHECK (parent_id IS NULL OR parent_id <> id),
  CONSTRAINT document_folders_name_check CHECK (
    btrim(name) <> '' AND name NOT IN ('.', '..') AND
    position('/' IN name) = 0 AND position(chr(92) IN name) = 0 AND name !~ '[[:cntrl:]]'
  ),
  CONSTRAINT document_folders_path_check CHECK (
    path <> '' AND path NOT LIKE '/%' AND path NOT LIKE '%/' AND
    position(chr(92) IN path) = 0 AND path !~ '[[:cntrl:]]' AND
    path !~ '(^|/)(\.|\.\.|)(/|$)'
  )
);
CREATE UNIQUE INDEX IF NOT EXISTS document_folders_project_path_uidx ON document_folders (project_id, digest(lower(path), 'sha256'));
CREATE INDEX IF NOT EXISTS document_folders_parent_idx ON document_folders (project_id, parent_id);

ALTER TABLE documents ADD COLUMN IF NOT EXISTS folder_id uuid;
ALTER TABLE documents ADD CONSTRAINT documents_folder_project_fk FOREIGN KEY (project_id, folder_id) REFERENCES document_folders(project_id, id);
CREATE INDEX IF NOT EXISTS documents_folder_idx ON documents (project_id, folder_id) WHERE deleted_at IS NULL;

-- 旧文档路径只用于补全目录，不改写文档、草稿或版本的路径快照。
-- 逐层插入父目录，目录没有预设层级上限；大小写不同的旧前缀共用一个目录。
DO $$
DECLARE
  document_row record;
  path_segments text[];
  segment_index integer;
  parent_folder_id uuid;
  current_folder_id uuid;
  current_folder_path text;
  requested_folder_path text;
BEGIN
  FOR document_row IN SELECT id, project_id, relative_path, created_by, created_at, updated_at FROM documents ORDER BY created_at, id LOOP
    path_segments := string_to_array(document_row.relative_path, '/');
    IF cardinality(path_segments) < 2 OR document_row.relative_path LIKE '/%' OR
       position(chr(92) IN document_row.relative_path) > 0 OR document_row.relative_path ~ '[[:cntrl:]]' OR
       EXISTS (SELECT 1 FROM unnest(path_segments) AS segment WHERE btrim(segment) = '' OR segment IN ('.', '..')) THEN
      CONTINUE;
    END IF;

    parent_folder_id := NULL;
    current_folder_path := '';
    FOR segment_index IN 1..cardinality(path_segments) - 1 LOOP
      requested_folder_path := CASE WHEN current_folder_path = '' THEN path_segments[segment_index]
        ELSE current_folder_path || '/' || path_segments[segment_index] END;
      SELECT id, path INTO current_folder_id, current_folder_path
        FROM document_folders
        WHERE project_id = document_row.project_id AND
          digest(lower(path), 'sha256') = digest(lower(requested_folder_path), 'sha256') AND
          lower(path) = lower(requested_folder_path);
      IF NOT FOUND THEN
        current_folder_path := requested_folder_path;
        INSERT INTO document_folders(project_id, parent_id, name, path, created_by, created_at, updated_at)
          VALUES(document_row.project_id, parent_folder_id, path_segments[segment_index], current_folder_path,
            document_row.created_by, document_row.created_at, document_row.updated_at)
          RETURNING id INTO current_folder_id;
      END IF;
      parent_folder_id := current_folder_id;
    END LOOP;
    UPDATE documents SET folder_id = parent_folder_id WHERE id = document_row.id;
  END LOOP;
END $$;

-- 路径可能很深，避免普通 B-tree 文本索引的单行长度上限。
DROP INDEX IF EXISTS documents_project_relative_path_active_uidx;
CREATE UNIQUE INDEX documents_project_relative_path_active_uidx ON documents (project_id, digest(lower(relative_path), 'sha256')) WHERE deleted_at IS NULL;
DROP INDEX IF EXISTS document_drafts_project_path_idx;
CREATE INDEX document_drafts_project_path_idx ON document_drafts (project_id, digest(relative_path, 'sha256'));
DROP INDEX IF EXISTS document_versions_project_path_idx;
CREATE INDEX document_versions_project_path_idx ON document_versions (project_id, digest(relative_path, 'sha256'));
