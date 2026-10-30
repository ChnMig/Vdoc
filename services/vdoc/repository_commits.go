package vdoc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	repo "vdoc/domain/documentrepo"
	domain "vdoc/domain/vdoc"
	"vdoc/utils/id"
)

func (s *Store) repositoryEnsure(storage repo.Storage, actor, project string) ([]repo.Branch, error) {
	branches, err := repositoryList[repo.Branch](s.requestContext(), storage, project, "branch")
	if err != nil || len(branches) > 0 {
		return branches, err
	}
	c := repo.NewCommit(project, actor, "Initialize repository", nil, nil, time.Now())
	if err = repositoryPut(s.requestContext(), storage, project, "commit", c.ID, c, 1, nil); err != nil {
		return nil, err
	}
	b := repo.Branch{ID: id.GenerateID(), ProjectID: project, Name: "main", HeadCommitID: c.ID, Generation: 1, IsProtected: true, CreatedBy: actor, CreatedAt: time.Now().UTC()}
	if err = repositoryPut(s.requestContext(), storage, project, "branch", b.ID, b, 1, nil); err != nil {
		return nil, err
	}
	return []repo.Branch{b}, nil
}

func (s *Store) RepositoryBranches(actor, project string) (out []repo.Branch, err error) {
	err = s.WithRepositoryRead(actor, project, func(storage repo.Storage) error {
		var e error
		out, e = s.repositoryEnsure(storage, actor, project)
		if e != nil {
			return e
		}
		required, e := s.repositoryImportRequired(storage, project)
		if e != nil {
			return e
		}
		for i := range out {
			out[i].ImportRequired = required
		}
		return nil
	})
	return
}

func (s *Store) RepositoryCreateBranch(actor, project string, input repo.BranchInput) (out *repo.Branch, err error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len(input.Name) > 200 || strings.ContainsAny(input.Name, " \t\n\r~^:?*[\\") || strings.Contains(input.Name, "..") || strings.HasPrefix(input.Name, "/") || strings.HasSuffix(input.Name, "/") {
		return nil, fmt.Errorf("%w: invalid branch name", ErrInvalidArgument)
	}
	permission := MemberRoleWriter
	if input.IsProtected {
		permission = MemberRoleAdmin
	}
	err = s.WithRepositoryWrite(actor, project, permission, func(storage repo.Storage) error {
		branches, e := s.repositoryEnsure(storage, actor, project)
		if e != nil {
			return e
		}
		for _, b := range branches {
			if b.Name == input.Name {
				return ErrAlreadyExists
			}
		}
		if input.IsProtected && !s.canPublishLocked(actor, project) {
			return ErrPermissionDenied
		}
		if input.FromCommitID == "" {
			input.FromCommitID = branches[0].HeadCommitID
		}
		if _, e = repositoryLoad[repo.Commit](s.requestContext(), storage, project, "commit", input.FromCommitID); e != nil {
			return e
		}
		out = &repo.Branch{ID: id.GenerateID(), ProjectID: project, Name: input.Name, HeadCommitID: input.FromCommitID, Generation: 1, IsProtected: input.IsProtected, CreatedBy: actor, CreatedAt: time.Now().UTC()}
		if e = repositoryPut(s.requestContext(), storage, project, "branch", out.ID, out, 1, nil); e != nil {
			return e
		}
		return s.repositoryEvent(storage, actor, project, "branch.created", out, "", out.ID)
	})
	return
}

func (s *Store) RepositoryCommit(actor, project, commitID string) (out *repo.Commit, err error) {
	err = s.WithRepositoryRead(actor, project, func(storage repo.Storage) error {
		out, err = repositoryLoad[repo.Commit](s.requestContext(), storage, project, "commit", commitID)
		return err
	})
	return
}

func fileByID(files []repo.File, documentID string) (repo.File, bool) {
	for _, f := range files {
		if f.DocumentID == documentID {
			return f, true
		}
	}
	return repo.File{}, false
}

