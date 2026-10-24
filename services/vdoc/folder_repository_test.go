package vdoc

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	domainvdoc "vdoc/domain/vdoc"
)

func TestSaveDocumentFoldersWritesParentsFirstAndSkipsUnchanged(t *testing.T) {
	store := NewStore()
	store.documentFolders["unchanged"] = &DocumentFolder{ID: "unchanged", ProjectID: "project", Name: "Existing", Path: "Existing"}
	store.persisted = store.cloneStateLocked()
	store.documentFolders["z-root"] = &DocumentFolder{ID: "z-root", ProjectID: "project", Name: "Docs", Path: "Docs"}
	store.documentFolders["a-child"] = &DocumentFolder{ID: "a-child", ProjectID: "project", ParentID: "z-root", Name: "API", Path: "Docs/API"}
	store.documentFolders["m-leaf"] = &DocumentFolder{ID: "m-leaf", ProjectID: "project", ParentID: "a-child", Name: "v1", Path: "Docs/API/v1"}
	repository := &folderOrderRepository{saved: map[string]bool{"unchanged": true}}
	if err := (&postgresPersistence{}).saveDocumentFoldersLocked(context.Background(), store, repository); err != nil {
		t.Fatal(err)
	}
	if want := []string{"z-root", "a-child", "m-leaf"}; !reflect.DeepEqual(repository.order, want) {
		t.Fatalf("folder write order = %v, want %v with unchanged rows omitted", repository.order, want)
	}
}

func TestSaveDocumentFoldersRejectsUnsupportedPersistenceAndStopsOnFailure(t *testing.T) {
	store := NewStore()
	store.documentFolders["root"] = &DocumentFolder{ID: "root", Name: "Docs", Path: "Docs"}
	store.documentFolders["child"] = &DocumentFolder{ID: "child", ParentID: "root", Name: "API", Path: "Docs/API"}
	if err := (&postgresPersistence{}).saveDocumentFoldersLocked(context.Background(), store, nil); err == nil || !strings.Contains(err.Error(), "does not support document folders") {
		t.Fatalf("missing repository capability error = %v", err)
	}
	failure := errors.New("folder write failed")
	repository := &folderOrderRepository{saved: map[string]bool{}, failure: failure}
	if err := (&postgresPersistence{}).saveDocumentFoldersLocked(context.Background(), store, repository); !errors.Is(err, failure) {
		t.Fatalf("folder persistence failure = %v, want %v", err, failure)
	}
	if len(repository.order) != 1 || repository.order[0] != "root" {
		t.Fatalf("folder writes continued after a failure: %v", repository.order)
	}
}

func TestFolderStateCloneDoesNotAliasMutationSnapshot(t *testing.T) {
	store := NewStore()
	store.documentFolders["folder"] = &DocumentFolder{ID: "folder", ProjectID: "project", Name: "Docs", Path: "Docs"}
	snapshot := store.cloneStateLocked()
	snapshot.DocumentFolders["folder"].Name = "Snapshot change"
	snapshot.DocumentFolders["new"] = &DocumentFolder{ID: "new"}
	if store.documentFolders["folder"].Name != "Docs" || store.documentFolders["new"] != nil {
		t.Fatal("mutation snapshot changed live folder state")
	}
	reloaded := NewStore()
	reloaded.applyStateLocked(snapshot)
	if reloaded.documentFolders["folder"].Name != "Snapshot change" || reloaded.stateLocked().DocumentFolders["new"] == nil {
		t.Fatal("folder state was omitted when applying or exporting a snapshot")
	}
}

type folderOrderRepository struct {
	domainvdoc.Repository
	saved   map[string]bool
	order   []string
	failure error
}

func (r *folderOrderRepository) UpsertDocumentFolder(_ context.Context, folder *domainvdoc.DocumentFolder) error {
	if folder.ParentID != "" && !r.saved[folder.ParentID] {
		return errors.New("parent folder must be written before child")
	}
	r.order = append(r.order, folder.ID)
	if r.failure != nil {
		return r.failure
	}
	r.saved[folder.ID] = true
	return nil
}
