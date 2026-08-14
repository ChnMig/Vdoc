package vdoc

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	domainai "vdoc/domain/ai"
	"vdoc/utils/id"
)

const (
	aiChatMessageMaxRunes = 4000
	aiChatHistoryMaxRunes = 12000
)

type AIChatSessionInput struct {
	ProjectID   string
	DocumentID  string
	ContextType string
	ContextID   string
	Title       string
}

func (s *Store) CreateAIChatSession(actorID string, input AIChatSessionInput, auditCtx ...AuditContext) (*AIChatSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	target := AISummaryTarget{ProjectID: input.ProjectID, DocumentID: input.DocumentID, OwnerType: input.ContextType, OwnerID: input.ContextID}
	if err := s.ensureReadableAITargetLocked(actorID, target); err != nil {
		return nil, err
	}
	if err := s.ensureActiveAITargetLocked(target); err != nil {
		return nil, err
	}
	now := time.Now()
	session := &AIChatSession{ID: id.GenerateID(), ProjectID: input.ProjectID, DocumentID: input.DocumentID, ContextType: input.ContextType, ContextID: input.ContextID, Title: firstNonEmpty(strings.TrimSpace(input.Title), input.ContextType+" chat"), CreatedBy: actorID, CreatedAt: now, UpdatedAt: now}
	s.aiChats[session.ID] = session
	s.auditLocked(ctx, AuditActorUser, actorID, "ai.chat.create", "ai_chat_session", session.ID, input.ProjectID, input.DocumentID, auditMetadata("result", "success", "context_type", input.ContextType, "context_id", input.ContextID))
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	return cloneAIChatSession(session), nil
}

func (s *Store) AIChatSession(actorID, projectID, sessionID string) (*AIChatSession, []*AIChatMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, nil, err
	}
	session := s.aiChats[sessionID]
	if session == nil || session.ProjectID != projectID {
		return nil, nil, ErrNotFound
	}
	if !s.canReadLocked(actorID, projectID) {
		return nil, nil, ErrPermissionDenied
	}
	return cloneAIChatSession(session), s.chatMessagesLocked(sessionID), nil
}

func (s *Store) ListAIChatSessions(actorID string, target AISummaryTarget) ([]*AIChatSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if err := s.ensureReadableAITargetLocked(actorID, target); err != nil {
		return nil, err
	}
	sessions := make([]*AIChatSession, 0)
	for _, session := range s.aiChats {
		if session.ProjectID == target.ProjectID &&
			session.DocumentID == target.DocumentID &&
			session.ContextType == target.OwnerType &&
			session.ContextID == target.OwnerID {
			sessions = append(sessions, cloneAIChatSession(session))
		}
	}
	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].UpdatedAt.Equal(sessions[j].UpdatedAt) {
			return sessions[i].ID > sessions[j].ID
		}
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})
	return sessions, nil
}

func (s *Store) SendAIChatMessage(actorID, projectID, sessionID, content string, auditCtx ...AuditContext) (*AIChatMessage, error) {
	request, err := s.prepareAIChatRequest(actorID, projectID, sessionID, content)
	if err != nil {
		return nil, err
	}
	result, callErr := s.completeAI(context.Background(), request.Completion)
	return s.finishAIChatMessage(actorID, projectID, sessionID, request, result, callErr, auditCtx...)
}

type aiChatRequest struct {
	UserMessage     *AIChatMessage
	Target          AISummaryTarget
	ContextText     string
	Provider        *AIProviderConfig
	Prompt          AIPromptTemplate
	HistoryRevision []string
	GenerationToken string
	Completion      aiCompletionRequest
}

