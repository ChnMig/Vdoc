package vdoc

import (
	"fmt"
	"strings"
	"testing"
)

func TestFollowupLiteralSchemaValues(t *testing.T) {
	for _, tc := range []struct{ name, before, after string }{
		{"additional_properties_const", `{"additionalProperties":{"const":[]}}`, `{"additionalProperties":{"const":null}}`},
		{"not_const", `{"not":{"const":[]}}`, `{"not":{"const":null}}`},
		{"nested_literal_array", `{"additionalProperties":{"const":{"values":[]}}}`, `{"additionalProperties":{"const":{"values":null}}}`},
		{"enum_const_intersection", `{"enum":[[]],"const":[]}`, `{"enum":[[]],"const":null}`},
		{"enum_nested_const_intersection", `{"enum":[{"values":[]}],"const":{"values":[]}}`, `{"enum":[{"values":[]}],"const":{"values":null}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			diff := compareSemanticSchemas(t, semanticDiffBodySchema(tc.before, false), semanticDiffBodySchema(tc.after, false))
			assertFollowupReview(t, diff)
		})
	}
}

func TestFollowupResponseHeadersAndMediaEncoding(t *testing.T) {
	response := func(headers string) string {
		return followupOperation(`"responses":{"200":{"description":"ok","headers":` + headers + `}}`)
	}
	request := func(encoding string) string {
		return followupOperation(`"requestBody":{"content":{"multipart/form-data":{"schema":{"type":"object","properties":{"file":{"type":"string"}}},"encoding":` + encoding + `}}},"responses":{"200":{"description":"ok"}}`)
	}
	for _, tc := range []struct{ name, before, after string }{
		{"header_schema", response(`{"X-Count":{"schema":{"type":"integer"}}}`), response(`{"X-Count":{"schema":{"type":"string"}}}`)},
		{"header_removed", response(`{"X-Count":{"required":true,"schema":{"type":"integer"}}}`), response(`{}`)},
		{"header_content", response(`{"X-Count":{"content":{"application/json":{"schema":{"type":"integer"}}}}}`), response(`{"X-Count":{"content":{"application/json":{"schema":{"type":"string"}}}}}`)},
		{"encoding_content_type", request(`{"file":{"contentType":"image/png"}}`), request(`{"file":{"contentType":"image/jpeg"}}`)},
		{"encoding_header", request(`{"file":{"headers":{"X-Mode":{"schema":{"const":"a"}}}}}`), request(`{"file":{"headers":{"X-Mode":{"schema":{"const":"b"}}}}}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertFollowupReview(t, compareSemanticSchemas(t, tc.before, tc.after))
		})
	}
}

func TestFollowupEffectiveServers(t *testing.T) {
	for _, level := range []string{"root", "path", "operation"} {
		t.Run(level, func(t *testing.T) {
			server := `"servers":[{"url":"https://before.example.test"}],`
			root, path, operation := "", "", ""
			switch level {
			case "root":
				root = server
			case "path":
				path = server
			case "operation":
				operation = server
			}
			before := fmt.Sprintf(`{"openapi":"3.1.0",%s"info":{"title":"Servers","version":"1"},"paths":{"/values":{%s"post":{%s"responses":{"200":{"description":"ok"}}}}}}`, root, path, operation)
			after := strings.Replace(before, "before.example.test", "after.example.test", 1)
			assertFollowupReview(t, compareSemanticSchemas(t, before, after))
		})
	}
}

func TestFollowupNullableDialect(t *testing.T) {
	for _, tc := range []struct {
		name, before, after string
		breaking            bool
	}{
		{"3.1_nullable_cannot_restore_null", semanticDiffBodySchema(`{"type":["string","null"]}`, false), semanticDiffBodySchema(`{"type":"string","nullable":true}`, false), true},
		{"3.0_nullable_removal", strings.Replace(semanticDiffBodySchema(`{"type":"string","nullable":true}`, false), "3.1.0", "3.0.3", 1), strings.Replace(semanticDiffBodySchema(`{"type":"string"}`, false), "3.1.0", "3.0.3", 1), true},
		{"3.1_nullable_is_annotation", semanticDiffBodySchema(`{"type":"string"}`, false), semanticDiffBodySchema(`{"type":"string","nullable":true}`, false), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			diff := compareSemanticSchemas(t, tc.before, strings.Replace(tc.after, `"version":"1"`, `"version":"2"`, 1))
			if tc.breaking {
				assertFollowupReview(t, diff)
			} else if len(diff.Items) != 0 {
				t.Fatalf("3.1 unknown nullable changed standard validation: %+v", diff.Items)
			}
		})
	}
}

