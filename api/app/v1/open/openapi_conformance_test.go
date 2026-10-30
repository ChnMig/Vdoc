package open_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.yaml.in/yaml/v3"

	"vdoc/api/app/v1/open"
	"vdoc/api/app/v1/private"
	apidocs "vdoc/docs/api"
)

var openAPIMethods = map[string]struct{}{
	"get":    {},
	"post":   {},
	"put":    {},
	"patch":  {},
	"delete": {},
}

var listRouteKeys = map[string]struct{}{
	"GET /api/v1/private/audit-logs":                                                                    {},
	"GET /api/v1/open/document-shares/{share_id}/versions":                                              {},
	"GET /api/v1/private/system/users":                                                                  {},
	"GET /api/v1/private/system/users/{user_id}/mcp-tokens":                                             {},
	"GET /api/v1/private/teams":                                                                         {},
	"GET /api/v1/private/projects":                                                                      {},
	"GET /api/v1/private/projects/{project_id}/members":                                                 {},
	"GET /api/v1/private/projects/{project_id}/member-candidates":                                       {},
	"GET /api/v1/private/projects/{project_id}/documents":                                               {},
	"GET /api/v1/private/projects/{project_id}/documents/{document_id}/activity":                        {},
	"GET /api/v1/private/projects/{project_id}/folders":                                                 {},
	"GET /api/v1/private/projects/{project_id}/documents/{document_id}/branches":                        {},
	"GET /api/v1/private/projects/{project_id}/documents/{document_id}/drafts":                          {},
	"GET /api/v1/private/projects/{project_id}/documents/{document_id}/shares":                          {},
	"GET /api/v1/private/projects/{project_id}/documents/{document_id}/versions":                        {},
	"GET /api/v1/private/projects/{project_id}/documents/{document_id}/diffs":                           {},
	"GET /api/v1/private/projects/{project_id}/documents/{document_id}/versions/{version_id}/endpoints": {},
	"GET /api/v1/private/mcp-tokens":                                                                    {},
	"GET /api/v1/private/mcp-usage":                                                                     {},
	"GET /api/v1/private/ai/prompts":                                                                    {},
	"GET /api/v1/private/projects/{project_id}/ai/prompts":                                              {},
	"GET /api/v1/private/projects/{project_id}/ai/chat-sessions":                                        {},
}

type requestBodyContract struct {
	required  bool
	schemaRef string
}

