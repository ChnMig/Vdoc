package db

import (
	"context"
	"os"
	"testing"

	commonvdoc "vdoc/common/vdoc"

	"gorm.io/gorm"
)

func TestPlainTextDocumentMigrationRoundTrip(t *testing.T) {
	dsn := os.Getenv("VDOC_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("VDOC_TEST_DATABASE_DSN not set; skipping PostgreSQL TXT constraint test")
	}
	database := openTestDB(t, dsn)
	defer closeTestDB(t, database)
	resetPublicSchema(t, database)
	if err := RunMigrations(context.Background(), database); err != nil {
		t.Fatal(err)
	}

	const (
		userID     = "11111111-1111-1111-1111-111111111111"
		teamID     = "22222222-2222-2222-2222-222222222222"
		projectID  = "33333333-3333-3333-3333-333333333333"
		documentID = "44444444-4444-4444-4444-444444444444"
		branchID   = "55555555-5555-5555-5555-555555555555"
		draftID    = "66666666-6666-6666-6666-666666666666"
		versionID  = "77777777-7777-7777-7777-777777777777"
	)
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO users(id,email,password_hash,display_name) VALUES(?,?,?,?)`, []any{userID, "text-migration@example.test", "unused-test-hash", "TXT constraint test"}},
		{`INSERT INTO teams(id,name,slug,created_by) VALUES(?,?,?,?)`, []any{teamID, "TXT team", "txt-team", userID}},
		{`INSERT INTO projects(id,team_id,name,slug,created_by) VALUES(?,?,?,?,?)`, []any{projectID, teamID, "TXT project", "txt-project", userID}},
		{`INSERT INTO documents(id,project_id,name,document_type,relative_path,created_by) VALUES(?,?,?,?,?,?)`, []any{documentID, projectID, "TXT notes", commonvdoc.DocumentTypeText, "notes.txt", userID}},
		{`INSERT INTO document_branches(id,document_id,name,kind,is_default,created_by) VALUES(?,?,?,?,?,?)`, []any{branchID, documentID, "main", commonvdoc.BranchKindEnvironment, true, userID}},
		{`INSERT INTO document_drafts(id,project_id,document_id,branch_id,version_name,relative_path,document_format,raw_schema_object_key,normalized_schema_object_key,raw_schema_hash,normalized_schema_hash,schema_size_bytes,created_by_actor_type,created_by_user_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, []any{draftID, projectID, documentID, branchID, "1.0.0", "notes.txt", commonvdoc.DocumentFormatText, "raw.txt", "stable.txt", "raw-hash", "stable-hash", 0, commonvdoc.AuditActorUser, userID}},
		{`INSERT INTO document_versions(id,project_id,document_id,branch_id,version_name,version_no,relative_path,source_draft_id,document_format,raw_schema_object_key,normalized_schema_object_key,raw_schema_hash,normalized_schema_hash,schema_size_bytes,published_by) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, []any{versionID, projectID, documentID, branchID, "1.0.0", 1, "notes.txt", draftID, commonvdoc.DocumentFormatText, "raw.txt", "stable.txt", "raw-hash", "stable-hash", 0, userID}},
	}
	for _, statement := range statements {
		if err := database.Exec(statement.query, statement.args...).Error; err != nil {
			t.Fatalf("insert TXT fixture: %v", err)
		}
	}
	for _, check := range []struct {
		table  string
		column string
		id     string
		want   int
	}{
		{"documents", "document_type", documentID, commonvdoc.DocumentTypeText},
		{"document_drafts", "document_format", draftID, commonvdoc.DocumentFormatText},
		{"document_versions", "document_format", versionID, commonvdoc.DocumentFormatText},
	} {
		var got int
		if err := database.Raw("SELECT "+check.column+" FROM "+check.table+" WHERE id = ?", check.id).Scan(&got).Error; err != nil || got != check.want {
			t.Fatalf("%s TXT code = %d, want %d; err=%v", check.table, got, check.want, err)
		}
		err := database.Transaction(func(tx *gorm.DB) error {
			return tx.Exec("UPDATE "+check.table+" SET "+check.column+" = ? WHERE id = ?", check.want+1, check.id).Error
		})
		if err == nil {
			t.Fatalf("%s accepted an unknown document code", check.table)
		}
	}
}
