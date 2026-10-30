package vdoc

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	repo "vdoc/domain/documentrepo"
	"vdoc/utils/id"
)

func (s *Store) repositoryMergeBase(storage repo.Storage, project, left, right string) (*repo.Commit, error) {
	commits := map[string]*repo.Commit{}
	walk := func(tip string) (map[string]bool, error) {
		seen := map[string]bool{}
		queue := []string{tip}
		for len(queue) > 0 {
			key := queue[0]
			queue = queue[1:]
			if seen[key] {
				continue
			}
			if len(seen) >= 10000 {
				return nil, fmt.Errorf("%w: merge history limit exceeded", ErrFailedPrecondition)
			}
			seen[key] = true
			c := commits[key]
			if c == nil {
				var err error
				c, err = repositoryLoad[repo.Commit](s.requestContext(), storage, project, "commit", key)
				if err != nil {
					return nil, err
				}
				commits[key] = c
			}
			queue = append(queue, c.ParentIDs...)
		}
		return seen, nil
	}
	ancestors, err := walk(left)
	if err != nil {
		return nil, err
	}
	rightAncestors, err := walk(right)
	if err != nil {
		return nil, err
	}
	common := map[string]bool{}
	queue := []string{}
	for key := range rightAncestors {
		if ancestors[key] {
			common[key] = true
			queue = append(queue, commits[key].ParentIDs...)
		}
	}
	// 共享节点的祖先不是最低公共祖先；同时遍历一次，避免重复合并后的祖先被误判成多个基点。
	nonLowest := map[string]bool{}
	for len(queue) > 0 {
		key := queue[0]
		queue = queue[1:]
		if nonLowest[key] {
			continue
		}
		nonLowest[key] = true
		queue = append(queue, commits[key].ParentIDs...)
	}
	var base *repo.Commit
	for key := range common {
		if !nonLowest[key] {
			if base != nil {
				return nil, fmt.Errorf("%w: merge requires a single proven common ancestor", ErrFailedPrecondition)
			}
			base = commits[key]
		}
	}
	if base == nil {
		return nil, fmt.Errorf("%w: merge has no proven common ancestor", ErrFailedPrecondition)
	}
	return base, nil
}

func mergeMetadata(base, left, right repo.File) (repo.File, bool) {
	result := left
	values := [][3]string{{base.RelativePath, left.RelativePath, right.RelativePath}, {base.Name, left.Name, right.Name}, {base.Description, left.Description, right.Description}, {fmt.Sprint(base.DocumentType), fmt.Sprint(left.DocumentType), fmt.Sprint(right.DocumentType)}}
	for _, v := range values {
		if v[1] != v[2] && v[1] != v[0] && v[2] != v[0] {
			return repo.File{}, false
		}
	}
	if left.RelativePath == base.RelativePath {
		result.RelativePath = right.RelativePath
	}
	if left.Name == base.Name {
		result.Name = right.Name
	}
	if left.Description == base.Description {
		result.Description = right.Description
	}
	if left.DocumentType == base.DocumentType {
		result.DocumentType = right.DocumentType
	}
	return result, true
}

