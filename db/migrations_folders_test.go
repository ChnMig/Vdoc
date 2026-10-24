package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	domainvdoc "vdoc/domain/vdoc"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestDocumentFolderMigrationBackfillsWithoutChangingSnapshots(t *testing.T) {
	database := openFolderMigrationTestDB(t)
	applyMigrationsBeforeFolders(t, database)
	const (
		userID         = "11111111-1111-1111-1111-111111111111"
		projectID      = "33333333-3333-3333-3333-333333333333"
		otherProjectID = "33333333-3333-3333-3333-333333333334"
	)
	insertFolderProjects(t, database, userID, projectID, otherProjectID)
	paths := []string{
		"Guides/Install/first.md", "guides/install/second.md", "README.md",
		"unsafe//notes.md", "../outside.md", "legacy\\bad/notes.md", "./notes.md", "/absolute.md", "control\n/notes.md",
	}
	for index, relativePath := range paths {
		id := fmt.Sprintf("44444444-4444-4444-4444-%012d", index)
		if err := database.Exec(`INSERT INTO documents(id,project_id,name,document_type,relative_path,created_by) VALUES(?,?,?,2,?,?)`, id, projectID, fmt.Sprintf("Document %d", index), relativePath, userID).Error; err != nil {
			t.Fatal(err)
		}
	}
	const otherDocumentID = "44444444-4444-4444-4444-999999999999"
	if err := database.Exec(`INSERT INTO documents(id,project_id,name,document_type,relative_path,created_by) VALUES(?,?,?,2,?,?)`, otherDocumentID, otherProjectID, "Other project", paths[0], userID).Error; err != nil {
		t.Fatal(err)
	}
	const snapshotPath = "Historical/Renamed.md"
	insertFolderPathSnapshots(t, database, userID, projectID, "44444444-4444-4444-4444-000000000000", snapshotPath)

	if err := RunMigrations(context.Background(), database); err != nil {
		t.Fatalf("upgrade existing documents: %v", err)
	}
	if err := RunMigrations(context.Background(), database); err != nil {
		t.Fatalf("repeat folder migration: %v", err)
	}
	var folders []struct{ ID, ProjectID, ParentID, Name, Path string }
	if err := database.Raw(`SELECT id,project_id,coalesce(parent_id::text,'') AS parent_id,name,path FROM document_folders ORDER BY project_id,path`).Scan(&folders).Error; err != nil {
		t.Fatal(err)
	}
	if len(folders) != 4 {
		t.Fatalf("folder count = %d, want 2 per project: %+v", len(folders), folders)
	}
	if folders[0].Name != "Guides" || folders[0].Path != "Guides" || folders[0].ParentID != "" || folders[1].Name != "Install" || folders[1].Path != "Guides/Install" || folders[1].ParentID != folders[0].ID {
		t.Fatalf("backfilled hierarchy = %+v", folders[:2])
	}
	for index, want := range paths {
		var document struct {
			RelativePath string
			FolderID     sql.NullString
		}
		if err := database.Raw(`SELECT relative_path,folder_id FROM documents WHERE id=?`, fmt.Sprintf("44444444-4444-4444-4444-%012d", index)).Scan(&document).Error; err != nil {
			t.Fatal(err)
		}
		if document.RelativePath != want {
			t.Fatalf("path %d changed from %q to %q", index, want, document.RelativePath)
		}
		if index < 2 && (!document.FolderID.Valid || document.FolderID.String != folders[1].ID) {
			t.Fatalf("case-insensitive directory mapping = %+v, want %s", document, folders[1].ID)
		}
		if index >= 2 && document.FolderID.Valid {
			t.Fatalf("root or unsafe historical path %q assigned a folder", want)
		}
	}
	for _, table := range []string{"document_drafts", "document_versions"} {
		var got string
		if err := database.Raw("SELECT relative_path FROM " + table).Scan(&got).Error; err != nil || got != snapshotPath {
			t.Fatalf("%s path = %q, want preserved %q, err=%v", table, got, snapshotPath, err)
		}
	}
	assertFolderStatementFails(t, database, `INSERT INTO document_folders(project_id,parent_id,name,path,created_by) VALUES(?,?,?,?,?)`, otherProjectID, folders[0].ID, "Cross", "Cross", userID)
	assertFolderStatementFails(t, database, `UPDATE documents SET folder_id=? WHERE id=?`, folders[1].ID, otherDocumentID)
	assertFolderStatementFails(t, database, `INSERT INTO document_folders(project_id,name,path,created_by) VALUES(?,?,?,?)`, projectID, "GUIDES", "GUIDES", userID)
	assertFolderStatementFails(t, database, `INSERT INTO document_folders(id,project_id,parent_id,name,path,created_by) VALUES(?,?,?,?,?,?)`, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", projectID, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "Self", "Self", userID)
}

func TestDocumentFoldersRoundTripDeepPathsAndEmptyFolders(t *testing.T) {
	database := openFolderMigrationTestDB(t)
	if err := RunMigrations(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	const (
		userID    = "11111111-1111-1111-1111-111111111111"
		projectID = "33333333-3333-3333-3333-333333333333"
	)
	insertFolderProjects(t, database, userID, projectID, "33333333-3333-3333-3333-333333333334")
	now := time.Now().UTC().Truncate(time.Microsecond)
	parentID, relativeDirectory := "", ""
	const depth = 96
	for index := range depth {
		name := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("folder %d", index))))
		if relativeDirectory != "" {
			relativeDirectory += "/"
		}
		relativeDirectory += name
		folder := &domainvdoc.DocumentFolder{ID: uuid.NewString(), ProjectID: projectID, ParentID: parentID, Name: name, Path: relativeDirectory, CreatedBy: userID, CreatedAt: now, UpdatedAt: now}
		if err := database.Exec(`INSERT INTO document_folders(id,project_id,parent_id,name,path,created_by,created_at,updated_at) VALUES(?,?,nullif(?,'')::uuid,?,?,?,?,?)`, folder.ID, folder.ProjectID, folder.ParentID, folder.Name, folder.Path, folder.CreatedBy, folder.CreatedAt, folder.UpdatedAt).Error; err != nil {
			t.Fatalf("persist depth %d (path length %d): %v", index+1, len(relativeDirectory), err)
		}
		parentID = folder.ID
	}
	emptyID := uuid.NewString()
	if err := database.Exec(`INSERT INTO document_folders(id,project_id,name,path,created_by,created_at,updated_at) VALUES(?,?,'Empty','Empty',?,?,?)`, emptyID, projectID, userID, now, now).Error; err != nil {
		t.Fatalf("persist empty folder: %v", err)
	}
	document := &domainvdoc.APIService{ID: uuid.NewString(), ProjectID: projectID, Name: "Human-readable title", DocumentType: domainvdoc.DocumentTypeMarkdown, FolderID: parentID, RelativePath: relativeDirectory + "/filename.md", Status: domainvdoc.DocumentStatusActive, CreatedBy: userID, CreatedAt: now, UpdatedAt: now}
	if err := database.Exec(`INSERT INTO documents(id,project_id,name,document_type,folder_id,relative_path,created_by,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, document.ID, document.ProjectID, document.Name, document.DocumentType, document.FolderID, document.RelativePath, document.CreatedBy, document.CreatedAt, document.UpdatedAt).Error; err != nil {
		t.Fatalf("persist long document path: %v", err)
	}
	insertFolderPathSnapshots(t, database, userID, projectID, document.ID, document.RelativePath)
	var folderCount int64
	if err := database.Table("document_folders").Count(&folderCount).Error; err != nil || folderCount != depth+1 {
		t.Fatalf("folder count = %d, want %d: %v", folderCount, depth+1, err)
	}
	var deepest domainvdoc.DocumentFolder
	if err := database.Raw(`SELECT id,project_id,parent_id,name,path,created_by,created_at,updated_at FROM document_folders WHERE id=?`, parentID).Scan(&deepest).Error; err != nil || deepest.Path != relativeDirectory {
		t.Fatalf("deepest folder path did not round-trip: %v", err)
	}
	var got domainvdoc.APIService
	if err := database.Raw(`SELECT id,project_id,name,folder_id,relative_path FROM documents WHERE id=?`, document.ID).Scan(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.FolderID != parentID || got.RelativePath != document.RelativePath || got.Name != document.Name {
		t.Fatalf("loaded document = %+v, want folder, filename and display name preserved", got)
	}
	var empty domainvdoc.DocumentFolder
	if err := database.Raw(`SELECT id,name,path,created_at FROM document_folders WHERE id=?`, emptyID).Scan(&empty).Error; err != nil || !empty.CreatedAt.Equal(now) || empty.Path != "Empty" {
		t.Fatalf("empty folder did not round-trip: %+v, %v", empty, err)
	}
	assertFolderStatementFails(t, database, `INSERT INTO documents(project_id,name,document_type,relative_path,created_by) VALUES(?,'Another title',2,?,?)`, projectID, strings.ToUpper(document.RelativePath), userID)
}

func openFolderMigrationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("VDOC_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("VDOC_TEST_DATABASE_DSN not set; skipping PostgreSQL document folder test")
	}
	database := openTestDB(t, dsn)
	t.Cleanup(func() { closeTestDB(t, database) })
	resetPublicSchema(t, database)
	return database
}

func applyMigrationsBeforeFolders(t *testing.T, database *gorm.DB) {
	t.Helper()
	if err := database.Exec(`CREATE TABLE schema_migrations(version text PRIMARY KEY,name text NOT NULL,checksum text,applied_at timestamptz NOT NULL DEFAULT now())`).Error; err != nil {
		t.Fatal(err)
	}
	migrations, err := EmbeddedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations {
		if migration.Version < "009" {
			if err := runMigration(context.Background(), database, migration); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func insertFolderProjects(t *testing.T, database *gorm.DB, userID, projectID, otherProjectID string) {
	t.Helper()
	insertUser(t, database, userID, "folders@example.test")
	const teamID = "22222222-2222-2222-2222-222222222222"
	if err := database.Exec(`INSERT INTO teams(id,name,slug,created_by) VALUES(?,'Folders','folders',?)`, teamID, userID).Error; err != nil {
		t.Fatal(err)
	}
	for index, id := range []string{projectID, otherProjectID} {
		if err := database.Exec(`INSERT INTO projects(id,team_id,name,slug,created_by) VALUES(?,?,?,?,?)`, id, teamID, fmt.Sprintf("Project %d", index), fmt.Sprintf("project-%d", index), userID).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func insertFolderPathSnapshots(t *testing.T, database *gorm.DB, userID, projectID, documentID, relativePath string) {
	t.Helper()
	branchID, draftID := uuid.NewString(), uuid.NewString()
	if err := database.Exec(`INSERT INTO document_branches(id,document_id,name,kind,is_default,created_by) VALUES(?,?,'main',1,true,?)`, branchID, documentID, userID).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Exec(`INSERT INTO document_drafts(id,project_id,document_id,branch_id,version_name,relative_path,document_format,raw_schema_object_key,normalized_schema_object_key,raw_schema_hash,normalized_schema_hash,schema_size_bytes,created_by_actor_type,created_by_user_id) VALUES(?,?,?,?,'1.0.0',?,3,'raw','normalized','raw-hash','normalized-hash',5,1,?)`, draftID, projectID, documentID, branchID, relativePath, userID).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Exec(`INSERT INTO document_versions(project_id,document_id,branch_id,version_name,version_no,relative_path,source_draft_id,document_format,raw_schema_object_key,normalized_schema_object_key,raw_schema_hash,normalized_schema_hash,schema_size_bytes,published_by) VALUES(?,?,?,'1.0.0',1,?,?,3,'raw','normalized','raw-hash','normalized-hash',5,?)`, projectID, documentID, branchID, relativePath, draftID, userID).Error; err != nil {
		t.Fatal(err)
	}
}

func assertFolderStatementFails(t *testing.T, database *gorm.DB, statement string, args ...any) {
	t.Helper()
	if err := database.Exec(statement, args...).Error; err == nil {
		t.Fatalf("expected folder constraint to reject %s", statement)
	}
}
