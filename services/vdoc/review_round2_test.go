package vdoc

import (
	"fmt"
	"strings"
	"testing"
)

func TestReviewNumericBoundEquivalenceAndDirection(t *testing.T) {
	for _, tc := range []struct {
		name, version, before, after string
		change, breaking             bool
	}{
		{"3.0_false_min", "3.0.3", `{"type":"number","minimum":0}`, `{"type":"number","minimum":0,"exclusiveMinimum":false}`, false, false},
		{"3.0_false_max", "3.0.3", `{"type":"number","maximum":10}`, `{"type":"number","maximum":10,"exclusiveMaximum":false}`, false, false},
		{"3.0_strict_min_removed", "3.0.3", `{"type":"number","minimum":0,"exclusiveMinimum":true}`, `{"type":"number","minimum":0}`, true, false},
		{"3.0_strict_max_removed", "3.0.3", `{"type":"number","maximum":10,"exclusiveMaximum":true}`, `{"type":"number","maximum":10}`, true, false},
		{"3.1_strict_min_to_inclusive", "3.1.0", `{"type":"number","exclusiveMinimum":0}`, `{"type":"number","minimum":0}`, true, false},
		{"3.1_strict_max_to_inclusive", "3.1.0", `{"type":"number","exclusiveMaximum":10}`, `{"type":"number","maximum":10}`, true, false},
		{"3.1_redundant_exclusive_min", "3.1.0", `{"type":"number","minimum":0}`, `{"type":"number","minimum":0,"exclusiveMinimum":-1}`, false, false},
		{"3.1_redundant_inclusive_min", "3.1.0", `{"type":"number","exclusiveMinimum":0}`, `{"type":"number","exclusiveMinimum":0,"minimum":0}`, false, false},
		{"3.1_redundant_exclusive_max", "3.1.0", `{"type":"number","maximum":10}`, `{"type":"number","maximum":10,"exclusiveMaximum":11}`, false, false},
		{"3.1_tighter_inclusive_min", "3.1.0", `{"type":"number","exclusiveMinimum":0}`, `{"type":"number","minimum":1}`, true, true},
		{"3.1_tighter_exclusive_max", "3.1.0", `{"type":"number","maximum":10}`, `{"type":"number","exclusiveMaximum":10}`, true, true},
	} {
		for _, response := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/response=%v", tc.name, response), func(t *testing.T) {
				before := strings.Replace(semanticDiffBodySchema(tc.before, response), "3.1.0", tc.version, 1)
				after := strings.Replace(semanticDiffBodySchema(tc.after, response), "3.1.0", tc.version, 1)
				diff := compareSemanticSchemas(t, before, after)
				if !tc.change {
					if len(diff.Items) != 0 {
						t.Fatalf("equivalent acceptance set reported as changed: %+v", diff.Items)
					}
					return
				}
				breaking := tc.breaking != response
				if len(diff.Items) != 1 || diff.Items[0].IsBreaking != breaking || diff.Items[0].MustHandle != breaking {
					t.Fatalf("expected one effective bound change, breaking=%v: %+v", breaking, diff.Items)
				}
			})
		}
	}
}

