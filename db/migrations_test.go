package db

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"

	commonvdoc "vdoc/common/vdoc"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestHistoricalMigrationChecksumsAreFrozen(t *testing.T) {
	migrations, err := EmbeddedMigrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	expected := map[string]string{
		"000": "60890cbc782984e7e5e4256511b71adb0b4491b88c82dbd23434c6e3d4ab21fd",
		"001": "6938bb3c042e38bd38d4c19719b6a3329fae07fac972f6681191cc1509d6c8ce",
		"002": "8d19b71da40adc59959d1c2c6cd9f9d3d7811d0fe248fdd9d39379d8afbf967b",
		"003": "3bce09d045935ea159c344c25aa2ba8afc0a8b4c2cbd687a7d5d4ed39848d413",
		"004": "2058513d63bef1b181964fae114bec70406f97f0d758c1675f77a9034e4c3420",
	}
	if len(migrations) != len(expected) {
		t.Fatalf("embedded migration count = %d, frozen list = %d; add new versions to the list without rewriting old SQL", len(migrations), len(expected))
	}
	for _, migration := range migrations {
		want, ok := expected[migration.Version]
		if !ok {
			t.Fatalf("migration %s %s is not in the frozen checksum list", migration.Version, migration.Name)
		}
		if migration.Checksum != want {
			t.Fatalf("migration %s %s checksum changed: got %s, want %s; add a new migration instead of editing applied SQL", migration.Version, migration.Name, migration.Checksum, want)
		}
	}
}

func TestInitialLegacyMigrationFixtureIsHistoricalArtifact(t *testing.T) {
	body, err := os.ReadFile("testdata/001_initial_legacy.sql")
	if err != nil {
		t.Fatalf("read initial legacy migration: %v", err)
	}
	const historicalChecksum = "3f0433999455af3ba99d8789a66ae6f8f534ef3fc4fbc665599f173bdbf0aa0d"
	if checksum := fmt.Sprintf("%x", sha256.Sum256(body)); checksum != historicalChecksum {
		t.Fatalf("initial legacy migration fixture checksum = %s, want %s", checksum, historicalChecksum)
	}
}

func TestEmbeddedMigrationsCoverV01Schema(t *testing.T) {
	migrations, err := EmbeddedMigrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if len(migrations) == 0 {
		t.Fatal("expected embedded migrations")
	}

	joined := strings.ToLower(strings.Join(migrationBodies(migrations), "\n"))
	for _, table := range []string{
		"schema_migrations",
		"users",
		"teams",
		"projects",
		"project_members",
		"documents",
		"document_branches",
		"document_drafts",
		"document_versions",
		"api_endpoints",
		"api_endpoint_details",
		"document_version_diffs",
		"document_diff_items",
		"mcp_tokens",
		"document_shares",
		"audit_logs",
		"vdoc_schema_objects",
	} {
		if !strings.Contains(joined, "create table if not exists "+table) {
			t.Fatalf("migration does not create %s", table)
		}
	}
	for _, forbidden := range []string{"create table if not exists vdoc_state", "insert into vdoc_state"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("prototype persistence leaked into migrations: %s", forbidden)
		}
	}
}

func TestEmbeddedMigrationsIncludeImportantConstraintsAndIndexes(t *testing.T) {
	migrations, err := EmbeddedMigrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	joined := strings.ToLower(strings.Join(migrationBodies(migrations), "\n"))
	checks := []string{
		"users_status_check",
		"users_email_active_uidx",
		"teams_slug_active_uidx",
		"projects_team_slug_active_uidx",
		"project_members_role_check",
		"project_members_status_check",
		"project_members_project_user_active_uidx",
		"documents_project_name_active_uidx",
		"document_branches_document_name_uidx",
		"document_branches_default_uidx",
		"document_branches_feature_name_check",
		"document_drafts_active_version_uidx",
		"document_drafts_promote_fields_check",
		"document_versions_document_branch_version_name_uidx",
		"document_versions_document_branch_version_no_uidx",
		"api_endpoints_version_method_path_uidx",
		"api_endpoint_details_endpoint_uidx",
		"document_version_diffs_versions_uidx",
		"document_diff_items_breaking_consistency_check",
		"mcp_tokens_hash_uidx",
		"mcp_tokens_revoked_fields_check",
		"mcp_tokens_scopes_check",
		"document_shares_token_hash_uidx",
		"document_shares_revoked_fields_check",
		"audit_logs_actor_idx",
		"vdoc_schema_objects_owner_idx",
	}
	for _, check := range checks {
		if !strings.Contains(joined, check) {
			t.Fatalf("migration missing %s", check)
		}
	}
}

