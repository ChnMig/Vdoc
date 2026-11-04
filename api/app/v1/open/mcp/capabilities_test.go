package mcp

import (
	"errors"
	"fmt"
	"testing"

	"github.com/gin-gonic/gin"
	app "vdoc/appstore"
)

func TestMCPCapabilitiesCoverDocumentAndFolderLifecycle(t *testing.T) {
	for _, kind := range []int{app.DocumentTypeOpenAPI, app.DocumentTypeMarkdown, app.DocumentTypeText} {
		t.Run(fmt.Sprint(kind), func(t *testing.T) { testMCPCapabilityLifecycle(t, kind) })
	}
}

func testMCPCapabilityLifecycle(t *testing.T, kind int) {
	t.Helper()
	f := newMCPFixture(t)
	read := catalogMCPToken(t, f.superID, app.ScopeRead)
	write := catalogMCPToken(t, f.superID, app.ScopeWrite)
	both := catalogMCPToken(t, f.superID, app.ScopeRead, app.ScopeWrite)
	for _, tool := range []string{"list_teams", "list_projects", "list_documents", "list_folders"} {
		args := gin.H{}
		if tool == "list_documents" || tool == "list_folders" {
			args["project_id"] = f.projectID
		}
		repositoryMCPCall(t, f, read, tool, args, nil)
	}
	for _, name := range []string{"source", "destination"} {
		repositoryMCPCall(t, f, write, "create_folder", gin.H{"project_id": f.projectID, "name": name}, nil)
	}
	source := "\ufeffopaque: 未解析格式\r\noriginal exact bytes\r\n"
	extension := map[int]string{app.DocumentTypeOpenAPI: ".yaml", app.DocumentTypeMarkdown: ".md", app.DocumentTypeText: ".txt"}[kind]
	path := fmt.Sprintf("source/type-%d.unknown%s", kind, extension)
	d := createContentToolDocument(t, f, write, path, kind, source)
	var full mcpRepositoryContent
	repositoryMCPCall(t, f, read, "get_document_at_commit", contentToolRef(f, d), &full)
	if full.Content != source {
		t.Fatal("capability read changed unparsed original bytes")
	}
	var section mcpDocumentSection
	repositoryMCPCall(t, f, read, "get_document_section", contentToolRef(f, d), &section)
	if section.Content != source || section.ContentKind != "raw" {
		t.Fatalf("unparsed section=%+v", section)
	}
	assertRPCError(t, callMCPToolRPC(t, f.router, write, "get_document_at_commit", contentToolRef(f, d)), -32003, "write does not imply read")
	assertRPCError(t, callMCPToolRPC(t, f.router, read, "delete_document", gin.H{"project_id": f.projectID, "document_id": d.Document.ID}), -32003, "read does not imply write")
	var replacement mcpRepositoryCommit
	repositoryMCPCall(t, f, write, "commit_changes", gin.H{
		"project_id": f.projectID, "document_id": d.Document.ID, "branch_id": d.Branch.ID,
		"expected_head_commit_id": d.Commit.ID, "expected_generation": d.Branch.Generation,
		"idempotency_key": fmt.Sprintf("capability-replace-%d", kind),
		"changes":         []gin.H{{"operation": "upsert", "document_id": d.Document.ID, "name": path, "relative_path": path, "document_type": kind, "content": "replacement\r\nexact\r\n"}},
	}, &replacement)
	var patched mcpRepositoryCommit
	repositoryMCPCall(t, f, both, "apply_document_patch", gin.H{
		"project_id": f.projectID, "document_id": d.Document.ID, "branch_id": d.Branch.ID,
		"expected_head_commit_id": replacement.ID, "expected_generation": d.Branch.Generation + 1,
		"idempotency_key": fmt.Sprintf("capability-patch-%d", kind),
		"edits":           []gin.H{{"old_text": "replacement", "new_text": "patched"}},
	}, &patched)
	ref := contentToolRef(f, d)
	ref["commit_id"] = patched.ID
	repositoryMCPCall(t, f, read, "get_document_at_commit", ref, &full)
	if full.Content != "patched\r\nexact\r\n" {
		t.Fatalf("replacement/patch bytes=%q", full.Content)
	}
	repositoryMCPCall(t, f, read, "get_document_at_commit", contentToolRef(f, d), &full)
	if full.Content != source {
		t.Fatal("write changed immutable full-file history")
	}
	var history mcpRepositoryHistory
	repositoryMCPCall(t, f, read, "list_commits", gin.H{"project_id": f.projectID, "document_id": d.Document.ID, "branch_id": d.Branch.ID}, &history)
	if len(history.Commits) != 3 {
		t.Fatalf("history=%+v", history)
	}
	var moved mcpRepositoryDocument
	target := fmt.Sprintf("destination/type-%d.unknown%s", kind, extension)
	repositoryMCPCall(t, f, write, "update_document", gin.H{"project_id": f.projectID, "document_id": d.Document.ID, "relative_path": target, "name": "Renamed"}, &moved)
	if moved.ID != d.Document.ID || moved.Name != "Renamed" || moved.RelativePath != target {
		t.Fatalf("document move=%+v", moved)
	}
	repositoryMCPCall(t, f, write, "delete_document", gin.H{"project_id": f.projectID, "document_id": d.Document.ID}, nil)
	assertRPCError(t, callMCPToolRPC(t, f.router, read, "get_document_at_commit", contentToolRef(f, d)), -32004, "deleted document")
	var renamed mcpCatalogFolder
	repositoryMCPCall(t, f, write, "update_folder", gin.H{"project_id": f.projectID, "path": "source", "name": "renamed", "parent_path": "destination"}, &renamed)
	if renamed.Path != "destination/renamed" || renamed.ParentPath != "destination" {
		t.Fatalf("folder rename/move=%+v", renamed)
	}
	repositoryMCPCall(t, f, write, "delete_folder", gin.H{"project_id": f.projectID, "path": renamed.Path}, nil)
	repositoryMCPCall(t, f, write, "delete_folder", gin.H{"project_id": f.projectID, "path": "destination"}, nil)
}

