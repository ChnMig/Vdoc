package db_test

import (
	"context"
	"errors"
	"os"
	"testing"

	databasepkg "vdoc/db"
	pgvdoc "vdoc/db/pgdb/vdoc"
	repo "vdoc/domain/documentrepo"
	app "vdoc/services/vdoc"
)

func TestPostgresDocumentFolderLocationsSurviveReload(t *testing.T) {
	dsn := os.Getenv("VDOC_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("VDOC_TEST_DATABASE_DSN not set")
	}
	database := openAIGenerationTestDB(t, dsn)
	defer closeAIGenerationTestDB(t, database)
	resetAIGenerationTestSchema(t, database)
	ctx := context.Background()
	if err := databasepkg.RunMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}
	cfg := app.RuntimeConfig{DatabaseEnabled: true, DatabaseRepository: pgvdoc.NewRepository(database), ObjectStorage: &optimizationObjects{bodies: map[string][]byte{}}, AllowRegistration: true}
	if err := app.InitDefaultStore(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	defer app.CloseDefaultStore()
	s := app.DefaultStore()
	u, err := s.Register("folder-locations@example.test", "Owner", "Folder-locations-test-password-2026!")
	if err != nil {
		t.Fatal(err)
	}
	team, err := s.CreateTeam(u.ID, "Folders", "")
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.CreateProject(u.ID, team.ID, "Folders", "", u.ID)
	if err != nil {
		t.Fatal(err)
	}
	created, err := s.RepositoryCreateDocument(u.ID, p.ID, repo.DocumentInput{Name: "Guide", RelativePath: "Docs/API/guide.md", DocumentType: 2, Content: "original\r\n", IdempotencyKey: "folder-persistent-doc"})
	if err != nil {
		t.Fatal(err)
	}
	empty, err := s.CreateDocumentFolderAtPath(u.ID, p.ID, "Docs/API", "Empty")
	if err != nil || empty.Path != "Docs/API/Empty" {
		t.Fatalf("empty inferred parent: %+v %v", empty, err)
	}
	if err = s.DeleteDocumentFolder(u.ID, p.ID, "Docs/API"); !errors.Is(err, app.ErrFailedPrecondition) {
		t.Fatalf("nonempty delete: %v", err)
	}
	if err = s.DeleteDocumentFolder(u.ID, p.ID, empty.Path); err != nil {
		t.Fatal(err)
	}
	archive, err := s.CreateDocumentFolderAtPath(u.ID, p.ID, "", "Archive")
	if err != nil {
		t.Fatal(err)
	}
	name, parent := "Reference", archive.Path
	moved, err := s.UpdateDocumentFolderLocation(u.ID, p.ID, "Docs/API", &name, &parent)
	if err != nil || moved.Path != "Archive/Reference" || moved.ParentID != archive.ID {
		t.Fatalf("move: %+v %v", moved, err)
	}
	if err = app.CloseDefaultStore(); err != nil {
		t.Fatal(err)
	}
	if err = app.InitDefaultStore(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	s = app.DefaultStore()
	folders, err := s.ListDocumentFolders(u.ID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, folder := range folders {
		if folder.ID == empty.ID {
			t.Fatal("deleted folder survived reload")
		}
		if folder.ID == moved.ID {
			found = folder.Path == "Archive/Reference" && folder.ParentID == archive.ID
		}
	}
	if !found {
		t.Fatal("folder move did not survive reload")
	}
	catalog, err := s.RepositoryDocuments(u.ID, p.ID)
	if err != nil || len(catalog) != 1 || catalog[0].RelativePath != "Archive/Reference/guide.md" {
		t.Fatalf("catalog: %+v %v", catalog, err)
	}
	scoped := s.WithRepositoryDocument(created.Document.ID)
	saved, err := scoped.RepositorySave(u.ID, p.ID, created.Branch.ID, repo.SaveInput{ExpectedHeadCommitID: created.Commit.ID, ExpectedGeneration: created.Branch.Generation, IdempotencyKey: "folder-persistent-save", Changes: []repo.Change{{Operation: "upsert", DocumentID: created.Document.ID, Name: "Guide", RelativePath: "Docs/API/guide.md", DocumentType: 2, Content: "updated"}}})
	if err != nil || saved.Files[0].RelativePath != "Archive/Reference/guide.md" {
		t.Fatalf("stale editor save: %+v %v", saved, err)
	}
	old, err := scoped.RepositoryContent(u.ID, p.ID, created.Commit.ID, created.Document.ID)
	if err != nil || old.Content != "original\r\n" || old.Document.RelativePath != "Docs/API/guide.md" {
		t.Fatalf("immutable history: %+v %v", old, err)
	}
}
