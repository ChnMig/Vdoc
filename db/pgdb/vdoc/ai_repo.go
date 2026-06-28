package vdoc

import (
	"context"
	"time"

	"vdoc/db/pgdb"
	domainai "vdoc/domain/ai"
	domainvdoc "vdoc/domain/vdoc"
)

func (r *Repository) UpsertAIProvider(ctx context.Context, provider *domainvdoc.AIProviderConfig) error {
	model := aiProviderModelFromDomain(provider)
	if model == nil {
		return nil
	}
	return r.upsertByID(ctx, model)
}

func (r *Repository) UpsertAIPrompt(ctx context.Context, prompt *domainvdoc.AIPromptOverride) error {
	model := aiPromptModelFromDomain(prompt)
	if model == nil {
		return nil
	}
	return r.upsertByID(ctx, model)
}

func (r *Repository) UpsertAISummary(ctx context.Context, summary *domainvdoc.AISummary) error {
	model := aiSummaryModelFromDomain(summary)
	if model == nil {
		return nil
	}
	return r.upsertByID(ctx, model)
}

func (r *Repository) UpsertAIChatSession(ctx context.Context, session *domainvdoc.AIChatSession) error {
	model := aiChatSessionModelFromDomain(session)
	if model == nil {
		return nil
	}
	return r.upsertByID(ctx, model)
}

func (r *Repository) UpsertAIChatMessage(ctx context.Context, message *domainvdoc.AIChatMessage) error {
	model := aiChatMessageModelFromDomain(message)
	if model == nil {
		return nil
	}
	return r.upsertByID(ctx, model)
}

func (r *Repository) loadAIProviders(ctx context.Context, loaded *domainvdoc.State) error {
	var models []AIProvider
	if err := r.database.WithContext(ctx).Find(&models).Error; err != nil {
		return err
	}
	for _, model := range models {
		provider := domainAIProviderFromModel(model)
		loaded.AIProviders[aiProviderStateKey(provider)] = provider
	}
	return nil
}

func (r *Repository) loadAIPrompts(ctx context.Context, loaded *domainvdoc.State) error {
	var models []AIPromptOverride
	if err := r.database.WithContext(ctx).Find(&models).Error; err != nil {
		return err
	}
	for _, model := range models {
		prompt := domainAIPromptFromModel(model)
		loaded.AIPrompts[aiPromptStateKey(prompt)] = prompt
	}
	return nil
}

func (r *Repository) loadAISummaries(ctx context.Context, loaded *domainvdoc.State) error {
	var models []AISummary
	if err := r.database.WithContext(ctx).Find(&models).Error; err != nil {
		return err
	}
	for _, model := range models {
		summary := domainAISummaryFromModel(model)
		loaded.AISummaries[aiSummaryStateKey(summary)] = summary
	}
	return nil
}

func (r *Repository) loadAIChats(ctx context.Context, loaded *domainvdoc.State) error {
	var models []AIChatSession
	if err := r.database.WithContext(ctx).Find(&models).Error; err != nil {
		return err
	}
	for _, model := range models {
		session := domainAIChatSessionFromModel(model)
		loaded.AIChats[session.ID] = session
	}
	return nil
}

func (r *Repository) loadAIMessages(ctx context.Context, loaded *domainvdoc.State) error {
	var models []AIChatMessage
	if err := r.database.WithContext(ctx).Order("created_at,id").Find(&models).Error; err != nil {
		return err
	}
	for _, model := range models {
		message := domainAIChatMessageFromModel(model)
		loaded.AIMessages[message.ID] = message
	}
	return nil
}

func aiProviderModelFromDomain(provider *domainvdoc.AIProviderConfig) *AIProvider {
	if provider == nil {
		return nil
	}
	return &AIProvider{Base: pgdb.Base{ID: provider.ID, CreatedAt: nonZeroTime(provider.CreatedAt), UpdatedAt: nonZeroTime(provider.UpdatedAt)}, Scope: provider.Scope, ProjectID: stringPtr(provider.ProjectID), Name: provider.Name, BaseURL: provider.BaseURL, Model: provider.Model, APIMode: provider.APIMode, APIKeyCiphertext: append([]byte(nil), provider.APIKeyCiphertext...), CipherKID: provider.CipherKID, APIKeyLast4: provider.APIKeyLast4, Enabled: provider.Enabled, Temperature: provider.Temperature, TimeoutMS: provider.TimeoutMS, MaxOutputTokens: provider.MaxOutputTokens, CreatedBy: provider.CreatedBy, UpdatedBy: provider.UpdatedBy}
}

func domainAIProviderFromModel(model AIProvider) *domainvdoc.AIProviderConfig {
	return &domainvdoc.AIProviderConfig{ID: domainID(model.ID), Scope: model.Scope, ProjectID: stringValueID(model.ProjectID), Name: model.Name, BaseURL: model.BaseURL, Model: model.Model, APIMode: model.APIMode, APIKeyCiphertext: append([]byte(nil), model.APIKeyCiphertext...), CipherKID: model.CipherKID, APIKeyLast4: model.APIKeyLast4, Enabled: model.Enabled, Temperature: model.Temperature, TimeoutMS: model.TimeoutMS, MaxOutputTokens: model.MaxOutputTokens, CreatedBy: domainID(model.CreatedBy), UpdatedBy: domainID(model.UpdatedBy), CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt}
}

