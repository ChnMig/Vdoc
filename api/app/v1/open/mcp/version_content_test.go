package mcp

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/gin-gonic/gin"
	app "vdoc/services/vdoc"
)

func TestMCPPublishedContentRequiresBranchAndReadsHistoricalVersions(t *testing.T) {
	for _, markdown := range []bool{false, true} {
		name := "OpenAPI"
		if markdown {
			name = "Markdown"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newMCPFixture(t)
			store := app.DefaultStore()
			documentID, scope := fixture.documentID, app.ScopeAPIRead
			latestTool, versionTool := "get_latest_schema", "get_schema_version"
			publish, content := publishMCPFixtureVersion, mcpTestOpenAPI
			if markdown {
				documentID, scope = fixture.markdownDocumentID, app.ScopeDocRead
				latestTool, versionTool = "get_latest_doc", "get_doc_version"
				publish, content = publishMCPFixtureMarkdownVersion, mcpTestMarkdown
			}
			branches, err := store.ListBranches(fixture.superID, fixture.projectID, documentID)
			if err != nil {
				t.Fatal(err)
			}
			prod := fixture
			var prodBranch string
			for _, branch := range branches {
				if branch.Name == "prod" {
					prodBranch = branch.ID
				}
			}
			if prodBranch == "" {
				t.Fatal("missing prod branch")
			}
			if markdown {
				prod.markdownBranchID = prodBranch
			} else {
				prod.branchID = prodBranch
			}
			old := publish(t, prod, "prod-1", content("historicalContent"))
			current := publish(t, prod, "prod-2", content("currentContent"))
			publish(t, fixture, "dev-3", content("developmentContent"))
			token, err := store.CreateMCPToken(fixture.readerID, "version-reader", []int{scope}, nil)
			if err != nil {
				t.Fatal(err)
			}
			args := gin.H{"project_id": fixture.projectID, "document_id": documentID}
			assertRPCError(t, callMCPToolRPC(t, fixture.router, token.Token, latestTool, args), -32602, "missing branch")
			args["branch_id"] = prodBranch
			body := assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, latestTool, args), "explicit prod")
			assertVersionContent(t, body, current.ID, "currentContent")
			args["version_id"] = old.ID
			assertRPCError(t, callMCPToolRPC(t, fixture.router, token.Token, latestTool, args), -32602, "unsupported historical selector")
			delete(args, "branch_id")
			body = assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, versionTool, args), "historical version")
			assertVersionContent(t, body, old.ID, "historicalContent")
			if bytes.Contains(body, []byte("developmentContent")) || bytes.Contains(body, []byte("currentContent")) {
				t.Fatal("historical read returned another version")
			}
			var audit *app.AuditLog
			for _, entry := range store.AuditLogsForTest() {
				if entry.Metadata["tool_name"] == versionTool && entry.Metadata["result"] == "success" {
					audit = entry
				}
			}
			if audit == nil || audit.Metadata["version_id"] != old.ID || audit.Metadata["branch_id"] != prodBranch || audit.Metadata["evidence_kind"] != "published_content_read" {
				t.Fatal("historical read lacks exact audit provenance")
			}
			args["version_id"] = "missing"
			assertRPCError(t, callMCPToolRPC(t, fixture.router, token.Token, versionTool, args), -32004, "unknown version")
			args["version_id"] = old.ID
			if markdown {
				args["document_id"] = fixture.documentID
			} else {
				args["document_id"] = fixture.markdownDocumentID
			}
			assertRPCError(t, callMCPToolRPC(t, fixture.router, token.Token, versionTool, args), -32004, "wrong document type")
			args["document_id"] = documentID
			otherDocument, err := store.CreateDocument(fixture.superID, fixture.projectID, "other", map[bool]int{true: app.DocumentTypeMarkdown, false: app.DocumentTypeOpenAPI}[markdown], "other.md", "")
			if err != nil {
				t.Fatal(err)
			}
			args["document_id"] = otherDocument.ID
			assertRPCError(t, callMCPToolRPC(t, fixture.router, token.Token, versionTool, args), -32004, "version belongs to another document")
			args["document_id"] = documentID
			wrongScope := app.ScopeDocRead
			if markdown {
				wrongScope = app.ScopeAPIRead
			}
			limited, err := store.CreateMCPToken(fixture.readerID, "wrong-scope", []int{wrongScope}, nil)
			if err != nil {
				t.Fatal(err)
			}
			assertRPCError(t, callMCPToolRPC(t, fixture.router, limited.Token, versionTool, args), -32003, "wrong scope")
		})
	}
}

func assertVersionContent(t *testing.T, body []byte, versionID, text string) {
	t.Helper()
	var result struct {
		Version mcpVersionDTO `json:"version"`
		Content mcpContentDTO `json:"content"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	if result.Version.ID != versionID || result.Content.VersionID != versionID || result.Content.Hash == "" || !bytes.Contains([]byte(result.Content.Content), []byte(text)) {
		t.Fatal("content and version provenance do not match the requested version")
	}
}

func TestMCPAPIDraftReadsContentAndRevisionFromOneSnapshot(t *testing.T) {
	fixture := newMCPFixture(t)
	store := app.DefaultStore()
	draft, err := store.CreateDocumentDraft(fixture.writerID, fixture.projectID, fixture.documentID, app.DraftInput{BranchID: fixture.branchID, VersionName: "unpublished", SchemaContent: mcpTestOpenAPI("unpublishedOperation")})
	if err != nil {
		t.Fatal(err)
	}
	token, err := store.CreateMCPToken(fixture.writerID, "draft-editor", []int{app.ScopeAPIRead, app.ScopeAPIDraft}, nil)
	if err != nil {
		t.Fatal(err)
	}
	body := assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, "get_api_version_draft", gin.H{"project_id": fixture.projectID, "document_id": fixture.documentID, "draft_id": draft.ID}), "read unpublished source")
	var result mcpDraftDTO
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	if result.ID != draft.ID || result.Revision == "" || result.Content == nil || result.Content.DraftID != draft.ID || result.Content.Hash != result.RawContentHash || result.Content.Content != mcpTestOpenAPI("unpublishedOperation") {
		t.Fatal("draft metadata, revision and original content must be returned together")
	}
	updated := assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, "update_api_version_draft", gin.H{"project_id": fixture.projectID, "document_id": fixture.documentID, "draft_id": draft.ID, "expected_revision": result.Revision, "schema_content": mcpTestOpenAPI("editedOperation")}), "edit using read revision")
	if !bytes.Contains(updated, []byte(draft.ID)) {
		t.Fatal("draft update did not preserve identity")
	}
}

func TestMCPUnknownArgumentsAreRejectedWithoutLeakingValues(t *testing.T) {
	fixture := newMCPFixture(t)
	token, err := app.DefaultStore().CreateMCPToken(fixture.readerID, "strict-args", []int{app.ScopeAPIRead}, nil)
	if err != nil {
		t.Fatal(err)
	}
	response := callMCPToolRPC(t, fixture.router, token.Token, "list_projects", gin.H{"schema_content": "private-review-payload"})
	assertRPCError(t, response, -32602, "unknown argument")
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("private-review-payload")) || mcpAuditContainsValue(app.DefaultStore().AuditLogsForTest(), "private-review-payload") {
		t.Fatal("unknown argument leaked into response or audit")
	}
}
