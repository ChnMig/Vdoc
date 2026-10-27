package vdoc

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	domainai "vdoc/domain/ai"
)

func (s *Store) ensureReadableAIChatTargetLocked(actorID string, target AISummaryTarget) error {
	if target.OwnerType != domainai.ChatContextDocument {
		return s.ensureReadableAITargetLocked(actorID, target)
	}
	if !s.canReadLocked(actorID, target.ProjectID) {
		return ErrPermissionDenied
	}
	if target.DocumentID == "" || target.OwnerID != target.DocumentID {
		return ErrInvalidArgument
	}
	if !s.serviceInProjectLocked(target.DocumentID, target.ProjectID) {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ensureActiveAIChatTargetLocked(target AISummaryTarget) error {
	if target.OwnerType == domainai.ChatContextDocument {
		if target.DocumentID == "" || target.OwnerID != target.DocumentID {
			return ErrInvalidArgument
		}
		return s.ensureActiveDocumentContextLocked(target.ProjectID, target.DocumentID)
	}
	return s.ensureActiveAITargetLocked(target)
}

func (s *Store) aiChatTargetContextLocked(target AISummaryTarget, question string) (string, string, error) {
	if target.OwnerType != domainai.ChatContextDocument {
		return s.aiTargetContextLocked(target)
	}
	text, err := s.documentAIChatContextLocked(target, question)
	return text, domainai.PromptPageChat, err
}

type aiDocumentSource struct {
	Kind         string    `json:"kind"`
	ID           string    `json:"id"`
	BranchID     string    `json:"branch_id"`
	Branch       string    `json:"branch"`
	Name         string    `json:"revision"`
	Status       int       `json:"status"`
	BranchStatus int       `json:"branch_status"`
	Changelog    string    `json:"changelog,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
	score        int
}

// 每轮在同一文档的全部来源元数据中选取相关正文。上限在提示中明示，不能把检索片段冒充完整文档。
func (s *Store) documentAIChatContextLocked(target AISummaryTarget, question string) (string, error) {
	const maxSources = 16
	const catalogBudget = 12_000
	const contentBudget = 48_000
	document := s.apiServices[target.DocumentID]
	if document == nil || document.ProjectID != target.ProjectID {
		return "", ErrNotFound
	}
	sources := make([]aiDocumentSource, 0)
	appendSource := func(kind, sourceID, branchID, name, changelog string, status int, updated time.Time) {
		branch := s.branches[branchID]
		if branch == nil || branch.ServiceID != target.DocumentID {
			return
		}
		sources = append(sources, aiDocumentSource{Kind: kind, ID: sourceID, BranchID: branchID, Branch: branch.Name, Name: name, Status: status, BranchStatus: branch.Status, Changelog: aiBoundText(changelog, 240), UpdatedAt: updated})
	}
	for _, version := range s.versions {
		if version.ProjectID == target.ProjectID && version.ServiceID == target.DocumentID {
			appendSource("published", version.ID, version.BranchID, version.VersionName, version.Changelog, version.Status, version.PublishedAt)
		}
	}
	for _, draft := range s.drafts {
		if draft.ProjectID == target.ProjectID && draft.ServiceID == target.DocumentID {
			appendSource("draft", draft.ID, draft.BranchID, draft.VersionName, draft.Changelog, draft.Status, draft.UpdatedAt)
		}
	}
	sort.Slice(sources, func(i, j int) bool {
		if sources[i].UpdatedAt.Equal(sources[j].UpdatedAt) {
			return sources[i].ID < sources[j].ID
		}
		return sources[i].UpdatedAt.After(sources[j].UpdatedAt)
	})
	terms := aiQuestionTerms(question)
	lowerQuestion := strings.ToLower(question)
	seen := map[string]bool{}
	for i := range sources {
		source := &sources[i]
		group := source.BranchID + ":" + source.Kind
		if !seen[group] {
			source.score += 10
			seen[group] = true
		}
		metadata := strings.ToLower(source.Branch + " " + source.Name + " " + source.ID + " " + source.Changelog)
		for _, term := range terms {
			if strings.Contains(metadata, term) {
				source.score += 100
			}
		}
		if source.Name != "" && strings.Contains(lowerQuestion, strings.ToLower(source.Name)) {
			source.score += 300
		}
		if source.Branch != "" && strings.Contains(lowerQuestion, strings.ToLower(source.Branch)) {
			source.score += 150
		}
		if strings.Contains(lowerQuestion, strings.ToLower(source.ID)) {
			source.score += 1000
		}
	}
	sort.SliceStable(sources, func(i, j int) bool { return sources[i].score > sources[j].score })
	parts := []string{fmt.Sprintf("Document-wide Q&A. Document %q id=%s project=%s. All %d published revisions and drafts are eligible across this document's branches (including archived branch history). Bodies are retrieved by branch/revision metadata relevance and recency; the catalog alone is not body evidence. Ask for a specific revision if needed. Draft status: 1=editing, 2=submitted, 3=changes_requested, 4=rejected, 5=published.\nSOURCE CATALOG:", aiBoundText(document.Name, 200), target.DocumentID, target.ProjectID, len(sources))}
	catalogRunes, catalogCount := 0, 0
	for _, source := range sources {
		encoded, _ := json.Marshal(source)
		if catalogRunes+len([]rune(string(encoded))) > catalogBudget {
			break
		}
		parts = append(parts, string(encoded))
		catalogRunes += len([]rune(string(encoded)))
		catalogCount++
	}
	if catalogCount < len(sources) {
		parts = append(parts, fmt.Sprintf("[Catalog budget: %d other sources are not listed. Specify a branch, revision name or ID to retrieve them.]", len(sources)-catalogCount))
	}
	count := min(len(sources), maxSources)
	parts = append(parts, fmt.Sprintf("\nRETRIEVED BODIES: %d of %d sources. Each excerpt is bounded; omitted content is unavailable to this answer.", count, len(sources)))
	for _, source := range sources[:count] {
		var content string
		if source.Kind == "draft" {
			draft := cloneDraft(s.drafts[source.ID])
			if err := s.hydrateDraftContentLocked(s.requestContext(), draft, "normalized"); err != nil {
				return "", err
			}
			content = draft.NormalizedSchema
		} else {
			version := cloneVersion(s.versions[source.ID])
			if err := s.hydrateVersionContentLocked(s.requestContext(), version, "normalized"); err != nil {
				return "", err
			}
			content = version.NormalizedSchema
		}
		encoded, _ := json.Marshal(source)
		parts = append(parts, "\nSOURCE "+string(encoded)+"\n"+aiDocumentExcerpt(content, terms, contentBudget/max(1, count))+"\nEND SOURCE")
	}
	return strings.Join(parts, "\n"), nil
}

func aiQuestionTerms(question string) []string {
	words := strings.FieldsFunc(strings.ToLower(question), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_' && r != '.' && r != '/'
	})
	terms, seen := make([]string, 0, len(words)), map[string]bool{}
	for _, term := range words {
		switch term {
		case "a", "an", "the", "is", "are", "was", "be", "to", "of", "for", "in", "on", "and", "or", "with", "what", "how", "which", "does", "do", "this", "that", "document", "explain", "compare", "all", "about", "please":
			continue
		}
		if !seen[term] {
			terms = append(terms, term)
			seen[term] = true
		}
	}
	return terms
}

func aiBoundText(text string, budget int) string {
	runes := []rune(text)
	if len(runes) <= budget {
		return text
	}
	return string(runes[:budget]) + " [truncated]"
}

func aiDocumentExcerpt(content string, terms []string, budget int) string {
	runes := []rune(content)
	if len(runes) <= budget {
		return "[Full body]\n" + content
	}
	// 优先显示问题命中的正文窗口；没有命中时从开头截取，并明示省略范围。
	start := 0
	lower := strings.ToLower(content)
	for _, term := range terms {
		if len([]rune(term)) < 2 {
			continue
		}
		if offset := strings.Index(lower, term); offset >= 0 {
			start = max(0, len([]rune(lower[:offset]))-budget/4)
			break
		}
	}
	start = min(start, len(runes)-budget)
	return fmt.Sprintf("[Excerpt characters %d–%d of %d; other characters omitted]\n%s", start+1, start+budget, len(runes), string(runes[start:start+budget]))
}
