package vdoc

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestSemanticDiffPropertyRulesRespectAdditionalProperties(t *testing.T) {
	for _, tc := range []struct {
		name, before, after        string
		response, breaking, manual bool
	}{
		{"closed request removal", `{"type":"object","additionalProperties":false,"properties":{"label":{"type":"string"}}}`, `{"type":"object","additionalProperties":false,"properties":{}}`, false, true, false},
		{"closed request required declaration removal", `{"type":"object","additionalProperties":false,"required":["label"],"properties":{"label":{"type":"string"}}}`, `{"type":"object","additionalProperties":false,"required":["label"]}`, false, true, false},
		{"typed extras required declaration removal", `{"type":"object","additionalProperties":{"type":"integer"},"required":["label"],"properties":{"label":{"type":"string"}}}`, `{"type":"object","additionalProperties":{"type":"integer"},"required":["label"]}`, false, true, false},
		{"request requirement added while declaration removed", `{"type":"object","properties":{"label":{"type":"string"}}}`, `{"type":"object","required":["label"]}`, false, true, false},
		{"response requirement removed while declaration added", `{"type":"object","required":["label"]}`, `{"type":"object","properties":{"label":{"type":"string"}}}`, true, true, false},
		{"open request required declaration removal", `{"type":"object","required":["label"],"properties":{"label":{"type":"string"}}}`, `{"type":"object","required":["label"]}`, false, false, false},
		{"open request existing requirement declaration added", `{"type":"object","required":["label"]}`, `{"type":"object","required":["label"],"properties":{"label":{"type":"string"}}}`, false, true, false},
		{"typed extras existing requirement declaration added", `{"type":"object","additionalProperties":{"type":"string"},"required":["label"]}`, `{"type":"object","additionalProperties":{"type":"string"},"required":["label"],"properties":{"label":{"type":"string"}}}`, false, false, false},
		{"closed existing requirement declaration added", `{"type":"object","additionalProperties":false,"required":["label"]}`, `{"type":"object","additionalProperties":false,"required":["label"],"properties":{"label":{"type":"string"}}}`, false, false, false},
		{"open request addition", `{"type":"object"}`, `{"type":"object","properties":{"label":{"type":"string"}}}`, false, true, false},
		{"typed extras request removal", `{"type":"object","additionalProperties":{"type":"integer"},"properties":{"label":{"type":"string"}}}`, `{"type":"object","additionalProperties":{"type":"integer"}}`, false, true, false},
		{"closed request addition", `{"type":"object","additionalProperties":false}`, `{"type":"object","additionalProperties":false,"properties":{"label":{"type":"string"}}}`, false, false, false},
		{"closed required request addition", `{"type":"object","additionalProperties":false}`, `{"type":"object","additionalProperties":false,"required":["label"],"properties":{"label":{"type":"string"}}}`, false, true, false},
		{"open request removal", `{"type":"object","properties":{"label":{"type":"string"}}}`, `{"type":"object"}`, false, false, false},
		{"typed extras same property addition", `{"type":"object","additionalProperties":{"type":"string"}}`, `{"type":"object","additionalProperties":{"type":"string"},"properties":{"label":{"type":"string"}}}`, false, false, false},
		{"integer to number property addition", `{"type":"object","additionalProperties":{"type":"integer"}}`, `{"type":"object","additionalProperties":{"type":"integer"},"properties":{"label":{"type":"number"}}}`, false, false, false},
		{"number to integer property addition", `{"type":"object","additionalProperties":{"type":"number"}}`, `{"type":"object","additionalProperties":{"type":"number"},"properties":{"label":{"type":"integer"}}}`, false, true, false},
		{"enum extras subset of property type", `{"type":"object","additionalProperties":{"enum":["a","b"]}}`, `{"type":"object","additionalProperties":{"enum":["a","b"]},"properties":{"label":{"type":"string"}}}`, false, false, false},
		{"enum extras filtered by type", `{"type":"object","additionalProperties":{"type":"string","enum":["a",1]}}`, `{"type":"object","additionalProperties":{"type":"string","enum":["a",1]},"properties":{"label":{"const":"a"}}}`, false, false, false},
		{"enum extras new narrower property", `{"type":"object","additionalProperties":{"enum":["a","b"]}}`, `{"type":"object","additionalProperties":{"enum":["a","b"]},"properties":{"label":{"const":"a"}}}`, false, true, false},
		{"boolean property added", `{"type":"object"}`, `{"type":"object","properties":{"label":false}}`, false, true, false},
		{"universal property added", `{"type":"object"}`, `{"type":"object","properties":{"label":true}}`, false, false, false},
		{"open response addition", `{"type":"object"}`, `{"type":"object","properties":{"label":{"type":"string"}}}`, true, false, false},
		{"closed response addition", `{"type":"object","additionalProperties":false}`, `{"type":"object","additionalProperties":false,"properties":{"label":{"type":"string"}}}`, true, true, false},
		{"typed extras response addition", `{"type":"object","additionalProperties":{"type":"integer"}}`, `{"type":"object","additionalProperties":{"type":"integer"},"properties":{"label":{"type":"string"}}}`, true, true, false},
		{"readOnly request addition", `{"type":"object"}`, `{"type":"object","properties":{"label":{"type":"string","readOnly":true}}}`, false, true, false},
		{"readOnly closed request removal", `{"type":"object","additionalProperties":false,"properties":{"label":{"type":"string","readOnly":true}}}`, `{"type":"object","additionalProperties":false}`, false, true, false},
		{"writeOnly response addition", `{"type":"object","additionalProperties":false}`, `{"type":"object","additionalProperties":false,"properties":{"label":{"type":"string","writeOnly":true}}}`, true, true, false},
		{"writeOnly request addition", `{"type":"object"}`, `{"type":"object","properties":{"label":{"type":"string","writeOnly":true}}}`, false, true, false},
		{"readOnly response addition", `{"type":"object","additionalProperties":false}`, `{"type":"object","additionalProperties":false,"properties":{"label":{"type":"string","readOnly":true}}}`, true, true, false},
		{"complex optional property addition", `{"type":"object"}`, `{"type":"object","properties":{"label":{"type":"string","pattern":"^[a-z]+$"}}}`, false, false, true},
		{"complex extras removal", `{"type":"object","additionalProperties":{"type":"string","pattern":"^[a-z]+$"},"properties":{"label":{"type":"string"}}}`, `{"type":"object","additionalProperties":{"type":"string","pattern":"^[a-z]+$"}}`, false, false, true},
		{"pattern parent request addition", `{"type":"object","patternProperties":{"^label$":{"type":"string"}},"additionalProperties":false}`, `{"type":"object","patternProperties":{"^label$":{"type":"string"}},"additionalProperties":false,"properties":{"label":{"type":"string"}}}`, false, false, true},
		{"conditional parent removal", `{"type":"object","maxProperties":0,"additionalProperties":false,"properties":{"label":{"type":"string"}}}`, `{"type":"object","maxProperties":0,"additionalProperties":false}`, false, false, true},
		{"unchanged declaration count bound loosening", `{"type":"object","maxProperties":1,"properties":{"label":{"type":"string"}}}`, `{"type":"object","maxProperties":2,"properties":{"label":{"type":"string"}}}`, false, false, false},
		{"literal property name", `{"type":"object","additionalProperties":false,"properties":{"label.properties.id":{"type":"string"}}}`, `{"type":"object","additionalProperties":false}`, false, true, false},
		{"large enum numeric implication", `{"type":"object","additionalProperties":{"enum":[1e2000]}}`, `{"type":"object","additionalProperties":{"enum":[1e2000]},"properties":{"label":{"type":"integer"}}}`, false, false, true},
	} {
		for _, dialect := range []string{"3.0.3", "3.1.0"} {
			if dialect == "3.0.3" && (strings.Contains(tc.before, `"const"`) || strings.Contains(tc.after, `"const"`) || strings.Contains(tc.after, `"label":false`) || strings.Contains(tc.after, `"label":true`)) {
				continue
			}
			t.Run(tc.name+"/"+dialect, func(t *testing.T) {
				diff := compareSemanticSchemas(t, directionDocument(tc.before, tc.response, dialect), directionDocument(tc.after, tc.response, dialect))
				if len(diff.Items) != 1 || diff.Items[0].IsBreaking != tc.breaking || diff.Items[0].MustHandle != (tc.breaking || tc.manual) {
					t.Fatalf("property classification = summary=%+v items=%+v, want breaking=%t manual=%t", diff.Summary, diff.Items, tc.breaking, tc.manual)
				}
				if tc.breaking && diff.Items[0].Severity != SeverityBreaking || tc.manual && diff.Items[0].Severity != SeverityWarning {
					t.Fatalf("property severity = %+v", diff.Items[0])
				}
			})
		}
	}
}