func TestEmbeddedMigrationsAllowAllCurrentMCPScopeCodes(t *testing.T) {
	migrations, err := EmbeddedMigrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	joined := strings.ToLower(strings.Join(migrationBodies(migrations), "\n"))
	expected := fmt.Sprintf("array[%d,%d,%d,%d]::smallint[]", commonvdoc.ScopeAPIRead, commonvdoc.ScopeAPIDraft, commonvdoc.ScopeDocRead, commonvdoc.ScopeDocDraft)
	if !strings.Contains(joined, expected) {
		t.Fatalf("mcp token scope check must allow all current scopes with %s", expected)
	}
	if strings.Contains(joined, "array[1,2]::smallint[]") {
		t.Fatal("mcp token scope check only allows legacy API scopes")
	}
}

func TestRunMigrationsCreatesSchemaAndIsIdempotent(t *testing.T) {
	dsn := os.Getenv("VDOC_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("VDOC_TEST_DATABASE_DSN not set; skipping PostgreSQL migration integration test")
	}
	database := openTestDB(t, dsn)
	defer closeTestDB(t, database)
	resetPublicSchema(t, database)

	if err := RunMigrations(context.Background(), database); err != nil {
		t.Fatalf("first RunMigrations: %v", err)
	}
	if err := RunMigrations(context.Background(), database); err != nil {
		t.Fatalf("second RunMigrations: %v", err)
	}

	for _, table := range []string{"users", "teams", "projects", "project_members", "documents", "document_branches", "document_drafts", "document_versions", "api_endpoints", "api_endpoint_details", "document_version_diffs", "document_diff_items", "mcp_tokens", "document_shares", "audit_logs", "vdoc_schema_objects"} {
		if !tableExists(t, database, table) {
			t.Fatalf("expected table %s", table)
		}
	}
	if tableExists(t, database, "vdoc_state") {
		t.Fatal("vdoc_state must not exist after normalized migrations")
	}
	assertAllMigrationChecksumsRecorded(t, database)
}

func TestRunMigrationsUpgradesInitialChecksumlessV01AndPreservesRows(t *testing.T) {
	dsn := os.Getenv("VDOC_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("VDOC_TEST_DATABASE_DSN not set; skipping historical PostgreSQL upgrade test")
	}
	database := openTestDB(t, dsn)
	defer closeTestDB(t, database)
	resetPublicSchema(t, database)

	legacySQL, err := os.ReadFile("testdata/001_initial_legacy.sql")
	if err != nil {
		t.Fatalf("read initial legacy migration: %v", err)
	}
	if err := database.Exec(string(legacySQL)).Error; err != nil {
		t.Fatalf("apply initial historical 001: %v", err)
	}
	if err := database.Exec(`INSERT INTO schema_migrations(version, name) VALUES('001', 'v01_schema')`).Error; err != nil {
		t.Fatalf("record historical 001: %v", err)
	}

	const (
		userID    = "11111111-1111-1111-1111-111111111111"
		teamID    = "22222222-2222-2222-2222-222222222222"
		projectID = "33333333-3333-3333-3333-333333333333"
		serviceID = "44444444-4444-4444-4444-444444444444"
		branchID  = "55555555-5555-5555-5555-555555555555"
	)
	if err := database.Exec(`INSERT INTO users(id,email,password_hash,display_name) VALUES(?,?,?,?)`, userID, "legacy@example.com", "hash", "Legacy User").Error; err != nil {
		t.Fatalf("insert legacy user: %v", err)
	}
	if err := database.Exec(`INSERT INTO teams(id,name,slug,created_by) VALUES(?,?,?,?)`, teamID, "Legacy Team", "legacy-team", userID).Error; err != nil {
		t.Fatalf("insert legacy team: %v", err)
	}
	if err := database.Exec(`INSERT INTO projects(id,team_id,name,slug,created_by) VALUES(?,?,?,?,?)`, projectID, teamID, "Legacy Project", "legacy-project", userID).Error; err != nil {
		t.Fatalf("insert legacy project: %v", err)
	}
	if err := database.Exec(`INSERT INTO api_services(id,project_id,name,base_path,created_by) VALUES(?,?,?,?,?)`, serviceID, projectID, "Legacy API", "/legacy", userID).Error; err != nil {
		t.Fatalf("insert legacy API service: %v", err)
	}
	if err := database.Exec(`INSERT INTO api_contract_branches(id,service_id,name,kind,is_default,created_by) VALUES(?,?,?,?,?,?)`, branchID, serviceID, "main", 1, true, userID).Error; err != nil {
		t.Fatalf("insert legacy API branch: %v", err)
	}

	if err := RunMigrations(context.Background(), database); err != nil {
		t.Fatalf("upgrade initial historical 001: %v", err)
	}
	if err := RunMigrations(context.Background(), database); err != nil {
		t.Fatalf("repeat upgraded migrations: %v", err)
	}

	for _, oldTable := range []string{"api_services", "api_contract_branches", "api_contract_drafts", "api_contract_versions", "api_version_diffs", "api_diff_items"} {
		if tableExists(t, database, oldTable) {
			t.Fatalf("legacy table %s remains after reconciliation", oldTable)
		}
	}
	for _, currentTable := range []string{"documents", "document_branches", "ai_providers", "document_shares"} {
		if !tableExists(t, database, currentTable) {
			t.Fatalf("current table %s missing after historical upgrade", currentTable)
		}
	}
	var document struct {
		ID           string
		ProjectID    string
		Name         string
		DocumentType int
		RelativePath string
	}
	if err := database.Raw(`SELECT id, project_id, name, document_type, relative_path FROM documents WHERE id=?`, serviceID).Scan(&document).Error; err != nil {
		t.Fatalf("load reconciled document: %v", err)
	}
	if document.ID != serviceID || document.ProjectID != projectID || document.Name != "Legacy API" || document.DocumentType != 1 || document.RelativePath == "" {
		t.Fatalf("reconciled document = %+v", document)
	}
	var branchDocumentID string
	if err := database.Raw(`SELECT document_id FROM document_branches WHERE id=?`, branchID).Scan(&branchDocumentID).Error; err != nil {
		t.Fatalf("load reconciled branch: %v", err)
	}
	if branchDocumentID != serviceID {
		t.Fatalf("reconciled branch document_id = %s, want %s", branchDocumentID, serviceID)
	}
	assertAllMigrationChecksumsRecorded(t, database)
}