func (s *Store) repositoryMergeFiles(project string, base, left, right []repo.File, uploaded ...*[]string) ([]repo.File, []string, error) {
	b, l, r := map[string]repo.File{}, map[string]repo.File{}, map[string]repo.File{}
	ids := map[string]bool{}
	for _, f := range base {
		b[f.DocumentID] = f
		ids[f.DocumentID] = true
	}
	for _, f := range left {
		l[f.DocumentID] = f
		ids[f.DocumentID] = true
	}
	for _, f := range right {
		r[f.DocumentID] = f
		ids[f.DocumentID] = true
	}
	out := map[string]repo.File{}
	conflicts := []string{}
	for key := range ids {
		bf, bok := b[key]
		lf, lok := l[key]
		rf, rok := r[key]
		switch {
		case lok == rok && (!lok || repo.EqualFile(lf, rf)):
			if lok {
				out[key] = lf
			}
		case bok == lok && (!bok || repo.EqualFile(bf, lf)):
			if rok {
				out[key] = rf
			}
		case bok == rok && (!bok || repo.EqualFile(bf, rf)):
			if lok {
				out[key] = lf
			}
		default:
			if !bok || !lok || !rok {
				conflicts = append(conflicts, key)
				continue
			}
			merged, ok := mergeMetadata(bf, lf, rf)
			if !ok {
				conflicts = append(conflicts, key)
				continue
			}
			bc, e := s.repositoryFileContent(bf)
			if e != nil {
				return nil, nil, e
			}
			lc, e := s.repositoryFileContent(lf)
			if e != nil {
				return nil, nil, e
			}
			rc, e := s.repositoryFileContent(rf)
			if e != nil {
				return nil, nil, e
			}
			content, ok := repo.MergeText(bc, lc, rc)
			if !ok {
				conflicts = append(conflicts, key)
				continue
			}
			merged.ContentHash = repo.Hash([]byte(content))
			switch merged.ContentHash {
			case lf.ContentHash:
				merged.ObjectKey = lf.ObjectKey
			case rf.ContentHash:
				merged.ObjectKey = rf.ObjectKey
			case bf.ContentHash:
				merged.ObjectKey = bf.ObjectKey
			default:
				merged, e = s.repositoryStoreFile(project, merged, content)
				for _, keys := range uploaded {
					*keys = append(*keys, merged.ObjectKey)
				}
				if e != nil {
					return nil, nil, e
				}
			}
			out[key] = merged
		}
	}
	files := repositorySortedFiles(out)
	if err := repo.ValidateFiles(files); err != nil {
		conflicts = append(conflicts, "path-conflict")
	}
	sort.Strings(conflicts)
	return files, conflicts, nil
}

func (s *Store) RepositoryMergeRequests(actor, project string) (out []repo.MergeRequest, err error) {
	err = s.WithRepositoryRead(actor, project, func(storage repo.Storage) error {
		out, err = repositoryList[repo.MergeRequest](s.requestContext(), storage, project, "merge_request")
		return err
	})
	return
}

func (s *Store) RepositoryMergeContent(actor, project, requestID, documentID string) (out *repo.MergeContent, err error) {
	err = s.WithRepositoryRead(actor, project, func(storage repo.Storage) error {
		m, e := repositoryLoad[repo.MergeRequest](s.requestContext(), storage, project, "merge_request", requestID)
		if e != nil {
			return e
		}
		file, ok := fileByID(m.ResultFiles, documentID)
		if !ok {
			return ErrNotFound
		}
		content, e := s.repositoryFileContent(file)
		if e != nil {
			return e
		}
		out = &repo.MergeContent{MergeRequestID: m.ID, Generation: m.Generation, Content: content, Document: file}
		return nil
	})
	return
}

func (s *Store) RepositoryCreateMerge(actor, project string, input repo.MergeInput) (out *repo.MergeRequest, err error) {
	if input.SourceBranchID == "" || input.SourceBranchID == input.TargetBranchID {
		return nil, ErrInvalidArgument
	}
	newKeys := []string{}
	err = s.WithRepositoryWrite(actor, project, MemberRoleWriter, func(storage repo.Storage) error {
		if e := s.repositoryRequireImported(storage, project); e != nil {
			return e
		}
		source, e := repositoryLoad[repo.Branch](s.requestContext(), storage, project, "branch", input.SourceBranchID)
		if e != nil {
			return e
		}
		target, e := repositoryLoad[repo.Branch](s.requestContext(), storage, project, "branch", input.TargetBranchID)
		if e != nil {
			return e
		}
		if (input.ExpectedSourceCommitID != "" && input.ExpectedSourceCommitID != source.HeadCommitID) || (input.ExpectedTargetCommitID != "" && input.ExpectedTargetCommitID != target.HeadCommitID) {
			return fmt.Errorf("%w: REF_CONFLICT: merge branch changed", ErrFailedPrecondition)
		}
		left, e := repositoryLoad[repo.Commit](s.requestContext(), storage, project, "commit", target.HeadCommitID)
		if e != nil {
			return e
		}
		right, e := repositoryLoad[repo.Commit](s.requestContext(), storage, project, "commit", source.HeadCommitID)
		if e != nil {
			return e
		}
		base, e := s.repositoryMergeBase(storage, project, left.ID, right.ID)
		if e != nil {
			return e
		}
		files, conflicts, e := s.repositoryMergeFiles(project, base.Files, left.Files, right.Files, &newKeys)
		if e != nil {
			return e
		}
		title := strings.TrimSpace(input.Title)
		if title == "" {
			title = "Merge " + source.Name + " into " + target.Name
		}
		out = &repo.MergeRequest{ID: id.GenerateID(), ProjectID: project, Title: title, SourceBranchID: source.ID, TargetBranchID: target.ID, SourceCommitID: source.HeadCommitID, TargetCommitID: target.HeadCommitID, SourceGeneration: source.Generation, TargetGeneration: target.Generation, BaseCommitID: base.ID, ResultFiles: files, Conflicts: conflicts, Status: "open", CreatedBy: actor, CreatedAt: time.Now().UTC(), Generation: 1}
		if e = repositoryPut(s.requestContext(), storage, project, "merge_request", out.ID, out, 1, nil); e != nil {
			return e
		}
		return s.repositoryEvent(storage, actor, project, "merge_request.created", source, "", out.ID)
	})
	if err != nil {
		err = s.repositoryCleanupUploads(err, newKeys)
	}
	return
}

