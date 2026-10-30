package mcp

import (
	"encoding/json"
	"strings"
	app "vdoc/appstore"
	"vdoc/domain/documentrepo"
)

type repositoryProjectArgs struct {
	ProjectID string `json:"project_id"`
}

func repositoryReadScope(scopes []int, documentType int) bool {
	switch documentType {
	case app.DocumentTypeOpenAPI:
		return hasScope(scopes, app.ScopeAPIRead)
	case app.DocumentTypeMarkdown, app.DocumentTypeText:
		return hasScope(scopes, app.ScopeDocRead)
	default:
		return false
	}
}

func repositoryWriteScope(scopes []int, documentType int) bool {
	switch documentType {
	case app.DocumentTypeOpenAPI:
		return hasScope(scopes, app.ScopeAPIDraft)
	case app.DocumentTypeMarkdown, app.DocumentTypeText:
		return hasScope(scopes, app.ScopeDocDraft)
	default:
		return false
	}
}

func repositoryFindBranch(store *app.Store, actorID, projectID, branchID string) (*documentrepo.Branch, error) {
	branches, err := store.RepositoryBranches(actorID, projectID)
	if err != nil {
		return nil, err
	}
	for _, branch := range branches {
		if branch.ID == branchID {
			return &branch, nil
		}
	}
	return nil, app.ErrNotFound
}

func repositoryFindRelease(store *app.Store, actorID, projectID, releaseID string) (*documentrepo.Release, error) {
	releases, err := store.RepositoryReleases(actorID, projectID)
	if err != nil {
		return nil, err
	}
	for _, release := range releases {
		if release.ID == releaseID {
			return &release, nil
		}
	}
	return nil, app.ErrNotFound
}

