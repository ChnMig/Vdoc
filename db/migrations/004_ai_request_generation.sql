ALTER TABLE ai_summaries
  ADD COLUMN IF NOT EXISTS generation_token text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS generation_started_at timestamptz;

ALTER TABLE ai_summaries
  DROP CONSTRAINT IF EXISTS ai_summaries_status_check;

ALTER TABLE ai_summaries
  ADD CONSTRAINT ai_summaries_status_check
  CHECK (status IN ('pending', 'skipped', 'succeeded', 'failed'));

ALTER TABLE ai_chat_sessions
  ADD COLUMN IF NOT EXISTS generation_token text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS generation_started_at timestamptz;
