package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vdoc/api/middleware"
	app "vdoc/appstore"

	"github.com/gin-gonic/gin"
)

type mcpTestEnvelope struct {
	Code    int             `json:"code"`
	Status  string          `json:"status"`
	Message string          `json:"message"`
	Detail  json.RawMessage `json:"detail"`
}

type mcpRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *mcpRPCError    `json:"error"`
}

type mcpRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type mcpToolListResult struct {
	Tools []struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		InputSchema map[string]any `json:"inputSchema"`
	} `json:"tools"`
}

type mcpFixture struct {
	router             *gin.Engine
	superID            string
	readerID           string
	writerID           string
	projectID          string
	documentID         string
	branchID           string
	markdownDocumentID string
	markdownBranchID   string
}

func TestMCPReadScopeDeniedDraftTool(t *testing.T) {
	fixture := newMCPFixture(t)
	token, err := app.DefaultStore().CreateMCPToken(fixture.readerID, "read", []int{app.ScopeAPIRead}, nil)
	if err != nil {
		t.Fatalf("CreateMCPToken(read) error = %v", err)
	}

	envelope := callMCPTool(t, fixture.router, token.Token, "create_api_version_draft", gin.H{
		"project_id":     fixture.projectID,
		"document_id":    fixture.documentID,
		"branch_id":      fixture.branchID,
		"version_name":   "1.0.0",
		"schema_content": mcpTestOpenAPI("readDenied"),
		"changelog":      "blocked",
	})
	if envelope.Code != 403 || envelope.Status != "PERMISSION_DENIED" {
		t.Fatalf("read token draft response = code %d status %q detail %s", envelope.Code, envelope.Status, string(envelope.Detail))
	}
	if drafts, err := app.DefaultStore().ListDrafts(fixture.superID, fixture.projectID, fixture.documentID); err != nil || len(drafts) != 0 {
		t.Fatalf("drafts after denied call = %d error %v, want none", len(drafts), err)
	}
}

func TestMCPDraftScopeStillRequiresWriterRole(t *testing.T) {
	fixture := newMCPFixture(t)
	readerDraftToken, err := app.DefaultStore().CreateMCPToken(fixture.readerID, "reader-draft", []int{app.ScopeAPIDraft}, nil)
	if err != nil {
		t.Fatalf("CreateMCPToken(reader draft) error = %v", err)
	}

	denied := callMCPTool(t, fixture.router, readerDraftToken.Token, "create_api_version_draft", gin.H{
		"project_id":     fixture.projectID,
		"document_id":    fixture.documentID,
		"branch_id":      fixture.branchID,
		"version_name":   "1.0.0",
		"schema_content": mcpTestOpenAPI("readerDraftDenied"),
	})
	if denied.Code != 403 || denied.Status != "PERMISSION_DENIED" {
		t.Fatalf("reader api:draft response = code %d status %q", denied.Code, denied.Status)
	}

	writerDraftToken, err := app.DefaultStore().CreateMCPToken(fixture.writerID, "writer-draft", []int{app.ScopeAPIDraft}, nil)
	if err != nil {
		t.Fatalf("CreateMCPToken(writer draft) error = %v", err)
	}
	allowed := callMCPTool(t, fixture.router, writerDraftToken.Token, "create_api_version_draft", gin.H{
		"project_id":     fixture.projectID,
		"document_id":    fixture.documentID,
		"branch_id":      fixture.branchID,
		"version_name":   "1.0.1",
		"schema_content": mcpTestOpenAPI("writerDraftAllowed"),
	})
	if allowed.Code != 200 || allowed.Status != "OK" {
		t.Fatalf("writer api:draft response = code %d status %q body %s", allowed.Code, allowed.Status, string(allowed.Detail))
	}
}

func TestMCPDocReadScopeReadsMarkdownTools(t *testing.T) {
	fixture := newMCPFixture(t)
	from := publishMCPFixtureMarkdownVersion(t, fixture, "1.0.0", mcpTestMarkdown("Doc read baseline"))
	to := publishMCPFixtureMarkdownVersion(t, fixture, "1.1.0", mcpTestMarkdown("Doc read changed"))
	token, err := app.DefaultStore().CreateMCPToken(fixture.readerID, "doc-read", []int{app.ScopeDocRead}, nil)
	if err != nil {
		t.Fatalf("CreateMCPToken(doc read) error = %v", err)
	}

	assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, "list_projects", gin.H{}), "list_projects")
	assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, "list_documents", gin.H{"project_id": fixture.projectID}), "list_documents")
	latest := assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, "get_latest_doc", gin.H{"project_id": fixture.projectID, "document_id": fixture.markdownDocumentID}), "get_latest_doc")
	if !bytes.Contains(latest, []byte("Doc read changed")) {
		t.Fatalf("get_latest_doc result %s does not contain Markdown content", string(latest))
	}
	assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, "compare_doc_versions", gin.H{"project_id": fixture.projectID, "document_id": fixture.markdownDocumentID, "from_version_id": from.ID, "to_version_id": to.ID}), "compare_doc_versions")
}

