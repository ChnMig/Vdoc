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
	"GET /api/v1/private/system/users":                                                                 {},
	"GET /api/v1/private/system/users/{user_id}/mcp-tokens":                                            {},
	"GET /api/v1/private/teams":                                                                        {},
	"GET /api/v1/private/projects":                                                                     {},
	"GET /api/v1/private/projects/{project_id}/members":                                                {},
	"GET /api/v1/private/projects/{project_id}/services":                                               {},
	"GET /api/v1/private/projects/{project_id}/services/{service_id}/branches":                         {},
	"GET /api/v1/private/projects/{project_id}/services/{service_id}/contract-drafts":                  {},
	"GET /api/v1/private/projects/{project_id}/services/{service_id}/contracts":                        {},
	"GET /api/v1/private/projects/{project_id}/services/{service_id}/contracts/{version_id}/endpoints": {},
	"GET /api/v1/private/mcp-tokens":                                                                   {},
}

var requiredMCPTools = []string{
	"list_projects",
	"list_services",
	"list_api_versions",
	"get_latest_schema",
	"get_endpoint_detail",
	"compare_api_versions",
	"get_change_summary",
	"create_api_version_draft",
	"update_api_version_draft",
	"submit_api_version_draft",
	"get_api_version_draft",
}

func TestOpenAPIDocumentRouteServesCheckedInSpec(t *testing.T) {
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

		response := responseRef(op)
		switch key {
		case "GET /api/v1/open/docs/openapi.yaml":
			if response != "#/components/responses/OpenAPISpecResponse" {
				t.Fatalf("%s response = %q", key, response)
			}
		case "POST /api/v1/open/mcp":
			if response != "#/components/responses/MCPJSONRPCResponse" {
				t.Fatalf("%s response = %q", key, response)
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
	for _, name := range []string{"publish_api_schema", "publish_api_version"} {
		if _, ok := found[name]; ok {
			t.Fatalf("MCPToolName enum must not expose %q", name)
		}
	}
	if len(found) != len(requiredMCPTools) {
		t.Fatalf("MCPToolName enum count = %d, want %d", len(found), len(requiredMCPTools))
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
