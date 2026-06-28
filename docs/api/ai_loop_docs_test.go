package apidocs_test

import (
	"math"
	"strings"
	"testing"
)

type docsNumberExpectation struct {
	name string
	def  float64
	min  float64
	max  float64
}

type docsNumberValue struct {
	name string
	want float64
}

func TestAPIDocsDescribeAILoopContract(t *testing.T) {
	document := readDocsMarkdown(t, "API.md")
	requiredPhrases := []string{
		"temperature` defaults to `0.2` and accepts `0` through `2`",
		"timeout_ms` defaults to `30000` and accepts `1000` through `120000`",
		"max_output_tokens` defaults to `1000` and accepts `1` through `32000`",
		"Submitting a draft automatically attempts a draft AI summary",
		"Approving a draft automatically attempts a version AI summary",
		"Skipped and failed automatic summaries are saved as non-blocking `skipped` or `failed` records",
		"AI audit metadata includes token usage fields when the provider returns them",
		"API keys, JWTs, MCP tokens, and Authorization headers are not stored in AI audit metadata",
		"Direct publish tools are intentionally not exposed in v0.1",
	}

	missing := make([]string, 0)
	for _, phrase := range requiredPhrases {
		if !strings.Contains(document, phrase) {
			missing = append(missing, phrase)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("API.md missing AI loop phrases: %s", strings.Join(missing, ", "))
	}
}

func TestAPIDocsProviderBaseURLExampleOmitsVersionPath(t *testing.T) {
	// Given
	document := readDocsMarkdown(t, "API.md")

	// When
	versionedExample := `"base_url":"https://api.openai.example/v1"`
	rootExample := `"base_url":"https://api.openai.example"`

	// Then
	if strings.Contains(document, versionedExample) {
		t.Fatalf("API.md provider base_url example includes API version path: %s", versionedExample)
	}
	if !strings.Contains(document, rootExample) {
		t.Fatalf("API.md missing provider base_url root example: %s", rootExample)
	}
}

func TestAPIDocsOpenAPIProviderTuningSchema(t *testing.T) {
	schemas := asDocsMap(t, asDocsMap(t, parseDocsOpenAPI(t)["components"], "components")["schemas"], "components.schemas")
	request := asDocsMap(t, schemas["AIProviderRequest"], "AIProviderRequest")
	response := asDocsMap(t, schemas["AIProviderResponse"], "AIProviderResponse")

	assertDocsTuningProperties(t, asDocsMap(t, request["properties"], "AIProviderRequest.properties"))
	assertDocsTuningProperties(t, asDocsMap(t, response["properties"], "AIProviderResponse.properties"))
}

func TestAPIDocsDatabaseSchemaDescribesAILoopPersistence(t *testing.T) {
	document := readDocsMarkdown(t, "../../../DATABASE_SCHEMA.md")
	requiredPhrases := []string{
		"`ai_providers`",
		"`temperature`",
		"`timeout_ms`",
		"`max_output_tokens`",
		"draft_submit",
		"version_publish",
		"`skipped`、`succeeded`、`failed`",
		"token usage",
		"API Key、JWT、MCP Token 和 Authorization header 不写入审计 metadata",
	}

	missing := make([]string, 0)
	for _, phrase := range requiredPhrases {
		if !strings.Contains(document, phrase) {
			missing = append(missing, phrase)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("DATABASE_SCHEMA.md missing AI loop phrases: %s", strings.Join(missing, ", "))
	}
}

func assertDocsTuningProperties(t *testing.T, properties map[string]any) {
	t.Helper()
	assertDocsNumberProperty(t, properties, docsNumberExpectation{name: "temperature", def: 0.2, min: 0, max: 2})
	assertDocsNumberProperty(t, properties, docsNumberExpectation{name: "timeout_ms", def: 30000, min: 1000, max: 120000})
	assertDocsNumberProperty(t, properties, docsNumberExpectation{name: "max_output_tokens", def: 1000, min: 1, max: 32000})
}

func assertDocsNumberProperty(t *testing.T, properties map[string]any, want docsNumberExpectation) {
	t.Helper()
	property := asDocsMap(t, properties[want.name], want.name)
	assertDocsNumber(t, property["default"], docsNumberValue{name: want.name + " default", want: want.def})
	assertDocsNumber(t, property["minimum"], docsNumberValue{name: want.name + " minimum", want: want.min})
	assertDocsNumber(t, property["maximum"], docsNumberValue{name: want.name + " maximum", want: want.max})
}

func assertDocsNumber(t *testing.T, value any, want docsNumberValue) {
	t.Helper()
	var got float64
	switch typed := value.(type) {
	case float64:
		got = typed
	case int:
		got = float64(typed)
	case int64:
		got = float64(typed)
	default:
		t.Fatalf("%s has type %T, want number", want.name, value)
	}
	if math.Abs(got-want.want) > 0.000001 {
		t.Fatalf("%s = %v, want %v", want.name, got, want.want)
	}
}
