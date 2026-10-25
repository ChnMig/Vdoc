package db

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestCustomBranchNamesMigrationPreservesHistoryAndConstraints(t *testing.T) {
	database := openBranchNameMigrationTestDB(t)
	applyMigrationsBeforeCustomBranchNames(t, database)
	const (
		userID        = "11111111-1111-1111-1111-111111111111"
		teamID        = "22222222-2222-2222-2222-222222222222"
		projectID     = "33333333-3333-3333-3333-333333333333"
		documentID    = "44444444-4444-4444-4444-444444444444"
		newDocumentID = "44444444-4444-4444-4444-444444444445"
		branchID      = "55555555-5555-5555-5555-555555555555"
	)
	insertUser(t, database, userID, "branches@example.test")
	insertGraph(t, database, userID, teamID, projectID, documentID, branchID,
		"66666666-6666-6666-6666-666666666666", "66666666-6666-6666-6666-666666666667",
		"77777777-7777-7777-7777-777777777777", "88888888-8888-8888-8888-888888888888", "99999999-9999-9999-9999-999999999999")
	// 历史 main 曾允许归为环境分支，升级不能强制改成新规则下的自定义类型。
	if err := database.Exec(`UPDATE document_branches SET name='main',kind=1 WHERE id=?`, branchID).Error; err != nil {
		t.Fatal(err)
	}
	for _, branch := range []struct {
		name      string
		kind      int
		protected bool
	}{
		{"dev", 1, false}, {"test", 1, false}, {"prod", 1, true}, {"feature/x", 2, false},
	} {
		if err := database.Exec(`INSERT INTO document_branches(document_id,name,kind,is_protected,created_by) VALUES(?,?,?,?,?)`, documentID, branch.name, branch.kind, branch.protected, userID).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := database.Exec(`INSERT INTO documents(id,project_id,name,document_type,relative_path,created_by) VALUES(?,?,'Custom branches',2,'custom.md',?)`, newDocumentID, projectID, userID).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Exec(`INSERT INTO document_branches(document_id,name,kind,created_by) VALUES(?,'release',2,?)`, newDocumentID, userID).Error; err == nil {
		t.Fatal("pre-010 schema unexpectedly accepted a custom branch name")
	}

	// 包含所有现有工作流中的 branch_id 和 diff 两端引用，同时覆盖默认、保护和时间字段。
	before := branchMigrationSnapshots(t, database)
	if err := RunMigrations(context.Background(), database); err != nil {
		t.Fatalf("upgrade pre-010 database: %v", err)
	}
	if err := RunMigrations(context.Background(), database); err != nil {
		t.Fatalf("repeat branch migration: %v", err)
	}
	after := branchMigrationSnapshots(t, database)
	for table, want := range before {
		if got := after[table]; got != want {
			t.Fatalf("migration rewrote historical rows or branch references in %s:\ngot %s\nwant %s", table, got, want)
		}
	}

	for _, name := range []string{"main", "release", "中文", "feature/x", "release/2026"} {
		if err := database.Exec(`INSERT INTO document_branches(document_id,name,kind,created_by) VALUES(?,?,2,?)`, newDocumentID, name, userID).Error; err != nil {
			t.Fatalf("custom branch %q rejected after upgrade: %v", name, err)
		}
	}
	if err := database.Exec(`INSERT INTO document_branches(document_id,name,kind,created_by) VALUES(?,'release',2,?)`, documentID, userID).Error; err != nil {
		t.Fatalf("same name in another document rejected: %v", err)
	}
	for name, statement := range map[string]string{
		"name uniqueness across kinds":   `INSERT INTO document_branches(document_id,name,kind,created_by) VALUES('` + newDocumentID + `','release',1,'` + userID + `')`,
		"default uniqueness":             `INSERT INTO document_branches(document_id,name,kind,is_default,created_by) VALUES('` + documentID + `','another-default',2,true,'` + userID + `')`,
		"kind check":                     `INSERT INTO document_branches(document_id,name,kind,created_by) VALUES('` + newDocumentID + `','invalid-kind',9,'` + userID + `')`,
		"status check":                   `INSERT INTO document_branches(document_id,name,kind,status,created_by) VALUES('` + newDocumentID + `','invalid-status',2,9,'` + userID + `')`,
		"document foreign key":           `INSERT INTO document_branches(document_id,name,kind,created_by) VALUES('aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa','missing-document',2,'` + userID + `')`,
		"creator foreign key":            `INSERT INTO document_branches(document_id,name,kind,created_by) VALUES('` + newDocumentID + `','missing-creator',2,'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa')`,
		"incoming workflow foreign keys": `DELETE FROM document_branches WHERE id='` + branchID + `'`,
	} {
		assertExecFails(t, database, statement, name)
	}
}

func TestCustomBranchNamesMigrationUpgradesOriginalLegacyConstraint(t *testing.T) {
	database := openBranchNameMigrationTestDB(t)
	legacySQL, err := os.ReadFile("testdata/001_initial_legacy.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Exec(string(legacySQL)).Error; err != nil {
		t.Fatalf("apply original legacy schema: %v", err)
	}
	if err := database.Exec(`INSERT INTO schema_migrations(version,name) VALUES('001','v01_schema')`).Error; err != nil {
		t.Fatal(err)
	}
	userID, teamID, projectID, documentID, branchID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	insertUser(t, database, userID, "legacy-branches@example.test")
	if err := database.Exec(`INSERT INTO teams(id,name,slug,created_by) VALUES(?,'Legacy','legacy',?)`, teamID, userID).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Exec(`INSERT INTO projects(id,team_id,name,slug,created_by) VALUES(?,?,'Legacy','legacy',?)`, projectID, teamID, userID).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Exec(`INSERT INTO api_services(id,project_id,name,base_path,created_by) VALUES(?,?,'Legacy','/legacy',?)`, documentID, projectID, userID).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Exec(`INSERT INTO api_contract_branches(id,service_id,name,kind,is_default,created_by) VALUES(?,?,'main',1,true,?)`, branchID, documentID, userID).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Exec(`INSERT INTO api_contract_branches(service_id,name,kind,created_by) VALUES(?,'release',2,?)`, documentID, userID).Error; err == nil {
		t.Fatal("original legacy schema unexpectedly accepted release")
	}
	if err := RunMigrations(context.Background(), database); err != nil {
		t.Fatalf("upgrade original checksum-less schema: %v", err)
	}
	var historical struct {
		Name      string
		Kind      int
		IsDefault bool
	}
	if err := database.Raw(`SELECT name,kind,is_default FROM document_branches WHERE id=?`, branchID).Scan(&historical).Error; err != nil {
		t.Fatal(err)
	}
	if historical.Name != "main" || historical.Kind != 1 || !historical.IsDefault {
		t.Fatalf("original main branch was reclassified or changed: %+v", historical)
	}
	for _, name := range []string{"release", "中文", "release/2026"} {
		if err := database.Exec(`INSERT INTO document_branches(document_id,name,kind,created_by) VALUES(?,?,2,?)`, documentID, name, userID).Error; err != nil {
			t.Fatalf("legacy constraint still blocks %q after upgrade: %v", name, err)
		}
	}
}

func openBranchNameMigrationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("VDOC_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("VDOC_TEST_DATABASE_DSN not set; skipping PostgreSQL custom branch name migration test")
	}
	database := openTestDB(t, dsn)
	t.Cleanup(func() { closeTestDB(t, database) })
	resetPublicSchema(t, database)
	return database
}

func applyMigrationsBeforeCustomBranchNames(t *testing.T, database *gorm.DB) {
	t.Helper()
	if err := database.Exec(`CREATE TABLE schema_migrations(version text PRIMARY KEY,name text NOT NULL,checksum text,applied_at timestamptz NOT NULL DEFAULT now())`).Error; err != nil {
		t.Fatal(err)
	}
	migrations, err := EmbeddedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations {
		if migration.Version < "010" {
			if err := runMigration(context.Background(), database, migration); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func branchMigrationSnapshots(t *testing.T, database *gorm.DB) map[string]string {
	t.Helper()
	snapshots := map[string]string{}
	for _, table := range []string{"document_branches", "document_drafts", "document_versions", "api_endpoints", "document_version_diffs"} {
		var rows string
		if err := database.Raw("SELECT coalesce(jsonb_agg(to_jsonb(row) ORDER BY id)::text,'[]') FROM " + table + " AS row").Scan(&rows).Error; err != nil {
			t.Fatalf("snapshot %s: %v", table, err)
		}
		snapshots[table] = rows
	}
	return snapshots
}
