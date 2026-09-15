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
