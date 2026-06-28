package vdoc

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	domainai "vdoc/domain/ai"
	"vdoc/utils/encryption"
	"vdoc/utils/id"
)

func (s *Store) SetAIHTTPClient(client *http.Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.aiHTTP = client
}

func (s *Store) SystemAIProvider(actorID string) (*AIProviderConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.isSuperAdminLocked(actorID) {
		return nil, ErrPermissionDenied
	}
	return cloneAIProvider(s.aiProviders[systemAIProviderKey()]), nil
}

func (s *Store) UpsertSystemAIProvider(actorID string, input AIProviderInput, auditCtx ...AuditContext) (*AIProviderConfig, error) {
	return s.upsertAIProvider(actorID, "", input, auditCtx...)
}

func (s *Store) ProjectAIProvider(actorID, projectID string) (*AIProviderConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.canReadLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	if _, ok := s.projects[projectID]; !ok {
		return nil, ErrNotFound
	}
	return cloneAIProvider(s.aiProviders[projectAIProviderKey(projectID)]), nil
}

func (s *Store) UpsertProjectAIProvider(actorID, projectID string, input AIProviderInput, auditCtx ...AuditContext) (*AIProviderConfig, error) {
	return s.upsertAIProvider(actorID, projectID, input, auditCtx...)
}

func (s *Store) TestSystemAIProvider(actorID string, input *AIProviderInput, auditCtx ...AuditContext) (string, error) {
	return s.testAIProvider(actorID, "", input, auditCtx...)
}

func (s *Store) TestProjectAIProvider(actorID, projectID string, input *AIProviderInput, auditCtx ...AuditContext) (string, error) {
	return s.testAIProvider(actorID, projectID, input, auditCtx...)
}

func (s *Store) upsertAIProvider(actorID, projectID string, input AIProviderInput, auditCtx ...AuditContext) (*AIProviderConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if projectID == "" {
		if !s.isSuperAdminLocked(actorID) {
			return nil, ErrPermissionDenied
		}
	} else if !s.canManageProjectLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	if projectID != "" {
		if _, ok := s.projects[projectID]; !ok {
			return nil, ErrNotFound
		}
	}
	provider, err := s.buildProviderLocked(actorID, projectID, input)
	if err != nil {
		return nil, err
	}
	s.aiProviders[aiProviderKey(projectID)] = provider
	s.auditLocked(ctx, AuditActorUser, actorID, "ai.provider.upsert", "ai_provider", provider.ID, projectID, "", auditMetadata("result", "success", "scope", provider.Scope, "api_mode", provider.APIMode))
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	return cloneAIProvider(provider), nil
}

func (s *Store) buildProviderLocked(actorID, projectID string, input AIProviderInput) (*AIProviderConfig, error) {
	baseURL, err := normalizeAIProviderBaseURL(input.BaseURL)
	if err != nil {
		return nil, err
	}
	model := strings.TrimSpace(input.Model)
	mode := strings.TrimSpace(input.APIMode)
	if model == "" || !validAIMode(mode) {
		return nil, ErrInvalidArgument
	}
	tuning, err := providerTuning(input)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	key := aiProviderKey(projectID)
	existing := s.aiProviders[key]
	provider := &AIProviderConfig{ID: id.GenerateID(), ProjectID: projectID, CreatedBy: actorID, CreatedAt: now}
	if existing != nil {
		copied := *existing
		provider = &copied
	}
	provider.Scope = domainai.ProviderScopeSystem
	if projectID != "" {
		provider.Scope = domainai.ProviderScopeProject
	}
	provider.Name = firstNonEmpty(strings.TrimSpace(input.Name), "OpenAI-compatible")
	provider.BaseURL = baseURL
	provider.Model = model
	provider.APIMode = mode
	provider.Enabled = input.Enabled
	provider.Temperature = tuning.temperature
	provider.TimeoutMS = tuning.timeoutMS
	provider.MaxOutputTokens = tuning.maxOutputTokens
	provider.UpdatedBy = actorID
	provider.UpdatedAt = now
	if strings.TrimSpace(input.APIKey) != "" {
		ciphertext, cipherKID, err := encryption.EncryptMCPToken(strings.TrimSpace(input.APIKey), mcpTokenCipherKey())
		if err != nil {
			return nil, fmt.Errorf("encrypt ai api key: %w", err)
		}
		provider.APIKeyCiphertext = ciphertext
		provider.CipherKID = cipherKID
		provider.APIKeyLast4 = last4(strings.TrimSpace(input.APIKey))
	}
	if len(provider.APIKeyCiphertext) == 0 {
		return nil, ErrInvalidArgument
	}
	return provider, nil
}