func TestSemanticDiffPropertyRulesAcrossNestedReferencesAndAllOf(t *testing.T) {
	for _, tc := range []struct {
		name, before, after, components string
		breaking, manual                bool
	}{
		{"nested object", `{"properties":{"parent":{"type":"object","additionalProperties":false,"properties":{"label":{"type":"string"}}}}}`, `{"properties":{"parent":{"type":"object","additionalProperties":false}}}`, "", true, false},
		{"array object", `{"type":"array","items":{"type":"object","additionalProperties":false,"properties":{"label":{"type":"string"}}}}`, `{"type":"array","items":{"type":"object","additionalProperties":false}}`, "", true, false},
		{"property reference", `{"type":"object","additionalProperties":false,"properties":{"label":{"$ref":"#/components/schemas/Value"}}}`, `{"type":"object","additionalProperties":false}`, `{"Value":{"type":"string"}}`, true, false},
		{"additional property reference", `{"type":"object","additionalProperties":{"$ref":"#/components/schemas/Value"},"properties":{"label":{"type":"string"}}}`, `{"type":"object","additionalProperties":{"$ref":"#/components/schemas/Value"}}`, `{"Value":{"type":"integer"}}`, true, false},
		{"allOf closed same branch", `{"allOf":[{"type":"object","additionalProperties":false,"properties":{"label":{"type":"string"}}}]}`, `{"allOf":[{"type":"object","additionalProperties":false}]}`, "", true, false},
		{"allOf separate closed branch", `{"allOf":[{"type":"object","additionalProperties":false},{"properties":{"label":{"type":"string"}}}]}`, `{"allOf":[{"type":"object","additionalProperties":false},{}]}`, "", false, false},
		{"allOf boolean prohibition", `{"allOf":[false,{"type":"object","properties":{"label":{"type":"string"}}}]}`, `{"allOf":[false,{"type":"object"}]}`, "", false, false},
		{"allOf parent type prohibits objects", `{"allOf":[{"type":"string"},{"properties":{"label":{"type":"string"}}}]}`, `{"allOf":[{"type":"string"},{}]}`, "", false, false},
		{"allOf typed extras intersection", `{"allOf":[{"type":"object","additionalProperties":{"type":"number"}},{"additionalProperties":{"type":"integer"}}]}`, `{"allOf":[{"type":"object","additionalProperties":{"type":"number"},"properties":{"label":{"type":"string"}}},{"additionalProperties":{"type":"integer"},"properties":{"label":{"type":"string"}}}]}`, "", true, false},
		{"allOf retained property loses branch declaration", `{"allOf":[{"type":"object","properties":{"label":{"type":"string"}}},{"additionalProperties":{"type":"integer"},"properties":{"label":{"type":"string"}}}]}`, `{"allOf":[{"type":"object","properties":{"label":{"type":"string"}}},{"additionalProperties":{"type":"integer"}}]}`, "", true, false},
		{"allOf retained property gains branch declaration", `{"allOf":[{"type":"object","properties":{"label":{"type":"string"}}},{"additionalProperties":{"type":"integer"}}]}`, `{"allOf":[{"type":"object","properties":{"label":{"type":"string"}}},{"additionalProperties":{"type":"integer"},"properties":{"label":{"type":"string"}}}]}`, "", false, false},
		{"allOf property moves to open branch", `{"allOf":[{"type":"object","additionalProperties":false,"properties":{"label":{"type":"string"}}},{"additionalProperties":true}]}`, `{"allOf":[{"type":"object","additionalProperties":false},{"additionalProperties":true,"properties":{"label":{"type":"string"}}}]}`, "", true, false},
		{"allOf property moves and branches reorder", `{"allOf":[{"additionalProperties":false,"properties":{"label":{"type":"string"}}},{"additionalProperties":true}]}`, `{"allOf":[{"additionalProperties":true,"properties":{"label":{"type":"string"}}},{"additionalProperties":false}]}`, "", true, false},
		{"allOf property moves to patterned branch", `{"allOf":[{"type":"object","additionalProperties":false,"patternProperties":{"^label$":true}},{"type":"object","additionalProperties":false,"properties":{"label":{"type":"string"}}}]}`, `{"allOf":[{"type":"object","additionalProperties":false,"patternProperties":{"^label$":true},"properties":{"label":{"type":"string"}}},{"type":"object","additionalProperties":false}]}`, "", false, true},
		{"optional object addition", `{"type":"object"}`, `{"type":"object","properties":{"parent":{"type":"object","required":["label"],"properties":{"label":{"type":"string"}}}}}`, "", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			document := func(schema string) string {
				doc := directionDocument(schema, false, "3.1.0")
				if tc.components != "" {
					doc = strings.Replace(doc, `"paths":`, `"components":{"schemas":`+tc.components+`},"paths":`, 1)
				}
				return doc
			}
			diff := compareSemanticSchemas(t, document(tc.before), document(tc.after))
			breaking, manual := false, false
			for _, item := range diff.Items {
				breaking = breaking || item.IsBreaking
				manual = manual || (item.MustHandle && !item.IsBreaking)
			}
			if breaking != tc.breaking || manual != tc.manual {
				t.Fatalf("nested property classification = summary=%+v items=%+v, want breaking=%t manual=%t", diff.Summary, diff.Items, tc.breaking, tc.manual)
			}
		})
	}
}

