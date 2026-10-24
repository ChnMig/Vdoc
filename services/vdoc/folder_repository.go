package vdoc

import (
	"context"
	"fmt"
	"sort"
	"strings"

	domainvdoc "vdoc/domain/vdoc"
)

type documentFolderMutationRepository interface {
	UpsertDocumentFolder(context.Context, *domainvdoc.DocumentFolder) error
}

func (p *postgresPersistence) saveDocumentFoldersLocked(ctx context.Context, store *Store, repository domainvdoc.Repository) error {
	var previous map[string]*DocumentFolder
	if store.persisted != nil {
		previous = store.persisted.DocumentFolders
	}
	folders := changedStoreValues(store.documentFolders, previous, func(folder *DocumentFolder) string { return folder.ID })
	if len(folders) == 0 {
		return nil
	}
	repo, ok := repository.(documentFolderMutationRepository)
	if !ok {
		return fmt.Errorf("repository does not support document folders")
	}
	// 先写入父节点，确保同一事务中的复合外键始终成立。
	sort.Slice(folders, func(i, j int) bool {
		left, right := strings.Count(folders[i].Path, "/"), strings.Count(folders[j].Path, "/")
		if left != right {
			return left < right
		}
		return folders[i].Path < folders[j].Path
	})
	for _, folder := range folders {
		if err := repo.UpsertDocumentFolder(ctx, folder); err != nil {
			return err
		}
	}
	return nil
}
