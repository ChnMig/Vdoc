package vdoc

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func directionDocument(schema string, response bool, dialect string) string {
	return strings.Replace(semanticDiffBodySchema(schema, response), `"openapi":"3.1.0"`, `"openapi":"`+dialect+`"`, 1)
}

func TestSemanticDiffRequiredUsesReadWriteDirection(t *testing.T) {
	for _, tc := range []struct {
		name, annotation string
		response         bool
		wantBreaking     int
	}{
		{"readOnly request", `,"readOnly":true`, false, 0},
		{"writeOnly response", `,"writeOnly":true`, true, 0},
		{"ordinary request", "", false, 1},
		{"ordinary response", "", true, 1},
		{"writeOnly request", `,"writeOnly":true`, false, 1},
		{"readOnly response", `,"readOnly":true`, true, 1},
		{"readOnly false request", `,"readOnly":false`, false, 1},
		{"writeOnly false response", `,"writeOnly":false`, true, 1},
	} {
		for _, dialect := range []string{"3.0.3", "3.1.0"} {
			t.Run(tc.name+"/"+dialect, func(t *testing.T) {
				optional := `{"type":"object","properties":{"id":{"type":"string"` + tc.annotation + `}}}`
				required := strings.Replace(optional, `"type":"object",`, `"type":"object","required":["id"],`, 1)
				before, after := optional, required
				if tc.response {
					before, after = required, optional
				}
				diff := compareSemanticSchemas(t, directionDocument(before, tc.response, dialect), directionDocument(after, tc.response, dialect))
				if diff.Summary.BreakingChanges != tc.wantBreaking || len(diff.Items) != tc.wantBreaking {
					t.Fatalf("required direction: summary=%+v items=%+v, want %d breaking changes", diff.Summary, diff.Items, tc.wantBreaking)
				}
				for _, item := range diff.Items {
					if !item.MustHandle || !item.IsBreaking || item.Severity != SeverityBreaking {
						t.Fatalf("real required obligation was suppressed: %+v", item)
					}
				}
			})
		}
	}
}

func TestSemanticDiffRequiredDirectionAcrossReferencesAndAllOf(t *testing.T) {
	for _, response := range []bool{false, true} {
		annotation := `"readOnly":true`
		if response {
			annotation = `"writeOnly":true`
		}
		for _, tc := range []struct{ name, schema, components string }{
			{"property allOf", `{"type":"object",REQUIRED"properties":{"id":{"allOf":[{"type":"string"},{ACCESS}]}}}`, ""},
			{"split allOf", `{"allOf":[{"type":"object",REQUIRED"properties":{"id":{"type":"string"}}},{"properties":{"id":{ACCESS}}}]}`, ""},
			{"split allOf reversed", `{"allOf":[{"properties":{"id":{ACCESS}}},{"type":"object",REQUIRED"properties":{"id":{"type":"string"}}}]}`, ""},
			{"nested parent", `{"properties":{"parent":{ACCESS,"type":"object",REQUIRED"properties":{"id":{"type":"string"}}}}}`, ""},
			{"array item property", `{"type":"array","items":{"type":"object",REQUIRED"properties":{"id":{"type":"string",ACCESS}}}}`, ""},
			{"split parent array", `{"allOf":[{"properties":{"parent":{"type":"array","items":{"type":"object",REQUIRED"properties":{"id":{"type":"string"}}}}}},{"properties":{"parent":{ACCESS}}}]}`, ""},
			{"property reference", `{"type":"object",REQUIRED"properties":{"id":{"$ref":"#/components/schemas/Value"}}}`, `{"Value":{"type":"string",ACCESS}}`},
			{"parent reference", `{"properties":{"parent":{"$ref":"#/components/schemas/Value"}}}`, `{"Value":{ACCESS,"type":"object",REQUIRED"properties":{"id":{"type":"string"}}}}`},
			{"required without property definition in parent", `{"allOf":[{"properties":{"parent":{ACCESS}}},{"properties":{"parent":{"type":"object",REQUIRED"additionalProperties":true}}}]}`, ""},
		} {
			t.Run(fmt.Sprintf("response=%t/%s", response, tc.name), func(t *testing.T) {
				document := func(required bool) string {
					doc := directionDocument(tc.schema, response, "3.0.3")
					if tc.components != "" {
						doc = strings.Replace(doc, `"paths":`, `"components":{"schemas":`+tc.components+`},"paths":`, 1)
					}
					requiredText := ""
					if required {
						requiredText = `"required":["id"],`
					}
					return strings.NewReplacer("ACCESS", annotation, "REQUIRED", requiredText).Replace(doc)
				}
				before, after := document(response), document(!response)
				if items := reviewParsedChanges(t, before, after); len(items) != 0 {
					t.Fatalf("inapplicable required changed the diff: %+v", items)
				}
				// 相同结构的另一方向仍须保留该必填义务。
				applicable := strings.ReplaceAll(annotation, "readOnly", "writeOnly")
				if response {
					applicable = `"readOnly":true`
				}
				items := reviewParsedChanges(t, strings.ReplaceAll(before, annotation, applicable), strings.ReplaceAll(after, annotation, applicable))
				if len(items) != 1 || !items[0].MustHandle || !items[0].IsBreaking {
					t.Fatalf("applicable nested requirement was suppressed: %+v", items)
				}
			})
		}
	}
}

