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
	domainvdoc "vdoc/domain/vdoc"
)

func TestPostgresPublicShareRechecksRevocationAndActiveParents(t *testing.T) {
	dsn := os.Getenv("VDOC_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("VDOC_TEST_DATABASE_DSN not set; skipping PostgreSQL public-share concurrency integration test")
	}
	database := openAIGenerationTestDB(t, dsn)
	defer closeAIGenerationTestDB(t, database)
	resetAIGenerationTestSchema(t, database)
	if err := databasepkg.RunMigrations(context.Background(), database); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	const (
		userID     = "11111111-1111-1111-1111-111111111111"
		teamID     = "22222222-2222-2222-2222-222222222222"
		projectID  = "33333333-3333-3333-3333-333333333333"
		documentID = "44444444-4444-4444-4444-444444444444"
		branchID   = "55555555-5555-5555-5555-555555555555"
		shareID    = "66666666-6666-6666-6666-666666666666"
		auditID    = "77777777-7777-7777-7777-777777777777"
	)
	statements := []string{
		`INSERT INTO users(id,email,password_hash,display_name,status) VALUES('` + userID + `','share-owner@example.com','hash','Share Owner',1)`,
		`INSERT INTO teams(id,name,slug,created_by) VALUES('` + teamID + `','Share Team','share-team','` + userID + `')`,
		`INSERT INTO projects(id,team_id,name,slug,status,created_by) VALUES('` + projectID + `','` + teamID + `','Share Project','share-project',1,'` + userID + `')`,
		`INSERT INTO project_members(project_id,user_id,role,status,added_by) VALUES('` + projectID + `','` + userID + `',3,1,'` + userID + `')`,
		`INSERT INTO documents(id,project_id,name,document_type,relative_path,status,created_by) VALUES('` + documentID + `','` + projectID + `','share-doc',2,'SHARE.md',1,'` + userID + `')`,
		`INSERT INTO document_branches(id,document_id,name,kind,status,created_by) VALUES('` + branchID + `','` + documentID + `','main',1,1,'` + userID + `')`,
	}
	for _, statement := range statements {
		if err := database.Exec(statement).Error; err != nil {
			t.Fatalf("seed public-share graph: %v", err)
		}
	}

	repository := pgvdoc.NewRepository(database)
	now := time.Now().UTC()
	share := &domainvdoc.DocumentShare{
		ID: shareID, ProjectID: projectID, DocumentID: documentID, BranchID: branchID,
		TokenHash: strings.Repeat("a", 64), TokenCiphertext: []byte("ciphertext"), CipherKID: "kid-1",
		VersionScope: domainvdoc.DocumentShareScopeLatest, Status: domainvdoc.DocumentShareStatusActive,
		CreatedBy: userID, CreatedAt: now, UpdatedAt: now,
	}
	if err := repository.UpsertDocumentShareIfUnchanged(context.Background(), share, nil); err != nil {
		t.Fatalf("UpsertDocumentShareIfUnchanged(create): %v", err)
	}
	state, err := repository.LoadState(context.Background())
	if err != nil {
		t.Fatalf("LoadState(share): %v", err)
	}
	loadedShare := state.Shares[stripUUID(shareID)]
	if loadedShare == nil {
		t.Fatalf("created share %s not loaded", shareID)
	}

	blocker := database.Begin()
	if blocker.Error != nil {
		t.Fatalf("begin blocker transaction: %v", blocker.Error)
	}
	if err := blocker.Exec(`SELECT id FROM projects WHERE id = ? FOR UPDATE`, projectID).Error; err != nil {
		_ = blocker.Rollback()
		t.Fatalf("lock project: %v", err)
	}
	accessResult := make(chan error, 1)
	go func() {
		accessResult <- repository.RecordPublicDocumentShareAccess(context.Background(), loadedShare.ID, &domainvdoc.AuditLog{
			ID: auditID, ActorType: domainvdoc.AuditActorAnonymous, Action: "document_share.view",
			ResourceType: "document_share", ResourceID: loadedShare.ID, ProjectID: loadedShare.ProjectID,
			ServiceID: loadedShare.DocumentID, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		})
	}()

	revoked := *loadedShare
	revokedAt := time.Now().UTC()
	revoked.Status = domainvdoc.DocumentShareStatusRevoked
	revoked.RevokedAt = &revokedAt
	revokedBy := stripUUID(userID)
	revoked.RevokedBy = &revokedBy
	if err := repository.UpsertDocumentShareIfUnchanged(context.Background(), &revoked, loadedShare); err != nil {
		_ = blocker.Rollback()
		t.Fatalf("UpsertDocumentShareIfUnchanged(revoke): %v", err)
	}
	if err := blocker.Rollback().Error; err != nil {
		t.Fatalf("release project lock: %v", err)
	}
	select {
	case err := <-accessResult:
		if !errors.Is(err, domainvdoc.ErrFailedPrecondition) {
			t.Fatalf("access racing revocation error = %v, want failed precondition", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("public share access did not finish after project lock release")
	}
	var auditCount int64
	if err := database.Table("audit_logs").Where("id = ?", auditID).Count(&auditCount).Error; err != nil {
		t.Fatalf("count rejected access audit: %v", err)
	}
	if auditCount != 0 {
		t.Fatalf("rejected access audits = %d, want 0", auditCount)
	}

	if err := database.Exec(`UPDATE projects SET status = 2, updated_at = now() WHERE id = ?`, projectID).Error; err != nil {
		t.Fatalf("archive project: %v", err)
	}
	staleCreate := *share
	staleCreate.ID = "88888888-8888-8888-8888-888888888888"
	staleCreate.TokenHash = strings.Repeat("b", 64)
	if err := repository.UpsertDocumentShareIfUnchanged(context.Background(), &staleCreate, nil); !errors.Is(err, domainvdoc.ErrNotFound) {
		t.Fatalf("share create against archived project error = %v, want not found", err)
	}
	var staleCount int64
	if err := database.Table("document_shares").Where("id = ?", staleCreate.ID).Count(&staleCount).Error; err != nil {
		t.Fatalf("count stale share: %v", err)
	}
	if staleCount != 0 {
		t.Fatalf("shares created against archived project = %d, want 0", staleCount)
	}
}
