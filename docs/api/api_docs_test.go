package apidocs_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.yaml.in/yaml/v3"

	"vdoc/api/app/v1/open"
	"vdoc/api/app/v1/private"
	"vdoc/api/middleware"
	app "vdoc/appstore"
	"vdoc/config"
	apidocs "vdoc/docs/api"
)

type docsEnvelope struct {
	Code   int             `json:"code"`
	Status string          `json:"status"`
	Detail json.RawMessage `json:"detail"`
	Total  *int            `json:"total"`
}

type docsAuthDetail struct {
	User struct {
		ID           string `json:"id"`
		IsSuperAdmin bool   `json:"is_super_admin"`
	} `json:"user"`
	Token string `json:"token"`
}

type docsResourceID struct {
	ID string `json:"id"`
}

type docsBranch struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type docsEndpoint struct {
	ID     string `json:"id"`
	Method string `json:"method"`
	Path   string `json:"path"`
}

type docsDiff struct {
	ID      string `json:"id"`
	Summary struct {
		AddedEndpoints int `json:"added_endpoints"`
	} `json:"summary"`
}

type docsMCPToken struct {
	ID    string `json:"id"`
	Token string `json:"token"`
}

type docsRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *docsRPCError   `json:"error"`
}

type docsRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func TestAPIDocsRouteCoverage(t *testing.T) {
	document := readDocsMarkdown(t, "API.md")
	root := parseDocsOpenAPI(t)
	paths := asDocsMap(t, root["paths"], "paths")
	missingPaths := make([]string, 0)
	for path := range paths {
		if !strings.Contains(document, path) {
			missingPaths = append(missingPaths, path)
		}
	}
	sort.Strings(missingPaths)
	if len(missingPaths) > 0 {
		t.Fatalf("API.md is missing OpenAPI paths: %s", strings.Join(missingPaths, ", "))
	}

	tags, ok := root["tags"].([]any)
	if !ok {
		t.Fatalf("OpenAPI tags have type %T, want []any", root["tags"])
	}
	missingTags := make([]string, 0)
	for _, tagValue := range tags {
		tag := asDocsMap(t, tagValue, "tag")
		name, ok := tag["name"].(string)
		if !ok || name == "" {
			t.Fatalf("tag name = %#v", tag["name"])
		}
		if !strings.Contains(document, name) {
			missingTags = append(missingTags, name)
		}
	}
	if len(missingTags) > 0 {
		t.Fatalf("API.md is missing route categories: %s", strings.Join(missingTags, ", "))
	}
	t.Logf("API.md documents %d OpenAPI paths and %d route categories", len(paths), len(tags))
}

