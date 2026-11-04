package db_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	databasepkg "vdoc/db"
	pgvdoc "vdoc/db/pgdb/vdoc"
	repo "vdoc/domain/documentrepo"
	"vdoc/domain/documentshare"
	app "vdoc/services/vdoc"
)

func TestPostgresRepositoryDocumentManagementPersistsAndRollsBackAtomically(t *testing.T) {
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
	port := pgvdoc.NewRepository(database)
	objects := &optimizationObjects{bodies: map[string][]byte{}}
	cfg := app.RuntimeConfig{DatabaseEnabled: true, DatabaseRepository: port, ObjectStorage: objects, AllowRegistration: true}
	if err := app.InitDefaultStore(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	defer app.CloseDefaultStore()
	s := app.DefaultStore()
	u, err := s.Register("document-management@example.test", "Owner", "Document-management-test-password-2026!")
	if err != nil {
		t.Fatal(err)
	}
	team, err := s.CreateTeam(u.ID, "Document management", "")
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.CreateProject(u.ID, team.ID, "Document management", "", u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateDocumentFolderAtPath(u.ID, p.ID, "", "Docs"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateDocumentFolderAtPath(u.ID, p.ID, "", "Manual"); err != nil {
		t.Fatal(err)
	}
	created, err := s.RepositoryCreateDocument(u.ID, p.ID, repo.DocumentInput{Name: "Original", RelativePath: "Docs/original.md", DocumentType: app.DocumentTypeMarkdown, Content: "original\r\nexact bytes\r\n", IdempotencyKey: "persistent-crud-document"})
	if err != nil {
		t.Fatal(err)
	}
	scoped := s.WithRepositoryDocument(created.Document.ID)
	release, err := scoped.RepositoryPublish(u.ID, p.ID, repo.PublishInput{BranchID: created.Branch.ID, CommitID: created.Commit.ID, ExpectedGeneration: 1})
	if err != nil {
		t.Fatal(err)
	}
	share, err := s.CreateRepositoryShare(u.ID, p.ID, release.ID, created.Document.ID, repo.ShareInput{ExpiryPreset: documentshare.ExpiryPresetPermanent})
	if err != nil {
		t.Fatal(err)
	}
	name, target, description := "Renamed", "Manual/renamed.md", "Current description"
	patch := app.RepositoryDocumentPatchInput{Name: &name, RelativePath: &target, Description: &description}
	// 在审计插入处注入真实数据库失败，目录状态和仓库引用必须随事务一起回滚。
	if err = database.Exec(`CREATE FUNCTION reject_crud_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action IN ('document.move','document.delete') THEN RAISE EXCEPTION 'injected CRUD audit failure'; END IF; RETURN NEW; END $$`).Error; err != nil {
		t.Fatal(err)
	}
	if err = database.Exec(`CREATE TRIGGER reject_crud_audit BEFORE INSERT ON audit_logs FOR EACH ROW EXECUTE FUNCTION reject_crud_audit()`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = s.RepositoryUpdateDocument(u.ID, p.ID, created.Document.ID, patch); err == nil {
		t.Fatal("injected audit failure did not abort metadata transaction")
	}
	current, err := s.RepositoryDocument(u.ID, p.ID, created.Document.ID)
	if err != nil || current.Name != "Original" || current.RelativePath != "Docs/original.md" {
		t.Fatalf("failed move changed catalog: %+v %v", current, err)
	}
	if err = s.RepositoryDeleteDocument(u.ID, p.ID, created.Document.ID); err == nil {
		t.Fatal("injected audit failure did not abort delete transaction")
	}
	if _, err = scoped.RepositoryContent(u.ID, p.ID, created.Commit.ID, created.Document.ID); err != nil {
		t.Fatalf("failed delete blocked original content: %v", err)
	}
	if err = database.Exec(`DROP TRIGGER reject_crud_audit ON audit_logs`).Error; err != nil {
		t.Fatal(err)
	}
	if err = database.Exec(`DROP FUNCTION reject_crud_audit()`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = s.RepositoryUpdateDocumentWithTypes(u.ID, p.ID, created.Document.ID, patch, []int{app.DocumentTypeOpenAPI}); !errors.Is(err, app.ErrPermissionDenied) {
		t.Fatalf("typed mutation accepted wrong type: %v", err)
	}
	if _, err = s.RepositoryUpdateDocument(u.ID, p.ID, created.Document.ID, patch); err != nil {
		t.Fatal(err)
	}
	if err = app.CloseDefaultStore(); err != nil {
		t.Fatal(err)
	}
	if err = app.InitDefaultStore(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	s = app.DefaultStore()
	scoped = s.WithRepositoryDocument(created.Document.ID)
	current, err = s.RepositoryDocument(u.ID, p.ID, created.Document.ID)
	if err != nil || current.Name != name || current.RelativePath != target || current.Description != description || current.DefaultBranchID != created.Branch.ID {
		t.Fatalf("metadata did not survive reload: %+v %v", current, err)
	}
	saved, err := scoped.RepositorySave(u.ID, p.ID, created.Branch.ID, repo.SaveInput{ExpectedHeadCommitID: created.Commit.ID, ExpectedGeneration: 1, IdempotencyKey: "persistent-crud-stale-save", Changes: []repo.Change{{Operation: "upsert", DocumentID: created.Document.ID, Name: "Original", RelativePath: "Docs/original.md", DocumentType: app.DocumentTypeMarkdown, Content: "updated body"}}})
	if err != nil || saved.Files[0].Name != name || saved.Files[0].RelativePath != target || saved.Files[0].Description != description {
		t.Fatalf("stale body save lost metadata: %+v %v", saved, err)
	}
	before, err := port.RepositoryRecord(ctx, p.ID, "commit", created.Commit.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RepositoryDeleteDocumentWithTypes(u.ID, p.ID, created.Document.ID, []int{app.DocumentTypeMarkdown}); err != nil {
		t.Fatal(err)
	}
	if err = s.RepositoryDeleteDocumentWithTypes(u.ID, p.ID, created.Document.ID, []int{app.DocumentTypeMarkdown}); err != nil {
		t.Fatalf("delete retry: %v", err)
	}
	if err = s.DeleteDocumentFolder(u.ID, p.ID, "Manual"); err != nil {
		t.Fatalf("tombstone blocked empty folder delete: %v", err)
	}
	if err = app.CloseDefaultStore(); err != nil {
		t.Fatal(err)
	}
	if err = app.InitDefaultStore(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	s = app.DefaultStore()
	if catalog, err := s.RepositoryDocuments(u.ID, p.ID); err != nil || len(catalog) != 0 {
		t.Fatalf("deleted document survived reload: %+v %v", catalog, err)
	}
	if _, err = s.RepositoryContent(u.ID, p.ID, created.Commit.ID, created.Document.ID); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("deleted raw read after reload: %v", err)
	}
	if _, err = s.PublicRepositoryShareContent(share.Share.ID, share.Secret, "", release.ID); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("deleted public share after reload: %v", err)
	}
	after, err := port.RepositoryRecord(ctx, p.ID, "commit", created.Commit.ID)
	if err != nil || string(after.Payload) != string(before.Payload) {
		t.Fatalf("delete rewrote immutable commit: %v", err)
	}
	var commit repo.Commit
	if err = json.Unmarshal(after.Payload, &commit); err != nil {
		t.Fatal(err)
	}
	if body, ok := objects.bodies[commit.Files[0].ObjectKey]; !ok || string(body) != "original\r\nexact bytes\r\n" {
		t.Fatal("delete removed original object")
	}
	var audits int64
	if err = database.Table("audit_logs").Where("project_id = ? AND action = 'document.delete'", p.ID).Count(&audits).Error; err != nil || audits != 1 {
		t.Fatalf("idempotent deletion audit count=%d: %v", audits, err)
	}
}