func (s *Store) prepareAIChatRequest(actorID, projectID, sessionID, content string) (aiChatRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return aiChatRequest{}, err
	}
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return aiChatRequest{}, ErrInvalidArgument
	}
	if len([]rune(trimmed)) > aiChatMessageMaxRunes {
		return aiChatRequest{}, fmt.Errorf("%w: chat message exceeds %d characters", ErrInvalidArgument, aiChatMessageMaxRunes)
	}
	if _, err := s.buildAIChatRequestLocked(actorID, projectID, sessionID, trimmed, ""); err != nil {
		return aiChatRequest{}, err
	}
	generationToken := id.GenerateID()
	if err := s.reserveAIChatRequestLocked(sessionID, generationToken, time.Now()); err != nil {
		return aiChatRequest{}, err
	}
	// The database reservation may have waited behind another completion. Reload
	// after it commits so this request sees the newly committed conversation.
	if err := s.refreshLocked(); err != nil {
		_ = s.releaseAIChatRequestLocked(sessionID, generationToken)
		return aiChatRequest{}, err
	}
	request, err := s.buildAIChatRequestLocked(actorID, projectID, sessionID, trimmed, generationToken)
	if err != nil {
		if releaseErr := s.releaseAIChatRequestLocked(sessionID, generationToken); releaseErr != nil && !Is(releaseErr, ErrFailedPrecondition) {
			return aiChatRequest{}, releaseErr
		}
		return aiChatRequest{}, err
	}
	return request, nil
}

func (s *Store) buildAIChatRequestLocked(actorID, projectID, sessionID, content, generationToken string) (aiChatRequest, error) {
	session := s.aiChats[sessionID]
	if session == nil || session.ProjectID != projectID {
		return aiChatRequest{}, ErrNotFound
	}
	if generationToken != "" && session.GenerationToken != generationToken {
		return aiChatRequest{}, staleAIChatRequestError()
	}
	if !s.canReadLocked(actorID, projectID) {
		return aiChatRequest{}, ErrPermissionDenied
	}
	target := AISummaryTarget{ProjectID: session.ProjectID, DocumentID: session.DocumentID, OwnerType: session.ContextType, OwnerID: session.ContextID}
	if err := s.ensureActiveAITargetLocked(target); err != nil {
		return aiChatRequest{}, err
	}
	contextText, _, err := s.aiTargetContextLocked(target)
	if err != nil {
		return aiChatRequest{}, err
	}
	provider, apiKey, err := s.effectiveAIProviderLocked(projectID)
	if err != nil {
		return aiChatRequest{}, err
	}
	prompt := s.effectivePromptLocked(projectID, domainai.PromptPageChat)
	if !prompt.Enabled {
		return aiChatRequest{}, ErrFailedPrecondition
	}
	messages := s.chatMessagesLocked(sessionID)
	now := time.Now()
	userMessage := &AIChatMessage{ID: id.GenerateID(), SessionID: sessionID, Role: domainai.ChatRoleUser, Content: content, CreatedAt: now}
	userPrompt := strings.ReplaceAll(prompt.UserPromptTemplate, "{{context}}", contextText)
	userPrompt = strings.ReplaceAll(userPrompt, "{{history}}", "")
	userPrompt = strings.ReplaceAll(userPrompt, "{{message}}", content)
	completion := aiCompletionRequest{Provider: cloneAIProvider(provider), APIKey: apiKey, System: prompt.SystemPrompt, User: userPrompt, History: limitedAIChatHistory(messages)}
	return aiChatRequest{
		UserMessage:     cloneAIChatMessage(userMessage),
		Target:          target,
		ContextText:     contextText,
		Provider:        cloneAIProvider(provider),
		Prompt:          prompt,
		HistoryRevision: aiChatHistoryRevision(messages),
		GenerationToken: generationToken,
		Completion:      completion,
	}, nil
}

