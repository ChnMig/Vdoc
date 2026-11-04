package mcp

import (
	"errors"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	app "vdoc/appstore"
)

func catalogMCPToken(t *testing.T, actor string, scopes ...int) string {
	t.Helper()
	token, err := app.DefaultStore().CreateMCPToken(actor, "catalog-tests", scopes, nil)
	if err != nil {
		t.Fatal(err)
	}
	return token.Token
}

func catalogMCPDocument(t *testing.T, fixture mcpFixture, token, relativePath, key string, kind int) mcpRepositoryDocument {
	t.Helper()
	var created struct {
		Document mcpRepositoryDocument `json:"document"`
	}
	repositoryMCPCall(t, fixture, token, "create_document", gin.H{
		"project_id": fixture.projectID, "name": "Catalog file", "relative_path": relativePath,
		"document_type": kind, "content": "\ufeff# Original\r\n", "idempotency_key": key,
	}, &created)
	return created.Document
}

func TestMCPCatalogDocumentAndFolderLifecycle(t *testing.T) {
	f := newMCPFixture(t)
	scopes := []int{app.ScopeAPIRead, app.ScopeAPIDraft, app.ScopeDocRead, app.ScopeDocDraft}
	token := catalogMCPToken(t, f.superID, scopes...)
	for _, name := range []string{"source", "destination"} {
		repositoryMCPCall(t, f, token, "create_folder", gin.H{"project_id": f.projectID, "name": name}, nil)
	}
	document := catalogMCPDocument(t, f, token, "source/guide.md", "catalog-create-guide", app.DocumentTypeMarkdown)
	var branches []mcpRepositoryBranch
	repositoryMCPCall(t, f, token, "list_branches", gin.H{"project_id": f.projectID, "document_id": document.ID}, &branches)
	if len(branches) != 1 {
		t.Fatalf("independent branches: %+v", branches)
	}
	initialCommit := branches[0].HeadCommitID
	var updated mcpRepositoryDocument
	repositoryMCPCall(t, f, token, "update_document", gin.H{
		"project_id": f.projectID, "document_id": document.ID, "name": "Renamed guide", "relative_path": "destination/renamed.md", "description": "Current description",
	}, &updated)
	if updated.ID != document.ID || updated.Name != "Renamed guide" || updated.RelativePath != "destination/renamed.md" || updated.DefaultBranchID != document.DefaultBranchID {
		t.Fatalf("metadata move changed identity: %+v", updated)
	}
	var historical mcpRepositoryContent
	repositoryMCPCall(t, f, token, "get_document_at_commit", gin.H{"project_id": f.projectID, "document_id": document.ID, "commit_id": initialCommit}, &historical)
	if historical.Content != "\ufeff# Original\r\n" || historical.Document.RelativePath != "source/guide.md" {
		t.Fatalf("metadata update rewrote immutable source: %+v", historical)
	}
	var folder mcpCatalogFolder
	repositoryMCPCall(t, f, token, "update_folder", gin.H{"project_id": f.projectID, "path": "destination", "name": "renamed-folder"}, &folder)
	if folder.Path != "renamed-folder" || folder.ParentPath != "" {
		t.Fatalf("folder rename: %+v", folder)
	}
	if _, err := execute(f.superID, scopes, "delete_folder", mustJSON(t, gin.H{"project_id": f.projectID, "path": folder.Path})); !errors.Is(err, app.ErrFailedPrecondition) {
		t.Fatalf("non-empty folder deletion: %v", err)
	}
	repositoryMCPCall(t, f, token, "update_document", gin.H{"project_id": f.projectID, "document_id": document.ID, "relative_path": "renamed.md"}, &updated)
	repositoryMCPCall(t, f, token, "delete_folder", gin.H{"project_id": f.projectID, "path": folder.Path}, nil)
	var folders []mcpCatalogFolder
	repositoryMCPCall(t, f, token, "list_folders", gin.H{"project_id": f.projectID}, &folders)
	for _, item := range folders {
		if item.Path == folder.Path {
			t.Fatalf("deleted folder remained visible: %+v", item)
		}
	}
	writeOnly := catalogMCPToken(t, f.superID, app.ScopeDocDraft)
	for i := 0; i < 2; i++ {
		repositoryMCPCall(t, f, writeOnly, "delete_document", gin.H{"project_id": f.projectID, "document_id": document.ID}, nil)
	}
	var documents []mcpRepositoryDocument
	repositoryMCPCall(t, f, token, "list_documents", gin.H{"project_id": f.projectID}, &documents)
	for _, item := range documents {
		if item.ID == document.ID {
			t.Fatal("deleted document remained in the active catalog")
		}
	}
	if _, err := execute(f.superID, scopes, "get_document_at_commit", mustJSON(t, gin.H{"project_id": f.projectID, "document_id": document.ID, "commit_id": initialCommit})); err == nil {
		t.Fatal("deleted document remained readable by a known immutable reference")
	}
	replacement := catalogMCPDocument(t, f, token, "renamed.md", "catalog-recreate-guide", app.DocumentTypeMarkdown)
	if replacement.ID == document.ID {
		t.Fatal("recreated document reused deleted identity")
	}
}

