package db_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	databasepkg "vdoc/db"
	pgvdoc "vdoc/db/pgdb/vdoc"
	domainvdoc "vdoc/domain/vdoc"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestPostgresFixedVersionShareOnlyLoadsAndAuditsPinnedVersion(t *testing.T) {
	dsn := os.Getenv("VDOC_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("VDOC_TEST_DATABASE_DSN not set; skipping PostgreSQL fixed-version share integration test")
	}
	database := openAIGenerationTestDB(t, dsn)
	defer closeAIGenerationTestDB(t, database)
	resetAIGenerationTestSchema(t, database)
	if err := databasepkg.RunMigrations(context.Background(), database); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	const (
		userID        = "11111111-1111-1111-1111-111111111111"
		teamID        = "22222222-2222-2222-2222-222222222222"
		projectID     = "33333333-3333-3333-3333-333333333333"
		documentID    = "44444444-4444-4444-4444-444444444444"
		otherDocument = "44444444-4444-4444-4444-444444444445"
		branchID      = "55555555-5555-5555-5555-555555555555"
		otherBranch   = "55555555-5555-5555-5555-555555555556"
		foreignBranch = "55555555-5555-5555-5555-555555555557"
	)
	for _, statement := range []string{
		`INSERT INTO users(id,email,password_hash,display_name) VALUES('` + userID + `','fixed-share@example.test','hash','Share Owner')`,
		`INSERT INTO teams(id,name,slug,created_by) VALUES('` + teamID + `','Fixed share','fixed-share','` + userID + `')`,
		`INSERT INTO projects(id,team_id,name,slug,created_by) VALUES('` + projectID + `','` + teamID + `','Fixed share','fixed-share','` + userID + `')`,
		`INSERT INTO documents(id,project_id,name,document_type,relative_path,created_by) VALUES('` + documentID + `','` + projectID + `','Shared text',2,'share.md','` + userID + `'),('` + otherDocument + `','` + projectID + `','Private text',2,'private.md','` + userID + `')`,
		`INSERT INTO document_branches(id,document_id,name,kind,created_by) VALUES('` + branchID + `','` + documentID + `','dev',1,'` + userID + `'),('` + otherBranch + `','` + documentID + `','prod',1,'` + userID + `'),('` + foreignBranch + `','` + otherDocument + `','dev',1,'` + userID + `')`,
	} {
		if err := database.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	oldVersion := insertFixedShareVersion(t, database, userID, projectID, documentID, branchID, 1, now.Add(-time.Hour))
	newVersion := insertFixedShareVersion(t, database, userID, projectID, documentID, branchID, 2, now)
	crossBranch := insertFixedShareVersion(t, database, userID, projectID, documentID, otherBranch, 1, now)
	crossDocument := insertFixedShareVersion(t, database, userID, projectID, otherDocument, foreignBranch, 1, now)
	repository := pgvdoc.NewRepository(database)
	share := &domainvdoc.DocumentShare{
		ID: uuid.NewString(), ProjectID: projectID, DocumentID: documentID, BranchID: branchID,
		TokenHash: strings.Repeat("a", 64), TokenCiphertext: []byte("ciphertext"), CipherKID: "test",
		VersionScope: domainvdoc.DocumentShareScopeFixedVersion, PinnedVersionID: &oldVersion, Status: domainvdoc.DocumentShareStatusActive,
		CreatedBy: userID, CreatedAt: now, UpdatedAt: now,
	}
	if err := repository.UpsertDocumentShareIfUnchanged(context.Background(), share, nil); err != nil {
		t.Fatalf("create pinned historical share: %v", err)
	}
	for _, assignment := range []string{
		"pinned_version_id=NULL",
		"pinned_version_id='99999999-9999-9999-9999-999999999999'",
		"version_scope=1", "version_scope=2", "version_scope=4",
	} {
		if err := database.Exec("UPDATE document_shares SET "+assignment+" WHERE id=?", share.ID).Error; err == nil {
			t.Fatalf("database accepted invalid fixed share shape: %s", assignment)
		}
	}
	for label, targetID := range map[string]string{"other branch": crossBranch, "other document": crossDocument, "missing version": uuid.NewString()} {
		t.Run("reject "+label, func(t *testing.T) {
			invalid := *share
			invalid.ID, invalid.TokenHash, invalid.PinnedVersionID = uuid.NewString(), strings.Repeat("b", 64), &targetID
			if err := repository.UpsertDocumentShareIfUnchanged(context.Background(), &invalid, nil); !errors.Is(err, domainvdoc.ErrNotFound) {
				t.Fatalf("create invalid pinned share error = %v, want not found", err)
			}
		})
	}
	snapshot, err := repository.LoadPublicDocumentShareSnapshot(context.Background(), share.ID)
	if err != nil || snapshot == nil || len(snapshot.Versions) != 1 || snapshot.Versions[0].ID != stripUUID(oldVersion) || snapshot.Share.PinnedVersionID == nil || *snapshot.Share.PinnedVersionID != stripUUID(oldVersion) {
		t.Fatalf("fixed snapshot = %+v, err=%v, want only historical pinned version", snapshot, err)
	}
	state, err := repository.LoadState(context.Background())
	if err != nil || state.Shares[stripUUID(share.ID)] == nil || state.Shares[stripUUID(share.ID)].PinnedVersionID == nil || *state.Shares[stripUUID(share.ID)].PinnedVersionID != stripUUID(oldVersion) {
		t.Fatalf("fixed share state did not retain target: %v", err)
	}
	for _, scope := range []int{domainvdoc.DocumentShareScopeLatest, domainvdoc.DocumentShareScopeAllVersions} {
		legacy := *share
		legacy.ID, legacy.TokenHash, legacy.VersionScope, legacy.PinnedVersionID = uuid.NewString(), fmt.Sprintf("legacy-%d", scope), scope, nil
		if err := repository.UpsertDocumentShareIfUnchanged(context.Background(), &legacy, nil); err != nil {
			t.Fatal(err)
		}
		legacySnapshot, err := repository.LoadPublicDocumentShareSnapshot(context.Background(), legacy.ID)
		if err != nil || len(legacySnapshot.Versions) != 2 || legacySnapshot.Versions[0].ID != stripUUID(newVersion) || legacySnapshot.Share.PinnedVersionID != nil {
			t.Fatalf("scope %d snapshot changed: %+v, err=%v", scope, legacySnapshot, err)
		}
	}
	newAudit := func(versionID string) *domainvdoc.AuditLog {
		return &domainvdoc.AuditLog{ID: uuid.NewString(), ActorType: domainvdoc.AuditActorAnonymous, Action: "document_share.view", ResourceType: "document_share", ResourceID: share.ID, ProjectID: projectID, ServiceID: documentID, Metadata: map[string]string{"version_id": versionID}, CreatedAt: now, UpdatedAt: now}
	}
	wrongVersionAudit := newAudit(newVersion)
	if err := repository.RecordPublicDocumentShareAccess(context.Background(), share.ID, wrongVersionAudit); !errors.Is(err, domainvdoc.ErrFailedPrecondition) {
		t.Fatalf("non-pinned access error = %v, want failed precondition", err)
	}
	validAudit := newAudit(oldVersion)
	if err := repository.RecordPublicDocumentShareAccess(context.Background(), share.ID, validAudit); err != nil {
		t.Fatalf("pinned access error = %v", err)
	}
	var auditCount int64
	if err := database.Table("audit_logs").Count(&auditCount).Error; err != nil || auditCount != 1 {
		t.Fatalf("successful access audits = %d, want 1, err=%v", auditCount, err)
	}
	for label, mutation := range map[string]string{
		"target changed after snapshot": `UPDATE document_shares SET pinned_version_id='` + newVersion + `' WHERE id='` + share.ID + `'`,
		"revoked":                       `UPDATE document_shares SET status=2,revoked_by='` + userID + `',revoked_at=now() WHERE id='` + share.ID + `'`,
		"expired":                       `UPDATE document_shares SET expires_at=now()-interval '1 second' WHERE id='` + share.ID + `'`,
		"parent archived":               `UPDATE document_branches SET status=2 WHERE id='` + branchID + `'`,
	} {
		t.Run(label, func(t *testing.T) {
			if err := database.Transaction(func(tx *gorm.DB) error {
				if err := tx.Exec(mutation).Error; err != nil {
					return err
				}
				err := pgvdoc.NewRepository(tx).RecordPublicDocumentShareAccess(context.Background(), share.ID, newAudit(oldVersion))
				if !errors.Is(err, domainvdoc.ErrFailedPrecondition) {
					t.Errorf("stale fixed share access error = %v, want failed precondition", err)
				}
				return errors.New("rollback test mutation")
			}); err == nil {
				t.Fatal("test mutation unexpectedly committed")
			}
		})
	}
	blocker := database.Begin()
	if blocker.Error != nil {
		t.Fatal(blocker.Error)
	}
	defer blocker.Rollback()
	if err := blocker.Exec(`SELECT id FROM projects WHERE id=? FOR UPDATE`, projectID).Error; err != nil {
		t.Fatal(err)
	}
	accessResult := make(chan error, 1)
	go func() {
		accessResult <- repository.RecordPublicDocumentShareAccess(context.Background(), share.ID, newAudit(oldVersion))
	}()
	if err := database.Exec(`UPDATE document_shares SET pinned_version_id=? WHERE id=?`, newVersion, share.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := blocker.Rollback().Error; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-accessResult:
		if !errors.Is(err, domainvdoc.ErrFailedPrecondition) {
			t.Fatalf("access racing a pin change error = %v, want failed precondition", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("fixed share access did not finish after the project lock was released")
	}
	if err := database.Table("audit_logs").Count(&auditCount).Error; err != nil || auditCount != 1 {
		t.Fatalf("rejected stale access added an audit: count=%d, err=%v", auditCount, err)
	}
	// 数据库只使用单列外键时，损坏的跨分支目标也不能进入公开快照。
	if err := database.Exec(`UPDATE document_shares SET pinned_version_id=? WHERE id=?`, crossBranch, share.ID).Error; err != nil {
		t.Fatal(err)
	}
	corruptSnapshot, err := repository.LoadPublicDocumentShareSnapshot(context.Background(), share.ID)
	if err != nil || len(corruptSnapshot.Versions) != 0 {
		t.Fatalf("cross-branch fixed target leaked into snapshot: %+v, err=%v", corruptSnapshot, err)
	}
}

func insertFixedShareVersion(t *testing.T, database *gorm.DB, userID, projectID, documentID, branchID string, versionNo int, publishedAt time.Time) string {
	t.Helper()
	draftID, versionID := uuid.NewString(), uuid.NewString()
	versionName := fmt.Sprintf("%d.0.0", versionNo)
	if err := database.Exec(`INSERT INTO document_drafts(id,project_id,document_id,branch_id,version_name,relative_path,document_format,raw_schema_object_key,normalized_schema_object_key,raw_schema_hash,normalized_schema_hash,schema_size_bytes,created_by_actor_type,created_by_user_id) VALUES(?,?,?,?,?,'share.md',3,?,?,'raw-hash','stable-hash',5,1,?)`, draftID, projectID, documentID, branchID, versionName, "raw/"+versionID, "stable/"+versionID, userID).Error; err != nil {
		t.Fatalf("insert version source draft: %v", err)
	}
	if err := database.Exec(`INSERT INTO document_versions(id,project_id,document_id,branch_id,version_name,version_no,relative_path,source_draft_id,document_format,raw_schema_object_key,normalized_schema_object_key,raw_schema_hash,normalized_schema_hash,schema_size_bytes,published_by,published_at) VALUES(?,?,?,?,?,?,'share.md',?,3,?,?,'raw-hash','stable-hash',5,?,?)`, versionID, projectID, documentID, branchID, versionName, versionNo, draftID, "raw/"+versionID, "stable/"+versionID, userID, publishedAt).Error; err != nil {
		t.Fatalf("insert published version: %v", err)
	}
	return versionID
}
