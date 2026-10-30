package vdoc

import (
	"context"
	"errors"
	"fmt"

	repo "vdoc/domain/documentrepo"
)

func (s *Store) repositoryRequireImported(storage repo.Storage, project string) error {
	required, err := s.repositoryImportRequired(storage, project)
	if err != nil {
		return err
	}
	if required {
		return fmt.Errorf("%w: LEGACY_IMPORT_REQUIRED: a project admin must import existing legacy snapshots before saving repository commits", ErrFailedPrecondition)
	}
	return nil
}

// 已有旧快照的项目必须先显式导入，不能用一个空项目提交遮断仍可导入的历史。
func (s *Store) repositoryImportRequired(storage repo.Storage, project string) (bool, error) {
	if _, err := storage.RepositoryRecord(s.requestContext(), project, "metadata", "converted"); err == nil {
		return false, nil
	} else if !errors.Is(err, ErrNotFound) {
		return false, err
	}
	if database, ok := storage.(interface {
		RepositoryHasLegacyContent(context.Context, string) (bool, error)
	}); ok {
		return database.RepositoryHasLegacyContent(s.requestContext(), project)
	}
	for _, version := range s.versions {
		if version.ProjectID == project {
			return true, nil
		}
	}
	for _, draft := range s.drafts {
		if draft.ProjectID == project {
			return true, nil
		}
	}
	return false, nil
}
