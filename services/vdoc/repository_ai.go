package vdoc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	domainai "vdoc/domain/ai"
	"vdoc/domain/documentrepo"
	domain "vdoc/domain/vdoc"
	"vdoc/utils/id"
)

const repositoryAnalysisContextBytes = 128 << 10

func (s *Store) RepositoryAnalyze(actorID, projectID string, input documentrepo.AnalysisInput) (*documentrepo.Analysis, error) {
	input.Message, input.Language = strings.TrimSpace(input.Message), strings.TrimSpace(input.Language)
	if input.ToCommitID == "" || len(input.Message) > 8000 || len(input.Language) > 32 {
		return nil, ErrInvalidArgument
	}
	commit, err := s.RepositoryCommit(actorID, projectID, input.ToCommitID)
	if err != nil {
		return nil, err
	}
	if input.FromCommitID == "" && len(commit.ParentIDs) > 0 {
		input.FromCommitID = commit.ParentIDs[0]
	}
	evidence, truncated, err := s.repositoryAnalysisEvidence(actorID, projectID, commit, input)
	if err != nil {
		return nil, err
	}
	completion, prompt, err := s.repositoryAnalysisCompletion(actorID, projectID, input, evidence)
	if err != nil {
		return nil, err
	}
	fingerprint, err := json.Marshal(struct {
		Input    documentrepo.AnalysisInput
		Evidence string
		Provider *AIProviderConfig
		Prompt   AIPromptTemplate
	}{input, evidence, completion.Provider, prompt})
	if err != nil {
		return nil, err
	}
	analysis := &documentrepo.Analysis{
		ID: id.GenerateID(), ProjectID: projectID, FromCommitID: input.FromCommitID,
		ToCommitID: input.ToCommitID, DocumentID: input.DocumentID, Message: input.Message,
		Language: input.Language, InputFingerprint: sha(string(fingerprint)),
		ProviderID: completion.Provider.ID, Model: completion.Provider.Model,
		Status: "running", EvidenceTruncated: truncated, CreatedBy: actorID, CreatedAt: time.Now().UTC(),
	}
	guard := &aiCompletionGuard{Mutation: domain.MutationGuard{ActorID: actorID, ProjectID: projectID, Permission: MemberRoleReader}, Provider: completion.Provider, Prompt: prompt}
	if err := s.WithRepositoryWrite(actorID, projectID, MemberRoleReader, func(storage documentrepo.Storage) error {
		if tokenID := mcpMutationTokenID(s.requestContext()); tokenID != "" {
			guard.Mutation.TokenID = tokenID
			guard.Mutation.TokenScope = ScopeAPIRead
			if token := s.tokens[tokenID]; token != nil && !slices.Contains(token.Scopes, ScopeAPIRead) {
				guard.Mutation.TokenScope = ScopeDocRead
			}
		}
		return putRepositoryAutomation(s.requestContext(), storage, projectID, "analysis", analysis.ID, analysis, 1, nil)
	}); err != nil {
		return nil, err
	}
	callCtx, callCancel := context.WithTimeout(s.requestContext(), 5*time.Minute)
	defer callCancel()
	result, callErr := s.completeAI(callCtx, completion)
	if cause := context.Cause(s.requestContext()); cause != nil {
		callErr = cause
	}
	// 完成记录使用独立的短时 context，取消请求仍能留下失败状态，不影响内容提交。
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(s.requestContext()), 10*time.Second)
	defer cancel()
	finisher := s.WithContext(finishCtx)
	if err := finisher.repositoryFinishAnalysis(analysis, guard, result.Content, callErr); err != nil {
		return nil, err
	}
	// 状态收尾不依赖请求者仍有权限，返回结果仍必须重新验证当前权限。
	return finisher.RepositoryAnalysis(actorID, projectID, analysis.ID)
}

