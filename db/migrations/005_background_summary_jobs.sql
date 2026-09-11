CREATE TABLE ai_summary_jobs (
    id uuid PRIMARY KEY,
    actor_id uuid NOT NULL REFERENCES users(id),
    project_id uuid NOT NULL REFERENCES projects(id),
    document_id uuid NOT NULL REFERENCES documents(id),
    owner_type text NOT NULL CHECK (owner_type IN ('draft', 'version', 'diff')),
    owner_id uuid NOT NULL,
    trigger text NOT NULL,
    request_id text NOT NULL DEFAULT '',
    attempts integer NOT NULL DEFAULT 0,
    lease_token text NOT NULL DEFAULT '',
    available_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ai_summary_jobs_available_idx ON ai_summary_jobs (available_at, created_at, id);
CREATE INDEX audit_logs_project_cursor_idx ON audit_logs (project_id, created_at DESC, id DESC);
CREATE INDEX document_versions_document_cursor_idx ON document_versions (document_id, published_at DESC, id ASC);
CREATE INDEX api_endpoints_version_page_idx ON api_endpoints (document_version_id, path, method, id);
