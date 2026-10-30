-- 不可变提交与可移动引用独立于旧草稿/版本，不重写现有文档或公开链接。
CREATE TABLE repository_records (
    project_id uuid NOT NULL REFERENCES projects(id),
    kind text NOT NULL,
    id text NOT NULL,
    generation bigint NOT NULL DEFAULT 1 CHECK (generation > 0),
    sequence bigserial NOT NULL,
    payload jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, kind, id),
    UNIQUE (sequence),
    CHECK (length(kind) BETWEEN 1 AND 80),
    CHECK (length(id) BETWEEN 1 AND 256)
);
CREATE INDEX repository_records_project_kind_sequence_idx ON repository_records(project_id, kind, sequence);
CREATE UNIQUE INDEX repository_branches_project_name_idx ON repository_records(project_id, (payload->>'name')) WHERE kind = 'branch';

CREATE FUNCTION protect_repository_history() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.kind IN ('commit', 'event', 'release', 'legacy_map', 'idempotency') THEN
        RAISE EXCEPTION 'repository history is immutable' USING ERRCODE = '23514';
    END IF;
    IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER repository_history_immutable BEFORE UPDATE OR DELETE ON repository_records FOR EACH ROW EXECUTE FUNCTION protect_repository_history();
