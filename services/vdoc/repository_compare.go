package vdoc

import (
	"fmt"
	"sort"

	repo "vdoc/domain/documentrepo"
)

const repositoryComparisonFileLimit = 100
const repositoryComparisonByteLimit = 8 << 20

func (s *Store) RepositoryCompare(actor, project string, input repo.CompareInput) (out *repo.Comparison, err error) {
	if input.FromCommitID == "" || input.ToCommitID == "" {
		return nil, ErrInvalidArgument
	}
	err = s.WithRepositoryRead(actor, project, func(storage repo.Storage) error {
		from, e := repositoryLoad[repo.Commit](s.requestContext(), storage, project, "commit", input.FromCommitID)
		if e != nil {
			return e
		}
		to, e := repositoryLoad[repo.Commit](s.requestContext(), storage, project, "commit", input.ToCommitID)
		if e != nil {
			return e
		}
		out, e = s.repositoryCompare(from, to, input.DocumentID)
		return e
	})
	return
}

func (s *Store) repositoryCompare(from, to *repo.Commit, documentID string) (*repo.Comparison, error) {
	out := &repo.Comparison{ID: repo.Hash([]byte(from.ProjectID + ":" + from.ID + ":" + to.ID + ":" + documentID + ":source-semantic-v1")), FromCommitID: from.ID, ToCommitID: to.ID, Files: []repo.FileChange{}}
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
	changed := 0
	for _, key := range keys {
		if documentID != "" && key != documentID {
			continue
		}
		a, aok := left[key]
		b, bok := right[key]
		if aok != bok || !repo.EqualFile(a, b) {
			changed++
		}
	}
	if changed > repositoryComparisonFileLimit {
		return nil, fmt.Errorf("%w: COMPARISON_LIMIT: compare at most %d changed documents per request; select a document to narrow the comparison", ErrFailedPrecondition, repositoryComparisonFileLimit)
	}
	bytesRead := 0
	for _, key := range keys {
		if documentID != "" && key != documentID {
			continue
		}
		a, aok := left[key]
		b, bok := right[key]
		if aok == bok && repo.EqualFile(a, b) {
			continue
		}
		f := b
		if !bok {
			f = a
		}
		change := repo.FileChange{DocumentID: key, RelativePath: f.RelativePath, DocumentType: f.DocumentType, ChangeType: "modified", SemanticStatus: "not_applicable"}
		var err error
		if aok {
			change.OldContent, err = s.repositoryFileContent(a)
			if err != nil {
				return nil, err
			}
			bytesRead += len(change.OldContent)
			if bytesRead > repositoryComparisonByteLimit {
				return nil, fmt.Errorf("%w: COMPARISON_LIMIT: comparison source exceeds %d bytes; compare fewer documents or read individual commit content", ErrFailedPrecondition, repositoryComparisonByteLimit)
			}
		}
		if bok {
			change.NewContent, err = s.repositoryFileContent(b)
			if err != nil {
				return nil, err
			}
			bytesRead += len(change.NewContent)
			if bytesRead > repositoryComparisonByteLimit {
				return nil, fmt.Errorf("%w: COMPARISON_LIMIT: comparison source exceeds %d bytes; compare fewer documents or read individual commit content", ErrFailedPrecondition, repositoryComparisonByteLimit)
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
				change.SemanticDiff = s.diffEndpointSetsLocked(key, from.ID, to.ID, format, before, after)
			}
		}
		out.Files = append(out.Files, change)
	}
	return out, nil
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
