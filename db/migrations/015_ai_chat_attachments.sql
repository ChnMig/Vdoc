ALTER TABLE ai_chat_messages ADD COLUMN IF NOT EXISTS attachments jsonb NOT NULL DEFAULT '[]';
ALTER TABLE ai_chat_messages ADD CONSTRAINT ai_chat_messages_attachments_array_check
  CHECK (jsonb_typeof(attachments) = 'array');