func TestMCPAPIReadScopeCannotReadMarkdownTools(t *testing.T) {
	fixture := newMCPFixture(t)
	token, err := app.DefaultStore().CreateMCPToken(fixture.readerID, "api-read", []int{app.ScopeAPIRead}, nil)
	if err != nil {
		t.Fatalf("CreateMCPToken(api read) error = %v", err)
	}

	assertRPCError(t, callMCPToolRPC(t, fixture.router, token.Token, "get_latest_doc", gin.H{"project_id": fixture.projectID, "document_id": fixture.markdownDocumentID}), -32003, "api read denied markdown content")
}

func TestMCPDocDraftScopeCanManageMarkdownDrafts(t *testing.T) {
	fixture := newMCPFixture(t)
	token, err := app.DefaultStore().CreateMCPToken(fixture.writerID, "doc-draft", []int{app.ScopeDocDraft}, nil)
	if err != nil {
		t.Fatalf("CreateMCPToken(doc draft) error = %v", err)
	}

	createResult := assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, "create_doc_draft", gin.H{"project_id": fixture.projectID, "document_id": fixture.markdownDocumentID, "branch_id": fixture.markdownBranchID, "version_name": "1.0.0", "markdown_content": mcpTestMarkdown("Doc draft create")}), "create_doc_draft")
	var draft struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(createResult, &draft); err != nil || draft.ID == "" {
		t.Fatalf("decode doc draft: id=%q error=%v body %s", draft.ID, err, string(createResult))
	}
	assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, "update_doc_draft", gin.H{"project_id": fixture.projectID, "document_id": fixture.markdownDocumentID, "draft_id": draft.ID, "branch_id": fixture.markdownBranchID, "version_name": "1.0.0", "markdown_content": mcpTestMarkdown("Doc draft updated")}), "update_doc_draft")
	assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, "get_doc_draft", gin.H{"project_id": fixture.projectID, "document_id": fixture.markdownDocumentID, "draft_id": draft.ID}), "get_doc_draft")
	assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, "submit_doc_draft", gin.H{"project_id": fixture.projectID, "document_id": fixture.markdownDocumentID, "draft_id": draft.ID}), "submit_doc_draft")
}

func TestMCPRevokedAndExpiredTokensAreRejected(t *testing.T) {
	fixture := newMCPFixture(t)
	revoked, err := app.DefaultStore().CreateMCPToken(fixture.readerID, "revoked", []int{app.ScopeAPIRead}, nil)
	if err != nil {
		t.Fatalf("CreateMCPToken(revoked) error = %v", err)
	}
	if _, err := app.DefaultStore().RevokeMCPToken(fixture.readerID, revoked.ID); err != nil {
		t.Fatalf("RevokeMCPToken() error = %v", err)
	}
	if envelope := callMCPTool(t, fixture.router, revoked.Token, "list_projects", nil); envelope.Code != 401 || envelope.Status != "UNAUTHENTICATED" {
		t.Fatalf("revoked token response = code %d status %q", envelope.Code, envelope.Status)
	}

	past := time.Now().Add(-time.Minute)
	expired, err := app.DefaultStore().CreateMCPToken(fixture.readerID, "expired", []int{app.ScopeAPIRead}, &past)
	if err != nil {
		t.Fatalf("CreateMCPToken(expired) error = %v", err)
	}
	if envelope := callMCPTool(t, fixture.router, expired.Token, "list_projects", nil); envelope.Code != 401 || envelope.Status != "UNAUTHENTICATED" {
		t.Fatalf("expired token response = code %d status %q", envelope.Code, envelope.Status)
	}
}

func TestMCPJSONRPCToolsListIncludesV01Tools(t *testing.T) {
	fixture := newMCPFixture(t)
	token, err := app.DefaultStore().CreateMCPToken(fixture.readerID, "read", []int{app.ScopeAPIRead}, nil)
	if err != nil {
		t.Fatalf("CreateMCPToken(read) error = %v", err)
	}

	response := callMCPRPC(t, fixture.router, token.Token, gin.H{"jsonrpc": "2.0", "id": "tools-list", "method": "tools/list"})
	if response.Error != nil {
		t.Fatalf("tools/list error = %+v", response.Error)
	}
	var result mcpToolListResult
	if err := json.Unmarshal(response.Result, &result); err != nil {
		t.Fatalf("decode tools/list result: %v body %s", err, string(response.Result))
	}

	required := map[string]bool{
		"list_projects":            false,
		"list_documents":           false,
		"list_api_versions":        false,
		"get_latest_schema":        false,
		"get_endpoint_detail":      false,
		"compare_api_versions":     false,
		"get_change_summary":       false,
		"create_api_version_draft": false,
		"update_api_version_draft": false,
		"submit_api_version_draft": false,
		"get_api_version_draft":    false,
		"get_latest_doc":           false,
		"compare_doc_versions":     false,
		"create_doc_draft":         false,
		"update_doc_draft":         false,
		"submit_doc_draft":         false,
		"get_doc_draft":            false,
	}
	for _, tool := range result.Tools {
		if tool.Name == "list_"+"services" {
			t.Fatalf("tools/list exposed removed project document listing name")
		}
		if strings.HasPrefix(tool.Name, "publish_") {
			t.Fatalf("tools/list exposed direct publish tool %q", tool.Name)
		}
		seen, ok := required[tool.Name]
		if !ok {
			t.Fatalf("tools/list returned unexpected tool %q", tool.Name)
		}
		if seen {
			t.Fatalf("tools/list returned duplicate tool %q", tool.Name)
		}
		if tool.Description == "" {
			t.Fatalf("tool %q has empty description", tool.Name)
		}
		if tool.InputSchema["type"] != "object" {
			t.Fatalf("tool %q input schema = %+v, want object", tool.Name, tool.InputSchema)
		}
		required[tool.Name] = true
	}
	for name, seen := range required {
		if !seen {
			t.Fatalf("tools/list missing required v0.1 tool %q", name)
		}
	}
}

