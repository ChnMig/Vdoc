ALTER TABLE ai_chat_sessions
    ADD COLUMN IF NOT EXISTS context_summary text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS context_summary_through text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS context_summary_count integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS context_compacted_at timestamptz;

ALTER TABLE ai_chat_messages
    ADD COLUMN IF NOT EXISTS reasoning text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS retry_of text NOT NULL DEFAULT '';
