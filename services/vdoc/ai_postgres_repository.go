package vdoc

import (
	"context"
	"time"

	domainvdoc "vdoc/domain/vdoc"
)

type aiMutationRepository interface {
	UpsertAIProvider(ctx context.Context, provider *domainvdoc.AIProviderConfig) error
	UpsertAIPrompt(ctx context.Context, prompt *domainvdoc.AIPromptOverride) error
	UpsertAISummary(ctx context.Context, summary *domainvdoc.AISummary) error
	UpsertAIChatSession(ctx context.Context, session *domainvdoc.AIChatSession) error
	UpsertAIChatMessage(ctx context.Context, message *domainvdoc.AIChatMessage) error
}

type optimisticAIMutationRepository interface {
	UpsertAIProviderIfUnchanged(ctx context.Context, provider, previous *domainvdoc.AIProviderConfig) error
	UpsertAIPromptIfUnchanged(ctx context.Context, prompt, previous *domainvdoc.AIPromptOverride) error
	UpsertAISummaryIfUnchanged(ctx context.Context, summary, previous *domainvdoc.AISummary) error
	UpsertAIChatSessionIfUnchanged(ctx context.Context, session, previous *domainvdoc.AIChatSession) error
	InsertAIChatMessageIfAbsent(ctx context.Context, message *domainvdoc.AIChatMessage) error
}

func aiSummaryStateKey(summary *domainvdoc.AISummary) string {
	if summary == nil {
		return ""
	}
	return aiSummaryKey(AISummaryTarget{ProjectID: summary.ProjectID, DocumentID: summary.DocumentID, OwnerType: summary.OwnerType, OwnerID: summary.OwnerID})
}

type aiGenerationRepository interface {
	aiMutationRepository
	ReserveAISummaryGeneration(ctx context.Context, summary *domainvdoc.AISummary) (*domainvdoc.AISummary, error)
	CompleteAISummaryGeneration(ctx context.Context, summary *domainvdoc.AISummary, expectedToken string) (bool, error)
	ReserveAIChatGeneration(ctx context.Context, sessionID, token string, startedAt time.Time) (bool, error)
	CompleteAIChatGeneration(ctx context.Context, sessionID, expectedToken string, updatedAt *time.Time) (bool, error)
}

func (p *postgresPersistence) reserveAISummaryGenerationLocked(ctx context.Context, summary *domainvdoc.AISummary) (*domainvdoc.AISummary, bool, error) {
	if _, ok := p.repo.(aiGenerationRepository); !ok {
		return nil, false, nil
	}
	var reserved *domainvdoc.AISummary
	save := func(repository domainvdoc.Repository) error {
		repo, ok := repository.(aiGenerationRepository)
		if !ok {
			return domainvdoc.ErrFailedPrecondition
		}
		var err error
		reserved, err = repo.ReserveAISummaryGeneration(ctx, summary)
		return err
	}
	if repo, ok := p.repo.(transactionalRepository); ok {
		err := repo.WithinTransaction(ctx, save)
		return reserved, true, err
	}
	err := save(p.repo)
	return reserved, true, err
}

func (p *postgresPersistence) completeAISummaryGenerationLocked(ctx context.Context, summary *domainvdoc.AISummary, expectedToken string, audit *domainvdoc.AuditLog) (bool, error) {
	if _, ok := p.repo.(aiGenerationRepository); !ok {
		return false, nil
	}
	save := func(repository domainvdoc.Repository) error {
		repo, ok := repository.(aiGenerationRepository)
		if !ok {
			return domainvdoc.ErrFailedPrecondition
		}
		updated, err := repo.CompleteAISummaryGeneration(ctx, summary, expectedToken)
		if err != nil {
			return err
		}
		if !updated {
			return domainvdoc.ErrFailedPrecondition
		}
		if audit == nil {
			return nil
		}
		return repository.RecordAudit(ctx, audit)
	}
	if repo, ok := p.repo.(transactionalRepository); ok {
		return true, repo.WithinTransaction(ctx, save)
	}
	return true, save(p.repo)
}

func (p *postgresPersistence) reserveAIChatGenerationLocked(ctx context.Context, sessionID, token string, startedAt time.Time) (bool, error) {
	if _, ok := p.repo.(aiGenerationRepository); !ok {
		return false, nil
	}
	save := func(repository domainvdoc.Repository) error {
		repo, ok := repository.(aiGenerationRepository)
		if !ok {
			return domainvdoc.ErrFailedPrecondition
		}
		updated, err := repo.ReserveAIChatGeneration(ctx, sessionID, token, startedAt)
		if err != nil {
			return err
		}
		if !updated {
			return domainvdoc.ErrNotFound
		}
		return nil
	}
	if repo, ok := p.repo.(transactionalRepository); ok {
		return true, repo.WithinTransaction(ctx, save)
	}
	return true, save(p.repo)
}

