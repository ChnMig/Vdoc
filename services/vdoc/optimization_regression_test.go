package vdoc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// 审查发现的实际兼容性和解析边界回归。
func TestRegressionSemanticCompatibility(t *testing.T) {
	cases := []struct {
		name, from, to string
		response       bool
		wantBreaking   bool
	}{
		{"response_required_removed", `{"type":"object","required":["id"],"properties":{"id":{"type":"string"}}}`, `{"type":"object","properties":{"id":{"type":"string"}}}`, true, true},
		{"response_enum_widened", `{"type":"string","enum":["active"]}`, `{"type":"string","enum":["active","disabled"]}`, true, true},
		{"oas31_type_union_changed", `{"type":["string","null"]}`, `{"type":["integer","null"]}`, true, true},
		{"oas31_oneof_variant_removed", `{"oneOf":[{"type":"string"},{"type":"integer"}]}`, `{"oneOf":[{"type":"string"}]}`, false, true},
		{"request_minimum_tightened", `{"type":"integer","minimum":0}`, `{"type":"integer","minimum":10}`, false, true},
		{"required_request_field_positive_control", `{"type":"object","properties":{"id":{"type":"string"}}}`, `{"type":"object","required":["id"],"properties":{"id":{"type":"string"}}}`, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diff := compareSemanticSchemas(t, semanticDiffBodySchema(tc.from, tc.response), semanticDiffBodySchema(tc.to, tc.response))
			t.Logf("modified=%d breaking=%d items=%+v", diff.Summary.ModifiedEndpoints, diff.Summary.BreakingChanges, diff.Items)
			if tc.wantBreaking && diff.Summary.BreakingChanges == 0 {
				t.Errorf("incompatible schema change was not marked breaking")
			}
		})
	}
}

func TestRegressionIntegerPrecisionAndDraft(t *testing.T) {
	from := semanticDiffBodySchema(`{"type":"integer","enum":[9007199254740992]}`, true)
	to := strings.ReplaceAll(from, "9007199254740992", "9007199254740993")
	a, err := ParseOpenAPI(from)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ParseOpenAPI(to)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("different raw inputs have identical normalized content=%v, endpoint hash=%v", a.Normalized == b.Normalized, a.Endpoints[0].Hash == b.Endpoints[0].Hash)
	store, _, project, doc, branch := newContractPipelineStore(t)
	publishContractDraft(t, store, "admin", project, doc, branch, "1.0", from)
	_, err = store.CreateDraft("admin", project, doc, DraftInput{BranchID: branch, VersionName: "1.1", SchemaContent: to})
	t.Logf("create changed numeric enum draft: %v", err)
	if a.Normalized == b.Normalized {
		t.Error("distinct exact integer schemas collapsed into the same normalized document")
	}
	if err != nil {
		t.Error("valid changed integer schema cannot be submitted")
	}
}

