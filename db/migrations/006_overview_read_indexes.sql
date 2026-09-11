CREATE INDEX audit_logs_document_readiness_idx
ON audit_logs (project_id, document_id, actor_token_id, created_at DESC)
WHERE action = 'mcp.tool_call'
  AND metadata->>'evidence_kind' = 'published_content_read'
  AND metadata->>'result' = 'success';

CREATE INDEX ai_summaries_orphan_recovery_idx
ON ai_summaries ((COALESCE(generation_started_at, updated_at)))
WHERE status = 'pending';
