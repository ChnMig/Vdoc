ALTER TABLE audit_logs DROP CONSTRAINT IF EXISTS audit_logs_actor_type_check;
ALTER TABLE audit_logs
  ADD CONSTRAINT audit_logs_actor_type_check CHECK (actor_type IN (1, 2, 3, 4));

CREATE TABLE IF NOT EXISTS document_shares (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id uuid NOT NULL REFERENCES projects(id),
  document_id uuid NOT NULL REFERENCES documents(id),
  branch_id uuid NOT NULL REFERENCES document_branches(id),
  token_hash text NOT NULL,
  token_ciphertext bytea NOT NULL,
  cipher_kid text NOT NULL,
  password_verifier text,
  version_scope smallint NOT NULL CONSTRAINT document_shares_version_scope_check CHECK (version_scope IN (1, 2)),
  status smallint NOT NULL DEFAULT 1 CONSTRAINT document_shares_status_check CHECK (status IN (1, 2)),
  expires_at timestamptz,
  created_by uuid NOT NULL REFERENCES users(id),
  revoked_by uuid REFERENCES users(id),
  revoked_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT document_shares_revoked_fields_check CHECK (
    (status = 1 AND revoked_by IS NULL AND revoked_at IS NULL) OR
    (status = 2 AND revoked_by IS NOT NULL AND revoked_at IS NOT NULL)
  )
);
CREATE UNIQUE INDEX IF NOT EXISTS document_shares_token_hash_uidx ON document_shares (token_hash);
CREATE INDEX IF NOT EXISTS document_shares_document_branch_idx ON document_shares (document_id, branch_id, created_at DESC);
CREATE INDEX IF NOT EXISTS document_shares_status_expiry_idx ON document_shares (status, expires_at);
