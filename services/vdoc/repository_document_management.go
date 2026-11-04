package vdoc

import (
	"context"
	"encoding/json"
	"errors"
	"path"
	"slices"
	"strings"
	"time"

	repo "vdoc/domain/documentrepo"
)

type RepositoryDocumentPatchInput struct {
	Name         *string `json:"name"`
	RelativePath *string `json:"relative_path"`
	Description  *string `json:"description"`
}

func (input *RepositoryDocumentPatchInput) UnmarshalJSON(body []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return err
	}
	if len(fields) == 0 {
		return ErrInvalidArgument
	}
	*input = RepositoryDocumentPatchInput{}
	for key, raw := range fields {
		var value string
		if strings.TrimSpace(string(raw)) == "null" || json.Unmarshal(raw, &value) != nil {
			return ErrInvalidArgument
		}
		switch key {
		case "name":
			input.Name = &value
		case "relative_path":
			input.RelativePath = &value
		case "description":
			input.Description = &value
		default:
			return ErrInvalidArgument
		}
	}
	return nil
}

func (s *Store) repositoryDocumentLocked(storage repo.Storage, project, documentID string) (*repo.Document, error) {
	if documentID == "" {
		return nil, ErrInvalidArgument
	}
	values, err := s.repositoryDocumentsLocked(storage, project)
	if err != nil {
		return nil, err
	}
	for _, document := range values {
		if document.ID == documentID {
			return &document, nil
		}
	}
	return nil, ErrNotFound
}

// 删除状态以当前目录为准，历史提交和旧分享中的文件不能重新开放访问。
func (s *Store) repositoryRequireActiveDocument(storage repo.Storage, project, documentID string) error {
	if scoped, ok := storage.(documentRepositoryStorage); ok {
		storage = scoped.Storage
	}
	if err := repositoryCatalogDocumentActive(s.requestContext(), storage, project, documentID); err != nil {
		return err
	}
	if legacy := s.apiServices[documentID]; legacy != nil && legacy.ProjectID == project && legacy.Status != DocumentStatusActive {
		return ErrNotFound
	}
	return nil
}