func TestMCPCatalogScopesAndProjectRoles(t *testing.T) {
	f := newMCPFixture(t)
	all := []int{app.ScopeAPIRead, app.ScopeAPIDraft, app.ScopeDocRead, app.ScopeDocDraft}
	token := catalogMCPToken(t, f.superID, all...)
	repositoryMCPCall(t, f, token, "create_folder", gin.H{"project_id": f.projectID, "name": "mixed"}, nil)
	markdown := catalogMCPDocument(t, f, token, "mixed/guide.md", "catalog-scope-markdown", app.DocumentTypeMarkdown)
	api := catalogMCPDocument(t, f, token, "mixed/api.yaml", "catalog-scope-api", app.DocumentTypeOpenAPI)
	for _, test := range []struct {
		name   string
		actor  string
		scopes []int
		tool   string
		args   gin.H
	}{
		{"mixed folder scopes", f.superID, []int{app.ScopeDocDraft}, "update_folder", gin.H{"path": "mixed", "name": "blocked"}},
		{"API metadata scope", f.superID, []int{app.ScopeDocDraft}, "update_document", gin.H{"document_id": api.ID, "name": "blocked"}},
		{"API deletion scope", f.superID, []int{app.ScopeDocDraft}, "delete_document", gin.H{"document_id": api.ID}},
		{"read-only deletion", f.superID, []int{app.ScopeDocRead}, "delete_document", gin.H{"document_id": markdown.ID}},
		{"writer metadata", f.writerID, all, "update_document", gin.H{"document_id": markdown.ID, "name": "blocked"}},
		{"writer deletion", f.writerID, all, "delete_document", gin.H{"document_id": markdown.ID}},
		{"writer folder management", f.writerID, all, "create_folder", gin.H{"name": "blocked"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.args["project_id"] = f.projectID
			if _, err := execute(test.actor, test.scopes, test.tool, mustJSON(t, test.args)); !errors.Is(err, app.ErrPermissionDenied) {
				t.Fatalf("permission boundary: %v", err)
			}
		})
	}
	var documents []mcpRepositoryDocument
	repositoryMCPCall(t, f, token, "list_documents", gin.H{"project_id": f.projectID}, &documents)
	for _, item := range documents {
		if (item.ID == markdown.ID || item.ID == api.ID) && (!strings.HasPrefix(item.RelativePath, "mixed/") || item.Name != "Catalog file") {
			t.Fatalf("denied mutation partially changed catalog: %+v", item)
		}
	}
}

func TestMCPCatalogRejectsMalformedAndCyclicOperations(t *testing.T) {
	f := newMCPFixture(t)
	token := catalogMCPToken(t, f.superID, app.ScopeDocRead, app.ScopeDocDraft)
	repositoryMCPCall(t, f, token, "create_folder", gin.H{"project_id": f.projectID, "name": "parent"}, nil)
	repositoryMCPCall(t, f, token, "create_folder", gin.H{"project_id": f.projectID, "parent_path": "parent", "name": "child"}, nil)
	document := catalogMCPDocument(t, f, token, "parent/file.md", "catalog-invalid-guide", app.DocumentTypeMarkdown)
	for _, test := range []struct {
		tool string
		args gin.H
	}{
		{"update_folder", gin.H{"path": "parent"}},
		{"update_folder", gin.H{"path": "parent", "parent_path": "parent/child"}},
		{"update_folder", gin.H{"path": "parent", "name": nil}},
		{"create_folder", gin.H{"name": "../outside"}},
		{"update_document", gin.H{"document_id": document.ID}},
		{"update_document", gin.H{"document_id": document.ID, "name": nil}},
		{"update_document", gin.H{"document_id": document.ID, "relative_path": "../outside.md"}},
		{"delete_folder", gin.H{"path": ""}},
		{"list_folders", gin.H{"unknown": "ignored?"}},
	} {
		test.args["project_id"] = f.projectID
		assertRPCError(t, callMCPToolRPC(t, f.router, token, test.tool, test.args), -32602, test.tool)
	}
}
