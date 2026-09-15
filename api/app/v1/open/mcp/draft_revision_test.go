package mcp

import (
	"encoding/json"
	"testing"

	app "vdoc/appstore"

	"github.com/gin-gonic/gin"
)

func TestMCPDraftUpdatesRequireCurrentRevision(t *testing.T) {
	for _, markdown := range []bool{false, true} {
		name := "openapi"
		if markdown {
			name = "markdown"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newMCPFixture(t)
			token, err := app.DefaultStore().CreateMCPToken(fixture.writerID, "revision-test", []int{app.ScopeAPIRead, app.ScopeAPIDraft, app.ScopeDocRead, app.ScopeDocDraft}, nil)
			if err != nil {
				t.Fatal(err)
			}
			documentID, branchID := fixture.documentID, fixture.branchID
			create, update, contentField := "create_api_version_draft", "update_api_version_draft", "schema_content"
			original, newer := mcpTestOpenAPI("revisionOriginal"), mcpTestOpenAPI("revisionNewer")
			if markdown {
				documentID, branchID = fixture.markdownDocumentID, fixture.markdownBranchID
				create, update, contentField = "create_doc_draft", "update_doc_draft", "markdown_content"
				original, newer = "# Original", "# Newer"
			}
			created := assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, create, gin.H{"project_id": fixture.projectID, "document_id": documentID, "branch_id": branchID, "version_name": "revision-test", contentField: original}), create)
			var draft mcpDraftDTO
			if err := json.Unmarshal(created, &draft); err != nil || draft.Revision == "" {
				t.Fatalf("create revision missing: %s err=%v", created, err)
			}
			args := gin.H{"project_id": fixture.projectID, "document_id": documentID, "draft_id": draft.ID, contentField: newer}
			assertRPCError(t, callMCPToolRPC(t, fixture.router, token.Token, update, args), -32602, "missing revision")
			args["expected_revision"] = draft.Revision
			saved := assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, update, args), update)
			var latest mcpDraftDTO
			if err := json.Unmarshal(saved, &latest); err != nil || latest.Revision == "" || latest.Revision == draft.Revision {
				t.Fatalf("updated revision missing or unchanged: %s err=%v", saved, err)
			}
			args[contentField] = original
			assertRPCError(t, callMCPToolRPC(t, fixture.router, token.Token, update, args), -32010, "stale revision")
			current, content, err := app.DefaultStore().ReadDraftContent(fixture.writerID, fixture.projectID, documentID, draft.ID, "raw")
			if err != nil || content.Content != newer || current.Revision() != latest.Revision {
				t.Fatalf("stale MCP save changed the draft: %+v err=%v", current, err)
			}
		})
	}
}
