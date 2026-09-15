package vdoc

import (
	"strings"
	"testing"
)

func parameterConstraintSchema(schema string) string {
	return `{"openapi":"3.1.0","info":{"title":"Parameter items audit","version":"1"},"paths":{"/values":{"get":{"parameters":[{"name":"ids","in":"query","schema":` + schema + `}],"responses":{"200":{"description":"ok"}}}}}}`
}

func TestSemanticDiffParameterArrayItemsAreBreaking(t *testing.T) {
	tests := []struct{ name, from, to string }{
		{"item_type", `{"type":"array","items":{"type":"string"}}`, `{"type":"array","items":{"type":"integer"}}`},
		{"item_enum_narrowed", `{"type":"array","items":{"type":"string","enum":["a","b"]}}`, `{"type":"array","items":{"type":"string","enum":["a"]}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diff := compareSemanticSchemas(t, parameterConstraintSchema(tt.from), parameterConstraintSchema(tt.to))
			for _, item := range diff.Items {
				if item.IsBreaking && item.MustHandle {
					return
				}
			}
			t.Fatalf("breaking query array change was missed: summary=%+v items=%+v", diff.Summary, diff.Items)
		})
	}
}

func TestSemanticDiffRemovingRequestEnumConstraintIsCompatible(t *testing.T) {
	tests := []struct{ name, from, to string }{
		{"query_parameter", parameterConstraintSchema(`{"type":"string","enum":["a","b"]}`), parameterConstraintSchema(`{"type":"string"}`)},
		{"request_body_root", semanticDiffBodySchema(`{"type":"string","enum":["a","b"]}`, false), semanticDiffBodySchema(`{"type":"string"}`, false)},
		{"request_body_property", semanticDiffBodySchema(`{"type":"object","properties":{"name":{"type":"string","enum":["a","b"]}}}`, false), semanticDiffBodySchema(`{"type":"object","properties":{"name":{"type":"string"}}}`, false)},
		{"request_body_array_item", semanticDiffBodySchema(`{"type":"array","items":{"type":"string","enum":["a","b"]}}`, false), semanticDiffBodySchema(`{"type":"array","items":{"type":"string"}}`, false)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diff := compareSemanticSchemas(t, tt.from, tt.to)
			for _, item := range diff.Items {
				if item.IsBreaking || item.MustHandle {
					t.Errorf("removing the enum constraint must keep all previous request values valid: location=%s message=%q old=%v breaking=%v must_handle=%v", item.Location, item.Message, item.OldValue, item.IsBreaking, item.MustHandle)
				}
			}
		})
	}
}

func TestSemanticDiffControlsRecognizeExistingBreakingChanges(t *testing.T) {
	tests := []struct{ name, from, to string }{
		{"query_scalar_type", parameterConstraintSchema(`{"type":"string"}`), parameterConstraintSchema(`{"type":"integer"}`)},
		{"body_array_item_type", semanticDiffBodySchema(`{"type":"array","items":{"type":"string"}}`, false), semanticDiffBodySchema(`{"type":"array","items":{"type":"integer"}}`, false)},
		{"request_enum_narrowing", parameterConstraintSchema(`{"type":"string","enum":["a","b"]}`), parameterConstraintSchema(`{"type":"string","enum":["a"]}`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diff := compareSemanticSchemas(t, tt.from, tt.to)
			for _, item := range diff.Items {
				if item.IsBreaking && item.MustHandle {
					return
				}
			}
			t.Fatalf("control must have a breaking change: %+v", diff)
		})
	}
}

func TestSemanticDiffAddingRequestEnumConstraintIsBreaking(t *testing.T) {
	tests := []struct{ name, from, to string }{
		{"query_parameter", parameterConstraintSchema(`{"type":"string"}`), parameterConstraintSchema(`{"type":"string","enum":["a","b"]}`)},
		{"request_body_root", semanticDiffBodySchema(`{"type":"string"}`, false), semanticDiffBodySchema(`{"type":"string","enum":["a","b"]}`, false)},
		{"request_body_property", semanticDiffBodySchema(`{"type":"object","properties":{"name":{"type":"string"}}}`, false), semanticDiffBodySchema(`{"type":"object","properties":{"name":{"type":"string","enum":["a","b"]}}}`, false)},
		{"request_body_array_item", semanticDiffBodySchema(`{"type":"array","items":{"type":"string"}}`, false), semanticDiffBodySchema(`{"type":"array","items":{"type":"string","enum":["a","b"]}}`, false)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diff := compareSemanticSchemas(t, tt.from, tt.to)
			for _, item := range diff.Items {
				if item.IsBreaking && item.MustHandle {
					return
				}
			}
			t.Fatalf("previously valid request value c is now rejected, but no breaking change was reported: summary=%+v items=%+v", diff.Summary, diff.Items)
		})
	}
}

func TestSemanticDiffTraversesNestedParameterSchemas(t *testing.T) {
	cases := []struct{ name, from, to, location string }{
		{"nested arrays", `{"type":"array","items":{"type":"array","items":{"type":"string"}}}`, `{"type":"array","items":{"type":"array","items":{"type":"integer"}}}`, "items.items"},
		{"object property", `{"type":"object","properties":{"value":{"type":"string"}}}`, `{"type":"object","properties":{"value":{"type":"integer"}}}`, "properties.value"},
		{"allOf array", `{"allOf":[{"type":"array","items":{"type":"string"}}]}`, `{"allOf":[{"type":"array","items":{"type":"integer"}}]}`, "items"},
	}
	for _, location := range []string{"query", "header", "cookie", "path"} {
		for _, tc := range cases {
			t.Run(location+"/"+tc.name, func(t *testing.T) {
				from, to := parameterConstraintSchema(tc.from), parameterConstraintSchema(tc.to)
				from = strings.Replace(from, `"in":"query"`, `"in":"`+location+`"`, 1)
				to = strings.Replace(to, `"in":"query"`, `"in":"`+location+`"`, 1)
				if location == "path" {
					from = strings.ReplaceAll(strings.Replace(from, `"/values"`, `"/values/{ids}"`, 1), `"in":"path"`, `"in":"path","required":true`)
					to = strings.ReplaceAll(strings.Replace(to, `"/values"`, `"/values/{ids}"`, 1), `"in":"path"`, `"in":"path","required":true`)
				}
				diff := compareSemanticSchemas(t, from, to)
				assertDiffItem(t, diff, ChangeParameterChanged, "parameters."+location+".ids."+tc.location, SeverityBreaking, true, "Parameter field type changed")
				if len(diff.Items) != 1 {
					t.Fatalf("nested parameter change was duplicated: %+v", diff.Items)
				}
			})
		}
	}
}

func TestSemanticDiffDistinguishesAbsentAndEmptyEnumConstraints(t *testing.T) {
	cases := []struct {
		name, from, to     string
		response, breaking bool
	}{
		{"empty enum added", `{"type":"string"}`, `{"type":"string","enum":[]}`, false, true},
		{"empty enum removed", `{"type":"string","enum":[]}`, `{"type":"string"}`, false, false},
		{"disjoint allOf", `{"type":"string","enum":["a"]}`, `{"type":"string","allOf":[{"enum":["a"]},{"enum":["b"]}]}`, false, true},
		{"disjoint property fragments", `{"type":"object","properties":{"value":{"type":"string","enum":["a"]}}}`, `{"type":"object","allOf":[{"properties":{"value":{"type":"string","enum":["a"]}}},{"properties":{"value":{"enum":["b"]}}}]}`, false, true},
		{"response enum added", `{"type":"string"}`, `{"type":"string","enum":["a","b"]}`, true, false},
		{"response enum removed", `{"type":"string","enum":["a","b"]}`, `{"type":"string"}`, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diff := compareSemanticSchemas(t, semanticDiffBodySchema(tc.from, tc.response), semanticDiffBodySchema(tc.to, tc.response))
			if len(diff.Items) != 1 || diff.Items[0].IsBreaking != tc.breaking || diff.Items[0].MustHandle != tc.breaking {
				t.Fatalf("enum classification = %+v, want one item breaking=%v", diff.Items, tc.breaking)
			}
		})
	}
}

func TestVersionThreeDiffAndPreviewRefreshParameterAndEnumRules(t *testing.T) {
	cases := []struct {
		name, from, to           string
		oldBreaking, newBreaking int
	}{
		{"missed parameter", parameterConstraintSchema(`{"type":"array","items":{"type":"string"}}`), parameterConstraintSchema(`{"type":"array","items":{"type":"integer"}}`), 0, 1},
		{"false enum alarm", semanticDiffBodySchema(`{"type":"string","enum":["a","b"]}`, false), semanticDiffBodySchema(`{"type":"string"}`, false), 2, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, projectID, documentID, branchID := newOpenAPIDocumentFlowStore(t)
			objects := newRecordingObjectStorage(nil)
			store.objects = objects
			from := publishOpenAPIDocumentDraft(t, store, "admin", projectID, documentID, branchID, "1.0.0", tc.from, "base")
			to := publishOpenAPIDocumentDraft(t, store, "admin", projectID, documentID, branchID, "1.1.0", tc.to, "changed")
			diff := store.diffForVersionsLocked(documentID, from.ID, to.ID)
			diff.Items = nil
			diff.Summary = DiffSummary{DocumentFormat: DocumentFormatOpenAPI31, ParserVersion: 3, BreakingChanges: tc.oldBreaking}
			draft := store.drafts[to.DraftID]
			draft.DiffPreview.Items = nil
			draft.DiffPreview.Summary = diff.Summary
			revision, timestamp := draft.Revision(), draft.UpdatedAt
			repo := &legacyFactsRepository{recordingRepository: newRecordingRepository(store.cloneStateLocked())}
			store.persistence = &postgresPersistence{repo: repo}
			preview, err := store.Draft("reader", projectID, documentID, draft.ID)
			if err != nil || preview.DiffPreview.Summary.ParserVersion != openAPIParserVersion || preview.DiffPreview.Summary.BreakingChanges != tc.newBreaking || preview.Revision() != revision || !preview.UpdatedAt.Equal(timestamp) {
				t.Fatalf("preview refresh = %+v err=%v", preview, err)
			}
			updated, err := store.Diff("reader", projectID, documentID, diff.ID)
			if err != nil || updated.ID != diff.ID || updated.Summary.ParserVersion != openAPIParserVersion || updated.Summary.BreakingChanges != tc.newBreaking || len(updated.Items) != 1 || repo.diffWrites != 1 {
				t.Fatalf("diff refresh = %+v err=%v writes=%d", updated, err, repo.diffWrites)
			}
			reloaded := NewStore()
			reloaded.persistence = &postgresPersistence{repo: repo}
			reloaded.objects = objects
			again, err := reloaded.Diff("reader", projectID, documentID, diff.ID)
			if err != nil || !valuesEqual(updated, again) || repo.diffWrites != 1 {
				t.Fatalf("persisted cache not reused: diff=%+v err=%v writes=%d", again, err, repo.diffWrites)
			}
		})
	}
}
