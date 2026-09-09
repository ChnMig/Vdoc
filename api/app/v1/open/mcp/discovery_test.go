package mcp

import (
	"bytes"
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/gin-gonic/gin"
	app "vdoc/services/vdoc"
)

func TestMCPToolArgumentManifest(t *testing.T) {
	path := os.Getenv("VDOC_MCP_CONTRACT_FILE")
	if path == "" {
		t.Skip("set VDOC_MCP_CONTRACT_FILE to validate the shared workspace manifest")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Tools []struct {
			Name     string   `json:"name"`
			Required []string `json:"required"`
			Optional []string `json:"optional"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(body, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Tools) != len(toolDefinitions) {
		t.Fatalf("manifest has %d tools; runtime has %d", len(manifest.Tools), len(toolDefinitions))
	}
	seen := map[string]bool{}
	for _, declared := range manifest.Tools {
		if seen[declared.Name] {
			t.Fatalf("duplicate manifest tool %s", declared.Name)
		}
		seen[declared.Name] = true
		found := false
		for _, tool := range toolDefinitions {
			if tool.Name != declared.Name {
				continue
			}
			found = true
			required := slices.Clone(tool.InputSchema["required"].([]string))
			optional := []string{}
			for key := range tool.InputSchema["properties"].(gin.H) {
				if !slices.Contains(required, key) {
					optional = append(optional, key)
				}
			}
			slices.Sort(required)
			slices.Sort(optional)
			slices.Sort(declared.Required)
			slices.Sort(declared.Optional)
			if !slices.Equal(required, declared.Required) || !slices.Equal(optional, declared.Optional) {
				t.Errorf("%s runtime required=%v optional=%v; manifest required=%v optional=%v", tool.Name, required, optional, declared.Required, declared.Optional)
			}
		}
		if !found {
			t.Errorf("unknown manifest tool %s", declared.Name)
		}
	}
}

func TestMCPDiscoveryCompletesEndpointAndFirstDraftWorkflows(t *testing.T) {
	fixture := newMCPFixture(t)
	publishMCPFixtureVersion(t, fixture, "1.0.0", mcpTestOpenAPIWithReport("listWidgets"))
	token, err := app.DefaultStore().CreateMCPToken(fixture.writerID, "discovery", []int{app.ScopeAPIRead, app.ScopeDocRead, app.ScopeDocDraft}, nil)
	if err != nil {
		t.Fatal(err)
	}
	call := func(name string, args gin.H, target any) json.RawMessage {
		t.Helper()
		result := assertRPCResult(t, callMCPToolRPC(t, fixture.router, token.Token, name, args), name)
		if target != nil {
			if err := json.Unmarshal(result, target); err != nil {
				t.Fatalf("decode %s: %v", name, err)
			}
		}
		return result
	}
	// 业务请求仅使用公开工具返回的 ID；夹具只负责预置管理员数据。
	var projects []mcpProjectDTO
	call("list_projects", gin.H{}, &projects)
	if len(projects) != 1 {
		t.Fatalf("projects = %+v", projects)
	}
	projectID := projects[0].ID
	var documents []mcpDocumentDTO
	call("list_documents", gin.H{"project_id": projectID}, &documents)
	var apiID, markdownID string
	for _, document := range documents {
		switch document.DocumentType {
		case app.DocumentTypeOpenAPI:
			apiID = document.ID
		case app.DocumentTypeMarkdown:
			markdownID = document.ID
		}
	}
	if apiID == "" || markdownID == "" {
		t.Fatal("document discovery failed")
	}
	var versions []mcpVersionDTO
	call("list_api_versions", gin.H{"project_id": projectID, "document_id": apiID}, &versions)
	if len(versions) != 1 {
		t.Fatalf("versions = %+v", versions)
	}
	var endpoints []mcpEndpointSummaryDTO
	result := call("list_api_endpoints", gin.H{"project_id": projectID, "document_id": apiID, "version_id": versions[0].ID, "method": "get", "path": "/widgets"}, &endpoints)
	assertPublicMCPResult(t, "list_api_endpoints", result, `"version_id"`, `"method"`, `"path"`)
	assertMCPResultOmits(t, "list_api_endpoints", result, `"normalized_operation"`, `"parameters"`, `"responses"`)
	if len(endpoints) != 1 || endpoints[0].Method != "GET" || endpoints[0].Path != "/widgets" {
		t.Fatalf("filtered endpoints = %+v", endpoints)
	}
	var detail mcpEndpointDTO
	call("get_endpoint_detail", gin.H{"project_id": projectID, "document_id": apiID, "version_id": versions[0].ID, "endpoint_id": endpoints[0].ID}, &detail)
	if detail.OperationID != "listWidgets" || detail.Responses == nil {
		t.Fatalf("endpoint detail = %+v", detail)
	}
	call("list_api_endpoints", gin.H{"project_id": projectID, "document_id": apiID, "version_id": versions[0].ID, "path": "/widget"}, &endpoints)
	if len(endpoints) != 0 {
		t.Fatal("path filter matched a substring rather than the exact path")
	}
	var docVersions []mcpVersionDTO
	call("list_doc_versions", gin.H{"project_id": projectID, "document_id": markdownID}, &docVersions)
	if len(docVersions) != 0 {
		t.Fatal("expected an unpublished document")
	}
	var branches []mcpBranchDTO
	branchResult := call("list_document_branches", gin.H{"project_id": projectID, "document_id": markdownID}, &branches)
	assertPublicMCPResult(t, "list_document_branches", branchResult, `"document_id"`, `"is_default"`, `"is_protected"`)
	var branchID string
	for _, branch := range branches {
		if branch.Name == "dev" {
			branchID = branch.ID
		}
	}
	if branchID == "" {
		t.Fatal("dev branch not discoverable before the first publication")
	}
	created := call("create_doc_draft", gin.H{"project_id": projectID, "document_id": markdownID, "branch_id": branchID, "version_name": "1.0.0", "markdown_content": "# First document"}, nil)
	if !bytes.Contains(created, []byte(branchID)) {
		t.Fatalf("draft did not use the discovered branch: %s", created)
	}
}

func TestMCPDiscoveryEnforcesScopeMembershipAndVersionBoundaries(t *testing.T) {
	fixture := newMCPFixture(t)
	version := publishMCPFixtureVersion(t, fixture, "1.0.0", mcpTestOpenAPI("discoveryPermissions"))
	newToken := func(scopes ...int) string {
		t.Helper()
		token, err := app.DefaultStore().CreateMCPToken(fixture.writerID, "scoped-discovery", scopes, nil)
		if err != nil {
			t.Fatal(err)
		}
		return token.Token
	}
	apiToken := newToken(app.ScopeAPIRead)
	docToken := newToken(app.ScopeDocRead)
	draftToken := newToken(app.ScopeDocDraft)
	for _, tc := range []struct {
		name, token, tool string
		args              gin.H
		code              int
	}{
		{"API scope cannot inspect Markdown branches", apiToken, "list_document_branches", gin.H{"project_id": fixture.projectID, "document_id": fixture.markdownDocumentID}, -32004},
		{"Markdown scope cannot inspect API branches", docToken, "list_document_branches", gin.H{"project_id": fixture.projectID, "document_id": fixture.documentID}, -32004},
		{"draft scope cannot read branches", draftToken, "list_document_branches", gin.H{"project_id": fixture.projectID, "document_id": fixture.markdownDocumentID}, -32003},
		{"Markdown scope cannot list endpoints", docToken, "list_api_endpoints", gin.H{"project_id": fixture.projectID, "document_id": fixture.documentID, "version_id": version.ID}, -32003},
		{"cannot substitute a foreign project", apiToken, "list_api_endpoints", gin.H{"project_id": "other-project", "document_id": fixture.documentID, "version_id": version.ID}, -32003},
		{"cannot substitute another version", apiToken, "list_api_endpoints", gin.H{"project_id": fixture.projectID, "document_id": fixture.documentID, "version_id": "other-version"}, -32004},
		{"version is required", apiToken, "list_api_endpoints", gin.H{"project_id": fixture.projectID, "document_id": fixture.documentID}, -32602},
		{"method must be valid", apiToken, "list_api_endpoints", gin.H{"project_id": fixture.projectID, "document_id": fixture.documentID, "version_id": version.ID, "method": "invalid"}, -32602},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertRPCError(t, callMCPToolRPC(t, fixture.router, tc.token, tc.tool, tc.args), tc.code, tc.name)
		})
	}
}
