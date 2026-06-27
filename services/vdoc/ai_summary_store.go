package vdoc

import (
	"context"
	"fmt"
	"strings"
	"time"

	domainai "vdoc/domain/ai"
	"vdoc/utils/id"
)

type AISummaryTarget struct {
	ProjectID  string
	DocumentID string
	OwnerType  string
	OwnerID    string
}

func (s *Store) AISummary(actorID string, target AISummaryTarget) (*AISummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if err := s.ensureReadableAITargetLocked(actorID, target); err != nil {
		return nil, err
	}
	return cloneAISummary(s.aiSummaries[aiSummaryKey(target)]), nil
}

func (s *Store) RegenerateAISummary(actorID string, target AISummaryTarget, auditCtx ...AuditContext) (*AISummary, error) {
	request, skipped, err := s.prepareAISummaryRequest(actorID, target)
	if err != nil {
		return nil, err
	}
	if skipped != nil {
		return skipped, nil
	}
	content, callErr := s.completeAI(context.Background(), request.Completion)
	return s.finishAISummary(actorID, target, request, content, callErr, auditCtx...)
}

type aiSummaryRequest struct {
	Provider   *AIProviderConfig
	Prompt     AIPromptTemplate
	Completion aiCompletionRequest
}

func (s *Store) prepareAISummaryRequest(actorID string, target AISummaryTarget) (aiSummaryRequest, *AISummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return aiSummaryRequest{}, nil, err
	}
	if !s.canManageProjectLocked(actorID, target.ProjectID) {
		return aiSummaryRequest{}, nil, ErrPermissionDenied
	}
	contextText, promptKey, err := s.aiTargetContextLocked(target)
	if err != nil {
		return aiSummaryRequest{}, nil, err
	}
	provider, apiKey, err := s.effectiveAIProviderLocked(target.ProjectID)
	if err != nil {
		if Is(err, ErrFailedPrecondition) {
			skipped := s.storeAISummaryLocked(actorID, target, promptKey, "", domainai.SummaryStatusSkipped, "ai provider is not configured", "")
			return aiSummaryRequest{}, cloneAISummary(skipped), s.persistLocked()
		}
		return aiSummaryRequest{}, nil, err
	}
	prompt := s.effectivePromptLocked(target.ProjectID, promptKey)
	if !prompt.Enabled {
		skipped := s.storeAISummaryLocked(actorID, target, promptKey, provider.ID, domainai.SummaryStatusSkipped, "ai prompt is disabled", "")
		return aiSummaryRequest{}, cloneAISummary(skipped), s.persistLocked()
	}
	userPrompt := strings.ReplaceAll(prompt.UserPromptTemplate, "{{context}}", contextText)
	completion := aiCompletionRequest{Provider: cloneAIProvider(provider), APIKey: apiKey, System: prompt.SystemPrompt, User: userPrompt, Temperature: 0.2, MaxTokens: 900}
	return aiSummaryRequest{Provider: cloneAIProvider(provider), Prompt: prompt, Completion: completion}, nil, nil
}

func (s *Store) finishAISummary(actorID string, target AISummaryTarget, request aiSummaryRequest, content string, callErr error, auditCtx ...AuditContext) (*AISummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	status := domainai.SummaryStatusSucceeded
	errorMessage := ""
	if callErr != nil {
		status = domainai.SummaryStatusFailed
		errorMessage = callErr.Error()
	}
	summary := s.storeAISummaryLocked(actorID, target, request.Prompt.PromptKey, request.Provider.ID, status, errorMessage, content)
	s.auditLocked(ctx, AuditActorUser, actorID, "ai.summary.regenerate", "ai_summary", summary.ID, target.ProjectID, target.DocumentID, auditMetadata("result", status, "owner_type", target.OwnerType, "owner_id", target.OwnerID, "prompt_key", request.Prompt.PromptKey))
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	return cloneAISummary(summary), nil
}

