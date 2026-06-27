package vdoc

import (
	"context"

	domainvdoc "vdoc/domain/vdoc"
)

type aiMutationRepository interface {
	UpsertAIProvider(ctx context.Context, provider *domainvdoc.AIProviderConfig) error
	UpsertAIPrompt(ctx context.Context, prompt *domainvdoc.AIPromptOverride) error
	UpsertAISummary(ctx context.Context, summary *domainvdoc.AISummary) error
	UpsertAIChatSession(ctx context.Context, session *domainvdoc.AIChatSession) error
	UpsertAIChatMessage(ctx context.Context, message *domainvdoc.AIChatMessage) error
}

func (p *postgresPersistence) saveAIStateLocked(ctx context.Context, store *Store, repo aiMutationRepository) error {
	for _, provider := range sortedStoreValues(store.aiProviders, func(value *domainvdoc.AIProviderConfig) string { return value.ID }) {
		if err := repo.UpsertAIProvider(ctx, provider); err != nil {
			return err
		}
	}
	for _, prompt := range sortedStoreValues(store.aiPrompts, func(value *domainvdoc.AIPromptOverride) string { return value.ID }) {
		if err := repo.UpsertAIPrompt(ctx, prompt); err != nil {
			return err
		}
	}
	for _, summary := range sortedStoreValues(store.aiSummaries, func(value *domainvdoc.AISummary) string { return value.ID }) {
		if err := repo.UpsertAISummary(ctx, summary); err != nil {
			return err
		}
	}
	for _, session := range sortedStoreValues(store.aiChats, func(value *domainvdoc.AIChatSession) string { return value.ID }) {
		if err := repo.UpsertAIChatSession(ctx, session); err != nil {
			return err
		}
	}
	for _, message := range sortedStoreValues(store.aiMessages, func(value *domainvdoc.AIChatMessage) string {
		return value.CreatedAt.Format(sortableTimeLayout) + ":" + value.ID
	}) {
		if err := repo.UpsertAIChatMessage(ctx, message); err != nil {
			return err
		}
	}
	return nil
}