func (s *Store) repositoryFinishAnalysis(analysis *documentrepo.Analysis, guard *aiCompletionGuard, content string, callErr error) error {
	return s.repositoryAutomationTransaction(analysis.ProjectID, func(database domain.Repository) error {
		if callErr == nil {
			var err error
			if database != nil {
				err = validateAICompletionContext(s.requestContext(), database, guard)
			} else {
				err = domain.ValidateMutationContext(guard.Mutation, s.stateLocked(), time.Now())
				provider, _, configErr := s.effectiveAIProviderLocked(analysis.ProjectID)
				if err == nil && (configErr != nil || !sameAIProviderRequest(provider, guard.Provider) || s.effectivePromptLocked(analysis.ProjectID, guard.Prompt.PromptKey) != guard.Prompt) {
					err = ErrFailedPrecondition
				}
			}
			if err != nil && !isAICompletionContextError(err) && !errors.Is(err, ErrPermissionDenied) && !errors.Is(err, ErrUnauthenticated) && !errors.Is(err, ErrFailedPrecondition) && !errors.Is(err, ErrNotFound) {
				return err
			}
			callErr = err
		}
		return nil
	}, func(storage documentrepo.Storage) error {
		var current documentrepo.Analysis
		record, err := getRepositoryAutomation(s.requestContext(), storage, analysis.ProjectID, "analysis", analysis.ID, &current)
		if err != nil {
			return err
		}
		if current.Status != "running" || current.InputFingerprint != analysis.InputFingerprint || current.CreatedBy != analysis.CreatedBy {
			return ErrFailedPrecondition
		}
		now := time.Now().UTC()
		current.CompletedAt = &now
		if callErr != nil {
			current.Status, current.Content, current.ErrorMessage = "failed", "", repositoryAnalysisError(callErr)
		} else {
			current.Status, current.Content, current.ErrorMessage = "succeeded", content, ""
		}
		return repositoryStoreFinishedAnalysis(s.requestContext(), storage, current, record.Generation)
	})
}

func repositoryStoreFinishedAnalysis(ctx context.Context, storage documentrepo.Storage, value documentrepo.Analysis, generation int64) error {
	if err := putRepositoryAutomation(ctx, storage, value.ProjectID, "analysis", value.ID, value, generation+1, &generation); err != nil {
		return err
	}
	event := documentrepo.Event{ID: id.GenerateID(), ProjectID: value.ProjectID, ActorID: value.CreatedBy, EventType: "ai.analysis." + value.Status, AfterCommitID: value.ToCommitID, BeforeCommitID: value.FromCommitID, CreatedAt: *value.CompletedAt, ResourceID: value.ID}
	return putRepositoryAutomation(ctx, storage, value.ProjectID, "event", event.ID, event, 1, nil)
}

// 仅用于完成已持久化任务和维护密钥；对内容图的用户操作仍走带权限的事务。
func (s *Store) repositoryAutomationTransaction(projectID string, validate func(domain.Repository) error, fn func(documentrepo.Storage) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return err
	}
	if s.persistence == nil {
		if validate != nil {
			if err := validate(nil); err != nil {
				return err
			}
		}
		if s.repoMemory == nil {
			s.repoMemory = newRepositoryMemory()
		}
		next := s.repoMemory.clone()
		if err := fn(next); err != nil {
			return err
		}
		s.repoMemory = next
		return nil
	}
	transaction, ok := s.persistence.repo.(transactionalRepository)
	if !ok {
		return ErrFailedPrecondition
	}
	return transaction.WithinTransaction(s.requestContext(), func(database domain.Repository) error {
		if validate != nil {
			if err := validate(database); err != nil {
				return err
			}
		}
		storage, ok := database.(documentrepo.Storage)
		if !ok {
			return ErrFailedPrecondition
		}
		if err := storage.LockRepository(s.requestContext(), projectID); err != nil {
			return err
		}
		return fn(storage)
	})
}