func (s *Store) RepositoryCommits(actor, project, branchID, documentID, cursor string) (out []repo.Commit, err error) {
	page, err := s.RepositoryHistory(actor, project, branchID, documentID, cursor)
	if err != nil {
		return nil, err
	}
	return page.Commits, nil
}

// RepositoryHistory 每页最多扫描 2,000 个节点，next_cursor 指向下一扫描节点，稀疏文档历史也不会丢页。
func (s *Store) RepositoryHistory(actor, project, branchID, documentID, cursor string) (out *repo.HistoryPage, err error) {
	out = &repo.HistoryPage{Commits: []repo.Commit{}}
	err = s.WithRepositoryRead(actor, project, func(storage repo.Storage) error {
		b, e := repositoryLoad[repo.Branch](s.requestContext(), storage, project, "branch", branchID)
		if e != nil {
			return e
		}
		tip := b.HeadCommitID
		if cursor != "" {
			tip = cursor
		}
		for scanned := 0; tip != "" && len(out.Commits) < 50 && scanned < 2000; scanned++ {
			c, e := repositoryLoad[repo.Commit](s.requestContext(), storage, project, "commit", tip)
			if e != nil {
				return e
			}
			include := documentID == ""
			if !include {
				f, ok := fileByID(c.Files, documentID)
				if len(c.ParentIDs) == 0 {
					include = ok
				} else {
					p, e := repositoryLoad[repo.Commit](s.requestContext(), storage, project, "commit", c.ParentIDs[0])
					if e != nil {
						return e
					}
					pf, pok := fileByID(p.Files, documentID)
					include = ok != pok || !repo.EqualFile(f, pf)
				}
			}
			if include {
				out.Commits = append(out.Commits, *c)
			}
			tip = ""
			if len(c.ParentIDs) > 0 {
				tip = c.ParentIDs[0]
			}
		}
		out.NextCursor = tip
		return nil
	})
	return
}

func (s *Store) repositoryFileContent(file repo.File) (string, error) {
	if s.objects == nil {
		if s.repoMemory == nil {
			return "", ErrNotFound
		}
		body, ok := s.repoMemory.blobs[file.ObjectKey]
		if !ok {
			return "", ErrNotFound
		}
		if repo.Hash(body) != file.ContentHash {
			return "", fmt.Errorf("stored repository content failed SHA-256 verification")
		}
		return string(body), nil
	}
	body, err := s.readVerifiedObject(s.requestContext(), file.ObjectKey, file.ContentHash)
	return string(body), err
}

func (s *Store) RepositoryContent(actor, project, commitID, documentID string) (out *repo.Content, err error) {
	err = s.WithRepositoryRead(actor, project, func(storage repo.Storage) error {
		c, e := repositoryLoad[repo.Commit](s.requestContext(), storage, project, "commit", commitID)
		if e != nil {
			return e
		}
		f, ok := fileByID(c.Files, documentID)
		if !ok {
			return ErrNotFound
		}
		content, e := s.repositoryFileContent(f)
		if e != nil {
			return e
		}
		out = &repo.Content{CommitID: c.ID, Content: content, Document: f}
		return nil
	})
	return
}

func (s *Store) repositoryStoreFile(project string, file repo.File, content string) (repo.File, error) {
	file.ObjectKey = ""
	if !utf8.ValidString(content) {
		return file, fmt.Errorf("%w: content must be valid UTF-8", ErrInvalidArgument)
	}
	if err := validateStoredObjectWriteSize(len(content)); err != nil {
		return file, err
	}
	file.ContentHash = repo.Hash([]byte(content))
	file.ObjectKey = fmt.Sprintf("projects/%s/repository/%s/%s", project, id.GenerateID(), file.ContentHash)
	if s.objects == nil {
		if s.persistence != nil {
			return file, fmt.Errorf("object storage is required for persistent repository content")
		}
		if s.repoMemory == nil {
			s.repoMemory = newRepositoryMemory()
		}
		s.repoMemory.blobs[file.ObjectKey] = []byte(content)
		return file, nil
	}
	_, err := s.objects.PutObject(s.requestContext(), ObjectWrite{Key: file.ObjectKey, ContentType: "text/plain; charset=utf-8", Body: []byte(content), Metadata: map[string]string{"project_id": project, "document_id": file.DocumentID, "sha256": file.ContentHash, "owner_type": "repository"}})
	return file, err
}