func TestAPIDocsRequiredPhrases(t *testing.T) {
	document := readDocsMarkdown(t, "API.md")
	requiredPhrases := []string{
		"Vdoc Response Envelope",
		"Projects directly own typed Documents",
		"document_type",
		"document path identity",
		"SuperAdmin",
		"Reader",
		"Writer",
		"Admin",
		"content_kind",
		"raw",
		"normalized",
		"register/login",
		"create project/document",
		"upload draft",
		"submit",
		"approve",
		"query endpoint",
		"compare diff",
		"create MCP token",
		"MCP tools/list",
		"MCP tools/call",
		"list_documents",
		"get_latest_doc",
		"compare_doc_versions",
		"create_doc_draft",
		"markdown_content",
		"one time copyable",
		"Direct publish tools are intentionally not exposed in v0.1",
	}
	missing := make([]string, 0)
	for _, phrase := range requiredPhrases {
		if !strings.Contains(document, phrase) {
			missing = append(missing, phrase)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("API.md missing required phrases: %s", strings.Join(missing, ", "))
	}
}

func TestAPIDocsUseProjectDocumentLanguage(t *testing.T) {
	files := []string{
		"API.md",
		"openapi.yaml",
		"../../README.md",
		"../../README.zh-CN.md",
		"../../../IMPROVEMENTS.md",
		"../../../IMPROVEMENTS.zh-CN.md",
		"../../../DATABASE_SCHEMA.md",
		"../../../IMPLEMENTATION_PLAN.md",
	}
	staleTerms := []string{
		"list_" + "services",
		"service" + "_id",
		"Service" + " ID",
		"/" + "services",
		"api" + "_contract",
		"file" + "name",
		"File" + "name",
		"file" + "_name",
		"File" + "Name",
		strings.Join([]string{"publish", "api", "schema"}, "_"),
		strings.Join([]string{"publish", "api", "version"}, "_"),
	}
	for _, path := range files {
		body := readDocsMarkdown(t, path)
		for _, term := range staleTerms {
			if strings.Contains(body, term) {
				t.Fatalf("%s contains stale active document wording %q", path, term)
			}
		}
	}
}

func TestAPIDocsMarkdownLinks(t *testing.T) {
	files := []string{"API.md", "../../README.md", "../../README.zh-CN.md"}
	linkPattern := regexp.MustCompile(`\[[^\]]+\]\(([^)]+)\)`)
	for _, markdownPath := range files {
		body := readDocsMarkdown(t, markdownPath)
		baseDirectory := filepath.Dir(markdownPath)
		for _, match := range linkPattern.FindAllStringSubmatch(body, -1) {
			link := strings.TrimSpace(match[1])
			if skipDocsLink(link) {
				continue
			}
			linkTarget := strings.Split(link, "#")[0]
			if linkTarget == "" {
				continue
			}
			resolved := filepath.Clean(filepath.Join(baseDirectory, linkTarget))
			if _, err := os.Stat(resolved); err != nil {
				t.Fatalf("%s links to missing local target %s resolved as %s: %v", markdownPath, link, resolved, err)
			}
		}
	}
}

func TestAPIDocsExampleSmoke(t *testing.T) {
	router := newDocsSmokeRouter(t)
	registerEnvelope := performDocsJSON(t, router, http.MethodPost, "/api/v1/open/auth/register", "", map[string]any{
		"email":    "docs-admin@example.test",
		"name":     "Docs Admin",
		"password": "sample-password-change-me",
	})
	registerDetail := decodeDocsDetail[docsAuthDetail](t, registerEnvelope)
	if registerDetail.Token == "" || registerDetail.User.ID == "" || !registerDetail.User.IsSuperAdmin {
		t.Fatalf("register detail = %+v, want super admin id and token", registerDetail)
	}

	loginEnvelope := performDocsJSON(t, router, http.MethodPost, "/api/v1/open/auth/login", "", map[string]any{
		"email":    "docs-admin@example.test",
		"password": "sample-password-change-me",
	})
	loginDetail := decodeDocsDetail[docsAuthDetail](t, loginEnvelope)
	jwtToken := loginDetail.Token
	if jwtToken == "" {
		t.Fatal("login returned empty JWT")
	}

	team := decodeDocsDetail[docsResourceID](t, performDocsJSON(t, router, http.MethodPost, "/api/v1/private/teams", jwtToken, map[string]any{
		"name":        "Docs Team",
		"description": "API docs smoke team",
	}))
	project := decodeDocsDetail[docsResourceID](t, performDocsJSON(t, router, http.MethodPost, "/api/v1/private/projects", jwtToken, map[string]any{
		"team_id":       team.ID,
		"name":          "Docs Project",
		"description":   "API docs smoke project",
		"admin_user_id": registerDetail.User.ID,
	}))
	document := decodeDocsDetail[docsResourceID](t, performDocsJSON(t, router, http.MethodPost, "/api/v1/private/projects/"+project.ID+"/documents", jwtToken, map[string]any{
		"name":          "petstore",
		"document_type": 1,
		"relative_path": "apis/petstore.yaml",
		"description":   "Docs sample document",
	}))
	branchesEnvelope := performDocsJSON(t, router, http.MethodGet, "/api/v1/private/projects/"+project.ID+"/documents/"+document.ID+"/branches", jwtToken, nil)
	branches := decodeDocsDetail[[]docsBranch](t, branchesEnvelope)
	branchID := docsBranchID(t, branches, "dev")

	draftOne := decodeDocsDetail[docsResourceID](t, performDocsJSON(t, router, http.MethodPost, "/api/v1/private/projects/"+project.ID+"/documents/"+document.ID+"/drafts", jwtToken, map[string]any{
		"branch_id":            branchID,
		"version_name":         "1.0.0",
		"changelog":            "Initial pet list",
		"source_git_commit_id": "abc1234",
		"schema_content":       docsSmokeOpenAPI("1.0.0", false),
	}))
	assertDocsSchema(t, performDocsJSON(t, router, http.MethodGet, "/api/v1/private/projects/"+project.ID+"/documents/"+document.ID+"/drafts/"+draftOne.ID+"/content/raw", jwtToken, nil), "raw")
	assertDocsSchema(t, performDocsJSON(t, router, http.MethodGet, "/api/v1/private/projects/"+project.ID+"/documents/"+document.ID+"/drafts/"+draftOne.ID+"/content/normalized", jwtToken, nil), "normalized")
	performDocsJSON(t, router, http.MethodPost, "/api/v1/private/projects/"+project.ID+"/documents/"+document.ID+"/drafts/"+draftOne.ID+"/submit", jwtToken, nil)
	versionOne := decodeDocsDetail[docsResourceID](t, performDocsJSON(t, router, http.MethodPost, "/api/v1/private/projects/"+project.ID+"/documents/"+document.ID+"/drafts/"+draftOne.ID+"/approve", jwtToken, nil))
	assertDocsSchema(t, performDocsJSON(t, router, http.MethodGet, "/api/v1/private/projects/"+project.ID+"/documents/"+document.ID+"/versions/"+versionOne.ID+"/content/raw", jwtToken, nil), "raw")
	assertDocsSchema(t, performDocsJSON(t, router, http.MethodGet, "/api/v1/private/projects/"+project.ID+"/documents/"+document.ID+"/versions/"+versionOne.ID+"/content/normalized", jwtToken, nil), "normalized")

	endpointList := decodeDocsDetail[[]docsEndpoint](t, performDocsJSON(t, router, http.MethodGet, "/api/v1/private/projects/"+project.ID+"/documents/"+document.ID+"/versions/"+versionOne.ID+"/endpoints?path=/pets", jwtToken, nil))
	if len(endpointList) != 1 || endpointList[0].ID == "" || endpointList[0].Path != "/pets" {
		t.Fatalf("endpoint list = %+v, want /pets endpoint", endpointList)
	}
	endpoint := decodeDocsDetail[docsEndpoint](t, performDocsJSON(t, router, http.MethodGet, "/api/v1/private/projects/"+project.ID+"/documents/"+document.ID+"/versions/"+versionOne.ID+"/endpoints/"+endpointList[0].ID, jwtToken, nil))
	if endpoint.ID != endpointList[0].ID {
		t.Fatalf("endpoint detail id = %q, want %q", endpoint.ID, endpointList[0].ID)
	}

	draftTwo := decodeDocsDetail[docsResourceID](t, performDocsJSON(t, router, http.MethodPost, "/api/v1/private/projects/"+project.ID+"/documents/"+document.ID+"/drafts", jwtToken, map[string]any{
		"branch_id":      branchID,
		"version_name":   "1.1.0",
		"changelog":      "Add pet detail",
		"schema_content": docsSmokeOpenAPI("1.1.0", true),
	}))
	performDocsJSON(t, router, http.MethodPost, "/api/v1/private/projects/"+project.ID+"/documents/"+document.ID+"/drafts/"+draftTwo.ID+"/submit", jwtToken, nil)
	versionTwo := decodeDocsDetail[docsResourceID](t, performDocsJSON(t, router, http.MethodPost, "/api/v1/private/projects/"+project.ID+"/documents/"+document.ID+"/drafts/"+draftTwo.ID+"/approve", jwtToken, nil))
	diff := decodeDocsDetail[docsDiff](t, performDocsJSON(t, router, http.MethodPost, "/api/v1/private/projects/"+project.ID+"/documents/"+document.ID+"/diffs", jwtToken, map[string]any{
		"from_version_id": versionOne.ID,
		"to_version_id":   versionTwo.ID,
	}))
	if diff.ID == "" || diff.Summary.AddedEndpoints == 0 {
		t.Fatalf("diff = %+v, want id and added endpoint count", diff)
	}
	performDocsJSON(t, router, http.MethodGet, "/api/v1/private/projects/"+project.ID+"/documents/"+document.ID+"/diffs/"+diff.ID+"/summary", jwtToken, nil)

	mcpToken := decodeDocsDetail[docsMCPToken](t, performDocsJSON(t, router, http.MethodPost, "/api/v1/private/mcp-tokens", jwtToken, map[string]any{
		"name":   "docs-agent",
		"scopes": []int{1, 2},
	}))
	if mcpToken.Token == "" {
		t.Fatal("create MCP token returned empty token")
	}
	toolsList := performDocsRPC(t, router, mcpToken.Token, map[string]any{"jsonrpc": "2.0", "id": "tools-list", "method": "tools/list"})
	if !bytes.Contains(toDocsJSON(t, toolsList.Result), []byte("get_endpoint_detail")) {
		t.Fatalf("tools/list result = %s, want get_endpoint_detail", string(toDocsJSON(t, toolsList.Result)))
	}
	toolCall := performDocsRPC(t, router, mcpToken.Token, map[string]any{
		"jsonrpc": "2.0",
		"id":      "endpoint-detail",
		"method":  "tools/call",
		"params": map[string]any{
			"name": "get_endpoint_detail",
			"arguments": map[string]any{
				"project_id":  project.ID,
				"document_id": document.ID,
				"version_id":  versionOne.ID,
				"endpoint_id": endpoint.ID,
			},
		},
	})
	if !bytes.Contains(toDocsJSON(t, toolCall.Result), []byte(endpoint.ID)) {
		t.Fatalf("tools/call result = %s, want endpoint id", string(toDocsJSON(t, toolCall.Result)))
	}
	t.Logf("documented curl sequence fixture passed: register/login, create project/document, upload draft, submit, approve, query endpoint, compare diff, create MCP token, MCP tools/list, MCP tools/call")
}

func newDocsSmokeRouter(t *testing.T) *gin.Engine {
	t.Helper()
	previousJWTKey := config.JWTKey
	previousJWTExpiration := config.JWTExpiration
	config.JWTKey = "docs-test-secret-key-for-task-14-32chars"
	config.JWTExpiration = time.Hour
	app.ResetDefaultStoreForTest()
	t.Cleanup(func() {
		config.JWTKey = previousJWTKey
		config.JWTExpiration = previousJWTExpiration
		app.ResetDefaultStoreForTest()
	})

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.TraceID())
	open.RegisterRoutes(router.Group("/api/v1/open"))
	private.RegisterRoutes(router.Group("/api/v1/private"))
	return router
}