func TestReviewOpenAPIExtensionValuesAreLiteral(t *testing.T) {
	for _, tc := range []struct{ name, before, after string }{
		{"response_extension", followupOperation(`"responses":{"200":{"description":"ok"}}`), followupOperation(`"responses":{"200":{"description":"ok"},"x-notes":{"info":"text"}}`)},
		{"response_extension_ref", followupOperation(`"responses":{"200":{"description":"ok"}}`), followupOperation(`"responses":{"200":{"description":"ok"},"x-notes":{"$ref":"this-is-literal-data"}}`)},
		{"request_body_extension_ref", followupOperation(`"requestBody":{"content":{"application/json":{"schema":{"type":"string"}}}},"responses":{"200":{"description":"ok"}}`), followupOperation(`"requestBody":{"x-example":{"$ref":"literal-data"},"content":{"application/json":{"schema":{"type":"string"}}}},"responses":{"200":{"description":"ok"}}`)},
		{"header_extension_ref", followupOperation(`"responses":{"200":{"description":"ok","headers":{"X-Mode":{"schema":{"type":"string"}}}}}`), followupOperation(`"responses":{"200":{"description":"ok","headers":{"X-Mode":{"x-example":{"$ref":"literal-data"},"schema":{"type":"string"}}}}}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			diff := compareSemanticSchemas(t, tc.before, tc.after)
			if len(diff.Items) != 0 {
				t.Fatalf("extension payload was treated as an API contract: %+v", diff.Items)
			}
		})
	}
}

func TestReviewNumericBoundsInSchemaContainers(t *testing.T) {
	for _, container := range []string{
		`%s`,
		`{"type":"object","properties":{"x-minimum":%s}}`,
		`{"type":"array","items":%s}`,
		`{"anyOf":[%s]}`,
		`{"oneOf":[%s]}`,
		`{"allOf":[%s]}`,
		`{"additionalProperties":%s}`,
		`{"not":%s}`,
		`{"if":%s,"then":{"type":"number"}}`,
		`{"type":"array","contains":%s}`,
		`{"patternProperties":{"^value":%s}}`,
		`{"type":"array","prefixItems":[%s]}`,
	} {
		for _, tc := range []struct{ version, before, after string }{
			{"3.0.3", `{"type":"number","minimum":0,"maximum":10}`, `{"type":"number","minimum":0,"maximum":10,"exclusiveMinimum":false,"exclusiveMaximum":false}`},
			{"3.1.0", `{"type":"number","minimum":0,"maximum":10}`, `{"type":"number","minimum":0,"maximum":10,"exclusiveMinimum":-1,"exclusiveMaximum":11}`},
		} {
			for _, response := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/response=%v", container, tc.version, response), func(t *testing.T) {
					before := strings.Replace(semanticDiffBodySchema(fmt.Sprintf(container, tc.before), response), "3.1.0", tc.version, 1)
					after := strings.Replace(semanticDiffBodySchema(fmt.Sprintf(container, tc.after), response), "3.1.0", tc.version, 1)
					if items := reviewParsedChanges(t, before, after); len(items) != 0 {
						t.Fatalf("equivalent nested numeric bounds reported as changed: %+v", items)
					}
				})
			}
		}
	}
}

func TestReviewNumericBoundAcceptanceMatrix(t *testing.T) {
	// 各掩码独立列出 -1、0、0.5、1、2 五个见证值，覆盖每对边界的全部区间。
	type boundCase struct {
		name, keywords string
		accepts        uint8
	}
	for _, axis := range []struct {
		name  string
		cases []boundCase
	}{
		{"lower", []boundCase{
			{"unbounded", "", 31},
			{"inclusive_zero", `,"minimum":0`, 30},
			{"exclusive_zero", `,"exclusiveMinimum":0`, 28},
			{"inclusive_one", `,"minimum":1`, 24},
			{"exclusive_one", `,"exclusiveMinimum":1`, 16},
			{"redundant_exclusive", `,"minimum":0,"exclusiveMinimum":-1`, 30},
			{"redundant_inclusive", `,"minimum":0,"exclusiveMinimum":0`, 28},
		}},
		{"upper", []boundCase{
			{"unbounded", "", 31},
			{"inclusive_zero", `,"maximum":0`, 3},
			{"exclusive_zero", `,"exclusiveMaximum":0`, 1},
			{"inclusive_one", `,"maximum":1`, 15},
			{"exclusive_one", `,"exclusiveMaximum":1`, 7},
			{"redundant_exclusive", `,"maximum":0,"exclusiveMaximum":1`, 3},
			{"redundant_inclusive", `,"maximum":0,"exclusiveMaximum":0`, 1},
		}},
	} {
		for _, before := range axis.cases {
			for _, after := range axis.cases {
				for _, response := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s_to_%s/response=%v", axis.name, before.name, after.name, response), func(t *testing.T) {
						items := reviewParsedChanges(t,
							semanticDiffBodySchema(`{"type":"number"`+before.keywords+`}`, response),
							semanticDiffBodySchema(`{"type":"number"`+after.keywords+`}`, response))
						if before.accepts == after.accepts {
							if len(items) != 0 {
								t.Fatalf("equal acceptance sets changed: %+v", items)
							}
							return
						}
						breaking := before.accepts&^after.accepts != 0
						if response {
							breaking = after.accepts&^before.accepts != 0
						}
						if len(items) != 1 || items[0].IsBreaking != breaking || items[0].MustHandle != breaking {
							t.Fatalf("acceptance sets %05b -> %05b: want one change, breaking=%v: %+v", before.accepts, after.accepts, breaking, items)
						}
					})
				}
			}
		}
	}
}

func TestReviewNumericKeywordNamesInsideLiterals(t *testing.T) {
	for _, container := range []string{
		`{"anyOf":[{"const":%s}]}`,
		`{"additionalProperties":{"const":%s}}`,
		`{"not":{"enum":[%s]}}`,
	} {
		t.Run(container, func(t *testing.T) {
			before := fmt.Sprintf(container, `{"minimum":0}`)
			after := fmt.Sprintf(container, `{"minimum":0,"exclusiveMinimum":-1}`)
			items := reviewParsedChanges(t, semanticDiffBodySchema(before, false), semanticDiffBodySchema(after, false))
			assertFollowupReview(t, &Diff{Items: items})
		})
	}
}

func TestReviewChangedAlternativeBoundsRequireReview(t *testing.T) {
	for _, keyword := range []string{"anyOf", "oneOf"} {
		for _, response := range []bool{false, true} {
			for _, relax := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/response=%v/relax=%v", keyword, response, relax), func(t *testing.T) {
					before := fmt.Sprintf(`{"%s":[{"type":"number","minimum":0},{"type":"string"}]}`, keyword)
					after := strings.Replace(before, `"minimum":0`, `"minimum":1`, 1)
					if relax {
						before, after = after, before
					}
					items := reviewParsedChanges(t, semanticDiffBodySchema(before, response), semanticDiffBodySchema(after, response))
					if len(items) != 1 || items[0].IsBreaking || !items[0].MustHandle || items[0].Severity != SeverityWarning {
						t.Fatalf("branch replacement needs review rather than an unproved breaking classification: %+v", items)
					}
				})
			}
		}
	}
}

func TestReviewExtensionLiteralsAndNamedReferencesRemainDistinct(t *testing.T) {
	const literal = `{"$ref":"https://example.invalid/data","nested":[{"$ref":"#/missing","schema":{"exclusiveMinimum":false}}]}`
	operation := `"requestBody":{"x-data":` + literal + `,"content":{"application/json":{"schema":{"type":"object","properties":{"x-value":{"$ref":"#/components/schemas/Value"}}}}}},"responses":{"x-data":` + literal + `,"200":{"description":"ok","headers":{"x-mode":{"$ref":"#/components/headers/Mode"}}}}`
	document := followupOperation(operation)
	document = strings.TrimSuffix(document, "}") + `,"components":{"schemas":{"Value":{"type":"number","minimum":0}},"headers":{"Mode":{"x-data":` + literal + `,"schema":{"type":"string"}}}}}`
	parsed, err := ParseOpenAPI(document)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := parsed.Endpoints[0]
	body, _ := asMap(endpoint.RequestBody)
	responses, _ := asMap(endpoint.Responses)
	response, _ := asMap(responses["200"])
	headers, _ := asMap(response["headers"])
	header, _ := asMap(headers["x-mode"])
	for name, value := range map[string]any{"body": body["x-data"], "responses": responses["x-data"], "header": header["x-data"]} {
		want, err := decodeOpenAPI(literal)
		if err != nil || !valuesEqual(value, normalizeValue(want)) {
			t.Fatalf("%s extension literal changed: value=%v err=%v", name, value, err)
		}
	}
	content, _ := asMap(body["content"])
	media, _ := asMap(content["application/json"])
	schema, _ := asMap(media["schema"])
	properties, _ := asMap(schema["properties"])
	if schemaType(properties["x-value"]) != "number" || schemaType(header["schema"]) != "string" {
		t.Fatalf("x-prefixed names lost real references: body=%v headers=%v", body, headers)
	}
	for _, tc := range []struct{ name, before, after string }{
		{"property", `"Value":{"type":"number"`, `"Value":{"type":"string"`},
		{"header", `"schema":{"type":"string"}`, `"schema":{"type":"integer"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := strings.Replace(document, tc.before, tc.after, 1)
			items := reviewParsedChanges(t, document, changed)
			assertFollowupReview(t, &Diff{Items: items})
		})
	}
}

func TestReviewActualReferencesStillRejectInvalidTargets(t *testing.T) {
	for _, ref := range []string{"#/components/schemas/Missing", "https://example.invalid/schema"} {
		for _, operation := range []string{
			`"responses":{"200":{"$ref":"%s"}}`,
			`"responses":{"200":{"description":"ok","headers":{"x-mode":{"$ref":"%s"}}}}`,
			`"requestBody":{"content":{"application/json":{"schema":{"properties":{"x-value":{"$ref":"%s"}}}}}},"responses":{"200":{"description":"ok"}}`,
		} {
			t.Run(operation+"/"+ref, func(t *testing.T) {
				_, err := ParseOpenAPI(followupOperation(fmt.Sprintf(operation, ref)))
				if !Is(err, ErrInvalidArgument) {
					t.Fatalf("real reference should still reject an invalid target: %v", err)
				}
			})
		}
	}
}

func TestReviewVersionSevenFactsRemoveFalseChanges(t *testing.T) {
	for _, tc := range []struct{ name, before, after string }{
		{"bound", semanticDiffBodySchema(`{"type":"number","minimum":0}`, false), semanticDiffBodySchema(`{"type":"number","minimum":0,"exclusiveMinimum":-1}`, false)},
		{"extension", followupOperation(`"responses":{"200":{"description":"ok"}}`), followupOperation(`"responses":{"200":{"description":"ok"},"x-data":{"info":"notes"}}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, project, document, branch := newOpenAPIDocumentFlowStore(t)
			objects := newRecordingObjectStorage(nil)
			store.objects = objects
			from := publishOpenAPIDocumentDraft(t, store, "admin", project, document, branch, "1", tc.before, "base")
			to := publishOpenAPIDocumentDraft(t, store, "admin", project, document, branch, "2", tc.after, "next")
			diff := store.diffForVersionsLocked(document, from.ID, to.ID)
			diff.Items = []DiffItem{{ID: "outdated", IsBreaking: true, MustHandle: true}}
			diff.Summary = DiffSummary{ParserVersion: 7, DocumentFormat: DocumentFormatOpenAPI31, ModifiedEndpoints: 1, BreakingChanges: 1}
			draft := store.drafts[to.DraftID]
			draft.DiffPreview = cloneDiff(diff)
			revision, timestamp := draft.Revision(), draft.UpdatedAt
			store.versions[from.ID].ParserVersion, store.versions[to.ID].ParserVersion = 7, 7
			repo := &legacyFactsRepository{recordingRepository: newRecordingRepository(store.cloneStateLocked())}
			store.persistence = &postgresPersistence{repo: repo}
			preview, err := store.Draft("reader", project, document, draft.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(preview.DiffPreview.Items) != 0 || preview.DiffPreview.Summary.ParserVersion != openAPIParserVersion {
				t.Fatalf("draft preview retained a false change: %+v", preview.DiffPreview)
			}
			if preview.Revision() != revision || !preview.UpdatedAt.Equal(timestamp) {
				t.Fatal("facts refresh changed draft review metadata")
			}
			updated, err := store.Diff("reader", project, document, diff.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(updated.Items) != 0 || updated.Summary.BreakingChanges != 0 || updated.Summary.ModifiedEndpoints != 0 || updated.Summary.ParserVersion != openAPIParserVersion || updated.ID != diff.ID || repo.diffWrites != 1 {
				t.Fatalf("stale diff facts were not replaced: %+v writes=%d", updated, repo.diffWrites)
			}
			for _, original := range []*ContractVersion{from, to} {
				current := store.versions[original.ID]
				if current.ParserVersion != openAPIParserVersion || current.RawSchemaHash != original.RawSchemaHash || current.NormalizedSchemaHash != original.NormalizedSchemaHash {
					t.Fatal("facts refresh did not preserve immutable source hashes")
				}
			}
			reloaded := NewStore()
			reloaded.persistence, reloaded.objects = &postgresPersistence{repo: repo}, objects
			again, err := reloaded.Diff("reader", project, document, diff.ID)
			if err != nil || !valuesEqual(updated, again) || repo.diffWrites != 1 {
				t.Fatalf("empty refreshed diff was regenerated: err=%v writes=%d", err, repo.diffWrites)
			}
		})
	}
}

func reviewParsedChanges(t *testing.T, before, after string) []DiffItem {
	t.Helper()
	from, err := ParseOpenAPI(before)
	if err != nil {
		t.Fatal(err)
	}
	to, err := ParseOpenAPI(after)
	if err != nil {
		t.Fatal(err)
	}
	if len(from.Endpoints) != 1 || len(to.Endpoints) != 1 {
		t.Fatal("test documents must have exactly one endpoint")
	}
	originalFrom, originalTo := cloneEndpoint(&from.Endpoints[0]), cloneEndpoint(&to.Endpoints[0])
	builder := semanticDiffBuilder{}
	builder.compareEndpoint(from.Endpoints[0], to.Endpoints[0])
	if !valuesEqual(originalFrom, &from.Endpoints[0]) || !valuesEqual(originalTo, &to.Endpoints[0]) {
		t.Fatal("comparison mutated source endpoint details")
	}
	return builder.items
}
