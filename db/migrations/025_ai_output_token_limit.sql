-- Zero disables the application output-token cap. Keep existing configured values.
ALTER TABLE ai_providers
  DROP CONSTRAINT IF EXISTS ai_providers_max_output_tokens_check;

ALTER TABLE ai_providers
  ADD CONSTRAINT ai_providers_max_output_tokens_check
  CHECK (max_output_tokens >= 0 AND max_output_tokens <= 32000);
