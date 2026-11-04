package mcp

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"vdoc/domain/documentrepo"
	app "vdoc/services/vdoc"
)

type contentToolDocument struct {
	Document mcpRepositoryDocument `json:"document"`
	Branch   mcpRepositoryBranch   `json:"branch"`
	Commit   mcpRepositoryCommit   `json:"commit"`
}

func createContentToolDocument(t *testing.T, f mcpFixture, token, path string, kind int, content string) contentToolDocument {
	t.Helper()
	var created contentToolDocument
	repositoryMCPCall(t, f, token, "create_document", gin.H{"project_id": f.projectID, "name": path, "relative_path": path, "document_type": kind, "content": content, "idempotency_key": "create-content-tool-" + path}, &created)
	return created
}

func contentToolRef(f mcpFixture, d contentToolDocument) gin.H {
	return gin.H{"project_id": f.projectID, "document_id": d.Document.ID, "commit_id": d.Commit.ID}
}

func TestMCPContentToolsPreserveUnparsedFullSourceAndSearchExactCommits(t *testing.T) {
	f, token, _ := repositoryMCPSetup(t)
	source := "\ufeffarbitrary: 未知格式\r\nkeyword = 原始数据\r\n# literal text\r\n"
	d := createContentToolDocument(t, f, token, "spec.unknown.txt", 3, source)
	other := createContentToolDocument(t, f, token, "second.txt", 3, "keyword in another document\n")
	createContentToolDocument(t, f, token, "private.yaml", 1, mcpTestOpenAPI("keywordPrivate"))
	readToken, err := app.DefaultStore().CreateMCPToken(f.writerID, "doc-search-only", []int{app.ScopeDocRead}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var full mcpRepositoryContent
	repositoryMCPCall(t, f, token, "get_document_at_commit", contentToolRef(f, d), &full)
	if full.Content != source {
		t.Fatal("full raw source changed")
	}
	args := contentToolRef(f, d)
	args["start_line"], args["end_line"] = 1, 2
	var section mcpDocumentSection
	repositoryMCPCall(t, f, readToken.Token, "get_document_section", args, &section)
	if section.Content != "\ufeffarbitrary: 未知格式\r\nkeyword = 原始数据\r\n" || section.NextLine != 3 || section.ContentKind != "raw" {
		t.Fatalf("raw section=%+v", section)
	}
	searchArgs := gin.H{"project_id": f.projectID, "query": "KEYWORD", "limit": 1}
	var first mcpDocumentSearch
	repositoryMCPCall(t, f, readToken.Token, "search_documents", searchArgs, &first)
	if len(first.Matches) != 1 || first.NextCursor == "" || !first.DocumentsFiltered || !strings.Contains(first.Matches[0].Snippet, "keyword") {
		t.Fatalf("first search=%+v", first)
	}
	searchArgs["cursor"] = first.NextCursor
	var second mcpDocumentSearch
	repositoryMCPCall(t, f, readToken.Token, "search_documents", searchArgs, &second)
	if len(second.Matches) != 1 || second.Matches[0].Document.DocumentID == first.Matches[0].Document.DocumentID {
		t.Fatalf("second search=%+v", second)
	}
	for _, match := range []mcpDocumentSearchMatch{first.Matches[0], second.Matches[0]} {
		if match.CommitID != d.Commit.ID && match.CommitID != other.Commit.ID || match.Document.DocumentType != 3 {
			t.Fatalf("unscoped/unpinned result=%+v", match)
		}
	}
	searchArgs["query"] = "different query"
	assertRPCError(t, callMCPToolRPC(t, f.router, readToken.Token, "search_documents", searchArgs), -32602, "cursor criteria")
	searchArgs = gin.H{"project_id": f.projectID, "query": "未知格式", "document_id": d.Document.ID, "commit_id": d.Commit.ID}
	var historical mcpDocumentSearch
	repositoryMCPCall(t, f, readToken.Token, "search_documents", searchArgs, &historical)
	if len(historical.Matches) != 1 || historical.Matches[0].Line != 1 {
		t.Fatalf("Unicode search=%+v", historical)
	}
}

func TestMCPStructuredViewsAreOptionalAndPreserveExactNumbers(t *testing.T) {
	f, token, _ := repositoryMCPSetup(t)
	md := createContentToolDocument(t, f, token, "sections.md", 2, "# Intro\r\ntext\r\n## Child\r\nnested\r\n# Next\r\n")
	args := contentToolRef(f, md)
	args["kind"] = "outline"
	var outline mcpDocumentSection
	repositoryMCPCall(t, f, token, "get_document_section", args, &outline)
	if len(outline.Headings) != 3 {
		t.Fatalf("outline=%+v", outline)
	}
	args["kind"], args["heading"] = "markdown_heading", "Intro"
	var section mcpDocumentSection
	repositoryMCPCall(t, f, token, "get_document_section", args, &section)
	if section.Content != "# Intro\r\ntext\r\n## Child\r\nnested\r\n" {
		t.Fatalf("section=%+v", section)
	}
	source := `{"openapi":"3.1.0","info":{"title":"Numbers","version":"1"},"paths":{"/n":{"get":{"responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"const":9007199254740993}}}}}}}}}`
	api := createContentToolDocument(t, f, token, "numbers.json", 1, source)
	args = contentToolRef(f, api)
	args["kind"] = "outline"
	repositoryMCPCall(t, f, token, "get_document_section", args, &outline)
	if len(outline.Operations) != 1 || outline.Operations[0].Path != "/n" {
		t.Fatalf("operations=%+v", outline)
	}
	args["kind"], args["method"], args["path"] = "openapi_operation", "get", "/n"
	result := repositoryMCPCall(t, f, token, "get_document_section", args, &section)
	if section.ContentKind != "derived_json" || !bytes.Contains(result, []byte("9007199254740993")) {
		t.Fatalf("operation=%s", result)
	}
	broken := createContentToolDocument(t, f, token, "unparseable.yaml", 1, "# unparseable\r\n{ broken source ")
	args = contentToolRef(f, broken)
	var raw mcpRepositoryContent
	repositoryMCPCall(t, f, token, "get_document_at_commit", args, &raw)
	repositoryMCPCall(t, f, token, "get_document_section", args, &section)
	if section.Content != raw.Content || section.ContentKind != "raw" {
		t.Fatal("parser failure interfered with raw access")
	}
	args["kind"] = "outline"
	response := callMCPToolRPC(t, f.router, token, "get_document_section", args)
	if response.Error == nil || !bytes.Contains(response.Error.Data, []byte("FAILED_PRECONDITION")) {
		t.Fatalf("structured parse failure=%+v", response)
	}
}

func TestMCPPatchesAreAtomicPinnedAndIdempotent(t *testing.T) {
	f, token, _ := repositoryMCPSetup(t)
	d := createContentToolDocument(t, f, token, "original.weird.txt", 3, "\ufeffone two\r\nunchanged\r\n")
	args := gin.H{"project_id": f.projectID, "document_id": d.Document.ID, "branch_id": d.Branch.ID, "expected_head_commit_id": d.Commit.ID, "expected_generation": d.Branch.Generation, "idempotency_key": "patch-first-request", "edits": []gin.H{{"old_text": "one", "new_text": "two"}, {"old_text": "two", "new_text": "three"}}}
	var patched mcpRepositoryCommit
	repositoryMCPCall(t, f, token, "apply_document_patch", args, &patched)
	if patched.VersionName != "main.2" {
		t.Fatalf("patch did not generate a normal version: %+v", patched)
	}
	var full mcpRepositoryContent
	ref := contentToolRef(f, d)
	ref["commit_id"] = patched.ID
	repositoryMCPCall(t, f, token, "get_document_at_commit", ref, &full)
	if full.Content != "\ufefftwo three\r\nunchanged\r\n" || full.Document.RelativePath != d.Document.RelativePath {
		t.Fatalf("patch changed unrelated bytes/metadata=%+v", full)
	}
	second := gin.H{"project_id": f.projectID, "document_id": d.Document.ID, "branch_id": d.Branch.ID, "expected_head_commit_id": patched.ID, "expected_generation": d.Branch.Generation + 1, "idempotency_key": "patch-second-request", "edits": []gin.H{{"old_text": "unchanged", "new_text": "still exact"}}}
	var next mcpRepositoryCommit
	repositoryMCPCall(t, f, token, "apply_document_patch", second, &next)
	var retry mcpRepositoryCommit
	repositoryMCPCall(t, f, token, "apply_document_patch", args, &retry)
	if retry.ID != patched.ID {
		t.Fatal("retry after later save created another commit")
	}
	args["edits"] = []gin.H{{"old_text": "one two", "new_text": "two three"}}
	assertRPCError(t, callMCPToolRPC(t, f.router, token, "apply_document_patch", args), -32602, "changed payload with same resulting bytes/key")
	args["idempotency_key"] = "patch-stale-head-key"
	stale := callMCPToolRPC(t, f.router, token, "apply_document_patch", args)
	if stale.Error == nil || !bytes.Contains(stale.Error.Data, []byte("REF_CONFLICT")) {
		t.Fatalf("stale patch=%+v", stale)
	}
	args["expected_head_commit_id"], args["expected_generation"] = next.ID, d.Branch.Generation+2
	args["edits"] = []gin.H{{"old_text": "absent", "new_text": "unsafe"}}
	conflict := callMCPToolRPC(t, f.router, token, "apply_document_patch", args)
	if conflict.Error == nil || !bytes.Contains(conflict.Error.Data, []byte("PATCH_CONFLICT")) {
		t.Fatalf("missing anchor=%+v", conflict)
	}
	var branch mcpRepositoryRef
	repositoryMCPCall(t, f, token, "resolve_ref", gin.H{"project_id": f.projectID, "document_id": d.Document.ID, "branch_id": d.Branch.ID}, &branch)
	if branch.Commit.ID != next.ID || branch.Branch.Generation != d.Branch.Generation+2 {
		t.Fatalf("failed patch mutated branch=%+v", branch)
	}
	readOnly, _ := app.DefaultStore().CreateMCPToken(f.writerID, "patch-read-only", []int{app.ScopeDocRead}, nil)
	assertRPCError(t, callMCPToolRPC(t, f.router, readOnly.Token, "apply_document_patch", args), -32003, "patch requires draft scope")
}

func TestMCPSearchContinuesPastEmptyBudgetPages(t *testing.T) {
	f, token, _ := repositoryMCPSetup(t)
	for i := 0; i < 101; i++ {
		path := fmt.Sprintf("budget-%03d.txt", i)
		if _, err := app.DefaultStore().RepositoryCreateDocument(f.writerID, f.projectID, documentrepo.DocumentInput{Name: path, RelativePath: path, DocumentType: 3, Content: "ordinary text", IdempotencyKey: "budget-fixture-" + path}); err != nil {
			t.Fatal(err)
		}
	}
	args := gin.H{"project_id": f.projectID, "query": "absent keyword"}
	var page mcpDocumentSearch
	repositoryMCPCall(t, f, token, "search_documents", args, &page)
	if len(page.Matches) != 0 || page.NextCursor == "" {
		t.Fatalf("empty page lost continuation=%+v", page)
	}
	args["cursor"] = page.NextCursor
	page = mcpDocumentSearch{}
	repositoryMCPCall(t, f, token, "search_documents", args, &page)
	if len(page.Matches) != 0 || page.NextCursor != "" {
		t.Fatalf("last search page=%+v", page)
	}
}
