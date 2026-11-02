CREATE TABLE IF NOT EXISTS audit_retention_settings (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    retention_days integer NOT NULL DEFAULT 30 CHECK (retention_days BETWEEN 0 AND 36500),
    purge_before timestamptz NOT NULL DEFAULT '-infinity',
    updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO audit_retention_settings(singleton) VALUES (true) ON CONFLICT DO NOTHING;

-- Calendar days and the daily 01:00 job always use Beijing time, regardless of
-- the database connection or host timezone. Zero discards history immediately.
CREATE OR REPLACE FUNCTION audit_retention_cutoff(keep_days integer, at_time timestamptz)
RETURNS timestamptz LANGUAGE sql STABLE STRICT AS $$
    SELECT CASE WHEN keep_days = 0 THEN at_time ELSE
        (date_trunc('day', at_time AT TIME ZONE 'Asia/Shanghai')
         - keep_days * interval '1 day') AT TIME ZONE 'Asia/Shanghai'
    END;
$$;

-- All writers, including older instances and replayed snapshots, obey the same
-- policy. A shared row lock serializes inserts against policy changes.
CREATE OR REPLACE FUNCTION enforce_audit_retention() RETURNS trigger AS $$
DECLARE policy audit_retention_settings%ROWTYPE;
BEGIN
    SELECT * INTO STRICT policy FROM audit_retention_settings WHERE singleton FOR SHARE;
    IF policy.retention_days = 0 OR NEW.created_at < GREATEST(
        policy.purge_before, audit_retention_cutoff(policy.retention_days, clock_timestamp())
    ) THEN
        RETURN NULL;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS audit_retention_insert ON audit_logs;
CREATE TRIGGER audit_retention_insert BEFORE INSERT ON audit_logs
    FOR EACH ROW EXECUTE FUNCTION enforce_audit_retention();

CREATE INDEX IF NOT EXISTS idx_audit_logs_retention ON audit_logs(created_at, id);
