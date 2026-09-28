package vdoc

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRepairSemanticBoundaries(t *testing.T) {
	cases := []struct {
		name, before, after string
		response            bool
	}{
		{"required_without_properties_request", `{"type":"object"}`, `{"type":"object","required":["token"]}`, false},
		{"required_without_properties_response", `{"type":"object","required":["token"]}`, `{"type":"object"}`, true},
		{"anyof_description_property", `{"anyOf":[{"type":"object","properties":{"description":{"type":"string"}}},{"type":"null"}]}`, `{"anyOf":[{"type":"object","properties":{"description":{"type":"integer"}}},{"type":"null"}]}`, false},
		{"anyof_object_enum", `{"anyOf":[{"enum":[{"description":"old"}]},{"type":"null"}]}`, `{"anyOf":[{"enum":[{"description":"new"}]},{"type":"null"}]}`, false},
		{"missing_schema_to_constrained", `null`, `{"type":"string","maxLength":1}`, false},
		{"nested_allof_required_only", `{"type":"object","allOf":[{"type":"object"}]}`, `{"type":"object","allOf":[{"required":["token"]}]}`, false},
		{"response_schema_removed", `{"type":"string","maxLength":1}`, `{}`, true},
		{"anyof_property_x", `{"anyOf":[{"properties":{"x-business":{"type":"string"}}}]}`, `{"anyOf":[{"properties":{"x-business":{"type":"integer"}}}]}`, false},
		{"anyof_literal_array_order", `{"anyOf":[{"enum":[{"required":["a","b"]}]}]}`, `{"anyOf":[{"enum":[{"required":["b","a"]}]}]}`, false},
		{"pattern_property_description", `{"patternProperties":{"description":{"type":"string"}}}`, `{"patternProperties":{"description":{"type":"integer"}}}`, false},
		{"additional_property_literal", `{"additionalProperties":{"const":{"title":"before"}}}`, `{"additionalProperties":{"const":{"title":"after"}}}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := semanticDiffBodySchema(tc.before, tc.response)
			if tc.name == "missing_schema_to_constrained" {
				before = strings.Replace(before, `"schema":null`, `"example":"any-length"`, 1)
			}
			diff := compareSemanticSchemas(t, before, semanticDiffBodySchema(tc.after, tc.response))
			must := 0
			for _, item := range diff.Items {
				if item.MustHandle {
					must++
				}
			}
			data, _ := json.Marshal(diff.Items)
			t.Logf("items=%d breaking=%d must_handle=%d details=%s", len(diff.Items), diff.Summary.BreakingChanges, must, data)
			if must == 0 {
				t.Error("incompatible input/output change has no required-review signal")
			}
		})
	}
}

func TestRepairParameterContent(t *testing.T) {
	before := `{"openapi":"3.1.0","info":{"title":"Content parameter","version":"1"},"paths":{"/values":{"get":{"parameters":[{"name":"filter","in":"query","content":{"application/json":{"schema":{"type":"object","properties":{"id":{"type":"string"}}}}}}],"responses":{"200":{"description":"ok"}}}}}}`
	after := strings.Replace(before, `"id":{"type":"string"}`, `"id":{"type":"integer"}`, 1)
	diff := compareSemanticSchemas(t, before, after)
	t.Logf("items=%d breaking=%d details=%+v", len(diff.Items), diff.Summary.BreakingChanges, diff.Items)
	if len(diff.Items) == 0 {
		t.Error("content-based parameter change disappeared")
	}
}

func TestRepairLocalPointerIntoArray(t *testing.T) {
	doc := `{"openapi":"3.1.0","info":{"title":"Pointer","version":"1"},"paths":{"/values":{"get":{"responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"$ref":"#/components/schemas/Choice/oneOf/0"}}}}}}}},"components":{"schemas":{"Choice":{"oneOf":[{"type":"string"},{"type":"integer"}]}}}}`
	_, err := ParseOpenAPI(doc)
	t.Logf("valid local pointer into array: %v", err)
	if err != nil {
		t.Error("local JSON Pointer array index is unsupported")
	}
}

func TestRepairCanonicalSchemaAnnotationsAndOrder(t *testing.T) {
	before := `{"anyOf":[{"properties":{"description":{"type":"string","description":"old"}},"enum":[{"x-value":1,"title":"literal"}]},{"type":"null"}]}`
	after := `{"anyOf":[{"type":"null"},{"enum":[{"title":"literal","x-value":1}],"properties":{"description":{"description":"new","type":"string"}}}]}`
	diff := compareSemanticSchemas(t, semanticDiffBodySchema(before, false), semanticDiffBodySchema(after, false))
	if len(diff.Items) != 0 {
		t.Fatalf("annotation/order-only change: %+v", diff.Items)
	}
}

func TestRepairJSONPointerBoundaries(t *testing.T) {
	root := map[string]any{"items": []any{"first", "second"}, "a/b": map[string]any{"m~n": "escaped"}, "a b": "space", "": "empty", "01": "object key"}
	for _, tc := range []struct {
		ref   string
		value any
		valid bool
	}{
		{"#/items/0", "first", true}, {"#/items/1", "second", true}, {"#/items/2", nil, false}, {"#/items/-", nil, false}, {"#/items/-1", nil, false}, {"#/items/+1", nil, false}, {"#/items/01", nil, false}, {"#/items/", nil, false}, {"#/items/99999999999999999999", nil, false},
		{"#/a~1b/m~0n", "escaped", true}, {"#%2Fa%20b", "space", true}, {"#/", "empty", true}, {"#/01", "object key", true}, {"#/a~2b", nil, false}, {"#/a~", nil, false}, {"#/%ff", nil, false}, {"#/%", nil, false},
	} {
		got, ok := lookupJSONPointer(root, tc.ref)
		if ok != tc.valid || !valuesEqual(got, tc.value) {
			t.Errorf("%s: %v %v", tc.ref, got, ok)
		}
	}
}

func TestRepairNumericRangeRejectedBeforeDraftMutation(t *testing.T) {
	store, project, document, branch := newOpenAPIDocumentFlowStore(t)
	valid := semanticDiffBodySchema(`{"enum":[1e131071,1e-16383]}`, false)
	draft, err := store.CreateDocumentDraft("admin", project, document, DraftInput{BranchID: branch, VersionName: "1", SchemaContent: valid})
	if err != nil {
		t.Fatal(err)
	}
	for _, number := range []string{"1e131072", "1e-16384", "1e200000"} {
		raw := semanticDiffBodySchema(`{"enum":[`+number+`]}`, false)
		if _, err := store.CreateDocumentDraft("admin", project, document, DraftInput{BranchID: branch, VersionName: number, SchemaContent: raw}); !Is(err, ErrInvalidArgument) {
			t.Fatalf("create %s: %v", number, err)
		}
		if _, err := store.UpdateDocumentDraft("admin", project, document, draft.ID, DraftPatchInput{ExpectedRevision: draft.Revision(), SchemaContent: raw}); !Is(err, ErrInvalidArgument) {
			t.Fatalf("update %s: %v", number, err)
		}
	}
	current, err := store.Draft("admin", project, document, draft.ID)
	if err != nil || current.Revision() != draft.Revision() {
		t.Fatalf("failed update changed draft: %v", err)
	}
}

func TestRepairParameterRepresentations(t *testing.T) {
	schema := `{"type":"object","properties":{"id":{"type":"string"}}}`
	base := parameterConstraintSchema(schema)
	for _, location := range []string{"query", "header", "cookie", "path"} {
		before := strings.Replace(base, `"in":"query"`, `"in":"`+location+`"`, 1)
		if location == "path" {
			before = strings.ReplaceAll(strings.Replace(before, `"/values"`, `"/values/{ids}"`, 1), `"in":"path"`, `"in":"path","required":true`)
		}
		content := strings.Replace(before, `"schema":`+schema, `"content":{"application/json":{"schema":`+schema+`}}`, 1)
		for _, pair := range [][2]string{
			{content, strings.Replace(content, `"id":{"type":"string"}`, `"id":{"type":"integer"}`, 1)},
			{before, content}, {content, before}, {content, strings.Replace(content, "application/json", "text/plain", 1)},
			{before, strings.Replace(before, `"schema":`, `"style":"form","explode":false,"schema":`, 1)},
		} {
			diff := compareSemanticSchemas(t, pair[0], pair[1])
			handled := false
			for _, item := range diff.Items {
				handled = handled || item.MustHandle
			}
			if !handled {
				t.Fatalf("%s representation change ignored: %+v", location, diff.Items)
			}
		}
	}
}

func TestRepairRequiredAndMediaCompatibleDirections(t *testing.T) {
	for _, tc := range []struct {
		before, after string
		response      bool
	}{
		{`{"type":"object","required":["token"]}`, `{"type":"object"}`, false},
		{`{"type":"object"}`, `{"type":"object","required":["token"]}`, true},
		{`{"type":"string","maxLength":1}`, `{}`, false},
		{`{}`, `{"type":"string","maxLength":1}`, true},
	} {
		diff := compareSemanticSchemas(t, semanticDiffBodySchema(tc.before, tc.response), semanticDiffBodySchema(tc.after, tc.response))
		if len(diff.Items) == 0 {
			t.Fatal("compatible change disappeared")
		}
		for _, item := range diff.Items {
			if item.IsBreaking || item.MustHandle {
				t.Fatalf("compatible direction flagged: %+v", diff.Items)
			}
		}
	}
	for _, response := range []bool{false, true} {
		before := semanticDiffBodySchema(`{}`, response)
		after := strings.Replace(before, `"schema":{}`, `"example":"unconstrained"`, 1)
		diff := compareSemanticSchemas(t, before, after)
		if len(diff.Items) != 0 {
			t.Fatalf("absent and empty schema differ: %+v", diff.Items)
		}
	}
}

func TestRepairUnconstrainedParameterAndArrayItems(t *testing.T) {
	for _, schemas := range [][2]string{{`{}`, `{"enum":["limited"]}`}, {`{"type":"array"}`, `{"type":"array","items":{"type":"string"}}`}, {`{"type":"array"}`, `{"type":"array","items":{"maxLength":1}}`}} {
		before := parameterConstraintSchema(schemas[0])
		if schemas[0] == `{}` {
			before = strings.Replace(before, `,"schema":{}`, "", 1)
		}
		diff := compareSemanticSchemas(t, before, parameterConstraintSchema(schemas[1]))
		handled := false
		for _, item := range diff.Items {
			handled = handled || item.MustHandle
		}
		if !handled {
			t.Errorf("new constraint has no review signal: before=%s after=%s items=%+v", schemas[0], schemas[1], diff.Items)
		}
	}
}

func TestRepairLiteralPropertyPathDoesNotCollideWithNestedPath(t *testing.T) {
	before := `{"type":"object","properties":{"a.properties.b":{"type":"string"},"a":{"type":"object","properties":{"b":{"type":"integer"}}}}}`
	after := strings.Replace(before, `"a.properties.b":{"type":"string"}`, `"a.properties.b":{"type":"boolean"}`, 1)
	diff := compareSemanticSchemas(t, semanticDiffBodySchema(before, false), semanticDiffBodySchema(after, false))
	if diff.Summary.BreakingChanges != 1 {
		t.Fatalf("literal field collided with nested path: %+v", diff.Items)
	}
}

func TestRepairJSONBStringBoundaries(t *testing.T) {
	store, project, document, branch := newOpenAPIDocumentFlowStore(t)
	draft, err := store.CreateDocumentDraft("admin", project, document, DraftInput{BranchID: branch, VersionName: "valid", SchemaContent: semanticDiffBodySchema(`{"type":"string"}`, false)})
	if err != nil {
		t.Fatal(err)
	}
	for _, schema := range []string{`{"enum":["\u0000"]}`, `{"properties":{"bad\u0000key":{"type":"string"}}}`} {
		raw := semanticDiffBodySchema(schema, false)
		if _, err := store.CreateDocumentDraft("admin", project, document, DraftInput{BranchID: branch, VersionName: "invalid", SchemaContent: raw}); !Is(err, ErrInvalidArgument) {
			t.Fatalf("create NUL: %v", err)
		}
		if _, err := store.UpdateDocumentDraft("admin", project, document, draft.ID, DraftPatchInput{ExpectedRevision: draft.Revision(), SchemaContent: raw}); !Is(err, ErrInvalidArgument) {
			t.Fatalf("update NUL: %v", err)
		}
		if _, err := ParseOpenAPI(semanticDiffBodySchema(schema, false)); !Is(err, ErrInvalidArgument) {
			t.Errorf("accepted JSONB-incompatible string: %s err=%v", schema, err)
		}
	}
}