func TestVersionNinePropertyFactsRefreshAndPersist(t *testing.T) {
	for _, failWrite := range []bool{false, true} {
		t.Run(fmt.Sprintf("failWrite=%t", failWrite), func(t *testing.T) {
			store, project, document, branch := newOpenAPIDocumentFlowStore(t)
			objects := newRecordingObjectStorage(nil)
			store.objects = objects
			base := semanticDiffBodySchema(`{"type":"object","additionalProperties":false,"properties":{"label":{"type":"string"}}}`, false)
			changed := semanticDiffBodySchema(`{"type":"object","additionalProperties":false}`, false)
			from := publishOpenAPIDocumentDraft(t, store, "admin", project, document, branch, "1", base, "base")
			to := publishOpenAPIDocumentDraft(t, store, "admin", project, document, branch, "2", changed, "next")
			diff := store.diffForVersionsLocked(document, from.ID, to.ID)
			original := cloneDiff(diff)
			if len(original.Items) != 1 || original.Summary.BreakingChanges != 1 {
				t.Fatalf("fixture must have a breaking removal: %+v", original)
			}
			diff.Summary.ParserVersion, diff.Summary.BreakingChanges = 9, 0
			diff.Items[0].Severity, diff.Items[0].IsBreaking, diff.Items[0].MustHandle = SeverityWarning, false, false
			draft := store.drafts[to.DraftID]
			draft.DiffPreview = cloneDiff(diff)
			revision, timestamp := draft.Revision(), draft.UpdatedAt
			store.versions[from.ID].ParserVersion, store.versions[to.ID].ParserVersion = 9, 9
			repo := &legacyFactsRepository{recordingRepository: newRecordingRepository(store.cloneStateLocked())}
			store.persistence = &postgresPersistence{repo: repo}
			preview, err := store.Draft("reader", project, document, draft.ID)
			if err != nil || preview.DiffPreview.Summary.BreakingChanges != 1 || preview.DiffPreview.Summary.ParserVersion != openAPIParserVersion || preview.Revision() != revision || !preview.UpdatedAt.Equal(timestamp) {
				t.Fatalf("preview refresh = %+v err=%v", preview, err)
			}
			beforeObjects := len(objects.objects)
			if failWrite {
				repo.diffErr = errors.New("property snapshot failed")
				_, err := store.Diff("reader", project, document, diff.ID)
				if !errors.Is(err, repo.diffErr) || store.diffs[diff.ID].Summary.ParserVersion != 9 || repo.state.Diffs[diff.ID].Summary.ParserVersion != 9 || len(objects.objects) != beforeObjects {
					t.Fatalf("failed property refresh changed snapshot: err=%v diff=%+v", err, store.diffs[diff.ID])
				}
				repo.diffErr = nil
			}
			updated, err := store.Diff("reader", project, document, diff.ID)
			if err != nil || updated.ID != diff.ID || updated.Summary.ParserVersion != openAPIParserVersion || updated.Summary.BreakingChanges != 1 || !updated.Items[0].MustHandle || !updated.CreatedAt.Equal(original.CreatedAt) {
				t.Fatalf("diff refresh = %+v err=%v", updated, err)
			}
			for _, version := range []*ContractVersion{from, to} {
				current := store.versions[version.ID]
				if current.ParserVersion != openAPIParserVersion || current.RawSchemaHash != version.RawSchemaHash || current.NormalizedSchemaHash != version.NormalizedSchemaHash {
					t.Fatalf("immutable property source changed: %+v", current)
				}
			}
			writes := repo.diffWrites
			reloaded := NewStore()
			reloaded.persistence, reloaded.objects = &postgresPersistence{repo: repo}, objects
			again, err := reloaded.Diff("reader", project, document, diff.ID)
			if err != nil || !valuesEqual(updated, again) || repo.diffWrites != writes {
				t.Fatalf("persisted property diff not reused: diff=%+v err=%v", again, err)
			}
		})
	}
}
