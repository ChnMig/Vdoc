package vdoc

import (
	"context"
	"errors"

	domainai "vdoc/domain/ai"
	domain "vdoc/domain/vdoc"
)

type aiCompletionGuard struct {
	Mutation domain.MutationGuard
	Provider *AIProviderConfig
	Prompt   AIPromptTemplate
}

type aiCompletionConfigurationRepository interface {
	LockAICompletionConfiguration(context.Context, string, string) (*domain.State, error)
}

type aiCompletionContextError struct{ cause error }

func (s *Store) withAIConfigurationMutationGuard(actorID, projectID string) *Store {
	permission := MemberRoleAdmin
	if projectID == "" {
		permission = domain.MutationPermissionSuperAdmin
	}
	return s.withMutationGuard(actorID, projectID, "", permission)
}

func (e *aiCompletionContextError) Error() string { return e.cause.Error() }
func (e *aiCompletionContextError) Unwrap() error { return e.cause }

func isAICompletionContextError(err error) bool {
	var stale *aiCompletionContextError
	return errors.As(err, &stale)
}

func isAICancellation(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func (s *Store) aiCompletionGuardLocked(actorID string, target AISummaryTarget, permission int, provider *AIProviderConfig, prompt AIPromptTemplate) (*aiCompletionGuard, error) {
	guarded, err := s.withAITargetMutationGuardLocked(actorID, target, permission)
	if err != nil {
		return nil, err
	}
	return &aiCompletionGuard{Mutation: *guarded.mutationGuard, Provider: cloneAIProvider(provider), Prompt: prompt}, nil
}

func (s *Store) withAITargetMutationGuardLocked(actorID string, target AISummaryTarget, permission int) (*Store, error) {
	branchID, err := s.aiTargetBranchLocked(target)
	if err != nil {
		return nil, err
	}
	guarded := s.withMutationGuard(actorID, target.ProjectID, target.DocumentID, permission, branchID)
	if target.OwnerType == domainai.SummaryOwnerDraft {
		guarded.mutationGuard.DraftID = target.OwnerID
		guarded.mutationGuard.ExpectedDraftRevision = s.drafts[target.OwnerID].Revision()
	}
	return guarded, nil
}

func validateAICompletionContext(ctx context.Context, repository domain.Repository, guard *aiCompletionGuard) error {
	if guard == nil {
		return nil
	}
	if err := validateRepositoryMutation(ctx, repository, &guard.Mutation); err != nil {
		if Is(err, ErrPermissionDenied) || Is(err, ErrFailedPrecondition) || Is(err, ErrNotFound) || Is(err, ErrUnauthenticated) {
			return &aiCompletionContextError{cause: err}
		}
		return err
	}
	if guard.Provider == nil && guard.Prompt.PromptKey == "" {
		return nil
	}
	repo, ok := repository.(aiCompletionConfigurationRepository)
	if !ok {
		return nil
	}
	state, err := repo.LockAICompletionConfiguration(ctx, guard.Mutation.ProjectID, guard.Prompt.PromptKey)
	if err != nil {
		return err
	}
	if state == nil {
		return &aiCompletionContextError{cause: ErrFailedPrecondition}
	}
	provider := state.AIProviders[projectAIProviderKey(guard.Mutation.ProjectID)]
	if provider == nil || !provider.Enabled {
		provider = state.AIProviders[systemAIProviderKey()]
	}
	config := &Store{storeState: &storeState{aiPrompts: state.AIPrompts}}
	if !sameAIProviderRequest(provider, guard.Provider) || config.effectivePromptLocked(guard.Mutation.ProjectID, guard.Prompt.PromptKey) != guard.Prompt {
		return &aiCompletionContextError{cause: ErrFailedPrecondition}
	}
	return nil
}