func TestRunMigrationsRejectsAppliedMigrationChecksumDrift(t *testing.T) {
	dsn := os.Getenv("VDOC_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("VDOC_TEST_DATABASE_DSN not set; skipping migration checksum drift test")
	}
	database := openTestDB(t, dsn)
	defer closeTestDB(t, database)
	resetPublicSchema(t, database)
	if err := RunMigrations(context.Background(), database); err != nil {
		t.Fatalf("initial RunMigrations: %v", err)
	}
	if err := database.Exec(`UPDATE schema_migrations SET checksum=repeat('0', 64) WHERE version='001'`).Error; err != nil {
		t.Fatalf("tamper migration checksum: %v", err)
	}

	err := RunMigrations(context.Background(), database)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("RunMigrations checksum drift error = %v, want checksum mismatch", err)
	}
}

func TestRunMigrationsEnforcesKeyConstraints(t *testing.T) {
	dsn := os.Getenv("VDOC_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("VDOC_TEST_DATABASE_DSN not set; skipping PostgreSQL constraint integration test")
	}
	database := openTestDB(t, dsn)
	defer closeTestDB(t, database)
	resetPublicSchema(t, database)
	if err := RunMigrations(context.Background(), database); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	userID := "11111111-1111-1111-1111-111111111111"
	insertUser(t, database, userID, "owner@example.com")
	assertExecFails(t, database, `INSERT INTO users(email,password_hash,display_name,status) VALUES('bad@example.com','hash','Bad',9)`, "users.status")
	assertExecFails(t, database, `INSERT INTO users(email,password_hash,display_name,status) VALUES('OWNER@example.com','hash','Dup',1)`, "users.email")

	teamID := "22222222-2222-2222-2222-222222222222"
	projectID := "33333333-3333-3333-3333-333333333333"
	documentID := "44444444-4444-4444-4444-444444444444"
	branchID := "55555555-5555-5555-5555-555555555555"
	draftID := "66666666-6666-6666-6666-666666666666"
	secondDraftID := "66666666-6666-6666-6666-666666666667"
	versionID := "77777777-7777-7777-7777-777777777777"
	endpointID := "88888888-8888-8888-8888-888888888888"
	toVersionID := "99999999-9999-9999-9999-999999999999"
	insertGraph(t, database, userID, teamID, projectID, documentID, branchID, draftID, secondDraftID, versionID, endpointID, toVersionID)

	assertExecFails(t, database, `INSERT INTO project_members(project_id,user_id,role,status,added_by) VALUES('`+projectID+`','`+userID+`',9,1,'`+userID+`')`, "member role")
	assertExecFails(t, database, `INSERT INTO project_members(project_id,user_id,role,status,added_by) VALUES('`+projectID+`','`+userID+`',1,9,'`+userID+`')`, "member status")
	assertExecFails(t, database, `INSERT INTO document_branches(document_id,name,kind,status,created_by) VALUES('`+documentID+`','feature/nope',1,1,'`+userID+`')`, "branch name unique")
	assertExecFails(t, database, `INSERT INTO document_branches(document_id,name,kind,status,created_by) VALUES('`+documentID+`','bugfix/nope',2,1,'`+userID+`')`, "feature name")
	assertExecFails(t, database, `INSERT INTO document_versions(project_id,document_id,branch_id,version_name,version_no,relative_path,status,source_draft_id,source_type,document_format,raw_schema_object_key,normalized_schema_object_key,raw_schema_hash,normalized_schema_hash,schema_size_bytes,published_by) VALUES('`+projectID+`','`+documentID+`','`+branchID+`','1.0.0',3,'openapi/pets.yaml',1,'`+draftID+`',1,1,'raw2','norm2','r2','n2',10,'`+userID+`')`, "version uniqueness")
	assertExecFails(t, database, `INSERT INTO api_endpoints(document_version_id,document_id,branch_id,method,path,tags,deprecated,request_hash,response_hash,endpoint_hash,sort_order) VALUES('`+versionID+`','`+documentID+`','`+branchID+`',9,'/bad','{}',false,'r','s','e',2)`, "endpoint method")
	assertExecFails(t, database, `INSERT INTO document_diff_items(diff_id,severity,change_type,message,is_breaking,sort_order) SELECT id,3,1,'bad',false,2 FROM document_version_diffs LIMIT 1`, "breaking consistency")
}