var requestBodyContracts = map[string]requestBodyContract{
	"POST /api/v1/open/auth/register":                                                                            {required: true, schemaRef: "#/components/schemas/AuthRequest"},
	"POST /api/v1/open/auth/login":                                                                               {required: true, schemaRef: "#/components/schemas/AuthRequest"},
	"POST /api/v1/open/mcp":                                                                                      {required: true, schemaRef: "#/components/schemas/MCPCallRequest"},
	"POST /api/v1/open/document-shares/{share_id}/unlock":                                                        {required: true, schemaRef: "#/components/schemas/DocumentShareUnlockRequest"},
	"POST /api/v1/private/identity/me/password":                                                                  {required: true, schemaRef: "#/components/schemas/ChangePasswordRequest"},
	"POST /api/v1/private/system/users":                                                                          {required: true, schemaRef: "#/components/schemas/CreateUserRequest"},
	"PATCH /api/v1/private/system/users/{user_id}":                                                               {required: true, schemaRef: "#/components/schemas/PatchUserRequest"},
	"POST /api/v1/private/teams":                                                                                 {required: true, schemaRef: "#/components/schemas/NameDescriptionRequest"},
	"PATCH /api/v1/private/teams/{team_id}":                                                                      {required: true, schemaRef: "#/components/schemas/PatchNameDescriptionRequest"},
	"POST /api/v1/private/projects":                                                                              {required: true, schemaRef: "#/components/schemas/CreateProjectRequest"},
	"PATCH /api/v1/private/projects/{project_id}":                                                                {required: true, schemaRef: "#/components/schemas/PatchNameDescriptionRequest"},
	"POST /api/v1/private/projects/{project_id}/members":                                                         {required: true, schemaRef: "#/components/schemas/AddProjectMemberRequest"},
	"PATCH /api/v1/private/projects/{project_id}/members/{user_id}/role":                                         {required: true, schemaRef: "#/components/schemas/PatchProjectMemberRoleRequest"},
	"POST /api/v1/private/projects/{project_id}/documents":                                                       {required: true, schemaRef: "#/components/schemas/DocumentRequest"},
	"POST /api/v1/private/projects/{project_id}/folders":                                                         {required: true, schemaRef: "#/components/schemas/CreateFolderRequest"},
	"PATCH /api/v1/private/projects/{project_id}/documents/{document_id}":                                        {required: true, schemaRef: "#/components/schemas/PatchDocumentRequest"},
	"POST /api/v1/private/projects/{project_id}/documents/{document_id}/shares":                                  {required: true, schemaRef: "#/components/schemas/CreateDocumentShareRequest"},
	"POST /api/v1/private/projects/{project_id}/documents/{document_id}/branches":                                {required: true, schemaRef: "#/components/schemas/CreateBranchRequest"},
	"PATCH /api/v1/private/projects/{project_id}/documents/{document_id}/branches/{branch_id}":                   {required: true, schemaRef: "#/components/schemas/PatchBranchRequest"},
	"POST /api/v1/private/projects/{project_id}/documents/{document_id}/drafts":                                  {required: true, schemaRef: "#/components/schemas/DraftRequest"},
	"PATCH /api/v1/private/projects/{project_id}/documents/{document_id}/drafts/{draft_id}":                      {required: true, schemaRef: "#/components/schemas/PatchDraftRequest"},
	"POST /api/v1/private/projects/{project_id}/documents/{document_id}/drafts/{draft_id}/approve":               {required: true, schemaRef: "#/components/schemas/ReviewRequest"},
	"POST /api/v1/private/projects/{project_id}/documents/{document_id}/drafts/{draft_id}/request-changes":       {required: true, schemaRef: "#/components/schemas/ReviewRequest"},
	"POST /api/v1/private/projects/{project_id}/documents/{document_id}/drafts/{draft_id}/reject":                {required: true, schemaRef: "#/components/schemas/ReviewRequest"},
	"POST /api/v1/private/projects/{project_id}/documents/{document_id}/drafts/promote":                          {required: true, schemaRef: "#/components/schemas/PromoteDraftRequest"},
	"POST /api/v1/private/projects/{project_id}/documents/{document_id}/diffs":                                   {required: true, schemaRef: "#/components/schemas/CreateDiffRequest"},
	"PUT /api/v1/private/ai/provider":                                                                            {required: true, schemaRef: "#/components/schemas/AIProviderRequest"},
	"POST /api/v1/private/ai/provider/test":                                                                      {required: false, schemaRef: "#/components/schemas/AIProviderRequest"},
	"POST /api/v1/private/ai/provider/models":                                                                    {required: false, schemaRef: "#/components/schemas/AIProviderModelsRequest"},
	"PUT /api/v1/private/projects/{project_id}/ai/provider":                                                      {required: true, schemaRef: "#/components/schemas/AIProviderRequest"},
	"POST /api/v1/private/projects/{project_id}/ai/provider/test":                                                {required: false, schemaRef: "#/components/schemas/AIProviderRequest"},
	"POST /api/v1/private/projects/{project_id}/ai/provider/models":                                              {required: false, schemaRef: "#/components/schemas/AIProviderModelsRequest"},
	"PUT /api/v1/private/ai/prompts/{prompt_key}":                                                                {required: true, schemaRef: "#/components/schemas/AIPromptRequest"},
	"PUT /api/v1/private/projects/{project_id}/ai/prompts/{prompt_key}":                                          {required: true, schemaRef: "#/components/schemas/AIPromptRequest"},
	"PATCH /api/v1/private/projects/{project_id}/ai/chat-sessions/{session_id}":                                  {required: true, schemaRef: "#/components/schemas/AIChatReasoningRequest"},
	"POST /api/v1/private/projects/{project_id}/ai/chat-sessions":                                                {required: true, schemaRef: "#/components/schemas/AIChatSessionRequest"},
	"POST /api/v1/private/projects/{project_id}/ai/chat-sessions/{session_id}/messages":                          {required: true, schemaRef: "#/components/schemas/AIChatMessageRequest"},
	"POST /api/v1/private/mcp-tokens":                                                                            {required: true, schemaRef: "#/components/schemas/CreateMCPTokenRequest"},
	"POST /api/v1/private/projects/{project_id}/repository/ai/analyses":                                          {required: true, schemaRef: "#/components/schemas/RepositoryAnalysisRequest"},
	"POST /api/v1/private/projects/{project_id}/repository/branches":                                             {required: true, schemaRef: "#/components/schemas/RepositoryBranchInput"},
	"POST /api/v1/private/projects/{project_id}/repository/branches/{branch_id}/commits":                         {required: true, schemaRef: "#/components/schemas/RepositorySaveInput"},
	"POST /api/v1/private/projects/{project_id}/repository/branches/{branch_id}/reverts":                         {required: true, schemaRef: "#/components/schemas/RepositoryRevertInput"},
	"POST /api/v1/private/projects/{project_id}/repository/comparisons":                                          {required: true, schemaRef: "#/components/schemas/RepositoryCompareInput"},
	"POST /api/v1/private/projects/{project_id}/repository/merge-requests":                                       {required: true, schemaRef: "#/components/schemas/RepositoryMergeInput"},
	"POST /api/v1/private/projects/{project_id}/repository/merge-requests/{request_id}/resolve":                  {required: true, schemaRef: "#/components/schemas/RepositoryResolveInput"},
	"POST /api/v1/private/projects/{project_id}/repository/releases":                                             {required: true, schemaRef: "#/components/schemas/RepositoryPublishInput"},
	"POST /api/v1/private/projects/{project_id}/repository/releases/{release_id}/documents/{document_id}/shares": {required: true, schemaRef: "#/components/schemas/RepositoryShareInput"},
	"POST /api/v1/private/projects/{project_id}/repository/subscriptions":                                        {required: true, schemaRef: "#/components/schemas/RepositorySubscriptionRequest"},
}

