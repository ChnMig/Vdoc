package vdoc

import (
	"context"
	"reflect"
	"testing"
	"time"

	"vdoc/db/pgdb"
	domainvdoc "vdoc/domain/vdoc"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestDocumentModelFolderMappingPreservesPathAndNullableRoot(t *testing.T) {
	for _, folderID := range []string{"", "11111111111111111111111111111111"} {
		t.Run(folderID, func(t *testing.T) {
			document := &domainvdoc.APIService{
				ID: "document-id", ProjectID: "project-id", Name: "Display name",
				FolderID: folderID, DocumentType: domainvdoc.DocumentTypeMarkdown,
				RelativePath: "Guides/Setup/install.md",
			}
			model := documentModelFromDomain(document)
			if stringValue(model.FolderID) != folderID || (model.FolderID == nil) != (folderID == "") {
				t.Fatalf("folder ID = %v, want nullable mapping of %q", model.FolderID, folderID)
			}
			if model.RelativePath != document.RelativePath || model.Name != document.Name {
				t.Fatalf("folder mapping changed the independent display name or file path: %+v", model)
			}
			if model.FolderID != nil {
				*model.FolderID = "other-folder"
				if document.FolderID != folderID {
					t.Fatal("persistence model aliases the domain folder ID")
				}
			}
		})
	}
}

func TestDocumentFolderRepositoryMappingRoundTripsHierarchy(t *testing.T) {
	now := time.Date(2026, time.October, 24, 1, 2, 3, 0, time.UTC)
	for _, parentID := range []string{"", "22222222222222222222222222222222"} {
		t.Run(parentID, func(t *testing.T) {
			database := folderMappingDryRunDB(t)
			folder := &domainvdoc.DocumentFolder{
				ID: "11111111111111111111111111111111", ProjectID: "33333333333333333333333333333333",
				ParentID: parentID, Name: "安装", Path: "Guides/安装", CreatedBy: "44444444444444444444444444444444",
				CreatedAt: now, UpdatedAt: now,
			}
			var written *DocumentFolder
			if err := database.Callback().Create().After("gorm:create").Register("test:capture_folder", func(tx *gorm.DB) {
				if model, ok := tx.Statement.Dest.(*DocumentFolder); ok {
					copied := *model
					written = &copied
				}
			}); err != nil {
				t.Fatalf("register create callback: %v", err)
			}
			repository := NewRepository(database)
			if err := repository.UpsertDocumentFolder(context.Background(), folder); err != nil {
				t.Fatalf("upsert folder: %v", err)
			}
			if written == nil || (written.ParentID == nil) != (parentID == "") || stringValue(written.ParentID) != parentID {
				t.Fatalf("written folder = %+v, want nullable parent %q", written, parentID)
			}
			if err := database.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
				models, ok := tx.Statement.Dest.(*[]DocumentFolder)
				if !ok {
					t.Fatalf("query destination = %T, want folder models", tx.Statement.Dest)
				}
				*models = []DocumentFolder{*written}
				tx.RowsAffected = 1
			}); err != nil {
				t.Fatalf("register query callback: %v", err)
			}
			loaded := domainvdoc.NewState()
			if err := repository.loadDocumentFolders(context.Background(), loaded); err != nil {
				t.Fatalf("load folder: %v", err)
			}
			if got := loaded.DocumentFolders[folder.ID]; !reflect.DeepEqual(got, folder) {
				t.Fatalf("loaded folder = %+v, want %+v", got, folder)
			}
		})
	}
}

func TestLoadDocumentFoldersNormalizesPostgresUUIDs(t *testing.T) {
	database := folderMappingDryRunDB(t)
	parentID := "22222222-2222-2222-2222-222222222222"
	if err := database.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
		*tx.Statement.Dest.(*[]DocumentFolder) = []DocumentFolder{{
			Base:      pgdb.Base{ID: "11111111-1111-1111-1111-111111111111"},
			ProjectID: "33333333-3333-3333-3333-333333333333", ParentID: &parentID,
			Name: "child", Path: "parent/child", CreatedBy: "44444444-4444-4444-4444-444444444444",
		}}
	}); err != nil {
		t.Fatalf("register query callback: %v", err)
	}
	state := domainvdoc.NewState()
	if err := NewRepository(database).loadDocumentFolders(context.Background(), state); err != nil {
		t.Fatalf("load folder: %v", err)
	}
	folder := state.DocumentFolders["11111111111111111111111111111111"]
	if folder == nil || folder.ProjectID != "33333333333333333333333333333333" || folder.ParentID != "22222222222222222222222222222222" || folder.CreatedBy != "44444444444444444444444444444444" {
		t.Fatalf("folder UUID mapping = %+v", folder)
	}
}

func TestUpsertDocumentFolderRejectsUninitializedRepository(t *testing.T) {
	for name, repository := range map[string]*Repository{"nil receiver": nil, "nil database": NewRepository(nil)} {
		t.Run(name, func(t *testing.T) {
			if err := repository.UpsertDocumentFolder(context.Background(), &domainvdoc.DocumentFolder{ID: "folder-id"}); err == nil {
				t.Fatal("uninitialized repository accepted a folder write")
			}
		})
	}
	if err := NewRepository(folderMappingDryRunDB(t)).UpsertDocumentFolder(context.Background(), nil); err != nil {
		t.Fatalf("nil folder should remain a no-op: %v", err)
	}
}

func folderMappingDryRunDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(postgres.New(postgres.Config{DSN: "host=localhost user=vdoc dbname=vdoc_test"}), &gorm.Config{
		DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true,
	})
	if err != nil {
		t.Fatalf("open dry-run database: %v", err)
	}
	return database
}