func (s *Store) testAIProvider(actorID, projectID string, input *AIProviderInput, auditCtx ...AuditContext) (string, error) {
	provider, apiKey, err := s.resolveProviderForTest(actorID, projectID, input)
	if err != nil {
		return "", err
	}
	result, callErr := s.completeAI(context.Background(), aiCompletionRequest{Provider: provider, APIKey: apiKey, System: immutableAIGuard(), User: "Reply with a short provider connectivity check."})
	if auditErr := s.auditAIProviderTest(actorID, projectID, provider, result.Usage, callErr, auditCtx...); auditErr != nil {
		return "", auditErr
	}
	if callErr != nil {
		return "", callErr
	}
	return result.Content, nil
}

type aiProviderTuning struct {
	temperature     float64
	timeoutMS       int
	maxOutputTokens int
}

func providerTuning(input AIProviderInput) (aiProviderTuning, error) {
	tuning := aiProviderTuning{temperature: domainai.ProviderDefaultTemperature, timeoutMS: domainai.ProviderDefaultTimeoutMS, maxOutputTokens: domainai.ProviderDefaultMaxTokens}
	if input.Temperature != nil {
		tuning.temperature = *input.Temperature
	}
	if input.TimeoutMS != nil {
		tuning.timeoutMS = *input.TimeoutMS
	}
	if input.MaxOutputTokens != nil {
		tuning.maxOutputTokens = *input.MaxOutputTokens
	}
	if tuning.temperature < domainai.ProviderMinTemperature || tuning.temperature > domainai.ProviderMaxTemperature {
		return aiProviderTuning{}, ErrInvalidArgument
	}
	if tuning.timeoutMS < domainai.ProviderMinTimeoutMS || tuning.timeoutMS > domainai.ProviderMaxTimeoutMS {
		return aiProviderTuning{}, ErrInvalidArgument
	}
	if tuning.maxOutputTokens < domainai.ProviderMinMaxTokens || tuning.maxOutputTokens > domainai.ProviderMaxMaxTokens {
		return aiProviderTuning{}, ErrInvalidArgument
	}
	return tuning, nil
}

func (s *Store) resolveProviderForTest(actorID, projectID string, input *AIProviderInput) (*AIProviderConfig, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, "", err
	}
	if projectID == "" {
		if !s.isSuperAdminLocked(actorID) {
			return nil, "", ErrPermissionDenied
		}
	} else if !s.canManageProjectLocked(actorID, projectID) {
		return nil, "", ErrPermissionDenied
	}
	if input != nil {
		provider, err := s.buildProviderLocked(actorID, projectID, *input)
		if err != nil {
			return nil, "", err
		}
		apiKey := strings.TrimSpace(input.APIKey)
		if apiKey == "" {
			apiKey, err = encryption.DecryptMCPToken(provider.APIKeyCiphertext, mcpTokenCipherKey(), provider.CipherKID)
			if err != nil {
				return nil, "", fmt.Errorf("decrypt ai api key: %w", err)
			}
		}
		return provider, apiKey, nil
	}
	provider, apiKey, err := s.effectiveAIProviderLocked(projectID)
	if err != nil {
		return nil, "", err
	}
	return cloneAIProvider(provider), apiKey, nil
}

func (s *Store) effectiveAIProviderLocked(projectID string) (*AIProviderConfig, string, error) {
	provider := s.aiProviders[projectAIProviderKey(projectID)]
	if provider == nil || !provider.Enabled {
		provider = s.aiProviders[systemAIProviderKey()]
	}
	if provider == nil || !provider.Enabled {
		return nil, "", ErrFailedPrecondition
	}
	apiKey, err := encryption.DecryptMCPToken(provider.APIKeyCiphertext, mcpTokenCipherKey(), provider.CipherKID)
	if err != nil {
		return nil, "", fmt.Errorf("decrypt ai api key: %w", err)
	}
	return provider, apiKey, nil
}

func validAIMode(mode string) bool {
	return mode == domainai.ProviderModeChatCompletions || mode == domainai.ProviderModeResponses
}

func aiProviderKey(projectID string) string {
	if projectID == "" {
		return systemAIProviderKey()
	}
	return projectAIProviderKey(projectID)
}

func systemAIProviderKey() string { return "system" }

func projectAIProviderKey(projectID string) string { return "project:" + projectID }

func last4(value string) string {
	if len(value) <= 4 {
		return value
	}
	return value[len(value)-4:]
}

func (s *Store) isSuperAdminLocked(actorID string) bool {
	actor := s.users[actorID]
	return actor != nil && actor.Status == UserStatusActive && actor.IsSuperAdmin
}

func cloneAIProvider(provider *AIProviderConfig) *AIProviderConfig {
	if provider == nil {
		return nil
	}
	copy := *provider
	copy.APIKeyCiphertext = append([]byte(nil), provider.APIKeyCiphertext...)
	return &copy
}
