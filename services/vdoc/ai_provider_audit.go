package vdoc

func (s *Store) auditAIProviderTest(actorID, projectID string, provider *AIProviderConfig, usage aiTokenUsage, callErr error, auditCtx ...AuditContext) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return err
	}
	if projectID == "" && !s.isSuperAdminLocked(actorID) {
		return ErrFailedPrecondition
	}
	if projectID != "" {
		if !s.canManageProjectLocked(actorID, projectID) {
			return ErrFailedPrecondition
		}
		if err := s.ensureActiveProjectLocked(projectID); err != nil {
			return ErrFailedPrecondition
		}
	}
	s = s.withAIConfigurationMutationGuard(actorID, projectID)
	metadata := auditMetadata("result", "success", "provider_id", provider.ID, "api_mode", provider.APIMode, "scope", provider.Scope)
	if callErr != nil {
		metadata["result"] = "failed"
		metadata["reason"] = callErr.Error()
	}
	addTokenUsageMetadata(metadata, usage)
	s.auditLocked(ctx, AuditActorUser, actorID, "ai.provider.test", "ai_provider", provider.ID, projectID, "", metadata)
	if err := s.persistLocked(); err != nil {
		if Is(err, ErrPermissionDenied) || Is(err, ErrFailedPrecondition) || Is(err, ErrNotFound) {
			return ErrFailedPrecondition
		}
		return err
	}
	return nil
}

// 替代请求的业务状态必须保持不变，仅追加已经发生的上游调用证据。
func (s *Store) persistDiscardedAIAuditLocked(audit *AuditLog) error {
	if s.persistence == nil {
		return nil
	}
	if err := s.persistence.recordAudit(s.requestContext(), audit); err != nil {
		delete(s.audits, audit.ID)
		return err
	}
	s.audits[audit.ID] = cloneAuditLog(audit)
	if s.persisted != nil {
		if s.persisted.AuditLogs == nil {
			s.persisted.AuditLogs = map[string]*AuditLog{}
		}
		s.persisted.AuditLogs[audit.ID] = cloneAuditLog(audit)
	}
	return nil
}