var repositoryResponseSchemas = map[string]string{
	"GET /api/v1/private/projects/{project_id}/repository/ai/analyses":                                                             "#/components/schemas/RepositoryAnalysesEnvelope",
	"GET /api/v1/private/projects/{project_id}/repository/ai/analyses/{analysis_id}":                                               "#/components/schemas/RepositoryAnalysisEnvelope",
	"GET /api/v1/private/projects/{project_id}/repository/branches":                                                                "#/components/schemas/RepositoryBranchListEnvelope",
	"GET /api/v1/private/projects/{project_id}/repository/commits":                                                                 "#/components/schemas/RepositoryHistoryEnvelope",
	"GET /api/v1/private/projects/{project_id}/repository/commits/{commit_id}":                                                     "#/components/schemas/RepositoryCommitEnvelope",
	"GET /api/v1/private/projects/{project_id}/repository/commits/{commit_id}/documents/{document_id}/content":                     "#/components/schemas/RepositoryContentEnvelope",
	"GET /api/v1/private/projects/{project_id}/repository/event-deliveries":                                                        "#/components/schemas/RepositoryDeliveriesEnvelope",
	"GET /api/v1/private/projects/{project_id}/repository/events":                                                                  "#/components/schemas/RepositoryEventListEnvelope",
	"GET /api/v1/private/projects/{project_id}/repository/merge-requests":                                                          "#/components/schemas/RepositoryMergeRequestListEnvelope",
	"GET /api/v1/private/projects/{project_id}/repository/merge-requests/{request_id}/documents/{document_id}/content":             "#/components/schemas/RepositoryMergeContentEnvelope",
	"GET /api/v1/private/projects/{project_id}/repository/releases":                                                                "#/components/schemas/RepositoryReleaseListEnvelope",
	"GET /api/v1/private/projects/{project_id}/repository/releases/{release_id}/documents/{document_id}/shares":                    "#/components/schemas/RepositoryShareListEnvelope",
	"GET /api/v1/private/projects/{project_id}/repository/subscriptions":                                                           "#/components/schemas/RepositorySubscriptionsEnvelope",
	"POST /api/v1/private/projects/{project_id}/repository/ai/analyses":                                                            "#/components/schemas/RepositoryAnalysisEnvelope",
	"POST /api/v1/private/projects/{project_id}/repository/bootstrap":                                                              "#/components/schemas/RepositoryBranchListEnvelope",
	"POST /api/v1/private/projects/{project_id}/repository/branches":                                                               "#/components/schemas/RepositoryBranchEnvelope",
	"POST /api/v1/private/projects/{project_id}/repository/branches/{branch_id}/commits":                                           "#/components/schemas/RepositoryCommitEnvelope",
	"POST /api/v1/private/projects/{project_id}/repository/branches/{branch_id}/reverts":                                           "#/components/schemas/RepositoryCommitEnvelope",
	"POST /api/v1/private/projects/{project_id}/repository/comparisons":                                                            "#/components/schemas/RepositoryComparisonEnvelope",
	"POST /api/v1/private/projects/{project_id}/repository/event-deliveries/{delivery_id}/retry":                                   "#/components/schemas/RepositoryDeliveryEnvelope",
	"POST /api/v1/private/projects/{project_id}/repository/merge-requests":                                                         "#/components/schemas/RepositoryMergeRequestEnvelope",
	"POST /api/v1/private/projects/{project_id}/repository/merge-requests/{request_id}/approve":                                    "#/components/schemas/RepositoryMergeRequestEnvelope",
	"POST /api/v1/private/projects/{project_id}/repository/merge-requests/{request_id}/merge":                                      "#/components/schemas/RepositoryCommitEnvelope",
	"POST /api/v1/private/projects/{project_id}/repository/merge-requests/{request_id}/resolve":                                    "#/components/schemas/RepositoryMergeRequestEnvelope",
	"POST /api/v1/private/projects/{project_id}/repository/releases":                                                               "#/components/schemas/RepositoryReleaseEnvelope",
	"POST /api/v1/private/projects/{project_id}/repository/releases/{release_id}/documents/{document_id}/shares":                   "#/components/schemas/RepositoryShareSecretEnvelope",
	"POST /api/v1/private/projects/{project_id}/repository/releases/{release_id}/documents/{document_id}/shares/{share_id}/reveal": "#/components/schemas/RepositoryShareSecretEnvelope",
	"POST /api/v1/private/projects/{project_id}/repository/releases/{release_id}/documents/{document_id}/shares/{share_id}/revoke": "#/components/schemas/RepositoryShareEnvelope",
	"POST /api/v1/private/projects/{project_id}/repository/subscriptions":                                                          "#/components/schemas/RepositoryCreatedSubscriptionEnvelope",
	"POST /api/v1/private/projects/{project_id}/repository/subscriptions/{subscription_id}/pause":                                  "#/components/schemas/RepositorySubscriptionEnvelope",
	"POST /api/v1/private/projects/{project_id}/repository/subscriptions/{subscription_id}/resume":                                 "#/components/schemas/RepositorySubscriptionEnvelope",
}

