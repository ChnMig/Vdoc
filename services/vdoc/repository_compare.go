package vdoc

import (
	"fmt"
	"slices"
	"sort"

	repo "vdoc/domain/documentrepo"
)

const repositoryComparisonFileLimit = 100
const repositoryComparisonByteLimit = 8 << 20

func (s *Store) RepositoryCompare(actor, project string, input repo.CompareInput) (out *repo.Comparison, err error) {
	if input.FromCommitID == "" || input.ToCommitID == "" {
		return nil, ErrInvalidArgument
	}
	var from, to *repo.Commit
	var snapshot *Store
	err = s.WithRepositoryRead(actor, project, func(storage repo.Storage) error {
		var e error
		from, e = repositoryLoad[repo.Commit](s.requestContext(), storage, project, "commit", input.FromCommitID)
		if e != nil {
			return e
		}
		if e = s.repositoryRequireActiveFiles(storage, project, from.Files, input.DocumentID); e != nil {
			return e
		}
		to, e = repositoryLoad[repo.Commit](s.requestContext(), storage, project, "commit", input.ToCommitID)
		if e != nil {
			return e
		}
		if e = s.repositoryRequireActiveFiles(storage, project, to.Files, input.DocumentID); e != nil {
			return e
		}
		files, e := repositoryChangedFiles(from, to, input.DocumentID)
		if e != nil {
			return e
		}
		// 只在锁内固定不可变引用及内存正文；对象存储读取和解析不占共享写锁或数据库事务。
		snapshot = &Store{ctx: s.requestContext(), storeState: &storeState{objects: s.objects}}
		if s.objects == nil && s.repoMemory != nil {
			snapshot.repoMemory = newRepositoryMemory()
			bytesRead := 0
			for _, file := range files {
				for _, source := range []repo.File{file.before, file.after} {
					if body, ok := s.repoMemory.blobs[source.ObjectKey]; ok {
						bytesRead += len(body)
						if bytesRead > repositoryComparisonByteLimit {
							return repositoryComparisonSizeError()
						}
						snapshot.repoMemory.blobs[source.ObjectKey] = slices.Clone(body)
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out, err = snapshot.repositoryCompare(from, to, input.DocumentID)
	if err != nil {
		return nil, err
	}
	// 锁外工作期间允许撤权和删除；返回正文前重新检查当前权限、token 和文档状态。
	err = s.WithRepositoryRead(actor, project, func(storage repo.Storage) error {
		if err := s.repositoryRequireActiveFiles(storage, project, from.Files, input.DocumentID); err != nil {
			return err
		}
		return s.repositoryRequireActiveFiles(storage, project, to.Files, input.DocumentID)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

type repositoryComparisonFile struct {
	before, after       repo.File
	hasBefore, hasAfter bool
}

func repositoryChangedFiles(from, to *repo.Commit, documentID string) ([]repositoryComparisonFile, error) {
	left, right := map[string]repo.File{}, map[string]repo.File{}
	ids := map[string]bool{}
	for _, f := range from.Files {
		left[f.DocumentID] = f
		ids[f.DocumentID] = true
	}
	for _, f := range to.Files {
		right[f.DocumentID] = f
		ids[f.DocumentID] = true
	}
	keys := make([]string, 0, len(ids))
	for key := range ids {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	files := []repositoryComparisonFile{}
	for _, key := range keys {
		if documentID != "" && key != documentID {
			continue
		}
		a, aok := left[key]
		b, bok := right[key]
		if aok != bok || !repo.EqualFile(a, b) {
			files = append(files, repositoryComparisonFile{a, b, aok, bok})
		}
	}
	if len(files) > repositoryComparisonFileLimit {
		return nil, fmt.Errorf("%w: COMPARISON_LIMIT: compare at most %d changed documents per request; select a document to narrow the comparison", ErrFailedPrecondition, repositoryComparisonFileLimit)
	}
	return files, nil
}

func repositoryComparisonSizeError() error {
	return fmt.Errorf("%w: COMPARISON_LIMIT: comparison source exceeds %d bytes; compare fewer documents or read individual commit content", ErrFailedPrecondition, repositoryComparisonByteLimit)
}

func (s *Store) repositoryCompare(from, to *repo.Commit, documentID string) (*repo.Comparison, error) {
	files, err := repositoryChangedFiles(from, to, documentID)
	if err != nil {
		return nil, err
	}
	out := &repo.Comparison{ID: repo.Hash([]byte(from.ProjectID + ":" + from.ID + ":" + to.ID + ":" + documentID + ":source-semantic-v1")), FromCommitID: from.ID, ToCommitID: to.ID, Files: []repo.FileChange{}}
	bytesRead := 0
	for _, file := range files {
		if err := s.requestContext().Err(); err != nil {
			return nil, err
		}
		a, b, aok, bok := file.before, file.after, file.hasBefore, file.hasAfter
		f := b
		if !bok {
			f = a
		}
		change := repo.FileChange{DocumentID: f.DocumentID, RelativePath: f.RelativePath, DocumentType: f.DocumentType, ChangeType: "modified", SemanticStatus: "not_applicable"}
		var err error
		if aok {
			change.OldContent, err = s.repositoryFileContent(a)
			if err != nil {
				return nil, err
			}
			bytesRead += len(change.OldContent)
			if bytesRead > repositoryComparisonByteLimit {
				return nil, repositoryComparisonSizeError()
			}
		}
		if bok {
			change.NewContent, err = s.repositoryFileContent(b)
			if err != nil {
				return nil, err
			}
			bytesRead += len(change.NewContent)
			if bytesRead > repositoryComparisonByteLimit {
				return nil, repositoryComparisonSizeError()
			}
		}
		if !aok {
			change.ChangeType = "added"
		} else if !bok {
			change.ChangeType = "deleted"
		} else if a.RelativePath != b.RelativePath {
			change.ChangeType = "renamed"
			change.OldRelativePath = a.RelativePath
		}
		if f.DocumentType == DocumentTypeOpenAPI {
			change.SemanticStatus = "invalid"
			var before, after []Endpoint
			format := DocumentFormatOpenAPI30
			valid := true
			if aok {
				parsed, e := s.parseOpenAPI(change.OldContent)
				if e != nil {
					valid = false
				} else {
					before = parsed.Endpoints
					format = parsed.SchemaFormat
				}
			}
			if bok {
				parsed, e := s.parseOpenAPI(change.NewContent)
				if e != nil {
					valid = false
				} else {
					after = parsed.Endpoints
					format = parsed.SchemaFormat
				}
			}
			if valid {
				change.SemanticStatus = "ready"
				change.SemanticDiff = s.diffEndpointSetsLocked(f.DocumentID, from.ID, to.ID, format, before, after)
			}
		}
		out.Files = append(out.Files, change)
	}
	return out, s.requestContext().Err()
}

func (s *Store) repositoryValidatePublished(c *repo.Commit) error {
	for _, file := range c.Files {
		if file.DocumentType != DocumentTypeOpenAPI {
			continue
		}
		content, err := s.repositoryFileContent(file)
		if err != nil {
			return err
		}
		if _, err = s.parseOpenAPI(content); err != nil {
			return fmt.Errorf("%w: %s has an invalid OpenAPI schema", ErrFailedPrecondition, file.RelativePath)
		}
	}
	return nil
}
