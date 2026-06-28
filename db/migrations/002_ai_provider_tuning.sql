ALTER TABLE ai_providers
  ADD COLUMN IF NOT EXISTS temperature double precision NOT NULL DEFAULT 0.2,
  ADD COLUMN IF NOT EXISTS timeout_ms integer NOT NULL DEFAULT 30000,
  ADD COLUMN IF NOT EXISTS max_output_tokens integer NOT NULL DEFAULT 1000;

ALTER TABLE ai_providers
  DROP CONSTRAINT IF EXISTS ai_providers_temperature_check,
  DROP CONSTRAINT IF EXISTS ai_providers_timeout_ms_check,
  DROP CONSTRAINT IF EXISTS ai_providers_max_output_tokens_check;

ALTER TABLE ai_providers
  ADD CONSTRAINT ai_providers_temperature_check CHECK (temperature >= 0 AND temperature <= 2),
  ADD CONSTRAINT ai_providers_timeout_ms_check CHECK (timeout_ms >= 1000 AND timeout_ms <= 120000),
  ADD CONSTRAINT ai_providers_max_output_tokens_check CHECK (max_output_tokens >= 1 AND max_output_tokens <= 32000);