var requiredMCPTools = []string{
	"list_projects",
	"list_documents",
	"list_document_branches",
	"list_api_endpoints",
	"list_api_versions",
	"list_doc_versions",
	"get_latest_schema",
	"get_schema_version",
	"get_endpoint_detail",
	"compare_api_versions",
	"get_change_summary",
	"create_api_version_draft",
	"update_api_version_draft",
	"submit_api_version_draft",
	"get_api_version_draft",
	"get_latest_doc",
	"get_doc_version",
	"compare_doc_versions",
	"create_doc_draft",
	"update_doc_draft",
	"submit_doc_draft",
	"get_doc_draft",
	"resolve_ref",
	"list_branches",
	"create_branch",
	"list_commits",
	"get_commit",
	"get_document_at_commit",
	"commit_changes",
	"compare_commits",
	"list_merge_requests",
	"create_merge_request",
	"list_releases",
	"get_release",
	"analyze_changes",
	"list_repository_events",
}

func TestOpenAPISpecRouteServesCheckedInSpec(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	open.RegisterRoutes(router.Group("/api/v1/open"))

	request := httptest.NewRequest(http.MethodGet, "/api/v1/open/docs/openapi.yaml", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("GET openapi.yaml status = %d", recorder.Code)
	}
	if contentType := strings.ToLower(recorder.Header().Get("Content-Type")); !strings.Contains(contentType, "yaml") {
		t.Fatalf("Content-Type = %q, want yaml", contentType)
	}
	want, err := os.ReadFile(openAPISpecPath(t))
	if err != nil {
		t.Fatalf("ReadFile(openapi.yaml) error = %v", err)
	}
	if !bytes.Equal(recorder.Body.Bytes(), want) {
		t.Fatalf("served spec differs from checked-in docs/api/openapi.yaml")
	}
}