func aiPromptModelFromDomain(prompt *domainvdoc.AIPromptOverride) *AIPromptOverride {
	if prompt == nil {
		return nil
	}
	return &AIPromptOverride{Base: pgdb.Base{ID: prompt.ID, CreatedAt: nonZeroTime(prompt.CreatedAt), UpdatedAt: nonZeroTime(prompt.UpdatedAt)}, Scope: prompt.Scope, ProjectID: stringPtr(prompt.ProjectID), PromptKey: prompt.PromptKey, SystemPrompt: prompt.SystemPrompt, UserPromptTemplate: prompt.UserPromptTemplate, Enabled: prompt.Enabled, CreatedBy: prompt.CreatedBy, UpdatedBy: prompt.UpdatedBy}
}

func domainAIPromptFromModel(model AIPromptOverride) *domainvdoc.AIPromptOverride {
	return &domainvdoc.AIPromptOverride{ID: domainID(model.ID), Scope: model.Scope, ProjectID: stringValueID(model.ProjectID), PromptKey: model.PromptKey, SystemPrompt: model.SystemPrompt, UserPromptTemplate: model.UserPromptTemplate, Enabled: model.Enabled, CreatedBy: domainID(model.CreatedBy), UpdatedBy: domainID(model.UpdatedBy), CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt}
}

func aiSummaryModelFromDomain(summary *domainvdoc.AISummary) *AISummary {
	if summary == nil {
		return nil
	}
	generatedAt := nonZeroTime(summary.GeneratedAt)
	return &AISummary{Base: pgdb.Base{ID: summary.ID, CreatedAt: nonZeroTime(summary.GeneratedAt), UpdatedAt: nonZeroTime(summary.UpdatedAt)}, ProjectID: summary.ProjectID, DocumentID: summary.DocumentID, OwnerType: summary.OwnerType, OwnerID: summary.OwnerID, PromptKey: summary.PromptKey, ProviderID: stringPtr(summary.ProviderID), Status: summary.Status, Content: stringPtr(summary.Content), ErrorMessage: stringPtr(summary.ErrorMessage), GeneratedBy: summary.GeneratedBy, GeneratedAt: &generatedAt}
}

func domainAISummaryFromModel(model AISummary) *domainvdoc.AISummary {
	return &domainvdoc.AISummary{ID: domainID(model.ID), ProjectID: domainID(model.ProjectID), DocumentID: domainID(model.DocumentID), OwnerType: model.OwnerType, OwnerID: domainID(model.OwnerID), PromptKey: model.PromptKey, ProviderID: stringValueID(model.ProviderID), Status: model.Status, Content: stringValue(model.Content), ErrorMessage: stringValue(model.ErrorMessage), GeneratedBy: domainID(model.GeneratedBy), GeneratedAt: timeFromPtr(model.GeneratedAt, model.CreatedAt), UpdatedAt: model.UpdatedAt}
}

func aiChatSessionModelFromDomain(session *domainvdoc.AIChatSession) *AIChatSession {
	if session == nil {
		return nil
	}
	return &AIChatSession{Base: pgdb.Base{ID: session.ID, CreatedAt: nonZeroTime(session.CreatedAt), UpdatedAt: nonZeroTime(session.UpdatedAt)}, ProjectID: session.ProjectID, DocumentID: stringPtr(session.DocumentID), ContextType: session.ContextType, ContextID: session.ContextID, Title: session.Title, CreatedBy: session.CreatedBy}
}

func domainAIChatSessionFromModel(model AIChatSession) *domainvdoc.AIChatSession {
	return &domainvdoc.AIChatSession{ID: domainID(model.ID), ProjectID: domainID(model.ProjectID), DocumentID: stringValueID(model.DocumentID), ContextType: model.ContextType, ContextID: domainID(model.ContextID), Title: model.Title, CreatedBy: domainID(model.CreatedBy), CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt}
}

func aiChatMessageModelFromDomain(message *domainvdoc.AIChatMessage) *AIChatMessage {
	if message == nil {
		return nil
	}
	createdAt := nonZeroTime(message.CreatedAt)
	return &AIChatMessage{Base: pgdb.Base{ID: message.ID, CreatedAt: createdAt, UpdatedAt: createdAt}, SessionID: message.SessionID, Role: message.Role, Content: message.Content, ProviderID: stringPtr(message.ProviderID)}
}

func domainAIChatMessageFromModel(model AIChatMessage) *domainvdoc.AIChatMessage {
	return &domainvdoc.AIChatMessage{ID: domainID(model.ID), SessionID: domainID(model.SessionID), Role: model.Role, Content: model.Content, ProviderID: stringValueID(model.ProviderID), CreatedAt: model.CreatedAt}
}

func aiProviderStateKey(provider *domainvdoc.AIProviderConfig) string {
	if provider.Scope == domainai.ProviderScopeSystem {
		return "system"
	}
	return "project:" + provider.ProjectID
}

func aiPromptStateKey(prompt *domainvdoc.AIPromptOverride) string {
	if prompt.Scope == domainai.ProviderScopeSystem {
		return "system:" + prompt.PromptKey
	}
	return "project:" + prompt.ProjectID + ":" + prompt.PromptKey
}

func aiSummaryStateKey(summary *domainvdoc.AISummary) string {
	return summary.ProjectID + ":" + summary.DocumentID + ":" + summary.OwnerType + ":" + summary.OwnerID
}

func timeFromPtr(value *time.Time, fallback time.Time) time.Time {
	if value == nil || value.IsZero() {
		return fallback
	}
	return *value
}