func (s *Store) finishAIChatMessage(actorID, projectID, sessionID string, request aiChatRequest, result aiCompletionResult, callErr error, auditCtx ...AuditContext) (*AIChatMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	session := s.aiChats[sessionID]
	if session == nil || session.ProjectID != projectID || session.GenerationToken != request.GenerationToken {
		return nil, staleAIChatRequestError()
	}
	if err := s.validateAIChatCompletionLocked(actorID, projectID, sessionID, request); err != nil {
		audit := s.auditAIChatMessageLocked(ctx, actorID, projectID, sessionID, request.Provider, "failed", err, aiTokenUsage{})
		if persistErr := s.completeAIChatRequestLocked(sessionID, request.GenerationToken, nil, nil, nil, audit); persistErr != nil && !Is(persistErr, ErrFailedPrecondition) {
			return nil, persistErr
		}
		return nil, err
	}
	if callErr != nil {
		audit := s.auditAIChatMessageLocked(ctx, actorID, projectID, sessionID, request.Provider, "failed", callErr, aiTokenUsage{})
		if err := s.completeAIChatRequestLocked(sessionID, request.GenerationToken, nil, nil, nil, audit); err != nil {
			return nil, err
		}
		return nil, callErr
	}
	now := time.Now()
	assistant := &AIChatMessage{ID: id.GenerateID(), SessionID: sessionID, Role: domainai.ChatRoleAssistant, Content: result.Content, ProviderID: request.Provider.ID, CreatedAt: now}
	audit := s.auditAIChatMessageLocked(ctx, actorID, projectID, sessionID, request.Provider, "success", nil, result.Usage)
	if err := s.completeAIChatRequestLocked(sessionID, request.GenerationToken, &now, request.UserMessage, assistant, audit); err != nil {
		return nil, err
	}
	return cloneAIChatMessage(assistant), nil
}

func (s *Store) validateAIChatCompletionLocked(actorID, projectID, sessionID string, request aiChatRequest) error {
	if !s.canReadLocked(actorID, projectID) {
		return staleAIChatRequestError()
	}
	session := s.aiChats[sessionID]
	if session == nil || session.ProjectID != projectID {
		return staleAIChatRequestError()
	}
	target := AISummaryTarget{ProjectID: session.ProjectID, DocumentID: session.DocumentID, OwnerType: session.ContextType, OwnerID: session.ContextID}
	if target != request.Target {
		return staleAIChatRequestError()
	}
	if err := s.ensureActiveAITargetLocked(target); err != nil {
		return staleAIChatRequestError()
	}
	contextText, _, err := s.aiTargetContextLocked(target)
	if err != nil || contextText != request.ContextText {
		return staleAIChatRequestError()
	}
	provider, _, err := s.effectiveAIProviderLocked(projectID)
	if err != nil || !sameAIProviderRequest(provider, request.Provider) {
		return staleAIChatRequestError()
	}
	prompt := s.effectivePromptLocked(projectID, domainai.PromptPageChat)
	if prompt != request.Prompt || !prompt.Enabled {
		return staleAIChatRequestError()
	}
	if !sameStringList(aiChatHistoryRevision(s.chatMessagesLocked(sessionID)), request.HistoryRevision) {
		return staleAIChatRequestError()
	}
	return nil
}

func (s *Store) reserveAIChatRequestLocked(sessionID, generationToken string, startedAt time.Time) error {
	session := s.aiChats[sessionID]
	if session == nil {
		return ErrNotFound
	}
	session.GenerationToken = generationToken
	session.GenerationStartedAt = startedAt
	if s.persistence == nil {
		return nil
	}
	handled, err := s.persistence.reserveAIChatGenerationLocked(context.Background(), sessionID, generationToken, startedAt)
	if err != nil {
		s.restorePersistedStateLocked()
		return err
	}
	if !handled {
		if err := s.persistLocked(); err != nil {
			return err
		}
		return nil
	}
	s.persisted = s.cloneStateLocked()
	return nil
}

func (s *Store) releaseAIChatRequestLocked(sessionID, generationToken string) error {
	session := s.aiChats[sessionID]
	if session == nil || session.GenerationToken != generationToken {
		return staleAIChatRequestError()
	}
	return s.completeAIChatRequestLocked(sessionID, generationToken, nil, nil, nil, nil)
}

