package vdoc

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	repo "vdoc/domain/documentrepo"
	"vdoc/utils/id"
)

// RepositoryResolveMerge 将人工解决结果保存为源分支的新提交，旧审核轮次保留，必须重新审核新请求。
func (s *Store) RepositoryResolveMerge(actor, project, requestID string, input repo.ResolveInput) (out *repo.MergeRequest, err error) {
	if input.ExpectedGeneration < 1 || input.ExpectedSourceCommitID == "" || input.ExpectedTargetCommitID == "" || input.ExpectedSourceGeneration < 1 || input.ExpectedTargetGeneration < 1 || len(input.IdempotencyKey) < 8 || len(input.IdempotencyKey) > 200 || len(input.Files) > 5000 || input.Files == nil || len(input.Message) > 4000 {
		return nil, ErrInvalidArgument
	}
	encoded, _ := json.Marshal([]any{"resolve_merge", actor, requestID, input})
	fingerprint := repo.Hash(encoded)
	key := repo.Hash([]byte(actor + ":" + input.IdempotencyKey))
	newKeys := []string{}
	err = s.WithRepositoryWrite(actor, project, MemberRoleWriter, func(storage repo.Storage) error {
		if e := s.repositoryRequireImported(storage, project); e != nil {
			return e
		}
		previous, e := repositoryLoad[repositoryIdempotency](s.requestContext(), storage, project, "idempotency", key)
		if e == nil {
			if previous.Fingerprint != fingerprint || previous.MergeRequestID == "" {
				return fmt.Errorf("%w: idempotency key reused for another request", ErrInvalidArgument)
			}
			out, e = repositoryLoad[repo.MergeRequest](s.requestContext(), storage, project, "merge_request", previous.MergeRequestID)
			return e
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		m, source, target, e := s.repositoryCurrentMerge(storage, project, requestID)
		if e != nil {
			return e
		}
		if m.Status != "open" || len(m.Conflicts) == 0 || source.IsProtected {
			return fmt.Errorf("%w: an open conflict on an unprotected source branch is required", ErrFailedPrecondition)
		}
		if input.ExpectedGeneration != m.Generation || input.ExpectedSourceCommitID != source.HeadCommitID || input.ExpectedTargetCommitID != target.HeadCommitID || input.ExpectedSourceGeneration != source.Generation || input.ExpectedTargetGeneration != target.Generation {
			return fmt.Errorf("%w: REVIEW_STALE: conflict resolution is for different branch heads", ErrFailedPrecondition)
		}
		head, e := repositoryLoad[repo.Commit](s.requestContext(), storage, project, "commit", source.HeadCommitID)
		if e != nil {
			return e
		}
		targetCommit, e := repositoryLoad[repo.Commit](s.requestContext(), storage, project, "commit", target.HeadCommitID)
		if e != nil {
			return e
		}
		files := make([]repo.File, 0, len(input.Files))
		for _, change := range input.Files {
			if change.Operation != "upsert" || change.DocumentID == "" {
				return fmt.Errorf("%w: resolution files must be complete upserts with document identities", ErrInvalidArgument)
			}
			files = append(files, repo.File{DocumentID: change.DocumentID, RelativePath: change.RelativePath, Name: change.Name, DocumentType: change.DocumentType, Description: change.Description, ContentHash: repo.Hash([]byte(change.Content))})
		}
		if e = repo.ValidateFiles(files); e != nil {
			return e
		}
		for i := range files {
			for _, tree := range [][]repo.File{head.Files, targetCommit.Files} {
				if old, ok := fileByID(tree, files[i].DocumentID); ok && old.ContentHash == files[i].ContentHash {
					files[i].ObjectKey = old.ObjectKey
					break
				}
			}
			if files[i].ObjectKey == "" {
				files[i], e = s.repositoryStoreFile(project, files[i], input.Files[i].Content)
				newKeys = append(newKeys, files[i].ObjectKey)
				if e != nil {
					return e
				}
			}
		}
		message := strings.TrimSpace(input.Message)
		if message == "" {
			message = "Resolve " + m.Title
		}
		commit := repo.NewCommit(project, actor, message, []string{source.HeadCommitID, target.HeadCommitID}, files, time.Now())
		if e = s.repositoryAdvance(storage, actor, project, source, commit); e != nil {
			return e
		}
		generation := m.Generation
		m.Status = "superseded"
		m.Generation++
		if e = repositoryPut(s.requestContext(), storage, project, "merge_request", m.ID, m, m.Generation, &generation); e != nil {
			return e
		}
		out = &repo.MergeRequest{ID: id.GenerateID(), ProjectID: project, Title: m.Title, SourceBranchID: source.ID, TargetBranchID: target.ID, SourceCommitID: commit.ID, TargetCommitID: target.HeadCommitID, SourceGeneration: source.Generation, TargetGeneration: target.Generation, BaseCommitID: target.HeadCommitID, ResultFiles: commit.Files, Conflicts: []string{}, Status: "open", CreatedBy: actor, CreatedAt: time.Now().UTC(), Generation: 1, ResolvesRequestID: m.ID}
		if e = repositoryPut(s.requestContext(), storage, project, "merge_request", out.ID, out, 1, nil); e != nil {
			return e
		}
		if e = repositoryPut(s.requestContext(), storage, project, "idempotency", key, repositoryIdempotency{Fingerprint: fingerprint, CommitID: commit.ID, MergeRequestID: out.ID}, 1, nil); e != nil {
			return e
		}
		if e = s.repositoryEvent(storage, actor, project, "merge_request.resolved", source, m.SourceCommitID, m.ID); e != nil {
			return e
		}
		return s.repositoryEvent(storage, actor, project, "merge_request.created", source, "", out.ID)
	})
	if err != nil {
		err = s.repositoryCleanupUploads(err, newKeys)
	}
	return
}
