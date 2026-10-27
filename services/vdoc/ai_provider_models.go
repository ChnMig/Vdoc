package vdoc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode"

	domainai "vdoc/domain/ai"
	"vdoc/utils/encryption"
)

func (s *Store) ListSystemAIProviderModels(actorID string, input *AIProviderInput) ([]string, error) {
	return s.listAIProviderModels(actorID, "", input)
}

func (s *Store) ListProjectAIProviderModels(actorID, projectID string, input *AIProviderInput) ([]string, error) {
	return s.listAIProviderModels(actorID, projectID, input)
}

func (s *Store) listAIProviderModels(actorID, projectID string, input *AIProviderInput) ([]string, error) {
	provider, apiKey, client, err := s.resolveProviderForModels(actorID, projectID, input)
	if err != nil {
		return nil, err
	}
	models, err := fetchAIProviderModels(s.requestContext(), client, provider, apiKey)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if err := s.checkAIProviderModelsAccessLocked(actorID, projectID); err != nil {
		return nil, err
	}
	return models, s.requestContext().Err()
}

func (s *Store) checkAIProviderModelsAccessLocked(actorID, projectID string) error {
	if projectID == "" {
		if !s.isSuperAdminLocked(actorID) {
			return ErrPermissionDenied
		}
		return nil
	}
	if !s.canManageProjectLocked(actorID, projectID) {
		return ErrPermissionDenied
	}
	return s.ensureActiveProjectLocked(projectID)
}

func (s *Store) resolveProviderForModels(actorID, projectID string, input *AIProviderInput) (*AIProviderConfig, string, *http.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, "", nil, err
	}
	if err := s.checkAIProviderModelsAccessLocked(actorID, projectID); err != nil {
		return nil, "", nil, err
	}
	existing := s.aiProviders[aiProviderKey(projectID)]
	if input == nil && existing == nil {
		existing = s.aiProviders[systemAIProviderKey()]
		if existing == nil || !existing.Enabled {
			return nil, "", nil, ErrFailedPrecondition
		}
	}
	provider := &AIProviderConfig{TimeoutMS: domainai.ProviderDefaultTimeoutMS}
	if existing != nil {
		provider = cloneAIProvider(existing)
	}
	apiKey := ""
	if input != nil {
		provider.BaseURL = input.BaseURL
		apiKey = strings.TrimSpace(input.APIKey)
		if input.TimeoutMS != nil {
			provider.TimeoutMS = *input.TimeoutMS
		}
	}
	baseURL, err := normalizeAIProviderBaseURL(provider.BaseURL)
	if err != nil {
		return nil, "", nil, err
	}
	provider.BaseURL = baseURL
	if provider.TimeoutMS < domainai.ProviderMinTimeoutMS || provider.TimeoutMS > domainai.ProviderMaxTimeoutMS {
		return nil, "", nil, ErrInvalidArgument
	}
	if apiKey == "" {
		// 不把已保存的凭证转发到表单中新填写的其他服务地址。
		if existing == nil || len(existing.APIKeyCiphertext) == 0 || existing.BaseURL != baseURL {
			return nil, "", nil, fmt.Errorf("%w: enter an API key for this provider URL", ErrInvalidArgument)
		}
		apiKey, err = encryption.DecryptMCPToken(existing.APIKeyCiphertext, s.cipherKeyring, existing.CipherKID)
		if err != nil {
			return nil, "", nil, fmt.Errorf("decrypt ai api key: %w", err)
		}
	}
	client := s.aiHTTP
	if client == nil {
		client = newAIHTTPClient()
	}
	return provider, apiKey, client, nil
}

func fetchAIProviderModels(ctx context.Context, client *http.Client, provider *AIProviderConfig, apiKey string) ([]string, error) {
	requestCtx, cancel := context.WithTimeout(ctx, time.Duration(provider.TimeoutMS)*time.Millisecond)
	defer cancel()
	baseURL := provider.BaseURL
	path := "/v1/models"
	if strings.HasSuffix(baseURL, "/v1") {
		path = "/models"
	}
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, baseURL+path, nil)
	if err != nil {
		return nil, ErrInvalidArgument
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	safeClient := *client
	safeClient.Timeout = 0
	safeClient.CheckRedirect = rejectAIProviderRedirect
	resp, err := safeClient.Do(req)
	if err != nil {
		if requestCtx.Err() != nil {
			return nil, requestCtx.Err()
		}
		// 兼容服务的传输错误可能含请求头，不能原样返回或记录凭证。
		return nil, fmt.Errorf("%w: unable to retrieve provider models", ErrFailedPrecondition)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("%w: model list provider status %d", ErrFailedPrecondition, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, aiProviderMaxResponseBytes+1))
	if err != nil {
		if requestCtx.Err() != nil {
			return nil, requestCtx.Err()
		}
		return nil, fmt.Errorf("%w: unable to read provider models", ErrFailedPrecondition)
	}
	if len(data) > aiProviderMaxResponseBytes {
		return nil, fmt.Errorf("%w: model list response is too large", ErrFailedPrecondition)
	}
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &out); err != nil || out.Data == nil || len(out.Data) > 4096 {
		return nil, fmt.Errorf("%w: invalid provider model list", ErrFailedPrecondition)
	}
	unique := make(map[string]struct{}, len(out.Data))
	for _, model := range out.Data {
		id := strings.TrimSpace(model.ID)
		if id == "" || len(id) > 256 || strings.ContainsFunc(id, unicode.IsControl) {
			return nil, fmt.Errorf("%w: invalid provider model ID", ErrFailedPrecondition)
		}
		unique[id] = struct{}{}
	}
	models := make([]string, 0, len(unique))
	for id := range unique {
		models = append(models, id)
	}
	sort.Strings(models)
	return models, nil
}