func TestMCPCapabilitiesDoNotBroadenProjectRoles(t *testing.T) {
	f := newMCPFixture(t)
	d := createContentToolDocument(t, f, catalogMCPToken(t, f.superID, app.ScopeWrite), "roles.txt", app.DocumentTypeText, "readable")
	for _, test := range []struct {
		actor  string
		scopes []int
		tool   string
		args   gin.H
	}{
		{f.readerID, []int{app.ScopeWrite}, "create_document", gin.H{"name": "Denied", "relative_path": "denied.txt", "document_type": app.DocumentTypeText, "content": "denied", "idempotency_key": "reader-denied"}},
		{f.writerID, []int{app.ScopeWrite}, "delete_document", gin.H{"document_id": d.Document.ID}},
		{f.writerID, []int{app.ScopeWrite}, "update_document", gin.H{"document_id": d.Document.ID, "name": "Denied"}},
		{f.writerID, []int{app.ScopeWrite}, "create_folder", gin.H{"name": "Denied"}},
		{f.readerID, []int{app.ScopeRead}, "delete_document", gin.H{"document_id": d.Document.ID}},
	} {
		test.args["project_id"] = f.projectID
		if _, err := execute(test.actor, test.scopes, test.tool, mustJSON(t, test.args)); !errors.Is(err, app.ErrPermissionDenied) {
			t.Errorf("actor %s tool %s: %v", test.actor, test.tool, err)
		}
	}
	if _, err := app.DefaultStore().RemoveProjectMember(f.superID, f.projectID, f.readerID); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"list_documents", "create_folder"} {
		args := gin.H{"project_id": f.projectID}
		if tool == "create_folder" {
			args["name"] = "Denied"
		}
		if _, err := execute(f.readerID, []int{app.ScopeRead, app.ScopeWrite}, tool, mustJSON(t, args)); !errors.Is(err, app.ErrPermissionDenied) {
			t.Errorf("removed membership %s: %v", tool, err)
		}
	}
}
