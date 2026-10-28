-- 将总时长与首次响应、响应空闲分开；0 允许显式禁用任意时限。
ALTER TABLE ai_providers
  ADD COLUMN IF NOT EXISTS first_response_timeout_ms integer NOT NULL DEFAULT 180000,
  ADD COLUMN IF NOT EXISTS idle_timeout_ms integer NOT NULL DEFAULT 120000;

ALTER TABLE ai_providers
  DROP CONSTRAINT IF EXISTS ai_providers_timeout_ms_check,
  DROP CONSTRAINT IF EXISTS ai_providers_first_response_timeout_ms_check,
  DROP CONSTRAINT IF EXISTS ai_providers_idle_timeout_ms_check;

ALTER TABLE ai_providers
  ALTER COLUMN timeout_ms SET DEFAULT 600000,
  ADD CONSTRAINT ai_providers_timeout_ms_check CHECK (timeout_ms = 0 OR timeout_ms >= 1000),
  ADD CONSTRAINT ai_providers_first_response_timeout_ms_check CHECK (first_response_timeout_ms = 0 OR first_response_timeout_ms >= 1000),
  ADD CONSTRAINT ai_providers_idle_timeout_ms_check CHECK (idle_timeout_ms = 0 OR idle_timeout_ms >= 1000);

-- 仅提升旧默认值，保留用户显式设置的其他时长。
UPDATE ai_providers SET timeout_ms = 600000 WHERE timeout_ms = 30000;