func TestRegressionReferenceExpansion(t *testing.T) {
	for _, depth := range []int{6, 12, 20} {
		schemas := map[string]any{"S0": map[string]any{"type": "string"}}
		for i := 1; i <= depth; i++ {
			ref := fmt.Sprintf("#/components/schemas/S%d", i-1)
			schemas[fmt.Sprintf("S%d", i)] = map[string]any{"type": "object", "properties": map[string]any{"left": map[string]any{"$ref": ref}, "right": map[string]any{"$ref": ref}}}
		}
		var root map[string]any
		if err := json.Unmarshal([]byte(semanticDiffBodySchema(fmt.Sprintf(`{"$ref":"#/components/schemas/S%d"}`, depth), true)), &root); err != nil {
			t.Fatal(err)
		}
		root["components"] = map[string]any{"schemas": schemas}
		raw, _ := json.Marshal(root)
		parsed, err := ParseOpenAPI(string(raw))
		if depth == 20 {
			if !Is(err, ErrInvalidArgument) {
				t.Fatalf("unbounded reference expansion: %v", err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		expanded, _ := json.Marshal(parsed.Endpoints[0].Responses)
		t.Logf("depth=%d input_bytes=%d resolved_response_bytes=%d expansion=%.1fx", depth, len(raw), len(expanded), float64(len(expanded))/float64(len(raw)))
	}
}

func TestRegressionAddingOptionalRequestBody(t *testing.T) {
	from := `{"openapi":"3.1.0","info":{"title":"Body","version":"1"},"paths":{"/values":{"post":{"responses":{"200":{"description":"ok"}}}}}}`
	to := semanticDiffBodySchema(`{"type":"object","required":["id"],"properties":{"id":{"type":"string"}}}`, false)
	diff := compareSemanticSchemas(t, from, to)
	t.Logf("new body omits required (defaults to false): breaking=%d items=%+v", diff.Summary.BreakingChanges, diff.Items)
	if diff.Summary.BreakingChanges != 0 {
		t.Error("adding an optional body rejects no previously valid requests but is marked breaking")
	}
}

func TestRegressionReferencedPathItems(t *testing.T) {
	inline := `{"openapi":"3.1.0","info":{"title":"Path refs","version":"1"},"paths":{"/health":{"get":{"responses":{"200":{"description":"ok"}}}},"/values":{"post":{"responses":{"200":{"description":"ok"}}}}}}`
	referenced := `{"openapi":"3.1.0","info":{"title":"Path refs","version":"1"},"paths":{"/health":{"get":{"responses":{"200":{"description":"ok"}}}},"/values":{"$ref":"#/components/pathItems/Values"}},"components":{"pathItems":{"Values":{"post":{"responses":{"200":{"description":"ok"}}}}}}}`
	parsed, err := ParseOpenAPI(referenced)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("valid local path-item reference: endpoints=%d; want GET /health and POST /values", len(parsed.Endpoints))
	diff := compareSemanticSchemas(t, inline, referenced)
	t.Logf("equivalent inline-to-ref refactor: removed=%d breaking=%d items=%+v", diff.Summary.RemovedEndpoints, diff.Summary.BreakingChanges, diff.Items)
	if len(parsed.Endpoints) != 2 {
		t.Error("referenced path item was silently omitted from endpoint index")
	}
	if len(diff.Items) != 0 {
		t.Error("equivalent path item refactor reported endpoint removal")
	}
}

func TestParserCancellationAndExactYAML(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ParseOpenAPIContext(ctx, semanticDiffBodySchema(`{"type":"string"}`, true)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled parser: %v", err)
	}
	jsonDoc := semanticDiffBodySchema(`{"type":"integer","enum":[9007199254740993,1.0,1e0]}`, true)
	raw, err := decodeOpenAPI(jsonDoc)
	if err != nil {
		t.Fatal(err)
	}
	yamlDoc := `openapi: 3.1.0
info: {title: Schema nodes, version: '1'}
paths:
  /values:
    post:
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema: {type: integer, enum: [9007199254740993, 1.0, 1e0]}
`
	yamlRaw, err := decodeOpenAPI(yamlDoc)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(raw["paths"])
	z, _ := json.Marshal(yamlRaw["paths"])
	if string(a) != string(z) {
		t.Fatalf("JSON/YAML differ: %s / %s", a, z)
	}
}

func TestParserYAMLDecimalsAndMerges(t *testing.T) {
	for _, pair := range [][2]string{{"001.0", "1"}, {"-001.e+2", "-100"}, {"-.25", "-0.25"}, {"0_01.00", "1"}, {"9007199254740993.0", "9007199254740993"}} {
		t.Run(pair[0], func(t *testing.T) {
			root, err := decodeOpenAPI("value: " + pair[0])
			if err != nil {
				t.Fatal(err)
			}
			if got, ok := root["value"].(json.Number); !ok || string(got) != pair[1] {
				t.Fatalf("exact YAML decimal: got %#v, want %s", root["value"], pair[1])
			}
		})
	}
	root, err := decodeOpenAPI("defaults: &base {type: integer, minimum: 1}\nvalue: {minimum: 2, <<: *base}\n")
	if err != nil {
		t.Fatal(err)
	}
	merged, _ := json.Marshal(root["value"])
	if string(merged) != `{"minimum":2,"type":"integer"}` {
		t.Fatalf("explicit fields must override merged defaults: %s", merged)
	}
	for _, invalid := range []string{"value: .inf", "value: .nan", "value: &cycle {child: *cycle}", "value: 1\nvalue: 2", "value: 1\n---\nvalue: 2", `{"value":1}}`} {
		if _, err := decodeOpenAPI(invalid); !Is(err, ErrInvalidArgument) {
			t.Fatalf("invalid YAML/JSON accepted: %q (%v)", invalid, err)
		}
	}
}

func TestParserCumulativeOperationOutputLimit(t *testing.T) {
	paths := map[string]any{}
	for i := range 100 {
		paths[fmt.Sprintf("/values/%d", i)] = map[string]any{"get": map[string]any{"responses": map[string]any{"200": map[string]any{"description": "ok"}}}}
	}
	doc := map[string]any{"openapi": "3.1.0", "info": map[string]any{"title": "Repeated root servers", "version": "1"}, "paths": paths, "servers": []any{map[string]any{"url": "https://example.test/" + strings.Repeat("x", 200000)}}}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseOpenAPI(string(raw)); !Is(err, ErrInvalidArgument) || !strings.Contains(err.Error(), "expanded operations") {
		t.Fatalf("cumulative output was not bounded: %v", err)
	}
}

func TestPathItemReferenceValidationAndInheritance(t *testing.T) {
	base := `{"openapi":"3.1.0","info":{"title":"Path refs","version":"1"},"paths":{"/values":{"$ref":"#/components/pathItems/Values"}},"components":{"pathItems":{"Values":{"parameters":[{"name":"id","in":"query","schema":{"type":"integer"}}],"servers":[{"url":"https://example.test"}],"get":{"responses":{"200":{"description":"ok"}}}}}}}`
	parsed, err := ParseOpenAPI(base)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Endpoints[0].Parameters == nil || parsed.Endpoints[0].Servers == nil {
		t.Fatal("path-level parameters or servers were lost")
	}
	for _, invalid := range []string{
		strings.Replace(base, `"#/components/pathItems/Values"`, `"#/components/pathItems/Missing"`, 1),
		strings.Replace(base, `"Values":{"parameters"`, `"Values":{"$ref":"#/components/pathItems/Values","parameters"`, 1),
		strings.Replace(base, `"/values":{"$ref"`, `"/values":{"get":{"responses":{"201":{"description":"created"}}},"$ref"`, 1),
	} {
		if _, err := ParseOpenAPI(invalid); !Is(err, ErrInvalidArgument) {
			t.Fatalf("invalid Path Item reference accepted: %v", err)
		}
	}
}

func TestSchemaCompatibilityDirectionsAndFallback(t *testing.T) {
	cases := []struct {
		name, from, to             string
		response, breaking, manual bool
	}{
		{"request type widening", `{"type":"string"}`, `{"type":["null","string"]}`, false, false, false},
		{"response type narrowing", `{"type":["null","string"]}`, `{"type":"string"}`, true, false, false},
		{"response minimum relaxed", `{"type":"number","minimum":10}`, `{"type":"number","minimum":0}`, true, true, false},
		{"request minimum relaxed", `{"type":"number","minimum":10}`, `{"type":"number","minimum":0}`, false, false, false},
		{"optional request parent", `{"type":"object"}`, `{"type":"object","properties":{"optional":{"type":"object","required":["id"],"properties":{"id":{"type":"integer"}}}}}`, false, false, false},
		{"required request parent", `{"type":"object"}`, `{"type":"object","required":["new"],"properties":{"new":{"type":"object","required":["id"],"properties":{"id":{"type":"integer"}}}}}`, false, true, false},
		{"enum number to string", `{"enum":[1]}`, `{"enum":["1"]}`, false, true, false},
		{"unsupported pattern", `{"type":"string","pattern":"a"}`, `{"type":"string","pattern":"b"}`, false, false, true},
		{"nested unsupported", `{"type":"object","properties":{"x":{"maxLength":10}}}`, `{"type":"object","properties":{"x":{"maxLength":5}}}`, false, true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := compareSemanticSchemas(t, semanticDiffBodySchema(c.from, c.response), semanticDiffBodySchema(c.to, c.response))
			if len(d.Items) == 0 || (d.Summary.BreakingChanges > 0) != c.breaking {
				t.Fatalf("unexpected classification: %+v", d)
			}
			if c.manual && !d.Items[0].MustHandle {
				t.Fatal("missing manual review signal")
			}
		})
	}
}
