ALTER TABLE ai_chat_sessions DROP CONSTRAINT ai_chat_sessions_context_type_check;
ALTER TABLE ai_chat_sessions ADD CONSTRAINT ai_chat_sessions_context_type_check
  CHECK (context_type IN ('draft', 'version', 'diff', 'document'));
ALTER TABLE ai_chat_sessions ADD CONSTRAINT ai_chat_sessions_document_context_check
  CHECK (context_type <> 'document' OR (document_id IS NOT NULL AND context_id = document_id));