func performDocsJSON(t *testing.T, router *gin.Engine, method string, path string, token string, body any) docsEnvelope {
	t.Helper()
	payload := []byte{}
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		payload = encoded
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(payload))
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set(middleware.AuthorizationHeader, token)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("%s %s HTTP status = %d body %s", method, path, recorder.Code, recorder.Body.String())
	}
	var envelope docsEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode %s %s envelope: %v body %s", method, path, err, recorder.Body.String())
	}
	if envelope.Code != 200 || envelope.Status != "OK" {
		t.Fatalf("%s %s envelope = code %d status %q body %s", method, path, envelope.Code, envelope.Status, recorder.Body.String())
	}
	return envelope
}

func performDocsRPC(t *testing.T, router *gin.Engine, token string, body any) docsRPCResponse {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal RPC body: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/open/mcp", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(middleware.AuthorizationHeader, token)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("MCP HTTP status = %d body %s", recorder.Code, recorder.Body.String())
	}
	var response docsRPCResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode MCP response: %v body %s", err, recorder.Body.String())
	}
	if response.Error != nil {
		t.Fatalf("MCP error = %+v body %s", response.Error, recorder.Body.String())
	}
	if response.JSONRPC != "2.0" || len(response.Result) == 0 {
		t.Fatalf("MCP response = %+v", response)
	}
	return response
}