func TestFollowupTransportEquivalentChanges(t *testing.T) {
	for _, tc := range []struct{ name, before, after string }{
		{"header_annotations", followupOperation(`"responses":{"200":{"description":"ok","headers":{"X-Data":{"description":"before","schema":{"type":"string","description":"before"}}}}}`), followupOperation(`"responses":{"200":{"description":"updated","headers":{"x-data":{"description":"after","schema":{"description":"after","type":"string"}}}}}`)},
		{"ignored_content_type_header", followupOperation(`"responses":{"200":{"description":"ok"}}`), followupOperation(`"responses":{"200":{"description":"ok","headers":{"Content-Type":{"schema":{"type":"string"}}}}}`)},
		{"default_servers", followupOperation(`"responses":{"200":{"description":"ok"}}`), followupOperation(`"servers":[{"url":"/"}],"responses":{"200":{"description":"ok"}}`)},
		{"server_annotations_and_enum_order", followupOperation(`"servers":[{"url":"https://{env}.example.test","description":"before","variables":{"env":{"description":"before","default":"dev","enum":["dev","prod"]}}}],"responses":{"200":{"description":"ok"}}`), followupOperation(`"servers":[{"url":"https://{env}.example.test","description":"after","variables":{"env":{"description":"after","default":"dev","enum":["prod","dev"]}}}],"responses":{"200":{"description":"ok"}}`)},
		{"encoding_header_annotations", followupOperation(`"requestBody":{"content":{"multipart/form-data":{"encoding":{"file":{"headers":{"X-Mode":{"description":"before","schema":{"type":"string"}}}}}}}},"responses":{"200":{"description":"ok"}}`), followupOperation(`"requestBody":{"content":{"multipart/form-data":{"encoding":{"file":{"headers":{"X-Mode":{"description":"after","schema":{"type":"string"}}}}}}}},"responses":{"200":{"description":"ok"}}`)},
		{"exact_numeric_const", semanticDiffBodySchema(`{"enum":[1.0],"const":1e0}`, false), semanticDiffBodySchema(`{"enum":[1],"const":1}`, false)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			diff := compareSemanticSchemas(t, tc.before, strings.Replace(tc.after, `"version":"1"`, `"version":"2"`, 1))
			if len(diff.Items) != 0 {
				t.Fatalf("equivalent contract change: %+v", diff.Items)
			}
		})
	}
}

func TestFollowupReferencesInNamedMaps(t *testing.T) {
	for _, status := range []string{"200", "default"} {
		t.Run(status, func(t *testing.T) {
			before := fmt.Sprintf(`{"openapi":"3.1.0","info":{"title":"Response refs","version":"1"},"paths":{"/values":{"post":{"responses":{"%s":{"$ref":"#/components/responses/Result"}}}}},"components":{"responses":{"Result":{"description":"ok","content":{"application/json":{"schema":{"type":"string"}}}}}}}`, status)
			after := strings.Replace(before, `"type":"string"`, `"type":"integer"`, 1)
			assertFollowupReview(t, compareSemanticSchemas(t, before, after))
		})
	}
	// 编码项的名字是业务属性名，不应被当成 $ref 或 example 关键字。
	for _, name := range []string{"file", "schema", "example", "$ref"} {
		t.Run("encoding_"+name, func(t *testing.T) {
			before := fmt.Sprintf(`{"openapi":"3.1.0","info":{"title":"Encoding refs","version":"1"},"paths":{"/values":{"post":{"requestBody":{"content":{"multipart/form-data":{"schema":{"type":"object","properties":{"%s":{"type":"string"}}},"encoding":{"%s":{"headers":{"X-Mode":{"$ref":"#/components/headers/Mode"}}}}}}},"responses":{"200":{"description":"ok"}}}}},"components":{"headers":{"Mode":{"schema":{"const":"a"}}}}}`, name, name)
			after := strings.Replace(before, `"const":"a"`, `"const":"b"`, 1)
			assertFollowupReview(t, compareSemanticSchemas(t, before, after))
		})
	}
}