func TestOpenAPISpecMatchesRegisteredRoutes(t *testing.T) {
	root := parseOpenAPI(t)
	registered := registeredAPIRouteSet()
	specRoutes, operations := specRouteSet(t)

	if missing := sortedDifference(registered, specRoutes); len(missing) > 0 {
		t.Fatalf("registered routes missing from OpenAPI spec: %s", strings.Join(missing, ", "))
	}
	if extra := sortedDifference(specRoutes, registered); len(extra) > 0 {
		t.Fatalf("OpenAPI spec routes not registered in Gin: %s", strings.Join(extra, ", "))
	}

	for key := range registered {
		method, path := splitRouteKey(key)
		op := operations[path][strings.ToLower(method)]
		if strings.HasPrefix(path, "/api/v1/private/") && !operationHasSecurity(op, "JWTAuth") {
			t.Fatalf("%s must declare JWTAuth security", key)
		}
		if key == "POST /api/v1/open/mcp" && !operationHasSecurity(op, "MCPTokenAuth") {
			t.Fatalf("%s must declare MCPTokenAuth security", key)
		}

		if schema, ok := repositoryResponseSchemas[key]; ok {
			assertRepositoryResponseEnvelope(t, root, op, key, schema)
			continue
		}

		response := responseRef(op)
		switch key {
		case "GET /api/v1/open/docs/openapi.yaml":
			if response != "#/components/responses/OpenAPISpecResponse" {
				t.Fatalf("%s response = %q", key, response)
			}
		case "GET /api/v1/open/document-shares/{share_id}/versions/{version_id}/download":
			if response != "#/components/responses/PublicDocumentDownloadResponse" {
				t.Fatalf("%s response = %q", key, response)
			}
		case "POST /api/v1/open/mcp":
			if response != "#/components/responses/MCPJSONRPCResponse" {
				t.Fatalf("%s response = %q", key, response)
			}
		case "POST /api/v1/private/projects/{project_id}/ai/chat-sessions/{session_id}/messages":
			if response != "#/components/responses/AIChatMessageResponse" {
				t.Fatalf("%s response = %q, want JSON/SSE chat response", key, response)
			}
			root := parseOpenAPI(t)
			components := asMap(t, root["components"], "components")
			responses := asMap(t, components["responses"], "responses")
			chat := asMap(t, responses["AIChatMessageResponse"], "AIChatMessageResponse")
			content := asMap(t, chat["content"], "chat content")
			jsonContent := asMap(t, content["application/json"], "JSON content")
			jsonSchema := asMap(t, jsonContent["schema"], "JSON schema")
			streamContent := asMap(t, content["text/event-stream"], "SSE content")
			streamSchema := asMap(t, streamContent["schema"], "SSE schema")
			if jsonSchema["$ref"] != "#/components/schemas/VdocEnvelope" || streamSchema["type"] != "string" {
				t.Fatal("chat must document both the original JSON envelope and SSE")
			}
		default:
			if _, ok := listRouteKeys[key]; ok {
				if response != "#/components/responses/VdocListEnvelopeResponse" {
					t.Fatalf("%s response = %q, want list envelope", key, response)
				}
				continue
			}
			if response != "#/components/responses/VdocEnvelopeResponse" {
				t.Fatalf("%s response = %q, want Vdoc envelope", key, response)
			}
		}
	}

	assertMCPToolEnum(t)
	assertRequestBodyContracts(t, operations)
	assertOpenAPICurrentDescriptions(t)
}

