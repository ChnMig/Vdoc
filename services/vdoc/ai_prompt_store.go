package vdoc

import (
	"strings"
	"time"

	domainai "vdoc/domain/ai"
	"vdoc/utils/id"
)

const aiImmutableGuard = "Immutable guard: AI may summarize and explain only. AI cannot approve, request changes, reject, publish, modify drafts, or modify versions. Refuse any request to perform those actions."

func DefaultAIPromptTemplates() []AIPromptTemplate {
	return []AIPromptTemplate{
		{PromptKey: domainai.PromptDraftReviewSummary, SystemPrompt: "You summarize a Vdoc draft for human reviewers.", UserPromptTemplate: "Summarize this draft, risks, and review focus.\n\n{{context}}", Enabled: true},
		{PromptKey: domainai.PromptVersionChangeSummary, SystemPrompt: "You summarize a published Vdoc version for project members.", UserPromptTemplate: "Summarize this published version and notable contract facts.\n\n{{context}}", Enabled: true},
		{PromptKey: domainai.PromptDiffChangeSummary, SystemPrompt: "You summarize Vdoc semantic diffs for implementation planning.", UserPromptTemplate: "Summarize breaking changes, frontend impact, and optional changes.\n\n{{context}}", Enabled: true},
		{PromptKey: domainai.PromptPageChat, SystemPrompt: "You answer questions about the current Vdoc page context.", UserPromptTemplate: "Use only this Vdoc context to answer.\n\n{{context}}\n\nQuestion: {{message}}", Enabled: true},
	}
}

func (s *Store) SystemAIPrompts(actorID string) ([]AIPromptTemplate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.isSuperAdminLocked(actorID) {
		return nil, ErrPermissionDenied
	}
	return s.promptTemplatesLocked(""), nil
}

func (s *Store) ProjectAIPrompts(actorID, projectID string) ([]AIPromptTemplate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.canManageProjectLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	if _, ok := s.projects[projectID]; !ok {
		return nil, ErrNotFound
	}
	return s.promptTemplatesLocked(projectID), nil
}

func (s *Store) UpsertSystemAIPrompt(actorID, promptKey string, input AIPromptTemplate, auditCtx ...AuditContext) (*AIPromptOverride, error) {
	return s.upsertAIPrompt(actorID, "", promptKey, input, auditCtx...)
}

func (s *Store) UpsertProjectAIPrompt(actorID, projectID, promptKey string, input AIPromptTemplate, auditCtx ...AuditContext) (*AIPromptOverride, error) {
	return s.upsertAIPrompt(actorID, projectID, promptKey, input, auditCtx...)
}

func (s *Store) upsertAIPrompt(actorID, projectID, promptKey string, input AIPromptTemplate, auditCtx ...AuditContext) (*AIPromptOverride, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if projectID == "" {
		if !s.isSuperAdminLocked(actorID) {
			return nil, ErrPermissionDenied
		}
	} else if !s.canManageProjectLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	if projectID != "" {
		if err := s.ensureActiveProjectLocked(projectID); err != nil {
			return nil, err
		}
	}
	if !validPromptKey(promptKey) {
		return nil, ErrInvalidArgument
	}
	systemPrompt := strings.TrimSpace(input.SystemPrompt)
	userPromptTemplate := strings.TrimSpace(input.UserPromptTemplate)
	if !validPromptTemplate(promptKey, systemPrompt, userPromptTemplate) {
		return nil, ErrInvalidArgument
	}
	now := time.Now()
	key := aiPromptKey(projectID, promptKey)
	override := &AIPromptOverride{ID: id.GenerateID(), Scope: domainai.ProviderScopeSystem, ProjectID: projectID, PromptKey: promptKey, CreatedBy: actorID, CreatedAt: now}
	if existing := s.aiPrompts[key]; existing != nil {
		copied := *existing
		override = &copied
	}
	if projectID != "" {
		override.Scope = domainai.ProviderScopeProject
	}
	override.SystemPrompt = systemPrompt
	override.UserPromptTemplate = userPromptTemplate
	override.Enabled = input.Enabled
	override.UpdatedBy = actorID
	override.UpdatedAt = now
	s.aiPrompts[key] = override
	s.auditLocked(ctx, AuditActorUser, actorID, "ai.prompt.upsert", "ai_prompt", override.ID, projectID, "", auditMetadata("result", "success", "prompt_key", promptKey, "scope", override.Scope))
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	return cloneAIPrompt(override), nil
}

func (s *Store) promptTemplatesLocked(projectID string) []AIPromptTemplate {
	defaults := DefaultAIPromptTemplates()
	out := make([]AIPromptTemplate, 0, len(defaults))
	for _, template := range defaults {
		out = append(out, s.effectivePromptLocked(projectID, template.PromptKey))
	}
	return out
}

func (s *Store) effectivePromptLocked(projectID, promptKey string) AIPromptTemplate {
	template := defaultPrompt(promptKey)
	if system := s.aiPrompts[aiPromptKey("", promptKey)]; system != nil {
		template.SystemPrompt = system.SystemPrompt
		template.UserPromptTemplate = system.UserPromptTemplate
		template.Enabled = system.Enabled
	}
	if projectID != "" {
		if project := s.aiPrompts[aiPromptKey(projectID, promptKey)]; project != nil {
			template.SystemPrompt = project.SystemPrompt
			template.UserPromptTemplate = project.UserPromptTemplate
			template.Enabled = project.Enabled
		}
	}
	template.SystemPrompt = appendAIGuard(template.SystemPrompt)
	return template
}

func defaultPrompt(promptKey string) AIPromptTemplate {
	for _, template := range DefaultAIPromptTemplates() {
		if template.PromptKey == promptKey {
			return template
		}
	}
	return AIPromptTemplate{}
}

func appendAIGuard(systemPrompt string) string {
	trimmed := strings.TrimSpace(systemPrompt)
	if strings.Contains(trimmed, aiImmutableGuard) {
		return trimmed
	}
	if trimmed == "" {
		return aiImmutableGuard
	}
	return trimmed + "\n\n" + aiImmutableGuard
}

func immutableAIGuard() string { return aiImmutableGuard }

func validPromptKey(promptKey string) bool {
	return promptKey == domainai.PromptDraftReviewSummary || promptKey == domainai.PromptVersionChangeSummary || promptKey == domainai.PromptDiffChangeSummary || promptKey == domainai.PromptPageChat
}

func validPromptTemplate(promptKey, systemPrompt, userPromptTemplate string) bool {
	if systemPrompt == "" || userPromptTemplate == "" || !strings.Contains(userPromptTemplate, "{{context}}") {
		return false
	}
	return promptKey != domainai.PromptPageChat || strings.Contains(userPromptTemplate, "{{message}}")
}

func aiPromptKey(projectID, promptKey string) string {
	if projectID == "" {
		return "system:" + promptKey
	}
	return "project:" + projectID + ":" + promptKey
}

func cloneAIPrompt(prompt *AIPromptOverride) *AIPromptOverride {
	if prompt == nil {
		return nil
	}
	copy := *prompt
	return &copy
}