func migrationBodies(migrations []Migration) []string {
	bodies := make([]string, 0, len(migrations))
	for _, migration := range migrations {
		bodies = append(bodies, migration.SQL)
	}
	return bodies
}

func openTestDB(t *testing.T, dsn string) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	pool, err := database.DB()
	if err != nil {
		t.Fatalf("database pool: %v", err)
	}
	if err := pool.Ping(); err != nil {
		_ = pool.Close()
		t.Fatalf("ping database: %v", err)
	}
	return database
}

func closeTestDB(t *testing.T, database *gorm.DB) {
	t.Helper()
	pool, err := database.DB()
	if err != nil {
		t.Fatalf("database pool: %v", err)
	}
	if err := pool.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
}

func resetPublicSchema(t *testing.T, database *gorm.DB) {
	t.Helper()
	dsn := os.Getenv("VDOC_TEST_DATABASE_DSN")
	var actualDatabase string
	if err := database.Raw(`SELECT current_database()`).Scan(&actualDatabase).Error; err != nil {
		t.Fatalf("read PostgreSQL database name: %v", err)
	}
	if err := ValidateDisposableTestDatabaseConnection(dsn, actualDatabase); err != nil {
		t.Fatalf("refusing to reset PostgreSQL schema: %v", err)
	}
	if err := database.Exec(`DROP SCHEMA public CASCADE`).Error; err != nil {
		t.Fatalf("drop public schema: %v", err)
	}
	if err := database.Exec(`CREATE SCHEMA public`).Error; err != nil {
		t.Fatalf("create public schema: %v", err)
	}
}

func tableExists(t *testing.T, database *gorm.DB, name string) bool {
	t.Helper()
	var exists bool
	if err := database.Raw(`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name=?)`, name).Scan(&exists).Error; err != nil {
		t.Fatalf("check table %s: %v", name, err)
	}
	return exists
}

func assertAllMigrationChecksumsRecorded(t *testing.T, database *gorm.DB) {
	t.Helper()
	var missing int64
	if err := database.Raw(`SELECT count(*) FROM schema_migrations WHERE checksum IS NULL OR checksum !~ '^[0-9a-f]{64}$'`).Scan(&missing).Error; err != nil {
		t.Fatalf("count missing migration checksums: %v", err)
	}
	if missing != 0 {
		t.Fatalf("schema_migrations has %d rows without a valid checksum", missing)
	}
}

func insertUser(t *testing.T, database *gorm.DB, id, email string) {
	t.Helper()
	if err := database.Exec(`INSERT INTO users(id,email,password_hash,display_name,status) VALUES(?,?,'hash','Owner',1)`, id, email).Error; err != nil {
		t.Fatalf("insert user: %v", err)
	}
}