func registeredAPIRouteSet() map[string]struct{} {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	open.RegisterRoutes(router.Group("/api/v1/open"))
	private.RegisterRoutes(router.Group("/api/v1/private"))

	routes := make(map[string]struct{})
	for _, route := range router.Routes() {
		if strings.HasPrefix(route.Path, "/api/v1/open/") || strings.HasPrefix(route.Path, "/api/v1/private/") {
			routes[route.Method+" "+ginPathToOpenAPI(route.Path)] = struct{}{}
		}
	}
	return routes
}

func specRouteSet(t *testing.T) (map[string]struct{}, map[string]map[string]map[string]any) {
	t.Helper()
	root := parseOpenAPI(t)
	paths := asMap(t, root["paths"], "paths")
	routes := make(map[string]struct{})
	operations := make(map[string]map[string]map[string]any)
	for path, pathValue := range paths {
		pathItem := asMap(t, pathValue, path)
		for method, operationValue := range pathItem {
			if _, ok := openAPIMethods[method]; !ok {
				continue
			}
			operation := asMap(t, operationValue, path+" "+method)
			routes[strings.ToUpper(method)+" "+path] = struct{}{}
			if operations[path] == nil {
				operations[path] = make(map[string]map[string]any)
			}
			operations[path][method] = operation
		}
	}
	return routes, operations
}

func assertMCPToolEnum(t *testing.T) {
	t.Helper()
	root := parseOpenAPI(t)
	components := asMap(t, root["components"], "components")
	schemas := asMap(t, components["schemas"], "components.schemas")
	toolName := asMap(t, schemas["MCPToolName"], "components.schemas.MCPToolName")
	rawEnum, ok := toolName["enum"].([]any)
	if !ok {
		t.Fatalf("MCPToolName enum missing")
	}
	found := make(map[string]struct{}, len(rawEnum))
	for _, item := range rawEnum {
		name, ok := item.(string)
		if !ok {
			t.Fatalf("MCPToolName enum contains non-string value %T", item)
		}
		found[name] = struct{}{}
	}
	for _, name := range requiredMCPTools {
		if _, ok := found[name]; !ok {
			t.Fatalf("MCPToolName enum missing %q", name)
		}
	}
	for name := range found {
		if strings.HasPrefix(name, "publish_") {
			t.Fatalf("MCPToolName enum must not expose direct publish tool %q", name)
		}
	}
	if len(found) != len(requiredMCPTools) {
		t.Fatalf("MCPToolName enum count = %d, want %d", len(found), len(requiredMCPTools))
	}
}