func (s *Store) repositoryEvent(storage repo.Storage, actor, project, kind string, branch *repo.Branch, before, resourceID string) error {
	e := repo.Event{ID: id.GenerateID(), ProjectID: project, EventType: kind, ActorID: actor, CreatedAt: time.Now().UTC(), ResourceID: resourceID}
	if branch != nil {
		e.BranchID = branch.ID
		e.RefGeneration = branch.Generation
		e.BeforeCommitID = before
		e.AfterCommitID = branch.HeadCommitID
	}
	return repositoryPut(s.requestContext(), storage, project, "event", e.ID, e, 1, nil)
}

func (s *Store) repositoryAdvance(storage repo.Storage, actor, project string, b *repo.Branch, c *repo.Commit) error {
	if err := repositoryPut(s.requestContext(), storage, project, "commit", c.ID, c, 1, nil); err != nil {
		return err
	}
	before := b.HeadCommitID
	generation := b.Generation
	b.HeadCommitID = c.ID
	b.Generation++
	if err := repositoryPut(s.requestContext(), storage, project, "branch", b.ID, b, b.Generation, &generation); err != nil {
		return err
	}
	return s.repositoryEvent(storage, actor, project, "commit.created", b, before, c.ID)
}

type repositoryIdempotency struct {
	Fingerprint    string `json:"fingerprint"`
	CommitID       string `json:"commit_id"`
	MergeRequestID string `json:"merge_request_id,omitempty"`
}

func (s *Store) repositoryCleanupUploads(err error, keys []string) error {
	refs := make([]domain.ObjectRef, 0, len(keys))
	for _, key := range keys {
		refs = append(refs, domain.ObjectRef{Key: key})
	}
	return s.cleanupNewObjectRefs(err, refs...)
}