func insertGraph(t *testing.T, database *gorm.DB, userID, teamID, projectID, documentID, branchID, draftID, secondDraftID, versionID, endpointID, toVersionID string) {
	t.Helper()
	statements := []string{
		`INSERT INTO teams(id,name,slug,created_by) VALUES('` + teamID + `','Team','team','` + userID + `')`,
		`INSERT INTO projects(id,team_id,name,slug,status,created_by) VALUES('` + projectID + `','` + teamID + `','Project','project',1,'` + userID + `')`,
		`INSERT INTO project_members(project_id,user_id,role,status,added_by) VALUES('` + projectID + `','` + userID + `',3,1,'` + userID + `')`,
		`INSERT INTO documents(id,project_id,name,document_type,relative_path,status,created_by) VALUES('` + documentID + `','` + projectID + `','svc',1,'openapi/pets.yaml',1,'` + userID + `')`,
		`INSERT INTO document_branches(id,document_id,name,kind,is_default,is_protected,status,created_by) VALUES('` + branchID + `','` + documentID + `','feature/nope',2,true,false,1,'` + userID + `')`,
		`INSERT INTO document_drafts(id,project_id,document_id,branch_id,version_name,relative_path,status,document_format,raw_schema_object_key,normalized_schema_object_key,raw_schema_hash,normalized_schema_hash,schema_size_bytes,schema_metadata,source_type,created_by_actor_type,created_by_user_id) VALUES('` + draftID + `','` + projectID + `','` + documentID + `','` + branchID + `','1.0.0','openapi/pets.yaml',5,1,'raw','norm','r','n',10,'{}',1,1,'` + userID + `')`,
		`INSERT INTO document_versions(id,project_id,document_id,branch_id,version_name,version_no,relative_path,status,source_draft_id,source_type,document_format,raw_schema_object_key,normalized_schema_object_key,raw_schema_hash,normalized_schema_hash,schema_size_bytes,published_by) VALUES('` + versionID + `','` + projectID + `','` + documentID + `','` + branchID + `','1.0.0',1,'openapi/pets.yaml',1,'` + draftID + `',1,1,'raw','norm','r','n',10,'` + userID + `')`,
		`INSERT INTO document_drafts(id,project_id,document_id,branch_id,version_name,relative_path,status,document_format,raw_schema_object_key,normalized_schema_object_key,raw_schema_hash,normalized_schema_hash,schema_size_bytes,schema_metadata,source_type,created_by_actor_type,created_by_user_id) VALUES('` + secondDraftID + `','` + projectID + `','` + documentID + `','` + branchID + `','1.0.1','openapi/pets.yaml',5,1,'raw3','norm3','r3','n3',10,'{}',1,1,'` + userID + `')`,
		`INSERT INTO document_versions(id,project_id,document_id,branch_id,version_name,version_no,relative_path,status,source_draft_id,source_type,document_format,raw_schema_object_key,normalized_schema_object_key,raw_schema_hash,normalized_schema_hash,schema_size_bytes,published_by) VALUES('` + toVersionID + `','` + projectID + `','` + documentID + `','` + branchID + `','1.0.1',2,'openapi/pets.yaml',1,'` + secondDraftID + `',1,1,'raw3','norm3','r3','n3',10,'` + userID + `')`,
		`INSERT INTO api_endpoints(id,document_version_id,document_id,branch_id,method,path,tags,deprecated,request_hash,response_hash,endpoint_hash,sort_order) VALUES('` + endpointID + `','` + versionID + `','` + documentID + `','` + branchID + `',1,'/pets','{}',false,'r','s','e',1)`,
		`INSERT INTO api_endpoint_details(endpoint_id,parameters_json,responses_json,normalized_operation_json) VALUES('` + endpointID + `','[]','{}','{}')`,
		`INSERT INTO document_version_diffs(document_id,from_branch_id,to_branch_id,from_version_id,to_version_id,diff_status,diff_summary_json,breaking_changes_json,added_count,modified_count,removed_count,breaking_count) VALUES('` + documentID + `','` + branchID + `','` + branchID + `','` + versionID + `','` + toVersionID + `',3,'{}','{}',0,0,0,0)`,
	}
	for _, statement := range statements {
		if err := database.Exec(statement).Error; err != nil {
			t.Fatalf("exec %s: %v", statement, err)
		}
	}
}

func assertExecFails(t *testing.T, database *gorm.DB, statement, label string) {
	t.Helper()
	if err := database.Exec(statement).Error; err == nil {
		t.Fatalf("expected %s constraint to reject statement", label)
	}
}
