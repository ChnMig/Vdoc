package private

import (
	"encoding/json"
	"net/http"
	"testing"

	app "vdoc/appstore"
)

func TestHistoryPaginationOmitsContentAndReviewCredentials(t *testing.T) {
	fixture := setupPrivateTask5Project(t)
	store := app.DefaultStore()
	document, err := store.CreateDocument(fixture.adminUser.ID, fixture.project.ID, "History", app.DocumentTypeOpenAPI, "api.json", "")
	if err != nil {
		t.Fatal(err)
	}
	branches, err := store.ListBranches(fixture.adminUser.ID, fixture.project.ID, document.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"first", "second"} {
		draft, err := store.CreateDocumentDraft(fixture.adminUser.ID, fixture.project.ID, document.ID, app.DraftInput{BranchID: branches[0].ID, VersionName: name, SchemaContent: privateTestOpenAPI(name)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.SubmitDocumentDraft(fixture.adminUser.ID, fixture.project.ID, document.ID, draft.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := store.ReviewDocumentDraft(fixture.adminUser.ID, fixture.project.ID, document.ID, draft.ID, "approve", reviewInputForTest(t, store, fixture.adminUser.ID, fixture.project.ID, document.ID, draft.ID)); err != nil {
			t.Fatal(err)
		}
	}
	base := "/api/v1/private/projects/" + fixture.project.ID + "/documents/" + document.ID
	for _, path := range []string{"/drafts?page_size=1&offset=1", "/diffs?page_size=1"} {
		envelope := decodePrivateEnvelope(t, performPrivateJSON(fixture.router, http.MethodGet, base+path, fixture.adminToken, ""))
		if envelope.Code != 200 || envelope.Total == nil {
			t.Fatalf("page envelope: %+v", envelope)
		}
		var items []map[string]any
		if err := json.Unmarshal(envelope.Detail, &items); err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 {
			t.Fatalf("page length: %d", len(items))
		}
		for _, key := range []string{"revision", "review_revision", "diff_preview", "items", "raw_schema", "normalized_schema"} {
			if _, ok := items[0][key]; ok {
				t.Fatalf("list leaked detail field %s", key)
			}
		}
	}
	for _, query := range []string{"?page_size=201", "?page_size=0", "?page_size=1&offset=-1"} {
		envelope := decodePrivateEnvelope(t, performPrivateJSON(fixture.router, http.MethodGet, base+"/drafts"+query, fixture.adminToken, ""))
		if envelope.Status != "INVALID_ARGUMENT" {
			t.Fatalf("invalid page accepted: %+v", envelope)
		}
	}
	legacy := decodePrivateEnvelope(t, performPrivateJSON(fixture.router, http.MethodGet, base+"/drafts", fixture.adminToken, ""))
	var detailed []map[string]any
	if err := json.Unmarshal(legacy.Detail, &detailed); err != nil {
		t.Fatal(err)
	}
	if len(detailed) != 2 || detailed[0]["review_revision"] == nil {
		t.Fatal("legacy list contract changed")
	}
}