func (s *Store) repositoryExpireAnalyses(projectID string) error {
	var after int64
	for {
		count := 0
		err := s.repositoryAutomationTransaction(projectID, nil, func(storage documentrepo.Storage) error {
			records, err := storage.RepositoryRecords(s.requestContext(), projectID, "analysis", after, 100)
			if err != nil {
				return err
			}
			count = len(records)
			for _, record := range records {
				after = record.Sequence
				var value documentrepo.Analysis
				if err := json.Unmarshal(record.Payload, &value); err != nil {
					return err
				}
				if value.Status != "running" || time.Since(value.CreatedAt) < 10*time.Minute {
					continue
				}
				now := time.Now().UTC()
				value.Status, value.Content, value.ErrorMessage, value.CompletedAt = "failed", "", "Analysis was interrupted; retry against the same commit IDs.", &now
				if err := repositoryStoreFinishedAnalysis(s.requestContext(), storage, value, record.Generation); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil || count < 100 {
			return err
		}
	}
}

func (s *Store) repositoryAnalysisEvidence(actorID, projectID string, commit *documentrepo.Commit, input documentrepo.AnalysisInput) (string, bool, error) {
	left := map[string]documentrepo.File{}
	if input.FromCommitID != "" {
		before, err := s.RepositoryCommit(actorID, projectID, input.FromCommitID)
		if err != nil {
			return "", false, err
		}
		for _, file := range before.Files {
			left[file.DocumentID] = file
		}
	}
	right := map[string]documentrepo.File{}
	ids := map[string]bool{}
	for _, file := range commit.Files {
		right[file.DocumentID], ids[file.DocumentID] = file, true
	}
	for documentID := range left {
		ids[documentID] = true
	}
	if input.DocumentID != "" && !ids[input.DocumentID] {
		return "", false, ErrNotFound
	}
	keys := make([]string, 0, len(ids))
	for documentID := range ids {
		keys = append(keys, documentID)
	}
	sort.Strings(keys)
	files := []json.RawMessage{}
	remaining, total, truncated := repositoryAnalysisContextBytes-4096, 0, false
	for _, documentID := range keys {
		if input.DocumentID != "" && input.DocumentID != documentID {
			continue
		}
		before, beforeOK := left[documentID]
		after, afterOK := right[documentID]
		if beforeOK == afterOK && documentrepo.EqualFile(before, after) {
			continue
		}
		total++
		if len(files) == 32 || remaining < 8192 {
			truncated = true
			continue
		}
		file := after
		changeType := "modified"
		if !beforeOK {
			changeType = "added"
		} else if !afterOK {
			changeType, file = "deleted", before
		} else if before.RelativePath != after.RelativePath {
			changeType = "renamed"
		}
		evidence := struct {
			DocumentID   string `json:"document_id"`
			RelativePath string `json:"relative_path"`
			OldPath      string `json:"old_relative_path,omitempty"`
			Name         string `json:"name"`
			OldName      string `json:"old_name,omitempty"`
			Description  string `json:"description,omitempty"`
			OldDesc      string `json:"old_description,omitempty"`
			DocumentType int    `json:"document_type"`
			OldType      int    `json:"old_document_type,omitempty"`
			ChangeType   string `json:"change_type"`
			BeforeHash   string `json:"before_content_hash,omitempty"`
			AfterHash    string `json:"after_content_hash,omitempty"`
			Before       string `json:"old_content"`
			After        string `json:"new_content"`
			Truncated    bool   `json:"reference_truncated"`
		}{DocumentID: documentID, RelativePath: file.RelativePath, OldPath: before.RelativePath, Name: file.Name, OldName: before.Name, Description: file.Description, OldDesc: before.Description, DocumentType: file.DocumentType, OldType: before.DocumentType, ChangeType: changeType, BeforeHash: before.ContentHash, AfterHash: after.ContentHash}
		for _, field := range []*string{&evidence.DocumentID, &evidence.RelativePath, &evidence.OldPath, &evidence.Name, &evidence.OldName, &evidence.Description, &evidence.OldDesc} {
			if len(*field) > 1024 {
				*field, evidence.Truncated = strings.ToValidUTF8((*field)[:1024], ""), true
			}
		}
		// JSON 最多把一个字节扩展为六个字符，给两侧正文和元数据预留固定预算。
		limit := min(16<<10, (remaining-4096)/12)
		for i, commitID := range []string{input.FromCommitID, input.ToCommitID} {
			if (i == 0 && !beforeOK) || (i == 1 && !afterOK) {
				continue
			}
			content, err := s.RepositoryContent(actorID, projectID, commitID, documentID)
			if err != nil {
				return "", false, err
			}
			body := content.Content
			if len(body) > limit {
				body, evidence.Truncated = strings.ToValidUTF8(body[:limit], ""), true
			}
			if i == 0 {
				evidence.Before = body
			} else {
				evidence.After = body
			}
		}
		body, err := json.Marshal(evidence)
		if err != nil {
			return "", false, err
		}
		if len(body) > remaining {
			truncated = true
			continue
		}
		files, remaining = append(files, body), remaining-len(body)-1
		truncated = truncated || evidence.Truncated
	}
	body, err := json.Marshal(struct {
		Files        []json.RawMessage `json:"files"`
		ChangedFiles int               `json:"total_changed_files"`
		SourceOnly   bool              `json:"source_evidence"`
	}{files, total, true})
	if err != nil {
		return "", false, err
	}
	return fmt.Sprintf("Fixed source: from_commit_id=%s to_commit_id=%s document_id=%s\nEvidence truncated: %t. Do not infer omitted content is unchanged.\n%s", input.FromCommitID, input.ToCommitID, input.DocumentID, truncated, body), truncated, nil
}

func (s *Store) repositoryAnalysisCompletion(actorID, projectID string, input documentrepo.AnalysisInput, evidence string) (aiCompletionRequest, AIPromptTemplate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return aiCompletionRequest{}, AIPromptTemplate{}, err
	}
	if !s.canReadLocked(actorID, projectID) {
		return aiCompletionRequest{}, AIPromptTemplate{}, ErrPermissionDenied
	}
	if err := s.ensureActiveProjectLocked(projectID); err != nil {
		return aiCompletionRequest{}, AIPromptTemplate{}, err
	}
	provider, apiKey, err := s.effectiveAIProviderLocked(projectID)
	if err != nil {
		return aiCompletionRequest{}, AIPromptTemplate{}, err
	}
	promptKey := domainai.PromptVersionChangeSummary
	if input.FromCommitID != "" {
		promptKey = domainai.PromptDiffChangeSummary
	}
	if input.Message != "" {
		promptKey = domainai.PromptPageChat
	}
	prompt := s.effectivePromptLocked(projectID, promptKey)
	if !prompt.Enabled {
		return aiCompletionRequest{}, AIPromptTemplate{}, ErrFailedPrecondition
	}
	user := strings.NewReplacer("{{context}}", evidence, "{{message}}", input.Message, "{{history}}", "").Replace(prompt.UserPromptTemplate)
	if input.Language != "" {
		user += "\nRequested response language (data): " + input.Language
	}
	return aiCompletionRequest{Provider: cloneAIProvider(provider), APIKey: apiKey, System: prompt.SystemPrompt + "\nCite full commit IDs and document paths supplied in the reference. A commit is immutable but may be unpublished. Do not claim a publication or approval unless supplied. Never change or publish repository contents.", User: user}, prompt, nil
}

func (s *Store) RepositoryAnalysis(actorID, projectID, analysisID string) (*documentrepo.Analysis, error) {
	var value documentrepo.Analysis
	err := s.WithRepositoryRead(actorID, projectID, func(storage documentrepo.Storage) error {
		record, err := getRepositoryAutomation(s.requestContext(), storage, projectID, "analysis", analysisID, &value)
		if err == nil {
			value.Sequence = record.Sequence
		}
		return err
	})
	return &value, err
}

func (s *Store) RepositoryAnalyses(actorID, projectID string, after int64) ([]documentrepo.Analysis, error) {
	if after < 0 {
		return nil, ErrInvalidArgument
	}
	values := []documentrepo.Analysis{}
	err := s.WithRepositoryRead(actorID, projectID, func(storage documentrepo.Storage) error {
		records, err := storage.RepositoryRecords(s.requestContext(), projectID, "analysis", after, 100)
		if err != nil {
			return err
		}
		for _, record := range records {
			var value documentrepo.Analysis
			if err := json.Unmarshal(record.Payload, &value); err != nil {
				return err
			}
			value.Sequence = record.Sequence
			values = append(values, value)
		}
		return nil
	})
	return values, err
}

func getRepositoryAutomation(ctx context.Context, storage documentrepo.Storage, projectID, kind, recordID string, out any) (*documentrepo.Record, error) {
	record, err := storage.RepositoryRecord(ctx, projectID, kind, recordID)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, ErrNotFound
	}
	if err := json.Unmarshal(record.Payload, out); err != nil {
		return nil, err
	}
	return record, nil
}

func putRepositoryAutomation(ctx context.Context, storage documentrepo.Storage, projectID, kind, recordID string, value any, generation int64, expected *int64) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return storage.PutRepositoryRecord(ctx, documentrepo.Record{ProjectID: projectID, Kind: kind, ID: recordID, Generation: generation, Payload: body}, expected)
}

func repositoryAnalysisError(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "Analysis canceled; saved commits are unchanged."
	case errors.Is(err, context.DeadlineExceeded):
		return "Analysis timed out; retry against the same commit IDs."
	case errors.Is(err, ErrPermissionDenied):
		return "Project access changed during analysis."
	default:
		return "Analysis failed or its configuration changed; retry against the same commit IDs."
	}
}