func (p *postgresPersistence) completeAIChatGenerationLocked(ctx context.Context, sessionID, expectedToken string, updatedAt *time.Time, userMessage, assistantMessage *domainvdoc.AIChatMessage, audit *domainvdoc.AuditLog) (bool, error) {
	if _, ok := p.repo.(aiGenerationRepository); !ok {
		return false, nil
	}
	save := func(repository domainvdoc.Repository) error {
		repo, ok := repository.(aiGenerationRepository)
		if !ok {
			return domainvdoc.ErrFailedPrecondition
		}
		updated, err := repo.CompleteAIChatGeneration(ctx, sessionID, expectedToken, updatedAt)
		if err != nil {
			return err
		}
		if !updated {
			return domainvdoc.ErrFailedPrecondition
		}
		optimisticRepo, supportsOptimisticWrites := repository.(optimisticAIMutationRepository)
		if userMessage != nil {
			var err error
			if supportsOptimisticWrites {
				err = optimisticRepo.InsertAIChatMessageIfAbsent(ctx, userMessage)
			} else {
				err = repo.UpsertAIChatMessage(ctx, userMessage)
			}
			if err != nil {
				return err
			}
		}
		if assistantMessage != nil {
			var err error
			if supportsOptimisticWrites {
				err = optimisticRepo.InsertAIChatMessageIfAbsent(ctx, assistantMessage)
			} else {
				err = repo.UpsertAIChatMessage(ctx, assistantMessage)
			}
			if err != nil {
				return err
			}
		}
		if audit == nil {
			return nil
		}
		return repository.RecordAudit(ctx, audit)
	}
	if repo, ok := p.repo.(transactionalRepository); ok {
		return true, repo.WithinTransaction(ctx, save)
	}
	return true, save(p.repo)
}

func (p *postgresPersistence) saveAISummaryLocked(ctx context.Context, summary, previous *domainvdoc.AISummary, audit *domainvdoc.AuditLog) (bool, error) {
	if _, ok := p.repo.(aiMutationRepository); !ok {
		return false, nil
	}
	save := func(repository domainvdoc.Repository) error {
		repo, ok := repository.(aiMutationRepository)
		if !ok {
			return domainvdoc.ErrFailedPrecondition
		}
		var err error
		if optimisticRepo, ok := repository.(optimisticAIMutationRepository); ok {
			err = optimisticRepo.UpsertAISummaryIfUnchanged(ctx, summary, previous)
		} else {
			err = repo.UpsertAISummary(ctx, summary)
		}
		if err != nil {
			return err
		}
		if audit == nil {
			return nil
		}
		return repository.RecordAudit(ctx, audit)
	}
	if repo, ok := p.repo.(transactionalRepository); ok {
		return true, repo.WithinTransaction(ctx, save)
	}
	return true, save(p.repo)
}

func (p *postgresPersistence) saveAIStateLocked(ctx context.Context, store *Store, repo aiMutationRepository) error {
	var persistedProviders map[string]*domainvdoc.AIProviderConfig
	var persistedPrompts map[string]*domainvdoc.AIPromptOverride
	var persistedSummaries map[string]*domainvdoc.AISummary
	var persistedChats map[string]*domainvdoc.AIChatSession
	var persistedMessages map[string]*domainvdoc.AIChatMessage
	if store.persisted != nil {
		persistedProviders = store.persisted.AIProviders
		persistedPrompts = store.persisted.AIPrompts
		persistedSummaries = store.persisted.AISummaries
		persistedChats = store.persisted.AIChats
		persistedMessages = store.persisted.AIMessages
	}
	optimisticRepo, supportsOptimisticWrites := repo.(optimisticAIMutationRepository)
	for _, provider := range changedStoreValues(store.aiProviders, persistedProviders, func(value *domainvdoc.AIProviderConfig) string { return value.ID }) {
		var err error
		if supportsOptimisticWrites {
			err = optimisticRepo.UpsertAIProviderIfUnchanged(ctx, provider, persistedProviders[aiProviderKey(provider.ProjectID)])
		} else {
			err = repo.UpsertAIProvider(ctx, provider)
		}
		if err != nil {
			return err
		}
	}
	for _, prompt := range changedStoreValues(store.aiPrompts, persistedPrompts, func(value *domainvdoc.AIPromptOverride) string { return value.ID }) {
		var err error
		if supportsOptimisticWrites {
			err = optimisticRepo.UpsertAIPromptIfUnchanged(ctx, prompt, persistedPrompts[aiPromptKey(prompt.ProjectID, prompt.PromptKey)])
		} else {
			err = repo.UpsertAIPrompt(ctx, prompt)
		}
		if err != nil {
			return err
		}
	}
	for _, summary := range changedStoreValues(store.aiSummaries, persistedSummaries, func(value *domainvdoc.AISummary) string { return value.ID }) {
		var err error
		if supportsOptimisticWrites {
			err = optimisticRepo.UpsertAISummaryIfUnchanged(ctx, summary, persistedSummaries[aiSummaryStateKey(summary)])
		} else {
			err = repo.UpsertAISummary(ctx, summary)
		}
		if err != nil {
			return err
		}
	}
	for _, session := range changedStoreValues(store.aiChats, persistedChats, func(value *domainvdoc.AIChatSession) string { return value.ID }) {
		var err error
		if supportsOptimisticWrites {
			err = optimisticRepo.UpsertAIChatSessionIfUnchanged(ctx, session, persistedChats[session.ID])
		} else {
			err = repo.UpsertAIChatSession(ctx, session)
		}
		if err != nil {
			return err
		}
	}
	for _, message := range changedStoreValues(store.aiMessages, persistedMessages, func(value *domainvdoc.AIChatMessage) string {
		return value.CreatedAt.Format(sortableTimeLayout) + ":" + value.ID
	}) {
		var err error
		if supportsOptimisticWrites {
			err = optimisticRepo.InsertAIChatMessageIfAbsent(ctx, message)
		} else {
			err = repo.UpsertAIChatMessage(ctx, message)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
