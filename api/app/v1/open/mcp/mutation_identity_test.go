package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vdoc/api/middleware"
	app "vdoc/appstore"

	"github.com/gin-gonic/gin"
)

func TestMCPDraftMutationsRetainAuthenticatedIdentity(t *testing.T) {
	for _, protocol := range []string{"jsonrpc", "legacy"} {
		for _, documentType := range []string{"openapi", "markdown"} {
			t.Run(protocol+"/"+documentType, func(t *testing.T) {
				fixture := newMCPFixture(t)
				document := mcpMutationDocumentForTest(fixture, documentType)
				token, err := app.DefaultStore().CreateMCPToken(fixture.writerID, "mutation-identity", []int{document.scope}, nil)
				if err != nil {
					t.Fatal(err)
				}
				invoke := func(tool string, args gin.H) mcpDraftDTO {
					t.Helper()
					payload := gin.H{"tool": tool, "arguments": args}
					if protocol == "jsonrpc" {
						payload = gin.H{"jsonrpc": "2.0", "id": tool, "method": "tools/call", "params": gin.H{"name": tool, "arguments": args}}
					}
					body := callMCPRPCBodyWithTrace(t, fixture.router, token.Token, "mutation-identity", payload)
					var result json.RawMessage
					if protocol == "jsonrpc" {
						var response mcpRPCResponse
						if err := json.Unmarshal(body, &response); err != nil {
							t.Fatal(err)
						}
						result = assertRPCResult(t, response, tool)
					} else {
						var response mcpTestEnvelope
						if err := json.Unmarshal(body, &response); err != nil || response.Code != 200 || response.Status != "OK" {
							t.Fatalf("%s response = %s err=%v", tool, body, err)
						}
						var detail struct {
							Result json.RawMessage `json:"result"`
						}
						if err := json.Unmarshal(response.Detail, &detail); err != nil {
							t.Fatal(err)
						}
						result = detail.Result
					}
					var draft mcpDraftDTO
					if err := json.Unmarshal(result, &draft); err != nil || draft.ID == "" || draft.Revision == "" {
						t.Fatalf("%s draft = %s err=%v", tool, result, err)
					}
					return draft
				}
				created := invoke(document.createTool, gin.H{"project_id": fixture.projectID, "document_id": document.id, "branch_id": document.branchID, "version_name": "identity", document.contentField: document.original})
				updated := invoke(document.updateTool, gin.H{"project_id": fixture.projectID, "document_id": document.id, "draft_id": created.ID, "expected_revision": created.Revision, document.contentField: document.newer})
				submitted := invoke(document.submitTool, gin.H{"project_id": fixture.projectID, "document_id": document.id, "draft_id": updated.ID})
				if submitted.Status != app.DraftStatusSubmitted {
					t.Fatalf("draft status = %d, want submitted", submitted.Status)
				}
				for _, action := range []string{"create", "update", "submit"} {
					audit := findMCPAudit(t, document.auditPrefix+action, token.ID)
					if audit.ActorType != app.AuditActorMCPToken || audit.ActorTokenID != token.ID || audit.ActorUserID != fixture.writerID || audit.RequestID != "mutation-identity" || audit.ResourceID != created.ID || audit.Metadata["result"] != "success" {
						t.Fatalf("%s business audit lost authenticated identity: %+v", action, audit)
					}
				}
			})
		}
	}
}

