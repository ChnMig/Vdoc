ALTER TABLE ai_chat_sessions
    ADD COLUMN IF NOT EXISTS title_generated boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS reasoning_effort text NOT NULL DEFAULT '';

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'ai_chat_sessions'::regclass AND conname = 'ai_chat_sessions_reasoning_effort_check') THEN
        ALTER TABLE ai_chat_sessions
            ADD CONSTRAINT ai_chat_sessions_reasoning_effort_check
            CHECK (reasoning_effort IN ('', 'none', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max'));
    END IF;
END $$;