func executeRepositoryTool(store *app.Store, actorID string, scopes []int, tool string, raw json.RawMessage) (any, error) {
	write := tool == "create_branch" || tool == "commit_changes" || tool == "create_merge_request"
	if write && !hasAnyScope(scopes, app.ScopeAPIDraft, app.ScopeDocDraft) || !write && !hasAnyScope(scopes, app.ScopeAPIRead, app.ScopeDocRead) {
		return nil, app.ErrPermissionDenied
	}
	if err := validateRepositoryArguments(tool, raw); err != nil {
		return nil, err
	}
	switch tool {
	case "resolve_ref":
		var args struct {
			ProjectID string `json:"project_id"`
			BranchID  string `json:"branch_id"`
			CommitID  string `json:"commit_id"`
			ReleaseID string `json:"release_id"`
		}
		if err := decodeArguments(raw, &args); err != nil {
			return nil, err
		}
		if err := requireNonEmpty(field("project_id", args.ProjectID)); err != nil {
			return nil, err
		}
		count := 0
		for _, id := range []string{args.BranchID, args.CommitID, args.ReleaseID} {
			if strings.TrimSpace(id) != "" {
				count++
			}
		}
		if count != 1 {
			return nil, invalidArgument("supply exactly one branch_id, commit_id or release_id")
		}
		result := mcpRepositoryRef{Kind: "commit"}
		if args.BranchID != "" {
			branch, err := repositoryFindBranch(store, actorID, args.ProjectID, args.BranchID)
			if err != nil {
				return nil, err
			}
			value := repositoryBranchDTO(*branch)
			result.Branch = &value
			result.Kind = "branch"
			args.CommitID = branch.HeadCommitID
		}
		if args.ReleaseID != "" {
			release, err := repositoryFindRelease(store, actorID, args.ProjectID, args.ReleaseID)
			if err != nil {
				return nil, err
			}
			value := repositoryReleaseDTO(*release)
			result.Release = &value
			result.Kind = "release"
			args.CommitID = release.CommitID
		}
		commit, err := store.RepositoryCommit(actorID, args.ProjectID, args.CommitID)
		if err != nil {
			return nil, err
		}
		result.Commit = repositoryCommitDTO(commit, scopes)
		return result, nil
	case "list_branches", "list_releases", "list_merge_requests":
		var args repositoryProjectArgs
		if err := decodeArguments(raw, &args); err != nil {
			return nil, err
		}
		if err := requireNonEmpty(field("project_id", args.ProjectID)); err != nil {
			return nil, err
		}
		switch tool {
		case "list_branches":
			items, err := store.RepositoryBranches(actorID, args.ProjectID)
			if err != nil {
				return nil, err
			}
			result := make([]mcpRepositoryBranch, 0, len(items))
			for _, item := range items {
				result = append(result, repositoryBranchDTO(item))
			}
			return result, nil
		case "list_releases":
			items, err := store.RepositoryReleases(actorID, args.ProjectID)
			if err != nil {
				return nil, err
			}
			result := make([]mcpRepositoryRelease, 0, len(items))
			for _, item := range items {
				result = append(result, repositoryReleaseDTO(item))
			}
			return result, nil
		default:
			items, err := store.RepositoryMergeRequests(actorID, args.ProjectID)
			if err != nil {
				return nil, err
			}
			result := make([]mcpRepositoryMerge, 0, len(items))
			for _, item := range items {
				result = append(result, repositoryMergeDTO(item, scopes))
			}
			return result, nil
		}
	case "create_branch":
		var args struct {
			ProjectID    string `json:"project_id"`
			Name         string `json:"name"`
			FromCommitID string `json:"from_commit_id"`
		}
		if err := decodeArguments(raw, &args); err != nil {
			return nil, err
		}
		if err := requireNonEmpty(field("project_id", args.ProjectID), field("name", args.Name), field("from_commit_id", args.FromCommitID)); err != nil {
			return nil, err
		}
		branch, err := store.RepositoryCreateBranch(actorID, args.ProjectID, documentrepo.BranchInput{Name: args.Name, FromCommitID: args.FromCommitID})
		if err != nil {
			return nil, err
		}
		return repositoryBranchDTO(*branch), nil
	case "list_commits":
		var args struct {
			ProjectID  string `json:"project_id"`
			BranchID   string `json:"branch_id"`
			DocumentID string `json:"document_id"`
			Cursor     string `json:"cursor"`
		}
		if err := decodeArguments(raw, &args); err != nil {
			return nil, err
		}
		if err := requireNonEmpty(field("project_id", args.ProjectID), field("branch_id", args.BranchID)); err != nil {
			return nil, err
		}
		page, err := store.RepositoryHistory(actorID, args.ProjectID, args.BranchID, args.DocumentID, args.Cursor)
		if err != nil {
			return nil, err
		}
		result := mcpRepositoryHistory{Commits: make([]mcpRepositoryCommit, 0, len(page.Commits)), NextCursor: page.NextCursor}
		for _, item := range page.Commits {
			if args.DocumentID != "" {
				visible := repositoryCanReadDocument(&item, args.DocumentID, scopes)
				if !visible && len(item.ParentIDs) > 0 {
					parent, err := store.RepositoryCommit(actorID, args.ProjectID, item.ParentIDs[0])
					if err != nil {
						return nil, err
					}
					visible = repositoryCanReadDocument(parent, args.DocumentID, scopes)
				}
				if !visible {
					return nil, app.ErrNotFound
				}
			}
			result.Commits = append(result.Commits, repositoryCommitDTO(&item, scopes))
		}
		return result, nil
	case "get_commit", "get_document_at_commit":
		var args struct {
			ProjectID  string `json:"project_id"`
			CommitID   string `json:"commit_id"`
			DocumentID string `json:"document_id"`
		}
		if err := decodeArguments(raw, &args); err != nil {
			return nil, err
		}
		if tool == "get_commit" && args.DocumentID != "" {
			return nil, invalidArgument("get_commit does not accept document_id")
		}
		if err := requireNonEmpty(field("project_id", args.ProjectID), field("commit_id", args.CommitID)); err != nil {
			return nil, err
		}
		commit, err := store.RepositoryCommit(actorID, args.ProjectID, args.CommitID)
		if err != nil {
			return nil, err
		}
		if tool == "get_commit" {
			return repositoryCommitDTO(commit, scopes), nil
		}
		if err := requireNonEmpty(field("document_id", args.DocumentID)); err != nil {
			return nil, err
		}
		if !repositoryCanReadDocument(commit, args.DocumentID, scopes) {
			return nil, app.ErrNotFound
		}
		content, err := store.RepositoryContent(actorID, args.ProjectID, args.CommitID, args.DocumentID)
		if err != nil {
			return nil, err
		}
		return mcpRepositoryContent{CommitID: content.CommitID, Content: content.Content, Document: repositoryFileDTO(content.Document)}, nil
	case "commit_changes":
		return executeRepositorySave(store, actorID, scopes, raw)
	case "compare_commits", "analyze_changes":
		return executeRepositoryComparison(store, actorID, scopes, tool, raw)
	case "create_merge_request":
		var args struct {
			ProjectID      string `json:"project_id"`
			SourceBranchID string `json:"source_branch_id"`
			TargetBranchID string `json:"target_branch_id"`
			Title          string `json:"title"`
		}
		if err := decodeArguments(raw, &args); err != nil {
			return nil, err
		}
		if err := requireNonEmpty(field("project_id", args.ProjectID), field("source_branch_id", args.SourceBranchID), field("target_branch_id", args.TargetBranchID)); err != nil {
			return nil, err
		}
		source, err := repositoryFindBranch(store, actorID, args.ProjectID, args.SourceBranchID)
		if err != nil {
			return nil, err
		}
		target, err := repositoryFindBranch(store, actorID, args.ProjectID, args.TargetBranchID)
		if err != nil {
			return nil, err
		}
		from, err := store.RepositoryCommit(actorID, args.ProjectID, target.HeadCommitID)
		if err != nil {
			return nil, err
		}
		to, err := store.RepositoryCommit(actorID, args.ProjectID, source.HeadCommitID)
		if err != nil {
			return nil, err
		}
		if !repositoryCanWriteDifference(from, to, scopes) {
			return nil, app.ErrPermissionDenied
		}
		request, err := store.RepositoryCreateMerge(actorID, args.ProjectID, documentrepo.MergeInput{SourceBranchID: args.SourceBranchID, TargetBranchID: args.TargetBranchID, Title: args.Title, ExpectedSourceCommitID: source.HeadCommitID, ExpectedTargetCommitID: target.HeadCommitID})
		if err != nil {
			return nil, err
		}
		return repositoryMergeDTO(*request, scopes), nil
	case "get_release":
		var args struct {
			ProjectID string `json:"project_id"`
			ReleaseID string `json:"release_id"`
		}
		if err := decodeArguments(raw, &args); err != nil {
			return nil, err
		}
		if err := requireNonEmpty(field("project_id", args.ProjectID), field("release_id", args.ReleaseID)); err != nil {
			return nil, err
		}
		release, err := repositoryFindRelease(store, actorID, args.ProjectID, args.ReleaseID)
		if err != nil {
			return nil, err
		}
		return repositoryReleaseDTO(*release), nil
	case "list_repository_events":
		var args struct {
			ProjectID string `json:"project_id"`
			After     int64  `json:"after"`
		}
		if err := decodeArguments(raw, &args); err != nil {
			return nil, err
		}
		if err := requireNonEmpty(field("project_id", args.ProjectID)); err != nil {
			return nil, err
		}
		if args.After < 0 {
			return nil, invalidArgument("after must be nonnegative")
		}
		items, err := store.RepositoryEvents(actorID, args.ProjectID, args.After)
		if err != nil {
			return nil, err
		}
		result := make([]mcpRepositoryEvent, 0, len(items))
		for _, item := range items {
			result = append(result, mcpRepositoryEvent{ID: item.ID, Sequence: item.Sequence, EventType: item.EventType, ProjectID: item.ProjectID, BranchID: item.BranchID, BeforeCommitID: item.BeforeCommitID, AfterCommitID: item.AfterCommitID, RefGeneration: item.RefGeneration, ActorID: item.ActorID, CreatedAt: item.CreatedAt, ResourceID: item.ResourceID})
		}
		return result, nil
	default:
		return nil, errUnknownTool
	}
}

