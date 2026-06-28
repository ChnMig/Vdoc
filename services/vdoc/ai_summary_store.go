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
	run := aiSummaryRun{ActorID: actorID, Target: target, Trigger: aiSummaryTriggerManual, RequireManage: true, Audit: auditContext(auditCtx)}
	return s.runAISummary(run)
}

type aiSummaryTrigger string

const (
	aiSummaryTriggerManual         aiSummaryTrigger = "manual"
	aiSummaryTriggerDraftSubmit    aiSummaryTrigger = "draft_submit"
	aiSummaryTriggerVersionPublish aiSummaryTrigger = "version_publish"
)

type aiSummaryRun struct {
	ActorID       string
	Target        AISummaryTarget
	Trigger       aiSummaryTrigger
	RequireManage bool
	Audit         AuditContext
}

func (s *Store) regenerateAISummaryForWorkflow(run aiSummaryRun) {
	_, _ = s.runAISummary(run)
}

func (s *Store) runAISummary(run aiSummaryRun) (*AISummary, error) {
	request, skipped, err := s.prepareAISummaryRequest(run)
	if err != nil {
		return nil, err
	}
	if skipped != nil {
		return skipped, nil
	}
	result, callErr := s.completeAI(context.Background(), request.Completion)
	return s.finishAISummary(run, aiSummaryCompletion{Request: request, Result: result, Err: callErr})
}

type aiSummaryRequest struct {
	Provider   *AIProviderConfig
	Prompt     AIPromptTemplate
	Completion aiCompletionRequest
}

type aiSummaryCompletion struct {
	Request aiSummaryRequest
	Result  aiCompletionResult
	Err     error
}

type aiSummarySkip struct {
	PromptKey    string
	ProviderID   string
	APIMode      string
	ErrorMessage string
}

type aiSummaryAuditInput struct {
	Summary    *AISummary
	PromptKey  string
	ProviderID string
	APIMode    string
	Status     string
	Usage      aiTokenUsage
}

func (s *Store) prepareAISummaryRequest(run aiSummaryRun) (aiSummaryRequest, *AISummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return aiSummaryRequest{}, nil, err
	}
	if run.RequireManage && !s.canManageProjectLocked(run.ActorID, run.Target.ProjectID) {
		return aiSummaryRequest{}, nil, ErrPermissionDenied
	}
	contextText, promptKey, err := s.aiTargetContextLocked(run.Target)
	if err != nil {
		return aiSummaryRequest{}, nil, err
	}
	provider, apiKey, err := s.effectiveAIProviderLocked(run.Target.ProjectID)
	if err != nil {
		if Is(err, ErrFailedPrecondition) {
			skipped, audit := s.storeSkippedAISummaryLocked(run, aiSummarySkip{PromptKey: promptKey, ErrorMessage: "ai provider is not configured"})
			return aiSummaryRequest{}, cloneAISummary(skipped), s.persistAISummaryLocked(skipped, audit)
		}
		return aiSummaryRequest{}, nil, err
	}
	prompt := s.effectivePromptLocked(run.Target.ProjectID, promptKey)
	if !prompt.Enabled {
		skipped, audit := s.storeSkippedAISummaryLocked(run, aiSummarySkip{PromptKey: promptKey, ProviderID: provider.ID, APIMode: provider.APIMode, ErrorMessage: "ai prompt is disabled"})
		return aiSummaryRequest{}, cloneAISummary(skipped), s.persistAISummaryLocked(skipped, audit)
	}
	userPrompt := strings.ReplaceAll(prompt.UserPromptTemplate, "{{context}}", contextText)
	completion := aiCompletionRequest{Provider: cloneAIProvider(provider), APIKey: apiKey, System: prompt.SystemPrompt, User: userPrompt}
	return aiSummaryRequest{Provider: cloneAIProvider(provider), Prompt: prompt, Completion: completion}, nil, nil
}

func (s *Store) finishAISummary(run aiSummaryRun, completion aiSummaryCompletion) (*AISummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	status := domainai.SummaryStatusSucceeded
	errorMessage := ""
	if completion.Err != nil {
		status = domainai.SummaryStatusFailed
		errorMessage = completion.Err.Error()
	}
	summary := s.storeAISummaryLocked(run.ActorID, run.Target, completion.Request.Prompt.PromptKey, completion.Request.Provider.ID, status, errorMessage, completion.Result.Content)
	audit := s.auditAISummaryLocked(run, aiSummaryAuditInput{Summary: summary, PromptKey: completion.Request.Prompt.PromptKey, ProviderID: completion.Request.Provider.ID, APIMode: completion.Request.Provider.APIMode, Status: status, Usage: completion.Result.Usage})
	if err := s.persistAISummaryLocked(summary, audit); err != nil {
		return nil, err
	}
	return cloneAISummary(summary), nil
}

func (s *Store) storeSkippedAISummaryLocked(run aiSummaryRun, skipped aiSummarySkip) (*AISummary, *AuditLog) {
	summary := s.storeAISummaryLocked(run.ActorID, run.Target, skipped.PromptKey, skipped.ProviderID, domainai.SummaryStatusSkipped, skipped.ErrorMessage, "")
	audit := s.auditAISummaryLocked(run, aiSummaryAuditInput{Summary: summary, PromptKey: skipped.PromptKey, ProviderID: skipped.ProviderID, APIMode: skipped.APIMode, Status: domainai.SummaryStatusSkipped})
	return summary, audit
}

func (s *Store) auditAISummaryLocked(run aiSummaryRun, input aiSummaryAuditInput) *AuditLog {
	metadata := auditMetadata("result", input.Status, "trigger", string(run.Trigger), "owner_type", run.Target.OwnerType, "owner_id", run.Target.OwnerID, "prompt_key", input.PromptKey, "provider_id", input.ProviderID, "api_mode", input.APIMode)
	addTokenUsageMetadata(metadata, input.Usage)
	if s.audits == nil {
		s.audits = map[string]*AuditLog{}
	}
	return appendAuditToState(s.audits, run.Audit, AuditActorUser, run.ActorID, "ai.summary.regenerate", "ai_summary", input.Summary.ID, run.Target.ProjectID, run.Target.DocumentID, metadata)
}

func (s *Store) persistAISummaryLocked(summary *AISummary, audit *AuditLog) error {
	if s.persistence == nil {
		return nil
	}
	ctx := context.Background()
	persisted, err := s.persistence.saveAISummaryLocked(ctx, summary, audit)
	if err != nil {
		return err
	}
	if !persisted {
		return nil
	}
	return s.persistence.load(ctx, s)
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