func (s *Store) storeAISummaryLocked(actorID string, target AISummaryTarget, promptKey, providerID, status, errorMessage, content string) *AISummary {
	now := time.Now()
	key := aiSummaryKey(target)
	summary := s.aiSummaries[key]
	if summary == nil {
		summary = &AISummary{ID: id.GenerateID(), ProjectID: target.ProjectID, DocumentID: target.DocumentID, OwnerType: target.OwnerType, OwnerID: target.OwnerID}
	}
	summary.PromptKey = promptKey
	summary.ProviderID = providerID
	summary.Status = status
	summary.Content = content
	summary.ErrorMessage = errorMessage
	summary.GeneratedBy = actorID
	summary.GeneratedAt = now
	summary.UpdatedAt = now
	s.aiSummaries[key] = summary
	return summary
}

func (s *Store) ensureReadableAITargetLocked(actorID string, target AISummaryTarget) error {
	if !s.canReadLocked(actorID, target.ProjectID) {
		return ErrPermissionDenied
	}
	_, _, err := s.aiTargetContextLocked(target)
	return err
}

func (s *Store) aiTargetContextLocked(target AISummaryTarget) (string, string, error) {
	switch target.OwnerType {
	case domainai.SummaryOwnerDraft:
		draft, ok := s.draftInProjectServiceLocked(target.ProjectID, target.DocumentID, target.OwnerID)
		if !ok {
			return "", "", ErrNotFound
		}
		return draftAIContext(draft), domainai.PromptDraftReviewSummary, nil
	case domainai.SummaryOwnerVersion:
		version := s.versions[target.OwnerID]
		if version == nil || version.ProjectID != target.ProjectID || version.ServiceID != target.DocumentID {
			return "", "", ErrNotFound
		}
		return versionAIContext(version), domainai.PromptVersionChangeSummary, nil
	case domainai.SummaryOwnerDiff:
		diff := s.diffs[target.OwnerID]
		if diff == nil || diff.ServiceID != target.DocumentID || !s.serviceInProjectLocked(target.DocumentID, target.ProjectID) {
			return "", "", ErrNotFound
		}
		return diffAIContext(diff), domainai.PromptDiffChangeSummary, nil
	default:
		return "", "", ErrInvalidArgument
	}
}

func draftAIContext(draft *ContractDraft) string {
	return fmt.Sprintf("Draft %s version %s status %d changelog: %s\nContent:\n%s", draft.ID, draft.VersionName, draft.Status, draft.Changelog, limitAIText(draft.NormalizedSchema))
}

func versionAIContext(version *ContractVersion) string {
	return fmt.Sprintf("Version %s name %s changelog: %s\nContent:\n%s", version.ID, version.VersionName, version.Changelog, limitAIText(version.NormalizedSchema))
}

func diffAIContext(diff *Diff) string {
	parts := []string{fmt.Sprintf("Diff %s from %s to %s. Added=%d removed=%d modified=%d breaking=%d.", diff.ID, diff.FromVersionID, diff.ToVersionID, diff.Summary.AddedEndpoints, diff.Summary.RemovedEndpoints, diff.Summary.ModifiedEndpoints, diff.Summary.BreakingChanges)}
	for _, item := range diff.Items {
		parts = append(parts, fmt.Sprintf("- %s %s %s breaking=%t: %s", item.Method, item.Path, item.Location, item.IsBreaking, item.Message))
	}
	return limitAIText(strings.Join(parts, "\n"))
}

func limitAIText(value string) string {
	const maxRunes = 12000
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes])
}

func aiSummaryKey(target AISummaryTarget) string {
	return target.ProjectID + ":" + target.DocumentID + ":" + target.OwnerType + ":" + target.OwnerID
}

func cloneAISummary(summary *AISummary) *AISummary {
	if summary == nil {
		return nil
	}
	copy := *summary
	return &copy
}
