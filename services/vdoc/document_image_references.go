package vdoc

import (
	"context"
	"fmt"
	"sort"

	"vdoc/domain/documentimage"
	repo "vdoc/domain/documentrepo"
	domain "vdoc/domain/vdoc"
)

// 与图片回收共用项目事务锁，不能把已回收的暂存引用写入新版本。
func (s *Store) validateDocumentImageReferences(storage repo.Storage, project, document, content string) error {
	ids, err := documentimage.References(content)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidArgument, err)
	}
	if len(ids) == 0 {
		return nil
	}
	if scoped, ok := storage.(documentRepositoryStorage); ok {
		storage = scoped.Storage
	}
	if storage == nil {
		return fmt.Errorf("%w: image storage unavailable", ErrFailedPrecondition)
	}
	for _, id := range ids {
		asset, err := repositoryLoad[storedDocumentImage](s.requestContext(), storage, project, documentImageKind, documentImageID(document, id))
		if err != nil || asset.ProjectID != project || asset.DocumentID != document || asset.AssetID != id || asset.ObjectKey == "" {
			return fmt.Errorf("%w: image %s is unavailable; upload it to this document again before saving", ErrFailedPrecondition, id)
		}
	}
	return nil
}

func (s *Store) validateLegacyImageDrafts(ctx context.Context, repository any) error {
	var previous map[string]*domain.ContractDraft
	if s.persisted != nil {
		previous = s.persisted.Drafts
	}
	drafts := changedStoreValues(s.drafts, previous, func(value *domain.ContractDraft) string { return value.ID })
	projects := map[string]bool{}
	selected := []*domain.ContractDraft{}
	for _, draft := range drafts {
		if draft.SchemaFormat != DocumentFormatMarkdown {
			continue
		}
		ids, err := documentimage.References(draft.RawSchema)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidArgument, err)
		}
		if len(ids) > 0 {
			projects[draft.ProjectID] = true
			selected = append(selected, draft)
		}
	}
	if len(selected) == 0 {
		return nil
	}
	storage, ok := repository.(repo.Storage)
	if !ok {
		return fmt.Errorf("%w: image storage unavailable", ErrFailedPrecondition)
	}
	ordered := []string{}
	for project := range projects {
		ordered = append(ordered, project)
	}
	sort.Strings(ordered)
	for _, project := range ordered {
		if err := storage.LockRepository(ctx, project); err != nil {
			return err
		}
	}
	for _, draft := range selected {
		if err := s.validateDocumentImageReferences(storage, draft.ProjectID, documentIdentity(draft.DocumentID, draft.ServiceID), draft.RawSchema); err != nil {
			return err
		}
	}
	return nil
}
