package vdoc

import (
	"context"
	"time"

	"vdoc/db/pgdb"
	domainai "vdoc/domain/ai"
	domainvdoc "vdoc/domain/vdoc"

	"gorm.io/gorm/clause"
)

func (r *Repository) UpsertAIProvider(ctx context.Context, provider *domainvdoc.AIProviderConfig) error {
	model := aiProviderModelFromDomain(provider)
	if model == nil {
		return nil
	}
	return r.upsertByID(ctx, model)
}

func (r *Repository) UpsertAIProviderIfUnchanged(ctx context.Context, provider, previous *domainvdoc.AIProviderConfig) error {
	model := aiProviderModelFromDomain(provider)
	if model == nil {
		return nil
	}
	return r.upsertByIDIfUnchanged(ctx, model, model.ID, domainUpdatedAt(previous))
}

func (r *Repository) UpsertAIPrompt(ctx context.Context, prompt *domainvdoc.AIPromptOverride) error {
	model := aiPromptModelFromDomain(prompt)
	if model == nil {
		return nil
	}
	return r.upsertByID(ctx, model)
}

func (r *Repository) UpsertAIPromptIfUnchanged(ctx context.Context, prompt, previous *domainvdoc.AIPromptOverride) error {
	model := aiPromptModelFromDomain(prompt)
	if model == nil {
		return nil
	}
	return r.upsertByIDIfUnchanged(ctx, model, model.ID, domainUpdatedAt(previous))
}

func (r *Repository) UpsertAISummary(ctx context.Context, summary *domainvdoc.AISummary) error {
	model := aiSummaryModelFromDomain(summary)
	if model == nil {
		return nil
	}
	return r.upsertByID(ctx, model)
}

func (r *Repository) UpsertAISummaryIfUnchanged(ctx context.Context, summary, previous *domainvdoc.AISummary) error {
	model := aiSummaryModelFromDomain(summary)
	if model == nil {
		return nil
	}
	return r.upsertByIDIfUnchanged(ctx, model, model.ID, domainUpdatedAt(previous))
}

func (r *Repository) UpsertAIChatSession(ctx context.Context, session *domainvdoc.AIChatSession) error {
	model := aiChatSessionModelFromDomain(session)
	if model == nil {
		return nil
	}
	return r.upsertByID(ctx, model)
}

func (r *Repository) UpsertAIChatSessionIfUnchanged(ctx context.Context, session, previous *domainvdoc.AIChatSession) error {
	model := aiChatSessionModelFromDomain(session)
	if model == nil {
		return nil
	}
	return r.upsertByIDIfUnchanged(ctx, model, model.ID, domainUpdatedAt(previous))
}

func (r *Repository) UpsertAIChatMessage(ctx context.Context, message *domainvdoc.AIChatMessage) error {
	model := aiChatMessageModelFromDomain(message)
	if model == nil {
		return nil
	}
	return r.upsertByID(ctx, model)
}

func (r *Repository) InsertAIChatMessageIfAbsent(ctx context.Context, message *domainvdoc.AIChatMessage) error {
	model := aiChatMessageModelFromDomain(message)
	if model == nil {
		return nil
	}
	return r.upsertByIDIfUnchanged(ctx, model, model.ID, nil)
}

// ReserveAISummaryGeneration atomically makes this request the latest
// generation for a summary target. The owner uniqueness constraint is the
// cross-process serialization point when the summary does not exist yet.
func (r *Repository) ReserveAISummaryGeneration(ctx context.Context, summary *domainvdoc.AISummary) (*domainvdoc.AISummary, error) {
	model := aiSummaryModelFromDomain(summary)
	if model == nil {
		return nil, domainvdoc.ErrInvalidArgument
	}
	conflict := clause.OnConflict{
		Columns: []clause.Column{
			{Name: "project_id"},
			{Name: "document_id"},
			{Name: "owner_type"},
			{Name: "owner_id"},
		},
		DoUpdates: clause.AssignmentColumns([]string{
			"prompt_key",
			"provider_id",
			"status",
			"content",
			"error_message",
			"generated_by",
			"generated_at",
			"generation_token",
			"generation_started_at",
			"updated_at",
		}),
	}
	if err := r.database.WithContext(ctx).Clauses(conflict, clause.Returning{}).Create(model).Error; err != nil {
		return nil, err
	}
	return domainAISummaryFromModel(*model), nil
}

// CompleteAISummaryGeneration updates only the request that still owns the
// generation token. A zero row count means a newer request superseded it.
func (r *Repository) CompleteAISummaryGeneration(ctx context.Context, summary *domainvdoc.AISummary, expectedToken string) (bool, error) {
	model := aiSummaryModelFromDomain(summary)
	if model == nil || expectedToken == "" {
		return false, domainvdoc.ErrInvalidArgument
	}
	result := r.database.WithContext(ctx).Model(&AISummary{}).
		Where("id = ? AND generation_token = ?", model.ID, expectedToken).
		UpdateColumns(map[string]any{
			"prompt_key":            model.PromptKey,
			"provider_id":           model.ProviderID,
			"status":                model.Status,
			"content":               model.Content,
			"error_message":         model.ErrorMessage,
			"generated_by":          model.GeneratedBy,
			"generated_at":          model.GeneratedAt,
			"generation_token":      model.GenerationToken,
			"generation_started_at": model.GenerationStartedAt,
			"updated_at":            model.UpdatedAt,
		})
	return result.RowsAffected == 1, result.Error
}