func repositoryCanReadDocument(commit *documentrepo.Commit, documentID string, scopes []int) bool {
	for _, file := range commit.Files {
		if file.DocumentID == documentID {
			return repositoryReadScope(scopes, file.DocumentType)
		}
	}
	return false
}

func repositoryCanWriteDifference(from, to *documentrepo.Commit, scopes []int) bool {
	old := map[string]documentrepo.File{}
	for _, file := range from.Files {
		old[file.DocumentID] = file
	}
	for _, file := range to.Files {
		previous, exists := old[file.DocumentID]
		if !exists || previous.ContentHash != file.ContentHash || previous.RelativePath != file.RelativePath || previous.Name != file.Name || previous.Description != file.Description || previous.DocumentType != file.DocumentType {
			if !repositoryWriteScope(scopes, file.DocumentType) || exists && !repositoryWriteScope(scopes, previous.DocumentType) {
				return false
			}
		}
		delete(old, file.DocumentID)
	}
	for _, file := range old {
		if !repositoryWriteScope(scopes, file.DocumentType) {
			return false
		}
	}
	return true
}

func executeRepositorySave(store *app.Store, actorID string, scopes []int, raw json.RawMessage) (any, error) {
	var args struct {
		ProjectID            string `json:"project_id"`
		BranchID             string `json:"branch_id"`
		ExpectedHeadCommitID string `json:"expected_head_commit_id"`
		ExpectedGeneration   *int64 `json:"expected_generation"`
		IdempotencyKey       string `json:"idempotency_key"`
		Message              string `json:"message"`
		Changes              []struct {
			Operation    string  `json:"operation"`
			DocumentID   string  `json:"document_id"`
			RelativePath string  `json:"relative_path"`
			Name         string  `json:"name"`
			DocumentType *int    `json:"document_type"`
			Description  string  `json:"description"`
			Content      *string `json:"content"`
		} `json:"changes"`
	}
	if err := decodeArguments(raw, &args); err != nil {
		return nil, err
	}
	if err := requireNonEmpty(field("project_id", args.ProjectID), field("branch_id", args.BranchID), field("expected_head_commit_id", args.ExpectedHeadCommitID), field("idempotency_key", args.IdempotencyKey)); err != nil {
		return nil, err
	}
	if args.ExpectedGeneration == nil || *args.ExpectedGeneration < 0 {
		return nil, invalidArgument("expected_generation must be a nonnegative integer")
	}
	if len(args.Changes) < 1 || len(args.Changes) > 500 {
		return nil, invalidArgument("changes must contain between 1 and 500 operations")
	}
	baseline, err := store.RepositoryCommit(actorID, args.ProjectID, args.ExpectedHeadCommitID)
	if err != nil {
		return nil, err
	}
	existing := map[string]documentrepo.File{}
	for _, file := range baseline.Files {
		existing[file.DocumentID] = file
	}
	input := documentrepo.SaveInput{ExpectedHeadCommitID: args.ExpectedHeadCommitID, ExpectedGeneration: *args.ExpectedGeneration, IdempotencyKey: args.IdempotencyKey, Message: args.Message, Changes: make([]documentrepo.Change, 0, len(args.Changes))}
	for _, change := range args.Changes {
		previous, exists := existing[change.DocumentID]
		if exists && !repositoryWriteScope(scopes, previous.DocumentType) {
			return nil, app.ErrPermissionDenied
		}
		switch change.Operation {
		case "upsert":
			if err := requireNonEmpty(field("relative_path", change.RelativePath), field("name", change.Name)); err != nil {
				return nil, err
			}
			if change.Content == nil || change.DocumentType == nil {
				return nil, invalidArgument("upsert requires content and document_type")
			}
			if !repositoryWriteScope(scopes, *change.DocumentType) {
				return nil, app.ErrPermissionDenied
			}
			input.Changes = append(input.Changes, documentrepo.Change{Operation: change.Operation, DocumentID: change.DocumentID, RelativePath: change.RelativePath, Name: change.Name, DocumentType: *change.DocumentType, Description: change.Description, Content: *change.Content})
		case "delete":
			if err := requireNonEmpty(field("document_id", change.DocumentID)); err != nil {
				return nil, err
			}
			if !exists {
				return nil, app.ErrNotFound
			}
			if change.Content != nil || change.DocumentType != nil || change.RelativePath != "" || change.Name != "" || change.Description != "" {
				return nil, invalidArgument("delete accepts only operation and document_id")
			}
			input.Changes = append(input.Changes, documentrepo.Change{Operation: change.Operation, DocumentID: change.DocumentID})
		default:
			return nil, invalidArgument("operation must be upsert or delete")
		}
	}
	commit, err := store.RepositorySave(actorID, args.ProjectID, args.BranchID, input)
	if err != nil {
		return nil, err
	}
	return repositoryCommitDTO(commit, scopes), nil
}