func decodeDocsDetail[target any](t *testing.T, envelope docsEnvelope) target {
	t.Helper()
	var value target
	if len(envelope.Detail) == 0 {
		t.Fatalf("empty detail in envelope %+v", envelope)
	}
	if err := json.Unmarshal(envelope.Detail, &value); err != nil {
		t.Fatalf("decode detail into %T: %v body %s", value, err, string(envelope.Detail))
	}
	return value
}

func docsBranchID(t *testing.T, branches []docsBranch, name string) string {
	t.Helper()
	for _, branch := range branches {
		if branch.Name == name {
			if branch.ID == "" {
				t.Fatalf("branch %q has empty id", name)
			}
			return branch.ID
		}
	}
	t.Fatalf("branch %q not found in %+v", name, branches)
	return ""
}

func assertDocsSchema(t *testing.T, envelope docsEnvelope, kind string) {
	t.Helper()
	var schema struct {
		Kind    string `json:"content_kind"`
		Content string `json:"content"`
		Hash    string `json:"hash"`
	}
	if err := json.Unmarshal(envelope.Detail, &schema); err != nil {
		t.Fatalf("decode schema detail: %v", err)
	}
	if schema.Kind != kind || schema.Content == "" || schema.Hash == "" {
		t.Fatalf("schema detail = %+v, want kind %q with content and hash", schema, kind)
	}
}