func TestSemanticDiffDirectionDoesNotTreatLiteralPathsAsAncestors(t *testing.T) {
	before := `{"properties":{"id":{"readOnly":true,"type":"object"},"id.properties.child":{"type":"string"}}}`
	after := strings.Replace(before, `{"properties":`, `{"required":["id.properties.child"],"properties":`, 1)
	items := reviewParsedChanges(t, directionDocument(before, false, "3.0.3"), directionDocument(after, false, "3.0.3"))
	if len(items) != 1 || !items[0].IsBreaking {
		t.Fatalf("literal property incorrectly inherited a sibling's access flags: %+v", items)
	}
}

func TestVersionEightRequiredDirectionFactsRefreshAndPersist(t *testing.T) {
	for _, failWrite := range []bool{false, true} {
		t.Run(fmt.Sprintf("failWrite=%t", failWrite), func(t *testing.T) {
			store, project, document, branch := newOpenAPIDocumentFlowStore(t)
			objects := newRecordingObjectStorage(nil)
			store.objects = objects
			base := directionDocument(`{"properties":{"id":{"type":"string","readOnly":true},"name":{"type":"string"}}}`, false, "3.0.3")
			changed := strings.Replace(base, `"properties":`, `"required":["id","name"],"properties":`, 1)
			from := publishOpenAPIDocumentDraft(t, store, "admin", project, document, branch, "1", base, "base")
			to := publishOpenAPIDocumentDraft(t, store, "admin", project, document, branch, "2", changed, "next")
			diff := store.diffForVersionsLocked(document, from.ID, to.ID)
			original := cloneDiff(diff)
			if len(original.Items) != 1 || original.Summary.BreakingChanges != 1 {
				t.Fatalf("fixture must retain a real required change: %+v", original)
			}
			diff.Items = append(diff.Items, DiffItem{ID: "obsolete-required-id", Location: "requestBody.application/json.properties.id", IsBreaking: true, MustHandle: true})
			diff.Summary.ParserVersion, diff.Summary.BreakingChanges = 8, 2
			draft := store.drafts[to.DraftID]
			draft.DiffPreview = cloneDiff(diff)
			revision, timestamp := draft.Revision(), draft.UpdatedAt
			endpointIDs := make(map[string]bool)
			for _, endpoint := range store.endpoints {
				endpointIDs[endpoint.ID] = true
			}
			store.versions[from.ID].ParserVersion, store.versions[to.ID].ParserVersion = 8, 8
			repo := &legacyFactsRepository{recordingRepository: newRecordingRepository(store.cloneStateLocked())}
			store.persistence = &postgresPersistence{repo: repo}
			preview, err := store.Draft("reader", project, document, draft.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(preview.DiffPreview.Items) != 1 || preview.DiffPreview.Summary.BreakingChanges != 1 || preview.DiffPreview.Summary.ParserVersion != openAPIParserVersion || preview.Revision() != revision || !preview.UpdatedAt.Equal(timestamp) {
				t.Fatalf("preview retained false facts or changed review metadata: %+v", preview)
			}
			if failWrite {
				repo.diffErr = errors.New("required direction facts write failed")
				beforeObjects := len(objects.objects)
				_, err := store.Diff("reader", project, document, diff.ID)
				if !errors.Is(err, repo.diffErr) || store.diffs[diff.ID].Summary.ParserVersion != 8 || repo.state.Diffs[diff.ID].Summary.ParserVersion != 8 || len(objects.objects) != beforeObjects {
					t.Fatalf("failed refresh did not roll back: err=%v", err)
				}
				repo.diffErr = nil
			}
			updated, err := store.Diff("reader", project, document, diff.ID)
			if err != nil {
				t.Fatal(err)
			}
			if updated.Summary.BreakingChanges != 1 || updated.Summary.ParserVersion != openAPIParserVersion || !valuesEqual(updated.Items, original.Items) || updated.ID != original.ID || !updated.CreatedAt.Equal(original.CreatedAt) {
				t.Fatalf("refresh did not preserve real changes and their IDs: %+v", updated)
			}
			for _, old := range []*ContractVersion{from, to} {
				current := store.versions[old.ID]
				if current.ParserVersion != openAPIParserVersion || current.RawSchemaHash != old.RawSchemaHash || current.NormalizedSchemaHash != old.NormalizedSchemaHash {
					t.Fatal("refresh changed immutable source hashes")
				}
			}
			if len(store.endpoints) != len(endpointIDs) {
				t.Fatal("refresh changed the endpoint count")
			}
			for id := range store.endpoints {
				if !endpointIDs[id] {
					t.Fatal("refresh changed an endpoint ID")
				}
			}
			writes := repo.diffWrites
			reloaded := NewStore()
			reloaded.persistence, reloaded.objects = &postgresPersistence{repo: repo}, objects
			again, err := reloaded.Diff("reader", project, document, diff.ID)
			if err != nil || !valuesEqual(updated, again) || repo.diffWrites != writes {
				t.Fatalf("refreshed facts did not persist or were rewritten: err=%v writes=%d", err, repo.diffWrites)
			}
		})
	}
}