func assertRequestBodyContracts(t *testing.T, operations map[string]map[string]map[string]any) {
	t.Helper()
	if len(requestBodyContracts) != 48 {
		t.Fatalf("request body route contract count = %d, want 48", len(requestBodyContracts))
	}

	found := make(map[string]struct{}, len(requestBodyContracts))
	for path, methods := range operations {
		for method, operation := range methods {
			key := strings.ToUpper(method) + " " + path
			bodyValue, hasBody := operation["requestBody"]
			contract, expectedBody := requestBodyContracts[key]
			if !expectedBody {
				if hasBody {
					t.Fatalf("%s declares an untracked requestBody", key)
				}
				continue
			}
			if !hasBody {
				t.Fatalf("%s missing requestBody", key)
			}

			body := asMap(t, bodyValue, key+" requestBody")
			actualRequired, ok := body["required"].(bool)
			if !ok {
				t.Fatalf("%s requestBody.required has type %T, want bool", key, body["required"])
			}
			if actualRequired != contract.required {
				t.Fatalf("%s requestBody.required = %t, want %t", key, actualRequired, contract.required)
			}

			content := asMap(t, body["content"], key+" requestBody.content")
			jsonMedia := asMap(t, content["application/json"], key+" requestBody.content.application/json")
			schema := asMap(t, jsonMedia["schema"], key+" requestBody.content.application/json.schema")
			if ref, _ := schema["$ref"].(string); ref != contract.schemaRef {
				t.Fatalf("%s JSON requestBody schema ref = %q, want %q", key, ref, contract.schemaRef)
			}
			found[key] = struct{}{}
		}
	}

	for key := range requestBodyContracts {
		if _, ok := found[key]; !ok {
			t.Fatalf("request body route contract not found in OpenAPI operations: %s", key)
		}
	}
}

func assertOpenAPICurrentDescriptions(t *testing.T) {
	t.Helper()
	body, err := os.ReadFile(openAPISpecPath(t))
	if err != nil {
		t.Fatalf("read openapi.yaml: %v", err)
	}
	document := string(body)
	requiredPhrases := []string{
		"Projects own typed Documents",
		"document_type 1 is OpenAPI and 2 is Markdown",
		"relative_path is the stored project-local path identity",
		"owner can reveal an active token again",
	}
	for _, phrase := range requiredPhrases {
		if !strings.Contains(document, phrase) {
			t.Fatalf("openapi.yaml missing current description %q", phrase)
		}
	}
}

func TestOpenAPIDocumentLocationSchemas(t *testing.T) {
	root := parseOpenAPI(t)
	components := asMap(t, root["components"], "components")
	schemas := asMap(t, components["schemas"], "components.schemas")
	for schemaName, fields := range map[string][]string{
		"CreateFolderRequest":  {"name", "parent_id"},
		"FolderDTO":            {"id", "project_id", "parent_id", "name", "path", "created_by", "created_at", "updated_at"},
		"DocumentRequest":      {"name", "document_type", "relative_path", "file_name", "folder_id", "initial_content"},
		"PatchDocumentRequest": {"name", "relative_path", "file_name", "folder_id"},
		"DocumentDTO":          {"folder_id", "file_name", "workflow_state", "has_draft", "latest_version_id", "latest_draft_id"},
		"ProjectDTO":           {"team_name"},
	} {
		schema := asMap(t, schemas[schemaName], schemaName)
		properties := asMap(t, schema["properties"], schemaName+".properties")
		for _, field := range fields {
			if _, ok := properties[field]; !ok {
				t.Errorf("%s missing %s", schemaName, field)
			}
		}
	}
	initial := asMap(t, schemas["InitialDocumentContent"], "InitialDocumentContent")
	required, ok := initial["required"].([]any)
	if !ok || len(required) != 1 || required[0] != "content" {
		t.Fatalf("initial upload required fields = %#v, want content", initial["required"])
	}
	properties := asMap(t, initial["properties"], "InitialDocumentContent.properties")
	content := asMap(t, properties["content"], "InitialDocumentContent.content")
	if content["type"] != "string" {
		t.Fatalf("initial upload content type = %#v", content["type"])
	}
	version := asMap(t, properties["version_name"], "InitialDocumentContent.version_name")
	if version["default"] != "0.1.0" {
		t.Fatalf("initial upload version default = %#v", version["default"])
	}
	for _, schemaName := range []string{"FolderDTO", "DocumentDTO", "PatchDocumentRequest"} {
		schema := asMap(t, schemas[schemaName], schemaName)
		properties := asMap(t, schema["properties"], schemaName+".properties")
		field := "folder_id"
		if schemaName == "FolderDTO" {
			field = "parent_id"
		}
		location := asMap(t, properties[field], schemaName+"."+field)
		types, ok := location["type"].([]any)
		if !ok || len(types) != 2 || types[0] != "string" || types[1] != "null" {
			t.Errorf("%s.%s type = %#v, want nullable string", schemaName, field, location["type"])
		}
	}
}