func docsSmokeOpenAPI(version string, includeDetail bool) string {
	paths := map[string]any{
		"/pets": map[string]any{
			"get": map[string]any{
				"operationId": "listPets",
				"summary":     "List pets",
				"responses": map[string]any{
					"200": map[string]any{
						"description": "OK",
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{
									"type": "object",
									"properties": map[string]any{
										"items": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	if includeDetail {
		paths["/pets/{pet_id}"] = map[string]any{
			"get": map[string]any{
				"operationId": "getPet",
				"summary":     "Get pet",
				"parameters": []map[string]any{{
					"name":     "pet_id",
					"in":       "path",
					"required": true,
					"schema":   map[string]any{"type": "string"},
				}},
				"responses": map[string]any{"200": map[string]any{"description": "OK"}},
			},
		}
	}
	document := map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":   "Docs Pet API",
			"version": version,
		},
		"paths": paths,
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		panic(fmt.Sprintf("marshal docs OpenAPI fixture: %v", err))
	}
	return string(encoded)
}

func toDocsJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal value: %v", err)
	}
	return encoded
}

func readDocsMarkdown(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(body)
}

func parseDocsOpenAPI(t *testing.T) map[string]any {
	t.Helper()
	var root map[string]any
	if err := yaml.Unmarshal(apidocs.OpenAPIYAML(), &root); err != nil {
		t.Fatalf("parse openapi.yaml: %v", err)
	}
	return root
}

func asDocsMap(t *testing.T, value any, name string) map[string]any {
	t.Helper()
	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s has type %T, want map[string]any", name, value)
	}
	return result
}

func skipDocsLink(link string) bool {
	return strings.HasPrefix(link, "#") || strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://") || strings.HasPrefix(link, "mailto:")
}
