package vdoc

import (
	"fmt"

	"vdoc/utils/encryption"
)

func (s *Store) RevealSystemAIProviderKey(actorID string, auditCtx ...AuditContext) (string, error) {
	return s.revealAIProviderKey(actorID, "", auditCtx...)
}

func (s *Store) RevealProjectAIProviderKey(actorID, projectID string, auditCtx ...AuditContext) (string, error) {
	return s.revealAIProviderKey(actorID, projectID, auditCtx...)
}

func (s *Store) revealAIProviderKey(actorID, projectID string, auditCtx ...AuditContext) (string, error) {
	s = s.withAIConfigurationMutationGuard(actorID, projectID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return "", err
	}
	if err := s.checkAIProviderModelsAccessLocked(actorID, projectID); err != nil {
		return "", err
	}
	// 只查看当前作用域自己的密钥；项目继承系统配置不授予系统密钥读取权。
	provider := s.aiProviders[aiProviderKey(projectID)]
	if provider == nil || len(provider.APIKeyCiphertext) == 0 {
		return "", ErrNotFound
	}
	key, err := encryption.DecryptMCPToken(provider.APIKeyCiphertext, s.cipherKeyring, provider.CipherKID)
	if err != nil {
		return "", fmt.Errorf("decrypt ai api key: %w", err)
	}
	s.auditLocked(auditContext(auditCtx), AuditActorUser, actorID, "ai.provider.key.reveal", "ai_provider", provider.ID, projectID, "", auditMetadata("scope", provider.Scope))
	if err := s.persistLocked(); err != nil {
		return "", err
	}
	if err := s.requestContext().Err(); err != nil {
		return "", err
	}
	return key, nil
}
