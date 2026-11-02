package vdoc

import (
	"context"
	"encoding/json"
	"sort"

	repo "vdoc/domain/documentrepo"
	domain "vdoc/domain/vdoc"
)

// 新建文档只存在于提交仓库；沿用 document 会话身份，让迁移前后的对话连续可见。
func (s *Store) aiRepositoryDocumentLocked(target AISummaryTarget) (*repo.Document, error) {
	storage := s.repositoryStorageLocked()
	if storage == nil {
		return nil, ErrNotFound
	}
	document, err := repositoryLoad[repo.Document](s.requestContext(), storage, target.ProjectID, "document", target.DocumentID)
	if err != nil {
		return nil, err
	}
	if document.ID != target.DocumentID || document.ProjectID != target.ProjectID {
		return nil, ErrNotFound
	}
	return document, nil
}

func (s *Store) aiRepositorySourcesLocked(target AISummaryTarget) ([]aiDocumentSource, string, error) {
	document, err := s.aiRepositoryDocumentLocked(target)
	if Is(err, ErrNotFound) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	storage := documentRepositoryStorage{Storage: s.repositoryStorageLocked(), documentID: target.DocumentID}
	branches, err := repositoryList[repo.Branch](s.requestContext(), storage, target.ProjectID, "branch")
	if err != nil {
		return nil, "", err
	}
	names := map[string]string{}
	for _, branch := range branches {
		names[branch.ID] = branch.Name
	}
	commits, err := repositoryList[repo.Commit](s.requestContext(), storage, target.ProjectID, "commit")
	if err != nil {
		return nil, "", err
	}
	sources := make([]aiDocumentSource, 0, len(commits))
	for _, commit := range commits {
		file, ok := fileByID(commit.Files, target.DocumentID)
		if !ok {
			continue
		}
		sources = append(sources, aiDocumentSource{Kind: "commit", ID: commit.ID, BranchID: commit.BranchID, Branch: names[commit.BranchID], Name: commit.VersionName, Changelog: aiBoundText(commit.Message, 240), UpdatedAt: commit.CreatedAt, file: &file})
	}
	return sources, document.Name, nil
}

// 完成事务再次校验来源目录，防止另一实例在生成期间提交或更改文档后保存过时回答。
// 只读元数据；内容对象已按哈希验证，水合缓存不会改变修订值。
func aiRepositoryRevision(ctx context.Context, storage repo.Storage, project, document string) (string, error) {
	if storage == nil {
		return "", nil
	}
	d, err := repositoryLoad[repo.Document](ctx, storage, project, "document", document)
	if Is(err, ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if d.ID != document || d.ProjectID != project {
		return "", ErrNotFound
	}
	scoped := documentRepositoryStorage{Storage: storage, documentID: document}
	rows := []string{}
	add := func(value any) { data, _ := json.Marshal(value); rows = append(rows, string(data)) }
	add(d)
	for _, kind := range []string{"branch", "commit"} {
		var after int64
		for {
			records, err := scoped.RepositoryRecords(ctx, project, kind, after, 500)
			if err != nil {
				return "", err
			}
			for _, record := range records {
				add([]any{kind, record.ID, record.Generation})
				after = record.Sequence
			}
			if len(records) < 500 {
				break
			}
		}
	}
	sort.Strings(rows)
	data, _ := json.Marshal(rows)
	return repo.Hash(data), nil
}

func validateAIRepositoryMutation(ctx context.Context, storage repo.Storage, guard *domain.MutationGuard) error {
	if guard == nil || guard.RepositoryDocumentID == "" {
		return nil
	}
	if storage == nil {
		return ErrFailedPrecondition
	}
	document, err := repositoryLoad[repo.Document](ctx, storage, guard.ProjectID, "document", guard.RepositoryDocumentID)
	if err != nil {
		return err
	}
	if document.ID != guard.RepositoryDocumentID || document.ProjectID != guard.ProjectID {
		return ErrNotFound
	}
	if document.Status != DocumentStatusActive {
		return ErrFailedPrecondition
	}
	revision, err := aiRepositoryRevision(ctx, storage, guard.ProjectID, guard.RepositoryDocumentID)
	if err != nil {
		return err
	}
	if revision != guard.ExpectedAIRepositoryRevision {
		return ErrFailedPrecondition
	}
	return nil
}