func (s *Store) repositoryCurrentMerge(storage repo.Storage, project, requestID string) (*repo.MergeRequest, *repo.Branch, *repo.Branch, error) {
	m, e := repositoryLoad[repo.MergeRequest](s.requestContext(), storage, project, "merge_request", requestID)
	if e != nil {
		return nil, nil, nil, e
	}
	source, e := repositoryLoad[repo.Branch](s.requestContext(), storage, project, "branch", m.SourceBranchID)
	if e != nil {
		return nil, nil, nil, e
	}
	target, e := repositoryLoad[repo.Branch](s.requestContext(), storage, project, "branch", m.TargetBranchID)
	if e != nil {
		return nil, nil, nil, e
	}
	if source.HeadCommitID != m.SourceCommitID || target.HeadCommitID != m.TargetCommitID || source.Generation != m.SourceGeneration || target.Generation != m.TargetGeneration {
		return nil, nil, nil, fmt.Errorf("%w: REVIEW_STALE: create a new review round for the latest branch heads", ErrFailedPrecondition)
	}
	return m, source, target, nil
}

func (s *Store) RepositoryApproveMerge(actor, project, requestID string) (out *repo.MergeRequest, err error) {
	if mcpMutationTokenID(s.requestContext()) != "" {
		return nil, ErrPermissionDenied
	}
	err = s.WithRepositoryWrite(actor, project, MemberRoleAdmin, func(storage repo.Storage) error {
		m, _, _, e := s.repositoryCurrentMerge(storage, project, requestID)
		if e != nil {
			return e
		}
		if m.Status != "open" || len(m.Conflicts) > 0 {
			return fmt.Errorf("%w: merge request has conflicts or is no longer open", ErrFailedPrecondition)
		}
		candidate := &repo.Commit{Files: m.ResultFiles}
		if e = s.repositoryValidatePublished(candidate); e != nil {
			return e
		}
		generation := m.Generation
		now := time.Now().UTC()
		m.Status = "approved"
		m.ApprovedBy = actor
		m.ApprovedAt = &now
		m.Generation++
		if e = repositoryPut(s.requestContext(), storage, project, "merge_request", m.ID, m, m.Generation, &generation); e != nil {
			return e
		}
		out = m
		return s.repositoryEvent(storage, actor, project, "merge_request.approved", nil, "", m.ID)
	})
	return
}

func (s *Store) RepositoryMerge(actor, project, requestID string) (out *repo.Commit, err error) {
	if mcpMutationTokenID(s.requestContext()) != "" {
		return nil, ErrPermissionDenied
	}
	err = s.WithRepositoryWrite(actor, project, MemberRoleAdmin, func(storage repo.Storage) error {
		m, source, target, e := s.repositoryCurrentMerge(storage, project, requestID)
		if e != nil {
			return e
		}
		if m.Status != "approved" || m.ApprovedBy == "" || len(m.Conflicts) > 0 {
			return fmt.Errorf("%w: current review approval required", ErrFailedPrecondition)
		}
		if m.BaseCommitID == target.HeadCommitID {
			out, e = repositoryLoad[repo.Commit](s.requestContext(), storage, project, "commit", source.HeadCommitID)
			if e != nil {
				return e
			}
			generation := target.Generation
			before := target.HeadCommitID
			target.HeadCommitID = out.ID
			target.Generation++
			if e = repositoryPut(s.requestContext(), storage, project, "branch", target.ID, target, target.Generation, &generation); e != nil {
				return e
			}
			if e = s.repositoryEvent(storage, actor, project, "branch.updated", target, before, m.ID); e != nil {
				return e
			}
		} else {
			out = repo.NewCommit(project, actor, m.Title, []string{target.HeadCommitID, source.HeadCommitID}, m.ResultFiles, time.Now())
			if e = s.repositoryAdvance(storage, actor, project, target, out); e != nil {
				return e
			}
		}
		generation := m.Generation
		m.Status = "merged"
		m.MergedCommitID = out.ID
		m.Generation++
		if e = repositoryPut(s.requestContext(), storage, project, "merge_request", m.ID, m, m.Generation, &generation); e != nil {
			return e
		}
		return s.repositoryEvent(storage, actor, project, "merge_request.merged", target, "", m.ID)
	})
	return
}

