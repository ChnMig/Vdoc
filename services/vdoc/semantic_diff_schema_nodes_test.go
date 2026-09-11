package vdoc

import (
	"fmt"
	"strings"
	"testing"
)

func TestCompareVersionsDetectsSchemaNodeChanges(t *testing.T) {
	cases := []struct {
		name, from, to, location, message string
	}{
		{"primitive array", `{"type":"array","items":{"type":"string"}}`, `{"type":"array","items":{"type":"integer"}}`, ".items", ""},
		{"nested array", `{"type":"array","items":{"type":"array","items":{"type":"string"}}}`, `{"type":"array","items":{"type":"array","items":{"type":"integer"}}}`, ".items.items", ""},
		{"property array", `{"type":"object","properties":{"values":{"type":"array","items":{"type":"string"}}}}`, `{"type":"object","properties":{"values":{"type":"array","items":{"type":"integer"}}}}`, ".properties.values.items", ""},
		{"allOf array", `{"allOf":[{"type":"array","items":{"type":"string"}}]}`, `{"allOf":[{"type":"array","items":{"type":"integer"}}]}`, ".items", ""},
		{"item enum", `{"type":"array","items":{"type":"string","enum":["a","b"]}}`, `{"type":"array","items":{"type":"string","enum":["a"]}}`, ".items", "Enum value removed"},
		{"root enum", `{"type":"string","enum":["a","b"]}`, `{"type":"string","enum":["a"]}`, "", "Enum value removed"},
	}
	for _, response := range []bool{false, true} {
		prefix, change := "requestBody.application/json", ChangeRequestBodyChanged
		if response {
			prefix, change = "responses.200.application/json", ChangeResponseChanged
		}
		for _, tc := range cases {
			t.Run(fmt.Sprintf("response=%t/%s", response, tc.name), func(t *testing.T) {
				diff := compareSemanticSchemas(t, semanticDiffBodySchema(tc.from, response), semanticDiffBodySchema(tc.to, response))
				message := tc.message
				if message == "" {
					message = fieldTypeChangeMessage(response)
				}
				item := assertDiffItem(t, diff, change, prefix+tc.location, SeverityBreaking, true, message)
				if !item.MustHandle || len(diff.Items) != 1 || diff.Summary.ModifiedEndpoints != 1 || diff.Summary.BreakingChanges != 1 {
					t.Fatalf("schema change must produce exactly one breaking item: summary=%+v items=%+v", diff.Summary, diff.Items)
				}
				if tc.message == "" && (item.OldValue != "string" || item.NewValue != "integer") {
					t.Fatalf("item type values = %#v -> %#v", item.OldValue, item.NewValue)
				}
				if tc.message != "" && item.OldValue != "b" {
					t.Fatalf("removed enum value = %#v, want b", item.OldValue)
				}
			})
		}
	}
}

func TestCompareVersionsIgnoresEquivalentSchemaNodes(t *testing.T) {
	for _, schema := range []string{
		`{"type":"array","items":{"type":"array","items":{"type":"string"}}}`,
		`{"type":"string","enum":["a","b"]}`,
		`{"type":"array","items":{"type":"string","enum":["a","b"]}}`,
	} {
		from := semanticDiffBodySchema(schema, true)
		to := strings.Replace(from, `"version":"1"`, `"version":"2"`, 1)
		diff := compareSemanticSchemas(t, from, to)
		if len(diff.Items) != 0 || diff.Summary.ModifiedEndpoints != 0 || diff.Summary.BreakingChanges != 0 {
			t.Fatalf("unchanged schema produced differences: %+v", diff)
		}
	}
	from := `{"type":"array","items":{"type":"string","enum":["a","b"]}}`
	to := `{"type":"array","items":{"type":"string","enum":["b","a"]}}`
	if diff := compareSemanticSchemas(t, semanticDiffBodySchema(from, false), semanticDiffBodySchema(to, false)); len(diff.Items) != 0 {
		t.Fatalf("enum ordering produced differences: %+v", diff.Items)
	}
}

func TestVersionTwoDiffAndDraftPreviewRefreshSchemaNodes(t *testing.T) {
	store, projectID, documentID, branchID := newOpenAPIDocumentFlowStore(t)
	objects := newRecordingObjectStorage(nil)
	store.objects = objects
	fromSchema := semanticDiffBodySchema(`{"type":"array","items":{"type":"string"}}`, true)
	toSchema := semanticDiffBodySchema(`{"type":"array","items":{"type":"integer"}}`, true)
	from := publishOpenAPIDocumentDraft(t, store, "admin", projectID, documentID, branchID, "1.0.0", fromSchema, "base")
	to := publishOpenAPIDocumentDraft(t, store, "admin", projectID, documentID, branchID, "1.1.0", toSchema, "changed")
	diff := store.diffForVersionsLocked(documentID, from.ID, to.ID)
	diff.Items = nil
	diff.Summary = DiffSummary{DocumentFormat: DocumentFormatOpenAPI31, ParserVersion: 2}
	draft := store.drafts[to.DraftID]
	draft.DiffPreview.Items = nil
	draft.DiffPreview.Summary = diff.Summary
	draftUpdatedAt := draft.UpdatedAt
	repo := &legacyFactsRepository{recordingRepository: newRecordingRepository(store.cloneStateLocked())}
	store.persistence = &postgresPersistence{repo: repo}

	preview, err := store.Draft("reader", projectID, documentID, to.DraftID)
	if err != nil {
		t.Fatal(err)
	}
	if preview.DiffPreview.Summary.BreakingChanges != 1 || !preview.UpdatedAt.Equal(draftUpdatedAt) {
		t.Fatalf("legacy preview not refreshed without changing the draft: %+v", preview)
	}
	updated, err := store.Diff("reader", projectID, documentID, diff.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertDiffItem(t, updated, ChangeResponseChanged, "responses.200.application/json.items", SeverityBreaking, true, "Response field type changed")
	if updated.ID != diff.ID || updated.Summary.ParserVersion <= 2 || repo.diffWrites != 1 {
		t.Fatalf("legacy diff upgrade = %+v, writes = %d", updated, repo.diffWrites)
	}
	reloaded := NewStore()
	reloaded.persistence = &postgresPersistence{repo: repo}
	reloaded.objects = objects
	again, err := reloaded.Diff("reader", projectID, documentID, diff.ID)
	if err != nil || !valuesEqual(updated, again) || repo.diffWrites != 1 {
		t.Fatalf("upgraded diff should persist and be reused: diff=%+v err=%v writes=%d", again, err, repo.diffWrites)
	}
}

func semanticDiffBodySchema(schema string, response bool) string {
	operation := `"requestBody":{"content":{"application/json":{"schema":` + schema + `}}},"responses":{"200":{"description":"ok"}}`
	if response {
		operation = `"responses":{"200":{"description":"ok","content":{"application/json":{"schema":` + schema + `}}}}`
	}
	return `{"openapi":"3.1.0","info":{"title":"Schema nodes","version":"1"},"paths":{"/values":{"post":{` + operation + `}}}}`
}
