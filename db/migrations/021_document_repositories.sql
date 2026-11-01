-- 同一项目中，不同文档可以使用相同的分支名。
DROP INDEX repository_branches_project_name_idx;
CREATE UNIQUE INDEX repository_branches_document_name_idx
    ON repository_records(project_id, (COALESCE(payload->>'document_id', '')), (payload->>'name'))
    WHERE kind = 'branch';
CREATE INDEX repository_records_document_sequence_idx
    ON repository_records(project_id, kind, (payload->>'document_id'), sequence);

-- 已有发布的文档投影也不可重写；保留原发布 ID 供既有分享读取。
CREATE OR REPLACE FUNCTION protect_repository_history() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.kind IN ('commit', 'event', 'release', 'imported_release', 'legacy_map', 'idempotency') THEN
        RAISE EXCEPTION 'repository history is immutable' USING ERRCODE = '23514';
    END IF;
    IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END;
$$;