func repositoryCatalogDocumentActive(ctx context.Context, storage repo.Storage, project, documentID string) error {
	if storage == nil {
		return nil
	}
	document, err := repositoryLoad[repo.Document](ctx, storage, project, "document", documentID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if document.Status != DocumentStatusActive {
		return ErrNotFound
	}
	return nil
}

func (s *Store) repositoryCatalogDocumentVisibleLocked(project, documentID string) bool {
	var storage repo.Storage
	if s.persistence != nil {
		storage, _ = s.persistence.repo.(repo.Storage)
	} else if s.repoMemory != nil {
		storage = s.repoMemory
	}
	return repositoryCatalogDocumentActive(s.requestContext(), storage, project, documentID) == nil
}

func (s *Store) repositoryRequireActiveFiles(storage repo.Storage, project string, files []repo.File, documentID string) error {
	for _, file := range files {
		if documentID == "" || file.DocumentID == documentID {
			if err := s.repositoryRequireActiveDocument(storage, project, file.DocumentID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) RepositoryDocument(actor, project, documentID string) (out *repo.Document, err error) {
	s = s.WithRepositoryDocument("")
	err = s.WithRepositoryRead(actor, project, func(storage repo.Storage) error {
		out, err = s.repositoryDocumentLocked(storage, project, documentID)
		if err == nil && out.Status != DocumentStatusActive {
			return ErrNotFound
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) RepositoryUpdateDocument(actor, project, documentID string, input RepositoryDocumentPatchInput, audit ...AuditContext) (*repo.Document, error) {
	return s.RepositoryUpdateDocumentWithTypes(actor, project, documentID, input, nil, audit...)
}

func (s *Store) RepositoryUpdateDocumentWithTypes(actor, project, documentID string, input RepositoryDocumentPatchInput, allowedTypes []int, audit ...AuditContext) (out *repo.Document, err error) {
	if input.Name == nil && input.RelativePath == nil && input.Description == nil {
		return nil, ErrInvalidArgument
	}
	err = s.withRepositoryStateWrite(actor, project, MemberRoleAdmin, func(storage repo.Storage) error {
		document, err := s.repositoryDocumentLocked(storage, project, documentID)
		if err != nil {
			return err
		}
		if allowedTypes != nil && !slices.Contains(allowedTypes, document.DocumentType) {
			return ErrPermissionDenied
		}
		if document.Status != DocumentStatusActive {
			return ErrNotFound
		}
		fromPath := document.RelativePath
		if input.Name != nil {
			document.Name = strings.TrimSpace(*input.Name)
		}
		if input.Description != nil {
			document.Description = *input.Description
		}
		if input.RelativePath != nil {
			document.RelativePath = *input.RelativePath
		}
		if err = repo.ValidateFiles([]repo.File{document.File}); err != nil {
			return err
		}
		catalog, err := s.repositoryDocumentsLocked(storage, project)
		if err != nil {
			return err
		}
		for _, other := range catalog {
			if other.ID == document.ID || other.Status != DocumentStatusActive {
				continue
			}
			if folderPathContains(other.RelativePath, document.RelativePath) || folderPathContains(document.RelativePath, other.RelativePath) {
				return ErrAlreadyExists
			}
		}
		for _, folder := range s.documentFolders {
			if folder.ProjectID == project && folderPathContains(folder.Path, document.RelativePath) {
				return ErrAlreadyExists
			}
		}
		parent := path.Dir(document.RelativePath)
		if parent != "." {
			if folder := s.folderByPathLocked(project, parent); folder != nil {
				document.RelativePath = folder.Path + "/" + path.Base(document.RelativePath)
			} else {
				exists := false
				for _, other := range catalog {
					if _, contained := replaceFolderPrefix(other.RelativePath, parent, parent); other.Status == DocumentStatusActive && contained {
						exists = true
						break
					}
				}
				if !exists {
					return ErrNotFound
				}
			}
		}
		now := time.Now().UTC()
		folderID, err := s.ensureDocumentParentFoldersLocked(actor, project, document.RelativePath, now)
		if err != nil {
			return err
		}
		document.UpdatedAt = now
		if legacy := s.apiServices[document.ID]; legacy != nil && legacy.ProjectID == project {
			legacy.Name, legacy.RelativePath, legacy.Description = document.Name, document.RelativePath, document.Description
			legacy.FolderID, legacy.UpdatedAt = folderID, now
		}
		if err = s.repositoryPutDocument(storage, project, document); err != nil {
			return err
		}
		action := "document.update"
		if fromPath != document.RelativePath {
			action = "document.move"
		}
		s.auditLocked(auditContext(audit), AuditActorUser, actor, action, "document", document.ID, project, "", auditMetadata("result", "success", "document_id", document.ID, "from_path", fromPath, "relative_path", document.RelativePath))
		out = document
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) repositoryPutDocument(storage repo.Storage, project string, document *repo.Document) error {
	record, err := storage.RepositoryRecord(s.requestContext(), project, "document", document.ID)
	generation := int64(1)
	var expected *int64
	if err == nil {
		generation, expected = record.Generation+1, &record.Generation
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	return repositoryPut(s.requestContext(), storage, project, "document", document.ID, document, generation, expected)
}

func (s *Store) RepositoryDeleteDocument(actor, project, documentID string, audit ...AuditContext) error {
	return s.RepositoryDeleteDocumentWithTypes(actor, project, documentID, nil, audit...)
}

func (s *Store) RepositoryDeleteDocumentWithTypes(actor, project, documentID string, allowedTypes []int, audit ...AuditContext) error {
	return s.withRepositoryStateWrite(actor, project, MemberRoleAdmin, func(storage repo.Storage) error {
		document, err := s.repositoryDocumentLocked(storage, project, documentID)
		if err != nil {
			return err
		}
		if allowedTypes != nil && !slices.Contains(allowedTypes, document.DocumentType) {
			return ErrPermissionDenied
		}
		if document.Status != DocumentStatusActive {
			return nil
		}
		document.Status, document.UpdatedAt = DocumentStatusArchived, time.Now().UTC()
		if legacy := s.apiServices[document.ID]; legacy != nil && legacy.ProjectID == project {
			legacy.Status, legacy.UpdatedAt = document.Status, document.UpdatedAt
		}
		if err = s.repositoryPutDocument(storage, project, document); err != nil {
			return err
		}
		s.auditLocked(auditContext(audit), AuditActorUser, actor, "document.delete", "document", document.ID, project, "", auditMetadata("result", "success", "document_id", document.ID, "relative_path", document.RelativePath))
		return nil
	})
}
