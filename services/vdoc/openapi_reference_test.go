package vdoc

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	domainvdoc "vdoc/domain/vdoc"
)

func TestOpenAPIReferenceSiblingSemantics(t *testing.T) {
	for _, version := range []string{"3.0.3", "3.1.0"} {
		t.Run(version, func(t *testing.T) {
			root, err := decodeOpenAPI(`{"openapi":"` + version + `","components":{"schemas":{"ID":{"type":"string","enum":["a","b"],"maxLength":20}},"responses":{"OK":{"description":"original","content":{}}}}}`)
			if err != nil {
				t.Fatal(err)
			}
			input := map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/ID", "enum": []any{"a"}, "maxLength": 10}}
			resolved, err := resolveRefs(root, input, map[string]bool{}, map[string]bool{})
			if err != nil {
				t.Fatal(err)
			}
			schema := resolved.(map[string]any)["schema"]
			if schemaType(schema) != "string" {
				t.Fatalf("resolved type = %s", schemaType(schema))
			}
			want := []string{"a", "b"}
			if version == "3.1.0" {
				want = []string{"a"}
			}
			if !valuesEqual(enumValues(schema), want) {
				t.Fatalf("enum = %v, want %v", enumValues(schema), want)
			}
			response, err := resolveRefs(root, map[string]any{"$ref": "#/components/responses/OK", "description": "overridden", "required": true}, map[string]bool{}, map[string]bool{})
			if err != nil {
				t.Fatal(err)
			}
			got := response.(map[string]any)
			wantDescription := "original"
			if version == "3.1.0" {
				wantDescription = "overridden"
			}
			if got["description"] != wantDescription || got["required"] != nil {
				t.Fatalf("reference object siblings = %v", got)
			}
		})
	}
}

func TestOpenAPISchemaPropertyNamesAndExamplesAreNotReferenceKeywords(t *testing.T) {
	root := map[string]any{"openapi": "3.1.0", "components": map[string]any{"schemas": map[string]any{"ID": map[string]any{"type": "string"}}}}
	input := map[string]any{"schema": map[string]any{
		"type":       "object",
		"properties": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/ID", "enum": []any{"a"}}},
		"examples":   []any{map[string]any{"$ref": "literal data", "schema": "not a schema"}},
	}}
	resolved, err := resolveRefs(root, input, map[string]bool{}, map[string]bool{})
	if err != nil {
		t.Fatal(err)
	}
	schema := resolved.(map[string]any)["schema"].(map[string]any)
	property := schema["properties"].(map[string]any)["schema"]
	if !valuesEqual(enumValues(property), []string{"a"}) {
		t.Fatalf("property = %v", property)
	}
	if !valuesEqual(schema["examples"], input["schema"].(map[string]any)["examples"]) {
		t.Fatal("examples were changed")
	}
}

func TestOpenAPIParameterOverrideResolvesReferencesAndRejectsSameLevelDuplicates(t *testing.T) {
	root := map[string]any{"openapi": "3.1.0", "components": map[string]any{"parameters": map[string]any{"Limit": map[string]any{"name": "limit", "in": "query", "required": true}}}}
	path := []any{map[string]any{"$ref": "#/components/parameters/Limit"}}
	operation := []any{map[string]any{"name": "limit", "in": "query", "required": false}, map[string]any{"name": "limit", "in": "header"}}
	parameters, err := resolveParameters(root, path, operation, map[string]bool{})
	if err != nil || len(parameters) != 2 || parameters[0].(map[string]any)["required"] != false {
		t.Fatalf("parameters=%v err=%v", parameters, err)
	}
	_, err = resolveParameters(root, nil, append(operation, path[0]), map[string]bool{})
	if !Is(err, ErrInvalidArgument) {
		t.Fatalf("duplicate error = %v", err)
	}
}