// ReserveAIChatGeneration records latest-request-wins ordering without
// changing the session's user-facing updated_at timestamp.
func (r *Repository) ReserveAIChatGeneration(ctx context.Context, sessionID, token string, startedAt time.Time) (bool, error) {
	if sessionID == "" || token == "" || startedAt.IsZero() {
		return false, domainvdoc.ErrInvalidArgument
	}
	result := r.database.WithContext(ctx).Model(&AIChatSession{}).
		Where("id = ?", sessionID).
		UpdateColumns(map[string]any{
			"generation_token":      token,
			"generation_started_at": startedAt,
		})
	return result.RowsAffected == 1, result.Error
}

// CompleteAIChatGeneration clears a request token conditionally. The caller
// keeps this update in the same transaction as messages and audit insertion.
func (r *Repository) CompleteAIChatGeneration(ctx context.Context, sessionID, expectedToken string, updatedAt *time.Time) (bool, error) {
	if sessionID == "" || expectedToken == "" {
		return false, domainvdoc.ErrInvalidArgument
	}
	updates := map[string]any{
		"generation_token":      "",
		"generation_started_at": nil,
	}
	if updatedAt != nil && !updatedAt.IsZero() {
		updates["updated_at"] = *updatedAt
	}
	result := r.database.WithContext(ctx).Model(&AIChatSession{}).
		Where("id = ? AND generation_token = ?", sessionID, expectedToken).
		UpdateColumns(updates)
	return result.RowsAffected == 1, result.Error
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
	return &AISummary{Base: pgdb.Base{ID: summary.ID, CreatedAt: nonZeroTime(summary.GeneratedAt), UpdatedAt: nonZeroTime(summary.UpdatedAt)}, ProjectID: summary.ProjectID, DocumentID: summary.DocumentID, OwnerType: summary.OwnerType, OwnerID: summary.OwnerID, PromptKey: summary.PromptKey, ProviderID: stringPtr(summary.ProviderID), Status: summary.Status, Content: stringPtr(summary.Content), ErrorMessage: stringPtr(summary.ErrorMessage), GeneratedBy: summary.GeneratedBy, GeneratedAt: &generatedAt, GenerationToken: summary.GenerationToken, GenerationStartedAt: nullableTime(summary.GenerationStartedAt)}
}

func domainAISummaryFromModel(model AISummary) *domainvdoc.AISummary {
	return &domainvdoc.AISummary{ID: domainID(model.ID), ProjectID: domainID(model.ProjectID), DocumentID: domainID(model.DocumentID), OwnerType: model.OwnerType, OwnerID: domainID(model.OwnerID), PromptKey: model.PromptKey, ProviderID: stringValueID(model.ProviderID), Status: model.Status, Content: stringValue(model.Content), ErrorMessage: stringValue(model.ErrorMessage), GeneratedBy: domainID(model.GeneratedBy), GeneratedAt: timeFromPtr(model.GeneratedAt, model.CreatedAt), UpdatedAt: model.UpdatedAt, GenerationToken: model.GenerationToken, GenerationStartedAt: timeFromPtr(model.GenerationStartedAt, time.Time{})}
}

func aiChatSessionModelFromDomain(session *domainvdoc.AIChatSession) *AIChatSession {
	if session == nil {
		return nil
	}
	return &AIChatSession{Base: pgdb.Base{ID: session.ID, CreatedAt: nonZeroTime(session.CreatedAt), UpdatedAt: nonZeroTime(session.UpdatedAt)}, ProjectID: session.ProjectID, DocumentID: stringPtr(session.DocumentID), ContextType: session.ContextType, ContextID: session.ContextID, Title: session.Title, CreatedBy: session.CreatedBy, GenerationToken: session.GenerationToken, GenerationStartedAt: nullableTime(session.GenerationStartedAt)}
}

func domainAIChatSessionFromModel(model AIChatSession) *domainvdoc.AIChatSession {
	return &domainvdoc.AIChatSession{ID: domainID(model.ID), ProjectID: domainID(model.ProjectID), DocumentID: stringValueID(model.DocumentID), ContextType: model.ContextType, ContextID: domainID(model.ContextID), Title: model.Title, CreatedBy: domainID(model.CreatedBy), CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt, GenerationToken: model.GenerationToken, GenerationStartedAt: timeFromPtr(model.GenerationStartedAt, time.Time{})}
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

func nullableTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	copy := value
	return &copy
}