func executeRepositoryComparison(store *app.Store, actorID string, scopes []int, tool string, raw json.RawMessage) (any, error) {
	var args struct {
		ProjectID    string `json:"project_id"`
		FromCommitID string `json:"from_commit_id"`
		ToCommitID   string `json:"to_commit_id"`
		DocumentID   string `json:"document_id"`
		Message      string `json:"message"`
		Language     string `json:"language"`
	}
	if err := decodeArguments(raw, &args); err != nil {
		return nil, err
	}
	if tool == "compare_commits" && (args.Message != "" || args.Language != "") {
		return nil, invalidArgument("compare_commits does not accept analysis settings")
	}
	if err := requireNonEmpty(field("project_id", args.ProjectID), field("to_commit_id", args.ToCommitID)); err != nil {
		return nil, err
	}
	if tool == "compare_commits" {
		if err := requireNonEmpty(field("from_commit_id", args.FromCommitID)); err != nil {
			return nil, err
		}
	}
	to, err := store.RepositoryCommit(actorID, args.ProjectID, args.ToCommitID)
	if err != nil {
		return nil, err
	}
	if tool == "analyze_changes" && args.FromCommitID == "" && len(to.ParentIDs) > 0 {
		args.FromCommitID = to.ParentIDs[0]
	}
	var from *documentrepo.Commit
	if args.FromCommitID != "" {
		from, err = store.RepositoryCommit(actorID, args.ProjectID, args.FromCommitID)
		if err != nil {
			return nil, err
		}
	}
	if args.DocumentID != "" && !repositoryCanReadDocument(to, args.DocumentID, scopes) && (from == nil || !repositoryCanReadDocument(from, args.DocumentID, scopes)) {
		return nil, app.ErrNotFound
	}
	// 分析不能把另一个文档类型的正文送给模型，局部 token 必须指定它能读的文档。
	if tool == "analyze_changes" {
		for _, commit := range []*documentrepo.Commit{from, to} {
			if commit == nil {
				continue
			}
			for _, file := range commit.Files {
				if (args.DocumentID == "" || file.DocumentID == args.DocumentID) && !repositoryReadScope(scopes, file.DocumentType) {
					return nil, app.ErrPermissionDenied
				}
			}
		}
		return executeRepositoryAnalysis(store, actorID, args.ProjectID, documentrepo.AnalysisInput{FromCommitID: args.FromCommitID, ToCommitID: args.ToCommitID, DocumentID: args.DocumentID, Message: args.Message, Language: args.Language})
	}
	comparison, err := store.RepositoryCompare(actorID, args.ProjectID, documentrepo.CompareInput{FromCommitID: args.FromCommitID, ToCommitID: args.ToCommitID, DocumentID: args.DocumentID})
	if err != nil {
		return nil, err
	}
	result := mcpRepositoryComparison{ID: comparison.ID, FromCommitID: comparison.FromCommitID, ToCommitID: comparison.ToCommitID, Files: make([]mcpRepositoryComparisonFile, 0, len(comparison.Files))}
	for _, file := range comparison.Files {
		allowed := repositoryReadScope(scopes, file.DocumentType)
		// 类型改变时旧正文也要检查权限，不能只相信新类型。
		for _, commit := range []*documentrepo.Commit{from, to} {
			if commit == nil {
				continue
			}
			for _, entry := range commit.Files {
				if entry.DocumentID == file.DocumentID && !repositoryReadScope(scopes, entry.DocumentType) {
					allowed = false
				}
			}
		}
		if !allowed {
			result.FilesFiltered = true
			continue
		}
		var semantic *mcpDiffDTO
		if diff, ok := file.SemanticDiff.(*app.Diff); ok {
			semantic = mcpDiffPointer(diff)
		}
		result.Files = append(result.Files, mcpRepositoryComparisonFile{DocumentID: file.DocumentID, RelativePath: file.RelativePath, OldRelativePath: file.OldRelativePath, ChangeType: file.ChangeType, OldContent: file.OldContent, NewContent: file.NewContent, DocumentType: file.DocumentType, SemanticStatus: file.SemanticStatus, SemanticDiff: semantic})
	}
	return result, nil
}
