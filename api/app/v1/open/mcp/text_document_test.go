package mcp

import (
	"encoding/json"
	"testing"

	"github.com/gin-gonic/gin"
	app "vdoc/services/vdoc"
)

func TestMCPDocContentAliasPreservesExplicitEmptyContent(t *testing.T) {
	value := func(s string) *string { return &s }
	for _, tc := range []struct {
		name            string
		content, legacy *string
		want            string
		wantError       bool
	}{
		{name: "missing", wantError: true},
		{name: "canonical", content: value("plain\r\ntext"), want: "plain\r\ntext"},
		{name: "legacy", legacy: value("# Markdown"), want: "# Markdown"},
		{name: "matching aliases", content: value("same"), legacy: value("same"), want: "same"},
		{name: "conflicting aliases", content: value("a"), legacy: value("b"), wantError: true},
		{name: "empty canonical", content: value("")},
		{name: "empty legacy", legacy: value("")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := docDraftContent(tc.content, tc.legacy)
			if (err != nil) != tc.wantError || got != tc.want {
				t.Fatalf("content = %q, err = %v; want %q, error=%v", got, err, tc.want, tc.wantError)
			}
		})
	}
}

func TestMCPTXTUsesDocScopesAndLiteralContentLifecycle(t *testing.T) {
	fixture := newMCPFixture(t)
	store := app.DefaultStore()
	document, err := store.CreateDocument(fixture.superID, fixture.projectID, "Plain notes", app.DocumentTypeText, "notes/literal.txt", "")
	if err != nil {
		t.Fatal(err)
	}
	branches, err := store.ListBranches(fixture.superID, fixture.projectID, document.ID)
	if err != nil {
		t.Fatal(err)
	}
	var branchID string
	for _, branch := range branches {
		if branch.Name == "dev" {
			branchID = branch.ID
		}
	}
	if branchID == "" {
		t.Fatal("missing TXT dev branch")
	}
	newToken := func(scopes ...int) string {
		t.Helper()
		token, err := store.CreateMCPToken(fixture.writerID, "txt-scopes", scopes, nil)
		if err != nil {
			t.Fatal(err)
		}
		return token.Token
	}
	docToken := newToken(app.ScopeDocRead, app.ScopeDocDraft)
	apiToken := newToken(app.ScopeAPIRead, app.ScopeAPIDraft)
	bothToken := newToken(app.ScopeDocRead, app.ScopeAPIRead, app.ScopeAPIDraft)
	call := func(token, tool string, args gin.H, target any) json.RawMessage {
		t.Helper()
		body := assertRPCResult(t, callMCPToolRPC(t, fixture.router, token, tool, args), tool)
		if target != nil {
			if err := json.Unmarshal(body, target); err != nil {
				t.Fatalf("decode %s: %v", tool, err)
			}
		}
		return body
	}
	for _, tc := range []struct {
		token string
		want  bool
	}{{docToken, true}, {apiToken, false}} {
		var documents []mcpDocumentDTO
		call(tc.token, "list_documents", gin.H{"project_id": fixture.projectID}, &documents)
		found := false
		for _, candidate := range documents {
			if candidate.ID == document.ID {
				found = candidate.DocumentType == app.DocumentTypeText
			}
		}
		if found != tc.want {
			t.Fatalf("TXT discovery with doc scope=%v: documents=%+v", tc.want, documents)
		}
	}
	identity := func() gin.H { return gin.H{"project_id": fixture.projectID, "document_id": document.ID} }
	assertRPCError(t, callMCPToolRPC(t, fixture.router, apiToken, "list_document_branches", identity()), -32004, "API scope cannot see TXT branches")
	assertRPCError(t, callMCPToolRPC(t, fixture.router, bothToken, "list_api_versions", identity()), -32004, "TXT cannot use API version tools")
	args := identity()
	args["branch_id"], args["version_name"] = branchID, "1.0.0"
	assertRPCError(t, callMCPToolRPC(t, fixture.router, apiToken, "create_doc_draft", args), -32003, "API draft scope cannot write TXT drafts")
	apiArgs := identity()
	apiArgs["branch_id"], apiArgs["version_name"], apiArgs["schema_content"] = branchID, "api-scope-bypass", "literal TXT"
	assertRPCError(t, callMCPToolRPC(t, fixture.router, apiToken, "create_api_version_draft", apiArgs), -32004, "API draft tool cannot write TXT drafts")
	assertRPCError(t, callMCPToolRPC(t, fixture.router, docToken, "create_doc_draft", args), -32602, "content must be supplied")
	args["content"], args["markdown_content"] = "canonical secret", "legacy secret"
	assertRPCError(t, callMCPToolRPC(t, fixture.router, docToken, "create_doc_draft", args), -32602, "ambiguous aliases")
	delete(args, "markdown_content")
	args["content"] = ""
	var draft mcpDraftDTO
	call(docToken, "create_doc_draft", args, &draft)
	if draft.ID == "" || draft.DocumentFormat != app.DocumentFormatText || draft.StableContentHash == "" || draft.StableContentHash != draft.RawContentHash || draft.NormalizedContentHash != "" {
		t.Fatalf("TXT draft DTO = %+v", draft)
	}
	literal := "# literal heading\r\n<script>literal</script>\r\n中文  \r\n"
	args = identity()
	args["draft_id"], args["expected_revision"], args["content"] = draft.ID, draft.Revision, literal
	call(docToken, "update_doc_draft", args, &draft)
	args = identity()
	args["draft_id"] = draft.ID
	read := call(docToken, "get_doc_draft", args, &draft)
	if draft.Content == nil || draft.Content.Content != literal || draft.Content.Hash != draft.StableContentHash || draft.Content.ContentKind != "stable" {
		t.Fatalf("TXT draft content changed: %+v", draft)
	}
	assertMCPResultOmits(t, "TXT draft", read, `"normalized_content_hash"`, "object_key")
	assertRPCError(t, callMCPToolRPC(t, fixture.router, apiToken, "get_doc_draft", args), -32003, "API scope cannot read TXT content")
	call(docToken, "submit_doc_draft", args, nil)
	published, err := store.ReviewMarkdownDraft(fixture.superID, fixture.projectID, document.ID, draft.ID, "approve", reviewInputForTest(t, store, fixture.superID, fixture.projectID, document.ID, draft.ID))
	if err != nil {
		t.Fatal(err)
	}
	version := published.(*app.ContractVersion)
	args = identity()
	args["version_id"] = version.ID
	var snapshot struct {
		Version mcpVersionDTO `json:"version"`
		Content mcpContentDTO `json:"content"`
	}
	call(docToken, "get_doc_version", args, &snapshot)
	if snapshot.Content.Content != literal || snapshot.Version.StableContentHash != snapshot.Content.Hash || snapshot.Version.DocumentFormat != app.DocumentFormatText || snapshot.Version.NormalizedContentHash != "" {
		t.Fatalf("TXT published snapshot = %+v", snapshot)
	}
	textFixture := fixture
	textFixture.markdownDocumentID, textFixture.markdownBranchID = document.ID, branchID
	next := publishMCPFixtureMarkdownVersion(t, textFixture, "1.1.0", literal+"appended\r\n")
	args = identity()
	args["from_version_id"], args["to_version_id"] = version.ID, next.ID
	diff := call(docToken, "compare_doc_versions", args, nil)
	assertPublicMCPResult(t, "TXT line diff", diff, `"items"`, "appended")
	args = identity()
	args["branch_id"] = branchID
	call(docToken, "get_latest_doc", args, &snapshot)
	if snapshot.Version.ID != next.ID || snapshot.Content.Content != literal+"appended\r\n" {
		t.Fatalf("latest TXT content = %+v", snapshot)
	}
	// 空正文必须作为有效 TXT 内容写入，不能被当作缺失参数或继续保留旧正文。
	args = identity()
	args["branch_id"], args["version_name"], args["markdown_content"] = branchID, "2.0.0", "legacy body"
	call(docToken, "create_doc_draft", args, &draft)
	args = identity()
	args["draft_id"], args["expected_revision"], args["content"] = draft.ID, draft.Revision, ""
	call(docToken, "update_doc_draft", args, &draft)
	args = identity()
	args["draft_id"] = draft.ID
	call(docToken, "get_doc_draft", args, &draft)
	if draft.Content == nil || draft.Content.Content != "" || draft.Content.Hash != draft.StableContentHash {
		t.Fatalf("explicit empty TXT update = %+v", draft)
	}
}