func (s *Store) completeAIChatRequestLocked(sessionID, generationToken string, updatedAt *time.Time, userMessage, assistantMessage *AIChatMessage, audit *AuditLog) error {
	session := s.aiChats[sessionID]
	if session == nil || session.GenerationToken != generationToken {
		return staleAIChatRequestError()
	}
	session.GenerationToken = ""
	session.GenerationStartedAt = time.Time{}
	if updatedAt != nil {
		session.UpdatedAt = *updatedAt
	}
	if userMessage != nil {
		s.aiMessages[userMessage.ID] = userMessage
	}
	if assistantMessage != nil {
		s.aiMessages[assistantMessage.ID] = assistantMessage
	}
	if s.persistence == nil {
		return nil
	}
	handled, err := s.persistence.completeAIChatGenerationLocked(context.Background(), sessionID, generationToken, updatedAt, userMessage, assistantMessage, audit)
	if err != nil {
		s.restorePersistedStateLocked()
		return err
	}
	if !handled {
		if err := s.persistLocked(); err != nil {
			return err
		}
		return nil
	}
	s.persisted = s.cloneStateLocked()
	return nil
}

func (s *Store) auditAIChatMessageLocked(ctx AuditContext, actorID, projectID, sessionID string, provider *AIProviderConfig, status string, resultErr error, usage aiTokenUsage) *AuditLog {
	metadata := auditMetadata("result", status, "provider_id", provider.ID, "api_mode", provider.APIMode)
	if resultErr != nil {
		metadata["reason"] = resultErr.Error()
	}
	addTokenUsageMetadata(metadata, usage)
	if s.audits == nil {
		s.audits = map[string]*AuditLog{}
	}
	return appendAuditToState(s.audits, ctx, AuditActorUser, actorID, "ai.chat.message", "ai_chat_session", sessionID, projectID, "", metadata)
}

func (s *Store) chatMessagesLocked(sessionID string) []*AIChatMessage {
	out := []*AIChatMessage{}
	for _, message := range s.aiMessages {
		if message.SessionID == sessionID {
			out = append(out, cloneAIChatMessage(message))
		}
	}
	sortAIChatMessages(out)
	return out
}

func sortAIChatMessages(values []*AIChatMessage) {
	sort.Slice(values, func(i, j int) bool {
		if values[i].CreatedAt.Equal(values[j].CreatedAt) {
			return values[i].ID < values[j].ID
		}
		return values[i].CreatedAt.Before(values[j].CreatedAt)
	})
}

func aiChatHistoryRevision(messages []*AIChatMessage) []string {
	revision := make([]string, 0, len(messages))
	for _, message := range messages {
		revision = append(revision, message.ID)
	}
	return revision
}

func limitedAIChatHistory(messages []*AIChatMessage) []aiMessagePayload {
	remaining := aiChatHistoryMaxRunes
	reversed := make([]aiMessagePayload, 0, len(messages))
	for i := len(messages) - 1; i >= 0 && remaining > 0; i-- {
		content := strings.TrimSpace(messages[i].Content)
		if content == "" {
			continue
		}
		runes := []rune(content)
		if len(runes) > remaining {
			runes = runes[:remaining]
		}
		reversed = append(reversed, aiMessagePayload{Role: messages[i].Role, Content: string(runes)})
		remaining -= len(runes)
	}
	history := make([]aiMessagePayload, len(reversed))
	for i := range reversed {
		history[len(reversed)-1-i] = reversed[i]
	}
	return history
}

func sameStringList(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func staleAIChatRequestError() error {
	return fmt.Errorf("%w: AI chat request became stale before completion", ErrFailedPrecondition)
}

func cloneAIChatSession(session *AIChatSession) *AIChatSession {
	if session == nil {
		return nil
	}
	copy := *session
	return &copy
}

func cloneAIChatMessage(message *AIChatMessage) *AIChatMessage {
	if message == nil {
		return nil
	}
	copy := *message
	return &copy
}
