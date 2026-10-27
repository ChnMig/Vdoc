package db

import (
	"context"
	"os"
	"testing"
)

func TestDocumentChatMigrationPreservesSessionsAndRestrictsDocumentScope(t *testing.T) {
	dsn := os.Getenv("VDOC_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("VDOC_TEST_DATABASE_DSN not set")
	}
	database := openTestDB(t, dsn)
	defer closeTestDB(t, database)
	resetPublicSchema(t, database)
	if err := database.Exec(`CREATE TABLE schema_migrations(version text PRIMARY KEY,name text NOT NULL,checksum text,applied_at timestamptz NOT NULL DEFAULT now())`).Error; err != nil {
		t.Fatal(err)
	}
	migrations, err := EmbeddedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations {
		if migration.Version < "012" {
			if err := runMigration(context.Background(), database, migration); err != nil {
				t.Fatal(err)
			}
		}
	}
	const (
		userID     = "11111111-1111-1111-1111-111111111111"
		teamID     = "22222222-2222-2222-2222-222222222222"
		projectID  = "33333333-3333-3333-3333-333333333333"
		documentID = "44444444-4444-4444-4444-444444444444"
	)
	for _, statement := range []string{
		`INSERT INTO users(id,email,password_hash,display_name) VALUES('` + userID + `','document-chat-migration@example.test','hash','Migration')`,
		`INSERT INTO teams(id,name,slug,created_by) VALUES('` + teamID + `','Migration','migration','` + userID + `')`,
		`INSERT INTO projects(id,team_id,name,slug,created_by) VALUES('` + projectID + `','` + teamID + `','Migration','migration','` + userID + `')`,
		`INSERT INTO documents(id,project_id,name,document_type,relative_path,created_by) VALUES('` + documentID + `','` + projectID + `','Migration',2,'migration.md','` + userID + `')`,
		`INSERT INTO ai_chat_sessions(project_id,document_id,context_type,context_id,title,created_by) VALUES('` + projectID + `','` + documentID + `','version','55555555-5555-5555-5555-555555555555','Legacy chat','` + userID + `')`,
	} {
		if err := database.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := RunMigrations(context.Background(), database); err != nil {
			t.Fatal(err)
		}
	}
	var count int64
	if err := database.Table("ai_chat_sessions").Where("title = ?", "Legacy chat").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("legacy session count=%d err=%v", count, err)
	}
	insert := `INSERT INTO ai_chat_sessions(project_id,document_id,context_type,context_id,title,created_by) VALUES(?,?,'document',?,'Document chat',?)`
	if err := database.Exec(insert, projectID, documentID, documentID, userID).Error; err != nil {
		t.Fatalf("valid document chat: %v", err)
	}
	if err := database.Exec(insert, projectID, documentID, "55555555-5555-5555-5555-555555555555", userID).Error; err == nil {
		t.Fatal("mismatched document context accepted")
	}
	if err := database.Exec(insert, projectID, nil, documentID, userID).Error; err == nil {
		t.Fatal("document context without document accepted")
	}
	if err := database.Exec(`INSERT INTO ai_summaries(project_id,document_id,owner_type,owner_id,prompt_key,status,generated_by) VALUES(?,?,'document',?,'page_chat','pending',?)`, projectID, documentID, documentID, userID).Error; err == nil {
		t.Fatal("chat scope incorrectly widened summaries")
	}
}
