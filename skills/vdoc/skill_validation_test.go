package vdocskill

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var requiredSkillFiles = []string{
	"skills/vdoc/SKILL.md",
	"skills/vdoc/templates/frontend-change-summary.md",
	"skills/vdoc/templates/endpoint-integration.md",
	"skills/vdoc/examples/compare-versions-example.md",
	"skills/vdoc/examples/endpoint-query-example.md",
}

var v01ToolSchemas = map[string]toolSchema{
	"list_projects":            {required: nil, optional: nil},
	"list_services":            {required: []string{"project_id"}, optional: nil},
	"list_api_versions":        {required: []string{"project_id", "service_id"}, optional: nil},
	"get_latest_schema":        {required: []string{"project_id", "service_id"}, optional: []string{"branch_id"}},
	"get_endpoint_detail":      {required: []string{"project_id", "service_id", "version_id", "endpoint_id"}, optional: nil},
	"compare_api_versions":     {required: []string{"project_id", "service_id", "from_version_id", "to_version_id"}, optional: nil},
	"get_change_summary":       {required: []string{"project_id", "service_id", "diff_id"}, optional: nil},
	"create_api_version_draft": {required: []string{"project_id", "service_id", "branch_id", "version_name", "schema_content"}, optional: []string{"changelog", "source_git_commit_id"}},
	"update_api_version_draft": {required: []string{"project_id", "service_id", "draft_id", "branch_id", "version_name", "schema_content"}, optional: []string{"changelog", "source_git_commit_id"}},
	"submit_api_version_draft": {required: []string{"project_id", "service_id", "draft_id"}, optional: nil},
	"get_api_version_draft":    {required: []string{"project_id", "service_id", "draft_id"}, optional: nil},
}

type toolSchema struct {
	required []string
	optional []string
}

func TestVdocSkillCompleteness(t *testing.T) {
	combined := strings.Builder{}
	for _, path := range requiredSkillFiles {
		body := readRootFile(t, path)
		if strings.TrimSpace(body) == "" {
			t.Fatalf("%s is empty", path)
		}
		combined.WriteString("\n")
		combined.WriteString(body)
	}

	skill := readRootFile(t, "skills/vdoc/SKILL.md")
	requiredPhrases := []string{
		"Vdoc MCP is the source of truth for API contract facts",
		"Do not infer or hallucinate endpoint fields, parameters, response properties, enum values, auth schemes, servers, or breaking-change claims",
		"Always call JSON-RPC `tools/list` if unsure",
		"You must call `get_endpoint_detail` before generating endpoint integration code or client types",
		"You must call `compare_api_versions` before migration advice or frontend impact analysis",
		"draft tools only",
		"Human Admin/SuperAdmin review publishes versions",
		"Output must distinguish `must_handle` / breaking changes from optional/non-breaking changes",
		"Never print, copy, or log MCP tokens or JWTs",
		"Never include Authorization headers in final output",
		"Forbidden/unavailable v0.1 tools: `publish_api_schema`, `publish_api_version`",
	}
	for _, phrase := range requiredPhrases {
		if !strings.Contains(skill, phrase) {
			t.Fatalf("SKILL.md missing required phrase %q", phrase)
		}
	}

	allDocs := combined.String()
	for toolName := range v01ToolSchemas {
		if !strings.Contains(allDocs, toolName) {
			t.Fatalf("skill docs missing v0.1 tool name %q", toolName)
		}
	}

	assertPublishToolsOnlyForbidden(t, allDocs)
	assertTemplateTerms(t, "skills/vdoc/templates/frontend-change-summary.md", []string{"must_handle", "is_breaking", "breaking", "optional", "non-breaking", "location", "message", "old_value", "new_value", "frontend_impact"})
	assertTemplateTerms(t, "skills/vdoc/templates/endpoint-integration.md", []string{"get_endpoint_detail", "method", "path", "operationId", "parameters", "request body", "responses", "security", "servers", "required fields", "enum values"})

	evidence := []string{
		"PASS TestVdocSkillCompleteness",
		fmt.Sprintf("files_checked=%d", len(requiredSkillFiles)),
		fmt.Sprintf("required_phrases_checked=%d", len(requiredPhrases)),
		fmt.Sprintf("v01_tools_checked=%d", len(v01ToolSchemas)),
		"publish_tools_status=forbidden_unavailable_only",
	}
	writeEvidence(t, "task-15-skill-completeness.txt", strings.Join(evidence, "\n")+"\n")
}

func TestVdocSkillExampleConsistency(t *testing.T) {
	paths := []string{
		"skills/vdoc/SKILL.md",
		"skills/vdoc/examples/compare-versions-example.md",
		"skills/vdoc/examples/endpoint-query-example.md",
	}
	seenTools := map[string]int{}
	checkedPayloads := 0
	for _, path := range paths {
		body := readRootFile(t, path)
		if strings.Contains(body, "Authorization:") {
			t.Fatalf("%s must not include Authorization header examples", path)
		}
		payloads := extractJSONPayloads(t, path, body)
		for index, payload := range payloads {
			checkedPayloads++
			toolName := validateJSONRPCPayload(t, path, index, payload)
			if toolName != "" {
				seenTools[toolName]++
			}
		}
	}

	requiredExampleTools := []string{"compare_api_versions", "get_change_summary", "get_endpoint_detail", "create_api_version_draft", "update_api_version_draft", "submit_api_version_draft"}
	for _, toolName := range requiredExampleTools {
		if seenTools[toolName] == 0 {
			t.Fatalf("examples missing tools/call payload for %s", toolName)
		}
	}
	if checkedPayloads == 0 {
		t.Fatal("no JSON-RPC payloads found in skill examples")
	}

	toolNames := make([]string, 0, len(seenTools))
	for toolName := range seenTools {
		toolNames = append(toolNames, toolName)
	}
	sort.Strings(toolNames)
	evidence := []string{
		"PASS TestVdocSkillExampleConsistency",
		fmt.Sprintf("json_rpc_payloads_checked=%d", checkedPayloads),
		"tools_call_seen=" + strings.Join(toolNames, ","),
		"forbidden_tools_seen=0",
	}
	writeEvidence(t, "task-15-skill-examples.txt", strings.Join(evidence, "\n")+"\n")
}

func readRootFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(repoRoot(t), path))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(body)
}

func repoRoot(t *testing.T) string {
	t.Helper()
	return filepath.Clean(filepath.Join("..", ".."))
}

func assertPublishToolsOnlyForbidden(t *testing.T, body string) {
	t.Helper()
	for lineNumber, line := range strings.Split(body, "\n") {
		if !strings.Contains(line, "publish_api_schema") && !strings.Contains(line, "publish_api_version") {
			continue
		}
		lower := strings.ToLower(line)
		if !strings.Contains(lower, "forbidden/unavailable") && !strings.Contains(lower, "not available") && !strings.Contains(lower, "do not call") && !strings.Contains(lower, "no direct publish") {
			t.Fatalf("publish tool mention on combined line %d is not explicitly forbidden/unavailable: %s", lineNumber+1, line)
		}
	}
}

func assertTemplateTerms(t *testing.T, path string, terms []string) {
	t.Helper()
	body := readRootFile(t, path)
	for _, term := range terms {
		if !strings.Contains(body, term) {
			t.Fatalf("%s missing %q", path, term)
		}
	}
}

func extractJSONPayloads(t *testing.T, path string, body string) []map[string]any {
	t.Helper()
	fencePattern := regexp.MustCompile("(?s)```json\\s*(.*?)\\s*```")
	matches := fencePattern.FindAllStringSubmatch(body, -1)
	payloads := make([]map[string]any, 0, len(matches))
	for index, match := range matches {
		var payload map[string]any
		if err := json.Unmarshal([]byte(match[1]), &payload); err != nil {
			t.Fatalf("parse JSON fence %d in %s: %v", index+1, path, err)
		}
		payloads = append(payloads, payload)
	}
	return payloads
}

func validateJSONRPCPayload(t *testing.T, path string, index int, payload map[string]any) string {
	t.Helper()
	if payload["jsonrpc"] != "2.0" {
		t.Fatalf("%s JSON payload %d jsonrpc = %#v, want 2.0", path, index+1, payload["jsonrpc"])
	}
	switch payload["id"].(type) {
	case string, float64:
		// JSON-RPC id can be a string or number in the v0.1 OpenAPI schema.
	default:
		t.Fatalf("%s JSON payload %d has invalid id type %T", path, index+1, payload["id"])
	}
	method, ok := payload["method"].(string)
	if !ok {
		t.Fatalf("%s JSON payload %d method has type %T", path, index+1, payload["method"])
	}
	if method == "tools/list" {
		if params, ok := payload["params"].(map[string]any); ok && len(params) > 0 {
			t.Fatalf("%s JSON payload %d tools/list params must be empty when present", path, index+1)
		}
		return ""
	}
	if method != "tools/call" {
		t.Fatalf("%s JSON payload %d method = %q, want tools/list or tools/call", path, index+1, method)
	}
	params, ok := payload["params"].(map[string]any)
	if !ok {
		t.Fatalf("%s JSON payload %d params has type %T", path, index+1, payload["params"])
	}
	name, ok := params["name"].(string)
	if !ok || name == "" {
		t.Fatalf("%s JSON payload %d params.name must be a non-empty string", path, index+1)
	}
	schema, ok := v01ToolSchemas[name]
	if !ok {
		t.Fatalf("%s JSON payload %d uses non-v0.1 MCP tool %q", path, index+1, name)
	}
	arguments, ok := params["arguments"].(map[string]any)
	if !ok {
		t.Fatalf("%s JSON payload %d params.arguments has type %T", path, index+1, params["arguments"])
	}
	allowedArguments := map[string]bool{}
	for _, key := range append(schema.required, schema.optional...) {
		allowedArguments[key] = true
	}
	for _, key := range schema.required {
		value, ok := arguments[key].(string)
		if !ok || strings.TrimSpace(value) == "" {
			t.Fatalf("%s JSON payload %d %s.%s must be a non-empty string", path, index+1, name, key)
		}
	}
	for key, value := range arguments {
		if !allowedArguments[key] {
			t.Fatalf("%s JSON payload %d %s has unsupported argument %q", path, index+1, name, key)
		}
		if _, ok := value.(string); !ok {
			t.Fatalf("%s JSON payload %d %s.%s has type %T, want string", path, index+1, name, key, value)
		}
	}
	return name
}

func writeEvidence(t *testing.T, filename string, body string) {
	t.Helper()
	path := filepath.Join(repoRoot(t), ".sisyphus", "evidence", filename)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create evidence dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write evidence %s: %v", filename, err)
	}
}