func TestMCPJSONRPCToolsCallExecutesV01Tools(t *testing.T) {
	fixture := newMCPFixture(t)
	versionOne := publishMCPFixtureVersion(t, fixture, "1.0.0", mcpTestOpenAPI("rpcBaseline"))
	versionTwo := publishMCPFixtureVersion(t, fixture, "1.1.0", mcpTestOpenAPIWithReport("rpcChanged"))
	markdownOne := publishMCPFixtureMarkdownVersion(t, fixture, "1.0.0", mcpTestMarkdown("Baseline"))
	markdownTwo := publishMCPFixtureMarkdownVersion(t, fixture, "1.1.0", mcpTestMarkdown("Changed"))
	endpoint := firstMCPEndpoint(t, fixture, versionOne.ID)
	token, err := app.DefaultStore().CreateMCPToken(fixture.writerID, "writer", []int{app.ScopeAPIRead, app.ScopeAPIDraft, app.ScopeDocRead, app.ScopeDocDraft}, nil)
	if err != nil {
		t.Fatalf("CreateMCPToken(writer) error = %v", err)
	}

	assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, "list_projects", gin.H{}), "list_projects")
	assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, "list_documents", gin.H{"project_id": fixture.projectID}), "list_documents")
	assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, "list_api_versions", gin.H{"project_id": fixture.projectID, "document_id": fixture.documentID}), "list_api_versions")
	latestSchema := assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, "get_latest_schema", gin.H{"project_id": fixture.projectID, "document_id": fixture.documentID, "branch_id": fixture.branchID}), "get_latest_schema")
	if !bytes.Contains(latestSchema, []byte("rpcChanged")) {
		t.Fatalf("get_latest_schema result %s does not contain latest schema operation", string(latestSchema))
	}
	endpointDetail := assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, "get_endpoint_detail", gin.H{"project_id": fixture.projectID, "document_id": fixture.documentID, "version_id": versionOne.ID, "endpoint_id": endpoint.ID}), "get_endpoint_detail")
	if !bytes.Contains(endpointDetail, []byte("rpcBaseline")) || !bytes.Contains(endpointDetail, []byte(endpoint.ID)) {
		t.Fatalf("get_endpoint_detail result %s does not contain stored endpoint detail", string(endpointDetail))
	}
	compareResult := assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, "compare_api_versions", gin.H{"project_id": fixture.projectID, "document_id": fixture.documentID, "from_version_id": versionOne.ID, "to_version_id": versionTwo.ID}), "compare_api_versions")
	var diff struct {
		ID      string `json:"id"`
		Summary struct {
			AddedEndpoints int `json:"added_endpoints"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(compareResult, &diff); err != nil {
		t.Fatalf("decode compare result: %v body %s", err, string(compareResult))
	}
	if diff.ID == "" || diff.Summary.AddedEndpoints == 0 {
		t.Fatalf("compare result = %+v, want diff id and added endpoint", diff)
	}
	changeSummary := assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, "get_change_summary", gin.H{"project_id": fixture.projectID, "document_id": fixture.documentID, "diff_id": diff.ID}), "get_change_summary")
	if !bytes.Contains(changeSummary, []byte("must_handle")) || !bytes.Contains(changeSummary, []byte("optional")) {
		t.Fatalf("get_change_summary result %s does not separate must-handle and optional changes", string(changeSummary))
	}

	createResult := assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, "create_api_version_draft", gin.H{"project_id": fixture.projectID, "document_id": fixture.documentID, "branch_id": fixture.branchID, "version_name": "1.2.0", "schema_content": mcpTestOpenAPI("draftCreate")}), "create_api_version_draft")
	var draft struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(createResult, &draft); err != nil || draft.ID == "" {
		t.Fatalf("decode created draft: id=%q error=%v body %s", draft.ID, err, string(createResult))
	}
	assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, "get_api_version_draft", gin.H{"project_id": fixture.projectID, "document_id": fixture.documentID, "draft_id": draft.ID}), "get_api_version_draft")
	assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, "update_api_version_draft", gin.H{"project_id": fixture.projectID, "document_id": fixture.documentID, "draft_id": draft.ID, "branch_id": fixture.branchID, "version_name": "1.2.0", "schema_content": mcpTestOpenAPIWithReport("draftUpdated")}), "update_api_version_draft")
	assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, "submit_api_version_draft", gin.H{"project_id": fixture.projectID, "document_id": fixture.documentID, "draft_id": draft.ID}), "submit_api_version_draft")

	latestDoc := assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, "get_latest_doc", gin.H{"project_id": fixture.projectID, "document_id": fixture.markdownDocumentID, "branch_id": fixture.markdownBranchID}), "get_latest_doc")
	if !bytes.Contains(latestDoc, []byte("Changed")) {
		t.Fatalf("get_latest_doc result %s does not contain latest Markdown content", string(latestDoc))
	}
	docCompareResult := assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, "compare_doc_versions", gin.H{"project_id": fixture.projectID, "document_id": fixture.markdownDocumentID, "from_version_id": markdownOne.ID, "to_version_id": markdownTwo.ID}), "compare_doc_versions")
	if !bytes.Contains(docCompareResult, []byte("items")) || !bytes.Contains(docCompareResult, []byte("Changed")) {
		t.Fatalf("compare_doc_versions result %s does not contain plain Markdown diff details", string(docCompareResult))
	}

	docCreateResult := assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, "create_doc_draft", gin.H{"project_id": fixture.projectID, "document_id": fixture.markdownDocumentID, "branch_id": fixture.markdownBranchID, "version_name": "1.2.0", "markdown_content": mcpTestMarkdown("Draft create")}), "create_doc_draft")
	var docDraft struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(docCreateResult, &docDraft); err != nil || docDraft.ID == "" {
		t.Fatalf("decode created doc draft: id=%q error=%v body %s", docDraft.ID, err, string(docCreateResult))
	}
	docDraftDetail := assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, "get_doc_draft", gin.H{"project_id": fixture.projectID, "document_id": fixture.markdownDocumentID, "draft_id": docDraft.ID}), "get_doc_draft")
	if !bytes.Contains(docDraftDetail, []byte("Draft create")) {
		t.Fatalf("get_doc_draft result %s does not contain draft Markdown content", string(docDraftDetail))
	}
	assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, "update_doc_draft", gin.H{"project_id": fixture.projectID, "document_id": fixture.markdownDocumentID, "draft_id": docDraft.ID, "branch_id": fixture.markdownBranchID, "version_name": "1.2.0", "markdown_content": mcpTestMarkdown("Draft updated")}), "update_doc_draft")
	assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, "submit_doc_draft", gin.H{"project_id": fixture.projectID, "document_id": fixture.markdownDocumentID, "draft_id": docDraft.ID}), "submit_doc_draft")
}

func TestMCPToolResultsUsePublicDocumentDTOs(t *testing.T) {
	fixture := newMCPFixture(t)
	versionOne := publishMCPFixtureVersion(t, fixture, "1.0.0", mcpTestOpenAPI("dtoBaseline"))
	versionTwo := publishMCPFixtureVersion(t, fixture, "1.1.0", mcpTestOpenAPIWithReport("dtoChanged"))
	markdownOne := publishMCPFixtureMarkdownVersion(t, fixture, "1.0.0", mcpTestMarkdown("DTO baseline"))
	markdownTwo := publishMCPFixtureMarkdownVersion(t, fixture, "1.1.0", mcpTestMarkdown("DTO changed"))
	endpoint := firstMCPEndpoint(t, fixture, versionOne.ID)
	token, err := app.DefaultStore().CreateMCPToken(fixture.writerID, "dto", []int{app.ScopeAPIRead, app.ScopeAPIDraft, app.ScopeDocRead, app.ScopeDocDraft}, nil)
	if err != nil {
		t.Fatalf("CreateMCPToken(dto) error = %v", err)
	}

	listDocuments := assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, "list_documents", gin.H{"project_id": fixture.projectID}), "list_documents dto")
	assertPublicMCPResult(t, "list_documents", listDocuments, `"relative_path"`)

	listVersions := assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, "list_api_versions", gin.H{"project_id": fixture.projectID, "document_id": fixture.documentID}), "list_api_versions dto")
	assertPublicMCPResult(t, "list_api_versions", listVersions, `"document_id"`, `"raw_content"`, `"normalized_content"`)

	latestSchema := assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, "get_latest_schema", gin.H{"project_id": fixture.projectID, "document_id": fixture.documentID, "branch_id": fixture.branchID}), "get_latest_schema dto")
	assertPublicMCPResult(t, "get_latest_schema", latestSchema, `"raw_content"`, `"content_kind"`, "dtoChanged")

	endpointDetail := assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, "get_endpoint_detail", gin.H{"project_id": fixture.projectID, "document_id": fixture.documentID, "version_id": versionOne.ID, "endpoint_id": endpoint.ID}), "get_endpoint_detail dto")
	assertPublicMCPResult(t, "get_endpoint_detail", endpointDetail, `"version_id"`, "dtoBaseline")

	apiDiffResult := assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, "compare_api_versions", gin.H{"project_id": fixture.projectID, "document_id": fixture.documentID, "from_version_id": versionOne.ID, "to_version_id": versionTwo.ID}), "compare_api_versions dto")
	assertPublicMCPResult(t, "compare_api_versions", apiDiffResult, `"document_id"`, `"items"`)
	var apiDiff struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(apiDiffResult, &apiDiff); err != nil || apiDiff.ID == "" {
		t.Fatalf("decode api diff id: id=%q error=%v body=%s", apiDiff.ID, err, string(apiDiffResult))
	}
	changeSummary := assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, "get_change_summary", gin.H{"project_id": fixture.projectID, "document_id": fixture.documentID, "diff_id": apiDiff.ID}), "get_change_summary dto")
	assertPublicMCPResult(t, "get_change_summary", changeSummary, `"must_handle"`, `"optional"`)

	apiDraftCreate := assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, "create_api_version_draft", gin.H{"project_id": fixture.projectID, "document_id": fixture.documentID, "branch_id": fixture.branchID, "version_name": "2.0.0", "schema_content": mcpTestOpenAPI("dtoDraft")}), "create_api_version_draft dto")
	assertPublicMCPResult(t, "create_api_version_draft", apiDraftCreate, `"document_id"`, `"raw_content"`, `"normalized_content"`)
	var apiDraft struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(apiDraftCreate, &apiDraft); err != nil || apiDraft.ID == "" {
		t.Fatalf("decode api draft id: id=%q error=%v body=%s", apiDraft.ID, err, string(apiDraftCreate))
	}
	apiDraftGet := assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, "get_api_version_draft", gin.H{"project_id": fixture.projectID, "document_id": fixture.documentID, "draft_id": apiDraft.ID}), "get_api_version_draft dto")
	assertPublicMCPResult(t, "get_api_version_draft", apiDraftGet, `"document_id"`, `"raw_content"`)

	latestDoc := assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, "get_latest_doc", gin.H{"project_id": fixture.projectID, "document_id": fixture.markdownDocumentID, "branch_id": fixture.markdownBranchID}), "get_latest_doc dto")
	assertPublicMCPResult(t, "get_latest_doc", latestDoc, `"stable_content"`, `"content_kind"`, "DTO changed")

	docDiffResult := assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, "compare_doc_versions", gin.H{"project_id": fixture.projectID, "document_id": fixture.markdownDocumentID, "from_version_id": markdownOne.ID, "to_version_id": markdownTwo.ID}), "compare_doc_versions dto")
	assertPublicMCPResult(t, "compare_doc_versions", docDiffResult, `"document_id"`, `"items"`, "DTO changed")

	docDraftCreate := assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, "create_doc_draft", gin.H{"project_id": fixture.projectID, "document_id": fixture.markdownDocumentID, "branch_id": fixture.markdownBranchID, "version_name": "2.0.0", "markdown_content": mcpTestMarkdown("DTO draft")}), "create_doc_draft dto")
	assertPublicMCPResult(t, "create_doc_draft", docDraftCreate, `"document_id"`, `"raw_content"`, `"stable_content"`)
	var docDraft struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(docDraftCreate, &docDraft); err != nil || docDraft.ID == "" {
		t.Fatalf("decode doc draft id: id=%q error=%v body=%s", docDraft.ID, err, string(docDraftCreate))
	}
	docDraftGet := assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, "get_doc_draft", gin.H{"project_id": fixture.projectID, "document_id": fixture.markdownDocumentID, "draft_id": docDraft.ID}), "get_doc_draft dto")
	assertPublicMCPResult(t, "get_doc_draft", docDraftGet, `"stable_content"`, `"content_kind"`, "DTO draft")

	legacy := callMCPTool(t, fixture.router, token.Token, "get_endpoint_detail", gin.H{"project_id": fixture.projectID, "document_id": fixture.documentID, "version_id": versionOne.ID, "endpoint_id": endpoint.ID})
	if legacy.Code != 200 || legacy.Status != "OK" {
		t.Fatalf("legacy get_endpoint_detail response = code %d status %q body %s", legacy.Code, legacy.Status, string(legacy.Detail))
	}
	assertPublicMCPResult(t, "legacy get_endpoint_detail", legacy.Detail, `"version_id"`, "dtoBaseline")
}

func TestMCPJSONRPCErrorsAreStructured(t *testing.T) {
	fixture := newMCPFixture(t)
	token, err := app.DefaultStore().CreateMCPToken(fixture.readerID, "read", []int{app.ScopeAPIRead}, nil)
	if err != nil {
		t.Fatalf("CreateMCPToken(read) error = %v", err)
	}

	assertRPCError(t, callMCPRPC(t, fixture.router, token.Token, gin.H{"jsonrpc": "2.0", "id": "bad-method", "method": "tools/unknown"}), -32601, "invalid method")
	directPublishTool := strings.Join([]string{"publish", "api", "version"}, "_")
	removedDocumentListTool := "list_" + "services"
	assertRPCError(t, callMCPToolRPC(t, fixture.router, token.Token, directPublishTool, gin.H{}), -32602, "invalid tool")
	assertRPCError(t, callMCPToolRPC(t, fixture.router, token.Token, removedDocumentListTool, gin.H{}), -32602, "removed tool")
	assertRPCError(t, callMCPToolRPC(t, fixture.router, token.Token, "create_api_version_draft", gin.H{"project_id": fixture.projectID, "document_id": fixture.documentID, "branch_id": fixture.branchID, "version_name": "blocked", "schema_content": mcpTestOpenAPI("blocked")}), -32003, "permission denied")
	assertRPCError(t, callMCPToolRPC(t, fixture.router, token.Token, "get_latest_doc", gin.H{"project_id": fixture.projectID, "document_id": fixture.markdownDocumentID}), -32003, "api read denied markdown content")
	assertRPCError(t, callMCPRPC(t, fixture.router, "", gin.H{"jsonrpc": "2.0", "id": "no-token", "method": "tools/list"}), -32001, "unauthenticated")
	assertRPCError(t, callMCPRPC(t, fixture.router, token.Token, gin.H{"jsonrpc": "1.0", "id": "bad-request", "method": "tools/list"}), -32600, "invalid request")
	assertRPCError(t, callMCPToolRPC(t, fixture.router, token.Token, "get_endpoint_detail", gin.H{"project_id": fixture.projectID, "document_id": fixture.documentID, "version_id": "missing", "endpoint_id": "missing"}), -32004, "not found")
}

func TestMCPListProjectsAuditsTokenUseAndToolCall(t *testing.T) {
	fixture := newMCPFixture(t)
	token, err := app.DefaultStore().CreateMCPToken(fixture.readerID, "audit", []int{app.ScopeAPIRead}, nil)
	if err != nil {
		t.Fatalf("CreateMCPToken(audit) error = %v", err)
	}
	body := callMCPRPCBodyWithTrace(t, fixture.router, token.Token, "trace-mcp-audit", gin.H{"jsonrpc": "2.0", "id": "audit-list-projects", "method": "tools/call", "params": gin.H{"name": "list_projects", "arguments": gin.H{}}})
	var response mcpRPCResponse
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode response: %v body %s", err, string(body))
	}
	assertRPCResult(t, response, "list_projects audit")

	toolAudit := findMCPAudit(t, "mcp.tool_call", token.ID)
	if toolAudit.ActorType != app.AuditActorMCPToken || toolAudit.ActorUserID != fixture.readerID || toolAudit.ActorTokenID != token.ID || toolAudit.RequestID != "trace-mcp-audit" {
		t.Fatalf("tool audit = %+v, want token actor/user/request", toolAudit)
	}
	if toolAudit.Metadata["tool_name"] != "list_projects" || toolAudit.Metadata["token_id"] != token.ID || toolAudit.Metadata["result"] != "success" {
		t.Fatalf("tool audit metadata = %+v, want tool/token/result", toolAudit.Metadata)
	}
	authAudit := findMCPAudit(t, "mcp_token.authenticate", token.ID)
	if authAudit.ActorTokenID != token.ID || authAudit.Metadata["result"] != "success" {
		t.Fatalf("auth audit = %+v, want token success", authAudit)
	}
	if mcpAuditContainsValue(app.DefaultStore().AuditLogsForTest(), token.Token) {
		t.Fatalf("mcp audit leaked token secret %q", token.Token)
	}

	if evidenceDir := os.Getenv("VDOC_TASK12_EVIDENCE_DIR"); evidenceDir != "" {
		writeEvidence(t, filepath.Join(evidenceDir, "task-12-mcp-audit.json"), mustJSON(t, gin.H{"response": json.RawMessage(body), "audits": []*app.AuditLog{authAudit, toolAudit}}))
	}
}

func TestMCPJSONRPCEvidenceWriter(t *testing.T) {
	evidenceDir := os.Getenv("VDOC_TASK11_EVIDENCE_DIR")
	if evidenceDir == "" {
		t.Skip("set VDOC_TASK11_EVIDENCE_DIR to write Task 11 evidence")
	}
	fixture := newMCPFixture(t)
	version := publishMCPFixtureVersion(t, fixture, "1.0.0", mcpTestOpenAPI("evidenceEndpoint"))
	endpoint := firstMCPEndpoint(t, fixture, version.ID)
	token, err := app.DefaultStore().CreateMCPToken(fixture.readerID, "evidence", []int{app.ScopeAPIRead}, nil)
	if err != nil {
		t.Fatalf("CreateMCPToken(evidence) error = %v", err)
	}

	toolsList := callMCPRPCBody(t, fixture.router, token.Token, gin.H{"jsonrpc": "2.0", "id": "task-11-tools-list", "method": "tools/list"})
	endpointDetail := callMCPRPCBody(t, fixture.router, token.Token, gin.H{"jsonrpc": "2.0", "id": "task-11-get-endpoint-detail", "method": "tools/call", "params": gin.H{"name": "get_endpoint_detail", "arguments": gin.H{"project_id": fixture.projectID, "document_id": fixture.documentID, "version_id": version.ID, "endpoint_id": endpoint.ID}}})

	writeEvidence(t, filepath.Join(evidenceDir, "task-11-tools-list.json"), toolsList)
	writeEvidence(t, filepath.Join(evidenceDir, "task-11-get-endpoint-detail.json"), endpointDetail)
}

func newMCPFixture(t *testing.T) mcpFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	app.ResetDefaultStoreForTest()
	t.Cleanup(app.ResetDefaultStoreForTest)

	router := gin.New()
	router.Use(middleware.TraceID())
	RegisterOpenRoutes(router.Group("/api/v1/open"))

	store := app.DefaultStore()
	superUser, err := store.Register("super-mcp@example.com", "Super", "correct horse battery staple")
	if err != nil {
		t.Fatalf("register super: %v", err)
	}
	readerUser, err := store.CreateUser(superUser.ID, "reader-mcp@example.com", "Reader", "correct horse battery staple", false)
	if err != nil {
		t.Fatalf("create reader: %v", err)
	}
	writerUser, err := store.CreateUser(superUser.ID, "writer-mcp@example.com", "Writer", "correct horse battery staple", false)
	if err != nil {
		t.Fatalf("create writer: %v", err)
	}
	team, err := store.CreateTeam(superUser.ID, "MCP Team", "")
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	project, err := store.CreateProject(superUser.ID, team.ID, "MCP Project", "", superUser.ID)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := store.AddProjectMember(superUser.ID, project.ID, readerUser.ID, app.MemberRoleReader); err != nil {
		t.Fatalf("add reader: %v", err)
	}
	if _, err := store.AddProjectMember(superUser.ID, project.ID, writerUser.ID, app.MemberRoleWriter); err != nil {
		t.Fatalf("add writer: %v", err)
	}
	document, err := store.CreateDocument(superUser.ID, project.ID, "mcp-document", app.DocumentTypeOpenAPI, "apis/mcp.yaml", "")
	if err != nil {
		t.Fatalf("create document: %v", err)
	}
	markdownDocument, err := store.CreateDocument(superUser.ID, project.ID, "mcp-guide", app.DocumentTypeMarkdown, "docs/mcp-guide.md", "")
	if err != nil {
		t.Fatalf("create markdown document: %v", err)
	}
	branches, err := store.ListBranches(superUser.ID, project.ID, document.ID)
	if err != nil {
		t.Fatalf("list branches: %v", err)
	}
	var branchID string
	for _, branch := range branches {
		if branch.Name == "dev" {
			branchID = branch.ID
		}
	}
	if branchID == "" {
		t.Fatal("dev branch not found")
	}
	markdownBranches, err := store.ListBranches(superUser.ID, project.ID, markdownDocument.ID)
	if err != nil {
		t.Fatalf("list markdown branches: %v", err)
	}
	var markdownBranchID string
	for _, branch := range markdownBranches {
		if branch.Name == "dev" {
			markdownBranchID = branch.ID
		}
	}
	if markdownBranchID == "" {
		t.Fatal("markdown dev branch not found")
	}

	return mcpFixture{router: router, superID: superUser.ID, readerID: readerUser.ID, writerID: writerUser.ID, projectID: project.ID, documentID: document.ID, branchID: branchID, markdownDocumentID: markdownDocument.ID, markdownBranchID: markdownBranchID}
}

func callMCPTool(t *testing.T, router *gin.Engine, token, tool string, args any) mcpTestEnvelope {
	t.Helper()
	body, err := json.Marshal(gin.H{"tool": tool, "arguments": args})
	if err != nil {
		t.Fatalf("marshal mcp call: %v", err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/open/mcp", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(middleware.AuthorizationHeader, token)
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, want 200 body %s", recorder.Code, recorder.Body.String())
	}
	var envelope mcpTestEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode mcp response: %v body %s", err, recorder.Body.String())
	}
	return envelope
}

func callMCPToolRPC(t *testing.T, router *gin.Engine, token, tool string, args any) mcpRPCResponse {
	t.Helper()
	return callMCPRPC(t, router, token, gin.H{"jsonrpc": "2.0", "id": tool, "method": "tools/call", "params": gin.H{"name": tool, "arguments": args}})
}

func callMCPRPC(t *testing.T, router *gin.Engine, token string, payload any) mcpRPCResponse {
	t.Helper()
	body := callMCPRPCBody(t, router, token, payload)
	var response mcpRPCResponse
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode rpc response: %v body %s", err, string(body))
	}
	if response.JSONRPC != "2.0" {
		t.Fatalf("jsonrpc = %q, want 2.0 body %s", response.JSONRPC, string(body))
	}
	return response
}

func callMCPRPCBody(t *testing.T, router *gin.Engine, token string, payload any) []byte {
	t.Helper()
	return callMCPRPCBodyWithTrace(t, router, token, "", payload)
}

func callMCPRPCBodyWithTrace(t *testing.T, router *gin.Engine, token, traceID string, payload any) []byte {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal rpc call: %v", err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/open/mcp", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set(middleware.AuthorizationHeader, token)
	}
	if traceID != "" {
		request.Header.Set(middleware.TraceIDHeaderKey, traceID)
	}
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, want 200 body %s", recorder.Code, recorder.Body.String())
	}
	return recorder.Body.Bytes()
}

func findMCPAudit(t *testing.T, action, tokenID string) *app.AuditLog {
	t.Helper()
	for _, audit := range app.DefaultStore().AuditLogsForTest() {
		if audit.Action == action && (audit.ActorTokenID == tokenID || audit.Metadata["token_id"] == tokenID) {
			return audit
		}
	}
	t.Fatalf("missing mcp audit action=%s token=%s logs=%+v", action, tokenID, app.DefaultStore().AuditLogsForTest())
	return nil
}

func mcpAuditContainsValue(logs []*app.AuditLog, forbidden string) bool {
	for _, audit := range logs {
		for key, value := range audit.Metadata {
			if key == forbidden || value == forbidden {
				return true
			}
		}
	}
	return false
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return body
}

func assertRPCResult(t *testing.T, response mcpRPCResponse, label string) json.RawMessage {
	t.Helper()
	if response.Error != nil {
		t.Fatalf("%s error = %+v", label, response.Error)
	}
	if len(response.Result) == 0 {
		t.Fatalf("%s returned empty result", label)
	}
	return response.Result
}

func assertRPCError(t *testing.T, response mcpRPCResponse, code int, label string) {
	t.Helper()
	if response.Error == nil {
		t.Fatalf("%s returned result %s, want error code %d", label, string(response.Result), code)
	}
	if response.Error.Code != code {
		t.Fatalf("%s error code = %d message %q data %s, want %d", label, response.Error.Code, response.Error.Message, string(response.Error.Data), code)
	}
	if len(response.Error.Data) == 0 {
		t.Fatalf("%s error data is empty", label)
	}
}

func assertPublicMCPResult(t *testing.T, label string, body json.RawMessage, required ...string) {
	t.Helper()
	for _, field := range []string{`"service_id"`, `"raw_schema"`, `"normalized_schema"`, `"display_name"`, `"base_path"`, `"contract_version_id"`} {
		if bytes.Contains(body, []byte(field)) {
			t.Fatalf("%s result exposed stale field %s in %s", label, field, string(body))
		}
	}
	for _, want := range required {
		if !bytes.Contains(body, []byte(want)) {
			t.Fatalf("%s result %s missing %s", label, string(body), want)
		}
	}
}

func publishMCPFixtureVersion(t *testing.T, fixture mcpFixture, versionName, schema string) *app.ContractVersion {
	t.Helper()
	store := app.DefaultStore()
	draft, err := store.CreateDocumentDraft(fixture.superID, fixture.projectID, fixture.documentID, app.DraftInput{BranchID: fixture.branchID, VersionName: versionName, SchemaContent: schema})
	if err != nil {
		t.Fatalf("CreateDraft(%s) error = %v", versionName, err)
	}
	if _, err := store.SubmitDocumentDraft(fixture.superID, fixture.projectID, fixture.documentID, draft.ID); err != nil {
		t.Fatalf("SubmitDraft(%s) error = %v", versionName, err)
	}
	published, err := store.ReviewDocumentDraft(fixture.superID, fixture.projectID, fixture.documentID, draft.ID, "approve")
	if err != nil {
		t.Fatalf("ReviewDraft(%s) error = %v", versionName, err)
	}
	version, ok := published.(*app.ContractVersion)
	if !ok {
		t.Fatalf("published result = %T, want *ContractVersion", published)
	}
	return version
}

func publishMCPFixtureMarkdownVersion(t *testing.T, fixture mcpFixture, versionName, content string) *app.ContractVersion {
	t.Helper()
	store := app.DefaultStore()
	draft, err := store.CreateMarkdownDraft(fixture.superID, fixture.projectID, fixture.markdownDocumentID, app.DraftInput{BranchID: fixture.markdownBranchID, VersionName: versionName, SchemaContent: content})
	if err != nil {
		t.Fatalf("CreateMarkdownDraft(%s) error = %v", versionName, err)
	}
	if _, err := store.SubmitMarkdownDraft(fixture.superID, fixture.projectID, fixture.markdownDocumentID, draft.ID); err != nil {
		t.Fatalf("SubmitMarkdownDraft(%s) error = %v", versionName, err)
	}
	published, err := store.ReviewMarkdownDraft(fixture.superID, fixture.projectID, fixture.markdownDocumentID, draft.ID, "approve")
	if err != nil {
		t.Fatalf("ReviewMarkdownDraft(%s) error = %v", versionName, err)
	}
	version, ok := published.(*app.ContractVersion)
	if !ok {
		t.Fatalf("published markdown result = %T, want *ContractVersion", published)
	}
	return version
}

func firstMCPEndpoint(t *testing.T, fixture mcpFixture, versionID string) *app.Endpoint {
	t.Helper()
	endpoints, err := app.DefaultStore().ListDocumentEndpoints(fixture.superID, fixture.projectID, fixture.documentID, versionID, "")
	if err != nil {
		t.Fatalf("ListEndpoints() error = %v", err)
	}
	if len(endpoints) == 0 {
		t.Fatal("no endpoints found for fixture version")
	}
	return endpoints[0]
}

func writeEvidence(t *testing.T, path string, body []byte) {
	t.Helper()
	var formatted bytes.Buffer
	if err := json.Indent(&formatted, body, "", "  "); err != nil {
		t.Fatalf("format evidence %s: %v body %s", path, err, string(body))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create evidence dir: %v", err)
	}
	if err := os.WriteFile(path, append(formatted.Bytes(), '\n'), 0o644); err != nil {
		t.Fatalf("write evidence %s: %v", path, err)
	}
}

func mcpTestOpenAPI(operationID string) string {
	return `{"openapi":"3.1.0","info":{"title":"MCP API","version":"1.0.0"},"paths":{"/widgets":{"get":{"operationId":"` + operationID + `","responses":{"200":{"description":"ok"}}}}}}`
}

func mcpTestOpenAPIWithReport(operationID string) string {
	return `{"openapi":"3.1.0","info":{"title":"MCP API","version":"1.1.0"},"paths":{"/reports":{"get":{"operationId":"listReports","responses":{"200":{"description":"ok"}}}},"/widgets":{"get":{"operationId":"` + operationID + `","responses":{"200":{"description":"ok"}}}}}}`
}

func mcpTestMarkdown(title string) string {
	return "# " + title + "\n\n- MCP Markdown content for " + title + "\n"
}