func TestOpenAPISecurityDefinitionsOnlyIncludeUsedSchemes(t *testing.T) {
	root, err := decodeOpenAPI(`{"openapi":"3.1.0","components":{"securitySchemes":{"used":{"type":"http","scheme":"bearer"},"unused":{"type":"apiKey","in":"header","name":"X-Key"}}}}`)
	if err != nil {
		t.Fatal(err)
	}
	definitions, err := resolveSecuritySchemes(root, []any{map[string]any{"used": []any{}}})
	if err != nil || len(definitions) != 1 || definitions["used"] == nil {
		t.Fatalf("definitions=%v err=%v", definitions, err)
	}
	before, _ := json.Marshal(definitions)
	root["components"].(map[string]any)["securitySchemes"].(map[string]any)["unused"] = map[string]any{"type": "http", "scheme": "basic"}
	after, err := resolveSecuritySchemes(root, []any{map[string]any{"used": []any{}}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(after)
	if string(encoded) != string(before) {
		t.Fatal("unused scheme changed endpoint facts")
	}
	anonymous, err := resolveSecuritySchemes(root, []any{})
	if err != nil || len(anonymous) != 0 {
		t.Fatalf("anonymous security = %v err=%v", anonymous, err)
	}
}

func TestLegacyEndpointAndDiffFactsRefreshFromImmutableSource(t *testing.T) {
	store, projectID, documentID, branchID := newOpenAPIDocumentFlowStore(t)
	objects := newRecordingObjectStorage(nil)
	store.objects = objects
	base := `{"openapi":"3.1.0","info":{"title":"Legacy facts","version":"1"},"security":[{"key":[]}],"paths":{"/widgets":{"get":{"responses":{"200":{"description":"ok"}}}}},"components":{"securitySchemes":{"key":{"type":"apiKey","in":"header","name":"X-Key"}}}}`
	from := publishOpenAPIDocumentDraft(t, store, "admin", projectID, documentID, branchID, "1.0.0", base, "base")
	to := publishOpenAPIDocumentDraft(t, store, "admin", projectID, documentID, branchID, "1.1.0", strings.Replace(base, "X-Key", "X-New-Key", 1), "changed")
	diff := store.diffForVersionsLocked(documentID, from.ID, to.ID)
	diff.Items = nil
	diff.Summary = DiffSummary{DocumentFormat: DocumentFormatOpenAPI31}
	oldDraft := store.drafts[to.DraftID]
	oldDraft.DiffPreview.Items = nil
	oldDraft.DiffPreview.Summary = DiffSummary{DocumentFormat: DocumentFormatOpenAPI31}
	draftUpdatedAt := oldDraft.UpdatedAt
	var endpointID string
	for _, endpoint := range store.endpoints {
		delete(endpoint.NormalizedOperation.(map[string]any), "securitySchemes")
		if endpoint.ContractVersionID == to.ID {
			endpointID = endpoint.ID
		}
	}
	state := store.cloneStateLocked()
	for _, version := range state.Versions {
		version.RawSchema = ""
		version.NormalizedSchema = ""
	}
	repo := &legacyFactsRepository{recordingRepository: newRecordingRepository(state)}
	store.persistence = &postgresPersistence{repo: repo}
	endpoint, err := store.Endpoint("reader", projectID, documentID, to.ID, endpointID)
	if err != nil {
		t.Fatal(err)
	}
	schemes := endpointSecuritySchemes(*endpoint).(map[string]any)
	if schemes["key"].(map[string]any)["name"] != "X-New-Key" || endpoint.ID != endpointID {
		t.Fatalf("endpoint upgrade = %+v", endpoint)
	}
	draft, err := store.Draft("reader", projectID, documentID, to.DraftID)
	if err != nil || len(draft.DiffPreview.Items) != 1 || !draft.UpdatedAt.Equal(draftUpdatedAt) {
		t.Fatalf("legacy draft preview upgrade = %+v, err=%v", draft, err)
	}
	updated, err := store.Diff("reader", projectID, documentID, diff.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Items) != 1 || updated.ID != diff.ID || updated.Summary.ParserVersion != openAPIParserVersion {
		t.Fatalf("diff upgrade = %+v", updated)
	}
	if repo.diffWrites != 1 || !valuesEqual(repo.state.Diffs[diff.ID], updated) {
		t.Fatal("upgraded diff was not persisted")
	}
	reloaded := NewStore()
	reloaded.persistence = &postgresPersistence{repo: repo}
	reloaded.objects = objects
	again, err := reloaded.Diff("reader", projectID, documentID, diff.ID)
	if err != nil || !valuesEqual(updated, again) {
		t.Fatalf("reload changed upgraded diff: %+v, err=%v", again, err)
	}
	if repo.diffWrites != 1 {
		t.Fatal("current diff was unnecessarily regenerated")
	}

	// 升级失败时保留原差异，并删除本次新建、尚未提交的对象。
	repo.state.Diffs[diff.ID] = cloneDiff(diff)
	repo.diffErr = errors.New("diff write failed")
	previousObjects := len(objects.objects)
	_, err = reloaded.Diff("reader", projectID, documentID, diff.ID)
	if !errors.Is(err, repo.diffErr) || repo.state.Diffs[diff.ID].Summary.ParserVersion != 0 {
		t.Fatalf("failed upgrade changed persisted diff: err=%v", err)
	}
	if reloaded.diffs[diff.ID].Summary.ParserVersion != 0 || len(objects.objects) != previousObjects {
		t.Fatal("failed upgrade was not rolled back or left a new snapshot object")
	}
}

type legacyFactsRepository struct {
	*recordingRepository
	diffWrites int
	diffErr    error
}

func (r *legacyFactsRepository) UpsertDocumentDiff(_ context.Context, diff *domainvdoc.Diff, _, _ *domainvdoc.ContractVersion) error {
	if r.diffErr != nil {
		return r.diffErr
	}
	r.diffWrites++
	r.state.Diffs[diff.ID] = cloneDiff(diff)
	return nil
}

func TestSummaryOnlyDraftPreviewDoesNotProducePhantomWrites(t *testing.T) {
	store, projectID, documentID, branchID := newOpenAPIDocumentFlowStore(t)
	from := publishOpenAPIDocumentDraft(t, store, "admin", projectID, documentID, branchID, "1.0.0", semanticDiffBaselineOpenAPI(), "base")
	draft, err := store.CreateDocumentDraft("admin", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.1.0", SchemaContent: semanticDiffChangedOpenAPI()})
	if err != nil {
		t.Fatal(err)
	}
	store.drafts[draft.ID].DiffPreview = &Diff{FromVersionID: from.ID, Summary: DiffSummary{ModifiedEndpoints: 1}}
	repo := newRecordingRepository(store.stateLocked())
	store.persistence = &postgresPersistence{repo: repo}
	store.persisted = store.cloneStateLocked()
	for range 2 {
		if err := store.persistLocked(); err != nil {
			t.Fatal(err)
		}
	}
	if repo.saves != 0 {
		t.Fatalf("unchanged draft produced %d phantom writes", repo.saves)
	}
}