func TestMCPDraftMutationsRejectTokenInvalidatedAfterAuthentication(t *testing.T) {
	for _, documentType := range []string{"openapi", "markdown"} {
		for _, invalidation := range []string{"revoked", "expired"} {
			t.Run(documentType+"/"+invalidation, func(t *testing.T) {
				fixture := newMCPFixture(t)
				store := app.DefaultStore()
				document := mcpMutationDocumentForTest(fixture, documentType)
				input := app.DraftInput{BranchID: document.branchID, VersionName: "existing", SchemaContent: document.original}
				var original *app.ContractDraft
				var err error
				if documentType == "markdown" {
					original, err = store.CreateMarkdownDraft(fixture.writerID, fixture.projectID, document.id, input)
				} else {
					original, err = store.CreateDocumentDraft(fixture.writerID, fixture.projectID, document.id, input)
				}
				if err != nil {
					t.Fatal(err)
				}
				var expiresAt *time.Time
				if invalidation == "expired" {
					expiry := time.Now().Add(time.Second)
					expiresAt = &expiry
				}
				issued, err := store.CreateMCPToken(fixture.writerID, "mutation-invalidation", []int{document.scope}, expiresAt)
				if err != nil {
					t.Fatal(err)
				}
				requestContext, _ := gin.CreateTestContext(httptest.NewRecorder())
				requestContext.Request = httptest.NewRequest(http.MethodPost, "/api/v1/open/mcp", nil)
				requestContext.Request.Header.Set(middleware.AuthorizationHeader, issued.Token)
				authenticatedToken, authenticatedUser, err := authenticateMCPToken(requestContext)
				if err != nil {
					t.Fatalf("initial authentication failed: %v", err)
				}
				if invalidation == "revoked" {
					if _, err := store.RevokeMCPToken(fixture.writerID, issued.ID); err != nil {
						t.Fatal(err)
					}
				} else {
					time.Sleep(time.Until(*expiresAt) + time.Millisecond)
				}
				previousAudits := map[string]bool{}
				for _, audit := range store.AuditLogsForTest() {
					previousAudits[audit.ID] = true
				}
				for _, operation := range []struct {
					name string
					tool string
					args gin.H
				}{
					{"create", document.createTool, gin.H{"project_id": fixture.projectID, "document_id": document.id, "branch_id": document.branchID, "version_name": "must-not-exist", document.contentField: document.newer}},
					{"update", document.updateTool, gin.H{"project_id": fixture.projectID, "document_id": document.id, "draft_id": original.ID, "expected_revision": original.Revision(), document.contentField: document.newer}},
					{"submit", document.submitTool, gin.H{"project_id": fixture.projectID, "document_id": document.id, "draft_id": original.ID}},
				} {
					t.Run(operation.name, func(t *testing.T) {
						result, err := executeAuthenticatedTool(requestContext, authenticatedToken, authenticatedUser, operation.tool, mustJSON(t, operation.args))
						if !app.Is(err, app.ErrUnauthenticated) || result != nil {
							t.Fatalf("%s after %s = %+v err=%v, want unauthenticated", operation.name, invalidation, result, err)
						}
						drafts, err := store.ListDrafts(fixture.writerID, fixture.projectID, document.id)
						if err != nil || len(drafts) != 1 || drafts[0].ID != original.ID {
							t.Fatalf("denied %s changed drafts: %+v err=%v", operation.name, drafts, err)
						}
						current, content, err := store.ReadDraftContent(fixture.writerID, fixture.projectID, document.id, original.ID, "raw")
						if err != nil || current.Revision() != original.Revision() || current.Status != app.DraftStatusDraft || content.Content != document.original {
							t.Fatalf("denied %s changed existing draft: %+v content=%+v err=%v", operation.name, current, content, err)
						}
						for _, audit := range store.AuditLogsForTest() {
							if !previousAudits[audit.ID] && (strings.HasPrefix(audit.Action, document.auditPrefix) || audit.Action == "mcp.tool_call") && audit.Metadata["result"] == "success" {
								t.Fatalf("denied %s left a success audit: %+v", operation.name, audit)
							}
							if audit.ActorTokenID == issued.ID && audit.Action != "mcp_token.authenticate" && audit.Action != "mcp_token.revoke" && audit.Metadata["result"] == "success" {
								t.Fatalf("denied %s left a token success audit: %+v", operation.name, audit)
							}
						}
					})
				}
			})
		}
	}
}

func TestMCPVersionCompareRetainsReadOnlyIdentity(t *testing.T) {
	for _, documentType := range []string{"openapi", "markdown"} {
		t.Run(documentType, func(t *testing.T) {
			fixture := newMCPFixture(t)
			store := app.DefaultStore()
			document := mcpMutationDocumentForTest(fixture, documentType)
			var from, to *app.ContractVersion
			tool, action, scope := "compare_api_versions", "api_version_diff.compare", app.ScopeAPIRead
			if documentType == "markdown" {
				from = publishMCPFixtureMarkdownVersion(t, fixture, "1.0.0", document.original)
				to = publishMCPFixtureMarkdownVersion(t, fixture, "1.1.0", document.newer)
				tool, action, scope = "compare_doc_versions", "markdown_version_diff.compare", app.ScopeDocRead
			} else {
				from = publishMCPFixtureVersion(t, fixture, "1.0.0", document.original)
				to = publishMCPFixtureVersion(t, fixture, "1.1.0", document.newer)
			}
			token, err := store.CreateMCPToken(fixture.readerID, "compare-identity", []int{scope}, nil)
			if err != nil {
				t.Fatal(err)
			}
			args := gin.H{"project_id": fixture.projectID, "document_id": document.id, "from_version_id": to.ID, "to_version_id": from.ID}
			assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, tool, args), tool)
			audit := findMCPAudit(t, action, token.ID)
			if audit.ActorType != app.AuditActorMCPToken || audit.ActorUserID != fixture.readerID || audit.ActorTokenID != token.ID || audit.Metadata["from_version_id"] != to.ID || audit.Metadata["to_version_id"] != from.ID {
				t.Fatalf("compare audit lost authenticated identity: %+v", audit)
			}
		})
	}
}

type mcpMutationDocument struct {
	id, branchID                       string
	createTool, updateTool, submitTool string
	contentField, original, newer      string
	auditPrefix                        string
	scope                              int
}

func mcpMutationDocumentForTest(fixture mcpFixture, documentType string) mcpMutationDocument {
	if documentType == "markdown" {
		return mcpMutationDocument{id: fixture.markdownDocumentID, branchID: fixture.markdownBranchID, createTool: "create_doc_draft", updateTool: "update_doc_draft", submitTool: "submit_doc_draft", contentField: "markdown_content", original: "# Original", newer: "# Newer", auditPrefix: "markdown_draft.", scope: app.ScopeDocDraft}
	}
	return mcpMutationDocument{id: fixture.documentID, branchID: fixture.branchID, createTool: "create_api_version_draft", updateTool: "update_api_version_draft", submitTool: "submit_api_version_draft", contentField: "schema_content", original: mcpTestOpenAPI("identityOriginal"), newer: mcpTestOpenAPI("identityNewer"), auditPrefix: "contract_draft.", scope: app.ScopeAPIDraft}
}