func (s *Store) RepositorySave(actor, project, branchID string, input repo.SaveInput) (out *repo.Commit, err error) {
	if len(input.Changes) == 0 || len(input.Changes) > 1000 || input.ExpectedHeadCommitID == "" || input.ExpectedGeneration < 1 || len(input.IdempotencyKey) < 8 || len(input.IdempotencyKey) > 200 || len(input.Message) > 4000 {
		return nil, fmt.Errorf("%w: changes, expected HEAD/generation and idempotency key are required", ErrInvalidArgument)
	}
	encoded, _ := json.Marshal([]any{actor, branchID, input})
	fingerprint := repo.Hash(encoded)
	key := repo.Hash([]byte(actor + ":" + input.IdempotencyKey))
	newKeys := []string{}
	err = s.WithRepositoryWrite(actor, project, MemberRoleWriter, func(storage repo.Storage) error {
		if required, e := s.repositoryImportRequired(storage, project); e != nil {
			return e
		} else if required {
			return fmt.Errorf("%w: LEGACY_IMPORT_REQUIRED: a project admin must import existing legacy snapshots before saving repository commits", ErrFailedPrecondition)
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
		if b.HeadCommitID != input.ExpectedHeadCommitID || b.Generation != input.ExpectedGeneration {
			return fmt.Errorf("%w: REF_CONFLICT: branch HEAD changed; reload and merge your edits", ErrFailedPrecondition)
		}
		if b.IsProtected {
			return fmt.Errorf("%w: protected branch requires a reviewed merge request", ErrFailedPrecondition)
		}
		parent, e := repositoryLoad[repo.Commit](s.requestContext(), storage, project, "commit", b.HeadCommitID)
		if e != nil {
			return e
		}
		files := map[string]repo.File{}
		for _, f := range parent.Files {
			files[f.DocumentID] = f
		}
		seen := map[string]bool{}
		for _, change := range input.Changes {
			if change.DocumentID == "" {
				if change.Operation == "delete" {
					return ErrInvalidArgument
				}
				change.DocumentID = id.GenerateID()
			}
			if seen[change.DocumentID] {
				return fmt.Errorf("%w: duplicate document change", ErrInvalidArgument)
			}
			seen[change.DocumentID] = true
			if change.Operation == "delete" {
				if _, ok := files[change.DocumentID]; !ok {
					return ErrNotFound
				}
				delete(files, change.DocumentID)
				continue
			}
			if change.Operation != "upsert" {
				return ErrInvalidArgument
			}
			f := repo.File{DocumentID: change.DocumentID, RelativePath: change.RelativePath, Name: change.Name, DocumentType: change.DocumentType, Description: change.Description, ContentHash: repo.Hash([]byte(change.Content))}
			if old, ok := files[change.DocumentID]; ok && old.ContentHash == f.ContentHash {
				f.ObjectKey = old.ObjectKey
			} else {
				f, e = s.repositoryStoreFile(project, f, change.Content)
				newKeys = append(newKeys, f.ObjectKey)
				if e != nil {
					return e
				}
			}
			files[f.DocumentID] = f
		}
		result := make([]repo.File, 0, len(files))
		for _, f := range files {
			result = append(result, f)
		}
		if len(result) > 5000 {
			return fmt.Errorf("%w: repository file limit exceeded", ErrInvalidArgument)
		}
		if e = repo.ValidateFiles(result); e != nil {
			return e
		}
		if repo.TreeHash(result) == parent.TreeHash {
			return fmt.Errorf("%w: no changes to commit", ErrFailedPrecondition)
		}
		message := input.Message
		if strings.TrimSpace(message) == "" {
			message = fmt.Sprintf("Update %d document(s)", len(input.Changes))
		}
		out = repo.NewCommit(project, actor, message, []string{parent.ID}, result, time.Now())
		if e = s.repositoryAdvance(storage, actor, project, b, out); e != nil {
			return e
		}
		if _, e = storage.RepositoryRecord(s.requestContext(), project, "metadata", "converted"); errors.Is(e, ErrNotFound) {
			e = repositoryPut(s.requestContext(), storage, project, "metadata", "converted", map[string]any{"converted_at": time.Now().UTC(), "actor_id": actor}, 1, nil)
		}
		if e != nil {
			return e
		}
		return repositoryPut(s.requestContext(), storage, project, "idempotency", key, repositoryIdempotency{Fingerprint: fingerprint, CommitID: out.ID}, 1, nil)
	})
	if err != nil {
		refs := make([]domain.ObjectRef, 0, len(newKeys))
		for _, k := range newKeys {
			refs = append(refs, domain.ObjectRef{Key: k})
		}
		err = s.cleanupNewObjectRefs(err, refs...)
	}
	return
}

func (s *Store) RepositoryEvents(actor, project string, after int64) (out []repo.Event, err error) {
	if after < 0 {
		return nil, ErrInvalidArgument
	}
	out = []repo.Event{}
	err = s.WithRepositoryRead(actor, project, func(storage repo.Storage) error {
		rows, e := storage.RepositoryRecords(s.requestContext(), project, "event", after, 200)
		if e != nil {
			return e
		}
		for _, r := range rows {
			var event repo.Event
			if e = json.Unmarshal(r.Payload, &event); e != nil {
				return e
			}
			event.Sequence = r.Sequence
			out = append(out, event)
		}
		return nil
	})
	return
}

func (m *repositoryMemory) RepositoryLatestSequence(_ context.Context, project, kind string) (int64, error) {
	var latest int64
	for _, r := range m.records {
		if r.ProjectID == project && r.Kind == kind {
			latest = max(latest, r.Sequence)
		}
	}
	return latest, nil
}

func repositorySortedFiles(files map[string]repo.File) []repo.File {
	out := make([]repo.File, 0, len(files))
	for _, f := range files {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RelativePath < out[j].RelativePath })
	return out
}