func (s *Store) RepositoryRevert(actor, project, branchID string, input repo.RevertInput) (out *repo.Commit, err error) {
	if input.CommitID == "" || input.ExpectedHeadCommitID == "" || input.ExpectedGeneration < 1 || len(input.IdempotencyKey) < 8 || len(input.IdempotencyKey) > 200 {
		return nil, ErrInvalidArgument
	}
	encoded, _ := json.Marshal([]any{"revert", actor, branchID, input})
	fingerprint := repo.Hash(encoded)
	key := repo.Hash([]byte(actor + ":" + input.IdempotencyKey))
	newKeys := []string{}
	err = s.WithRepositoryWrite(actor, project, MemberRoleWriter, func(storage repo.Storage) error {
		if e := s.repositoryRequireImported(storage, project); e != nil {
			return e
		}
		previous, e := repositoryLoad[repositoryIdempotency](s.requestContext(), storage, project, "idempotency", key)
		if e == nil {
			if previous.Fingerprint != fingerprint {
				return fmt.Errorf("%w: idempotency key reused for another request", ErrInvalidArgument)
			}
			out, e = repositoryLoad[repo.Commit](s.requestContext(), storage, project, "commit", previous.CommitID)
			return e
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		b, e := repositoryLoad[repo.Branch](s.requestContext(), storage, project, "branch", branchID)
		if e != nil {
			return e
		}
		if b.IsProtected {
			return fmt.Errorf("%w: revert on a feature branch and request review", ErrFailedPrecondition)
		}
		if b.HeadCommitID != input.ExpectedHeadCommitID || b.Generation != input.ExpectedGeneration {
			return fmt.Errorf("%w: REF_CONFLICT: branch changed", ErrFailedPrecondition)
		}
		head, e := repositoryLoad[repo.Commit](s.requestContext(), storage, project, "commit", b.HeadCommitID)
		if e != nil {
			return e
		}
		reverted, e := repositoryLoad[repo.Commit](s.requestContext(), storage, project, "commit", input.CommitID)
		if e != nil {
			return e
		}
		if len(reverted.ParentIDs) == 0 || input.ParentIndex < 0 || input.ParentIndex >= len(reverted.ParentIDs) {
			return fmt.Errorf("%w: a valid parent is required", ErrInvalidArgument)
		}
		parent, e := repositoryLoad[repo.Commit](s.requestContext(), storage, project, "commit", reverted.ParentIDs[input.ParentIndex])
		if e != nil {
			return e
		}
		files, conflicts, e := s.repositoryMergeFiles(project, reverted.Files, head.Files, parent.Files, &newKeys)
		if e != nil {
			return e
		}
		if len(conflicts) > 0 {
			return fmt.Errorf("%w: REVERT_CONFLICT: %v", ErrFailedPrecondition, conflicts)
		}
		if repo.TreeHash(files) == head.TreeHash {
			return fmt.Errorf("%w: no changes to revert", ErrFailedPrecondition)
		}
		out = repo.NewCommit(project, actor, "Revert "+reverted.ID[:12], []string{head.ID}, files, time.Now())
		if e = s.repositoryAdvance(storage, actor, project, b, out); e != nil {
			return e
		}
		return repositoryPut(s.requestContext(), storage, project, "idempotency", key, repositoryIdempotency{Fingerprint: fingerprint, CommitID: out.ID}, 1, nil)
	})
	if err != nil {
		err = s.repositoryCleanupUploads(err, newKeys)
	}
	return
}