func TestFollowupNullablePreservesLiteralsAndPublishedDetails(t *testing.T) {
	before := semanticDiffBodySchema(`{"type":"object","properties":{"nullable":{"type":"string","nullable":true}},"additionalProperties":{"const":{"nullable":true}}}`, false)
	after := strings.Replace(before, `"const":{"nullable":true}`, `"const":{"nullable":false}`, 1)
	assertFollowupReview(t, compareSemanticSchemas(t, before, after))
	parsed, err := ParseOpenAPI(before)
	if err != nil {
		t.Fatal(err)
	}
	original := cloneEndpoint(&parsed.Endpoints[0])
	builder := semanticDiffBuilder{}
	builder.compareEndpoint(parsed.Endpoints[0], parsed.Endpoints[0])
	if !valuesEqual(original, &parsed.Endpoints[0]) {
		t.Fatal("comparison mutated published endpoint details")
	}
}

func TestFollowupVersionSixFactsRefresh(t *testing.T) {
	for _, tc := range []struct{ name, before, after string }{
		{"literal", semanticDiffBodySchema(`{"additionalProperties":{"const":[]}}`, false), semanticDiffBodySchema(`{"additionalProperties":{"const":null}}`, false)},
		{"dialect", semanticDiffBodySchema(`{"type":["string","null"]}`, false), semanticDiffBodySchema(`{"type":"string","nullable":true}`, false)},
		{"server", followupOperation(`"servers":[{"url":"https://old.example.test"}],"responses":{"200":{"description":"ok"}}`), followupOperation(`"servers":[{"url":"https://new.example.test"}],"responses":{"200":{"description":"ok"}}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, project, document, branch := newOpenAPIDocumentFlowStore(t)
			objects := newRecordingObjectStorage(nil)
			store.objects = objects
			from := publishOpenAPIDocumentDraft(t, store, "admin", project, document, branch, "1", tc.before, "base")
			to := publishOpenAPIDocumentDraft(t, store, "admin", project, document, branch, "2", tc.after, "next")
			diff := store.diffForVersionsLocked(document, from.ID, to.ID)
			diff.Items = nil
			diff.Summary = DiffSummary{ParserVersion: 6, DocumentFormat: DocumentFormatOpenAPI31}
			draft := store.drafts[to.DraftID]
			draft.DiffPreview = cloneDiff(diff)
			revision, timestamp := draft.Revision(), draft.UpdatedAt
			for _, version := range []*ContractVersion{store.versions[from.ID], store.versions[to.ID]} {
				version.ParserVersion = 6
			}
			for _, endpoint := range store.endpoints {
				operation, _ := asMap(endpoint.NormalizedOperation)
				delete(operation, "openapi")
			}
			repo := &legacyFactsRepository{recordingRepository: newRecordingRepository(store.cloneStateLocked())}
			store.persistence = &postgresPersistence{repo: repo}
			preview, err := store.Draft("reader", project, document, draft.ID)
			if err != nil {
				t.Fatal(err)
			}
			assertFollowupReview(t, preview.DiffPreview)
			if preview.Revision() != revision || !preview.UpdatedAt.Equal(timestamp) {
				t.Fatal("facts refresh changed the draft revision")
			}
			updated, err := store.Diff("reader", project, document, diff.ID)
			if err != nil {
				t.Fatal(err)
			}
			assertFollowupReview(t, updated)
			if updated.Summary.ParserVersion != openAPIParserVersion || updated.ID != diff.ID || repo.diffWrites != 1 {
				t.Fatalf("stale facts persisted: %+v writes=%d", updated, repo.diffWrites)
			}
			for _, version := range []*ContractVersion{from, to} {
				current := store.versions[version.ID]
				if current.RawSchemaHash != version.RawSchemaHash || current.NormalizedSchemaHash != version.NormalizedSchemaHash {
					t.Fatal("facts refresh changed an immutable source hash")
				}
			}
			reloaded := NewStore()
			reloaded.persistence, reloaded.objects = &postgresPersistence{repo: repo}, objects
			again, err := reloaded.Diff("reader", project, document, diff.ID)
			if err != nil || !valuesEqual(updated, again) || repo.diffWrites != 1 {
				t.Fatalf("refreshed diff was not reused: err=%v writes=%d", err, repo.diffWrites)
			}
		})
	}
}

func followupOperation(operation string) string {
	return `{"openapi":"3.1.0","info":{"title":"Followup","version":"1"},"paths":{"/values":{"post":{` + operation + `}}}}`
}

func assertFollowupReview(t *testing.T, diff *Diff) {
	t.Helper()
	for _, item := range diff.Items {
		if item.MustHandle {
			return
		}
	}
	t.Fatalf("contract change has no required review signal: summary=%+v items=%+v", diff.Summary, diff.Items)
}
