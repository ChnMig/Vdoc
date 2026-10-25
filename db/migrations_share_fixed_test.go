package db

import (
	"context"
	"database/sql"
	"os"
	"testing"
)

func TestFixedVersionShareMigrationPreservesLegacyShares(t *testing.T) {
	dsn := os.Getenv("VDOC_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("VDOC_TEST_DATABASE_DSN not set; skipping PostgreSQL fixed-version share migration test")
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
		if migration.Version < "011" {
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
		branchID   = "55555555-5555-5555-5555-555555555555"
	)
	statements := []string{
		`INSERT INTO users(id,email,password_hash,display_name) VALUES('` + userID + `','fixed-upgrade@example.test','hash','Share migration')`,
		`INSERT INTO teams(id,name,slug,created_by) VALUES('` + teamID + `','Share migration','share-migration','` + userID + `')`,
		`INSERT INTO projects(id,team_id,name,slug,created_by) VALUES('` + projectID + `','` + teamID + `','Share migration','share-migration','` + userID + `')`,
		`INSERT INTO documents(id,project_id,name,document_type,relative_path,created_by) VALUES('` + documentID + `','` + projectID + `','Share migration',2,'migration.md','` + userID + `')`,
		`INSERT INTO document_branches(id,document_id,name,kind,created_by) VALUES('` + branchID + `','` + documentID + `','main',1,'` + userID + `')`,
	}
	for _, statement := range statements {
		if err := database.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, scope := range []int{1, 2} {
		if err := database.Exec(`INSERT INTO document_shares(project_id,document_id,branch_id,token_hash,token_ciphertext,cipher_kid,version_scope,created_by) VALUES(?,?,?,?,'ciphertext','test',?,?)`, projectID, documentID, branchID, string(rune('a'+scope)), scope, userID).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := RunMigrations(context.Background(), database); err != nil {
		t.Fatalf("upgrade fixed-version shares: %v", err)
	}
	if err := RunMigrations(context.Background(), database); err != nil {
		t.Fatalf("repeat fixed-version migration: %v", err)
	}
	var shares []struct {
		VersionScope    int
		PinnedVersionID sql.NullString
	}
	if err := database.Raw(`SELECT version_scope,pinned_version_id FROM document_shares ORDER BY version_scope`).Scan(&shares).Error; err != nil {
		t.Fatal(err)
	}
	if len(shares) != 2 || shares[0].VersionScope != 1 || shares[1].VersionScope != 2 || shares[0].PinnedVersionID.Valid || shares[1].PinnedVersionID.Valid {
		t.Fatalf("legacy shares changed after upgrade: %+v", shares)
	}
	for _, query := range []string{
		`UPDATE document_shares SET version_scope=3 WHERE version_scope=1`,
		`UPDATE document_shares SET pinned_version_id='99999999-9999-9999-9999-999999999999' WHERE version_scope=1`,
		`UPDATE document_shares SET version_scope=4 WHERE version_scope=1`,
	} {
		if err := database.Exec(query).Error; err == nil {
			t.Fatalf("share constraint accepted invalid statement: %s", query)
		}
	}
}
