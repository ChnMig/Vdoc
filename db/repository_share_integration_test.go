package db_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	databasepkg "vdoc/db"
	pgvdoc "vdoc/db/pgdb/vdoc"
	repo "vdoc/domain/documentrepo"
	"vdoc/domain/documentshare"
	domain "vdoc/domain/vdoc"
	app "vdoc/services/vdoc"
	"vdoc/utils/encryption"
	"vdoc/utils/id"
)

func TestPostgresRepositoryShareSurvivesReloadAndRejectsIdentityCollision(t *testing.T) {
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
	objects := &optimizationObjects{bodies: map[string][]byte{}}
	keyring, err := encryption.NewKeyring("repository-share-old", "repository-share-old-key-0123456789012345", nil)
	if err != nil {
		t.Fatal(err)
	}
	config := app.RuntimeConfig{DatabaseEnabled: true, DatabaseRepository: pgvdoc.NewRepository(database), ObjectStorage: objects, AllowRegistration: true, CipherKeyring: keyring}
	if err = app.InitDefaultStore(ctx, config); err != nil {
		t.Fatal(err)
	}
	defer app.CloseDefaultStore()
	store := app.DefaultStore()
	user, err := store.Register("repository-share@example.test", "Owner", "Repository-share-test-password-2026!")
	if err != nil {
		t.Fatal(err)
	}
	team, err := store.CreateTeam(user.ID, "Repository share", "")
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(user.ID, team.ID, "Repository share", "", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	branches, err := store.RepositoryBranches(user.ID, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	branch, err := store.RepositoryCreateBranch(user.ID, project.ID, repo.BranchInput{Name: "test-sharing", FromCommitID: branches[0].HeadCommitID})
	if err != nil {
		t.Fatal(err)
	}
	commit, err := store.RepositorySave(user.ID, project.ID, branch.ID, repo.SaveInput{ExpectedHeadCommitID: branch.HeadCommitID, ExpectedGeneration: branch.Generation, IdempotencyKey: "repository-share-create", Changes: []repo.Change{{Operation: "upsert", Name: "Shared text", RelativePath: "docs/release.txt", DocumentType: app.DocumentTypeText, Content: "Exact text\r\nincluding CRLF\r\n"}}})
	if err != nil {
		t.Fatal(err)
	}
	release, err := store.RepositoryPublish(user.ID, project.ID, repo.PublishInput{BranchID: branch.ID, CommitID: commit.ID, ExpectedGeneration: branch.Generation + 1})
	if err != nil {
		t.Fatal(err)
	}
	document := commit.Files[0].DocumentID
	share, err := store.CreateRepositoryShare(user.ID, project.ID, release.ID, document, repo.ShareInput{ExpiryPreset: documentshare.ExpiryPresetPermanent, Password: "public-share-password-2026"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PublicRepositoryShareMetadata(share.Share.ID, share.Secret, ""); !errors.Is(err, app.ErrPublicSharePasswordRequired) {
		t.Fatal("persistent protected share did not challenge")
	}
	proof, _, err := store.UnlockPublicRepositoryShare(share.Share.ID, share.Secret, "public-share-password-2026")
	if err != nil {
		t.Fatal(err)
	}
	content, err := store.PublicRepositoryShareContent(share.Share.ID, share.Secret, proof, release.ID)
	if err != nil || content.Content != "Exact text\r\nincluding CRLF\r\n" {
		t.Fatalf("persistent content err=%v", err)
	}
	if err = app.CloseDefaultStore(); err != nil {
		t.Fatal(err)
	}
	config.CipherKeyring, err = encryption.NewKeyring("repository-share-new", "repository-share-new-key-9876543210987654", map[string]string{"repository-share-old": "repository-share-old-key-0123456789012345"})
	if err != nil {
		t.Fatal(err)
	}
	if err = app.InitDefaultStore(ctx, config); err != nil {
		t.Fatal(err)
	}
	store = app.DefaultStore()
	revealed, err := store.RevealRepositoryShare(user.ID, project.ID, release.ID, document, share.Share.ID)
	if err != nil || revealed.Secret != share.Secret {
		t.Fatal("reload and key rotation changed capability")
	}
	download, err := store.PublicRepositoryShareDownload(share.Share.ID, share.Secret, proof, release.ID)
	if err != nil || download.Filename != "release.txt" || string(download.Body) != content.Content {
		t.Fatalf("reload download err=%v", err)
	}
	// 活跃父级判断必须等待另一实例的归档事务，不能使用等待前的旧状态。
	blocker := database.Begin()
	if blocker.Error != nil {
		t.Fatal(blocker.Error)
	}
	defer blocker.Rollback()
	if err = blocker.Exec("SELECT id FROM projects WHERE id = ? FOR UPDATE", project.ID).Error; err != nil {
		t.Fatal(err)
	}
	access := make(chan error, 1)
	go func() {
		_, e := store.WithContext(ctx).PublicRepositoryShareMetadata(share.Share.ID, share.Secret, proof)
		access <- e
	}()
	select {
	case e := <-access:
		t.Fatalf("public read bypassed parent row lock: %v", e)
	case <-time.After(50 * time.Millisecond):
	}
	if err = blocker.Exec("UPDATE projects SET status = 2 WHERE id = ?", project.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err = blocker.Commit().Error; err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-access:
		if !errors.Is(e, app.ErrNotFound) {
			t.Fatalf("concurrent archive read error=%v", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("public read did not finish after archive")
	}
	if err = database.Exec("UPDATE projects SET status = 1 WHERE id = ?", project.ID).Error; err != nil {
		t.Fatal(err)
	}
	legacyDocument, err := store.CreateDocument(user.ID, project.ID, "Legacy text", app.DocumentTypeText, "legacy.txt", "")
	if err != nil {
		t.Fatal(err)
	}
	legacyBranches, err := store.ListBranches(user.ID, project.ID, legacyDocument.ID)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	legacy := &domain.DocumentShare{ID: share.Share.ID, ProjectID: project.ID, DocumentID: legacyDocument.ID, BranchID: legacyBranches[0].ID, TokenHash: strings.Repeat("a", 64), TokenCiphertext: []byte("ciphertext"), CipherKID: "test", VersionScope: domain.DocumentShareScopeLatest, Status: domain.DocumentShareStatusActive, CreatedBy: user.ID, CreatedAt: now, UpdatedAt: now}
	storage := pgvdoc.NewRepository(database)
	if err = storage.UpsertDocumentShareIfUnchanged(ctx, legacy, nil); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("legacy collision error=%v", err)
	}
	legacy.ID = id.GenerateID()
	if err = storage.UpsertDocumentShareIfUnchanged(ctx, legacy, nil); err != nil {
		t.Fatal(err)
	}
	if err = storage.PutRepositoryShareIndex(ctx, legacy.ID, project.ID); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("repository collision error=%v", err)
	}
	if _, err = store.RevokeRepositoryShare(user.ID, project.ID, release.ID, document, share.Share.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.PublicRepositoryShareContent(share.Share.ID, share.Secret, proof, release.ID); !errors.Is(err, app.ErrNotFound) {
		t.Fatal("persistent revoked capability readable")
	}
	if err = app.CloseDefaultStore(); err != nil {
		t.Fatal(err)
	}
	config.CipherKeyring, err = encryption.NewKeyring("repository-share-new", "repository-share-new-key-9876543210987654", nil)
	if err != nil {
		t.Fatal(err)
	}
	// 此测试中 SQL 创建的旧占位能力不参加服务密钥验证。
	if err = database.Exec("DELETE FROM document_shares WHERE id = ?", legacy.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err = app.InitDefaultStore(ctx, config); err != nil {
		t.Fatal(err)
	}
	if _, err = app.DefaultStore().PublicRepositoryShareMetadata(share.Share.ID, share.Secret, proof); !errors.Is(err, app.ErrNotFound) {
		t.Fatal("reload revived revoked capability")
	}
}