func parseOpenAPI(t *testing.T) map[string]any {
	t.Helper()
	var root map[string]any
	if err := yaml.Unmarshal(apidocs.OpenAPIYAML(), &root); err != nil {
		t.Fatalf("yaml.Unmarshal(openapi.yaml) error = %v", err)
	}
	return root
}

func asMap(t *testing.T, value any, name string) map[string]any {
	t.Helper()
	mapped, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s has type %T, want map", name, value)
	}
	return mapped
}

// 新仓库接口保留明确的 detail 类型，同时验证它仍继承统一响应信封。
func assertRepositoryResponseEnvelope(t *testing.T, root, operation map[string]any, key, expected string) {
	t.Helper()
	responses := asMap(t, operation["responses"], key+" responses")
	response := asMap(t, responses["200"], key+" response")
	content := asMap(t, response["content"], key+" content")
	media := asMap(t, content["application/json"], key+" JSON")
	schema := asMap(t, media["schema"], key+" schema")
	if schema["$ref"] != expected {
		t.Fatalf("%s response schema = %v, want %s", key, schema["$ref"], expected)
	}
	components := asMap(t, root["components"], "components")
	schemas := asMap(t, components["schemas"], "schemas")
	envelope := asMap(t, schemas[strings.TrimPrefix(expected, "#/components/schemas/")], expected)
	parts, ok := envelope["allOf"].([]any)
	if !ok || len(parts) != 2 {
		t.Fatalf("%s must combine VdocEnvelope with a typed detail", expected)
	}
	base := asMap(t, parts[0], expected+" base")
	if base["$ref"] != "#/components/schemas/VdocEnvelope" {
		t.Fatalf("%s does not inherit the Vdoc envelope", expected)
	}
	detail := asMap(t, parts[1], expected+" detail object")
	properties := asMap(t, detail["properties"], expected+" properties")
	asMap(t, properties["detail"], expected+" detail")
}

func responseRef(operation map[string]any) string {
	responses, ok := operation["responses"].(map[string]any)
	if !ok {
		return ""
	}
	response, ok := responses["200"].(map[string]any)
	if !ok {
		return ""
	}
	ref, _ := response["$ref"].(string)
	return ref
}

func operationHasSecurity(operation map[string]any, name string) bool {
	security, ok := operation["security"].([]any)
	if !ok {
		return false
	}
	for _, item := range security {
		securityItem, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if _, ok := securityItem[name]; ok {
			return true
		}
	}
	return false
}

func ginPathToOpenAPI(path string) string {
	parts := strings.Split(path, "/")
	for index, part := range parts {
		if param, ok := strings.CutPrefix(part, ":"); ok {
			parts[index] = "{" + param + "}"
		}
	}
	return strings.Join(parts, "/")
}

func sortedDifference(left, right map[string]struct{}) []string {
	var diff []string
	for item := range left {
		if _, ok := right[item]; !ok {
			diff = append(diff, item)
		}
	}
	sort.Strings(diff)
	return diff
}

func splitRouteKey(key string) (string, string) {
	parts := strings.SplitN(key, " ", 2)
	if len(parts) != 2 {
		panic(fmt.Sprintf("invalid route key %q", key))
	}
	return parts[0], parts[1]
}

func openAPISpecPath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "../../../../docs/api/openapi.yaml")
}
