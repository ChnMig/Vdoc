package vdoc

import (
	"context"
	"strings"
	"time"

	domainai "vdoc/domain/ai"
	"vdoc/utils/id"
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

func (s *Store) SendAIChatMessage(actorID, projectID, sessionID, content string, auditCtx ...AuditContext) (*AIChatMessage, error) {
	request, err := s.prepareAIChatRequest(actorID, projectID, sessionID, content)
	if err != nil {
		return nil, err
	}
	answer, callErr := s.completeAI(context.Background(), request.Completion)
	if callErr != nil {
		return nil, callErr
	}
	return s.finishAIChatMessage(actorID, projectID, sessionID, request, answer, auditCtx...)
}

type aiChatRequest struct {
	UserMessage *AIChatMessage
	Completion  aiCompletionRequest
}

func (s *Store) prepareAIChatRequest(actorID, projectID, sessionID, content string) (aiChatRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return aiChatRequest{}, err
	}
	session := s.aiChats[sessionID]
	if session == nil || session.ProjectID != projectID {
		return aiChatRequest{}, ErrNotFound
	}
	if !s.canReadLocked(actorID, projectID) {
		return aiChatRequest{}, ErrPermissionDenied
	}
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return aiChatRequest{}, ErrInvalidArgument
	}
	contextText, _, err := s.aiTargetContextLocked(AISummaryTarget{ProjectID: session.ProjectID, DocumentID: session.DocumentID, OwnerType: session.ContextType, OwnerID: session.ContextID})
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
	now := time.Now()
	userMessage := &AIChatMessage{ID: id.GenerateID(), SessionID: sessionID, Role: domainai.ChatRoleUser, Content: trimmed, CreatedAt: now}
	userPrompt := strings.ReplaceAll(prompt.UserPromptTemplate, "{{context}}", contextText)
	userPrompt = strings.ReplaceAll(userPrompt, "{{message}}", trimmed)
	completion := aiCompletionRequest{Provider: cloneAIProvider(provider), APIKey: apiKey, System: prompt.SystemPrompt, User: userPrompt, Temperature: 0.2, MaxTokens: 1000}
	return aiChatRequest{UserMessage: cloneAIChatMessage(userMessage), Completion: completion}, nil
}

func (s *Store) finishAIChatMessage(actorID, projectID, sessionID string, request aiChatRequest, answer string, auditCtx ...AuditContext) (*AIChatMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	now := time.Now()
	assistant := &AIChatMessage{ID: id.GenerateID(), SessionID: sessionID, Role: domainai.ChatRoleAssistant, Content: answer, ProviderID: request.Completion.Provider.ID, CreatedAt: now}
	s.aiMessages[request.UserMessage.ID] = request.UserMessage
	s.aiMessages[assistant.ID] = assistant
	if session := s.aiChats[sessionID]; session != nil {
		session.UpdatedAt = now
	}
	s.auditLocked(ctx, AuditActorUser, actorID, "ai.chat.message", "ai_chat_session", sessionID, projectID, "", auditMetadata("result", "success", "provider_id", request.Completion.Provider.ID))
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	return cloneAIChatMessage(assistant), nil
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
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j].CreatedAt.Before(values[j-1].CreatedAt); j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
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
