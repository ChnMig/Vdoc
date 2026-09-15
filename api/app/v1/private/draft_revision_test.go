package private

import (
	"encoding/json"
	"net/http"
	"testing"

	"vdoc/api/app/v1/private/shared"
	app "vdoc/appstore"
)

func TestPrivateDraftSaveRequiresCurrentRevision(t *testing.T) {
	for _, documentType := range []int{app.DocumentTypeOpenAPI, app.DocumentTypeMarkdown} {
		name, original, newer := "openapi", privateTestOpenAPI("original"), privateTestOpenAPI("newer")
		if documentType == app.DocumentTypeMarkdown {
			name, original, newer = "markdown", "# Original", "# Editor A"
		}
		t.Run(name, func(t *testing.T) {
			fixture := setupPrivateTask5Project(t)
			store := app.DefaultStore()
			document, err := store.CreateDocument(fixture.adminUser.ID, fixture.project.ID, name, documentType, name+"/draft.md", "")
			if err != nil {
				t.Fatal(err)
			}
			branches, err := store.ListBranches(fixture.adminUser.ID, fixture.project.ID, document.ID)
			if err != nil {
				t.Fatal(err)
			}
			base := "/api/v1/private/projects/" + fixture.project.ID + "/documents/" + document.ID + "/drafts"
			created := decodePrivateEnvelope(t, performPrivateJSON(fixture.router, http.MethodPost, base, fixture.writerToken, `{"branch_id":`+jsonString(branches[0].ID)+`,"version_name":"1.0.0","content":`+jsonString(original)+`}`))
			var draft shared.DraftDTO
			if err := json.Unmarshal(created.Detail, &draft); err != nil || draft.Revision == "" {
				t.Fatalf("create draft revision: %+v err=%v", created, err)
			}
			path := base + "/" + draft.ID
			patch := func(revision, content string) privateTestEnvelope {
				return decodePrivateEnvelope(t, performPrivateJSON(fixture.router, http.MethodPatch, path, fixture.writerToken, `{"expected_revision":`+jsonString(revision)+`,"content":`+jsonString(content)+`}`))
			}
			if missing := patch("", newer); missing.Status != "INVALID_ARGUMENT" {
				t.Fatalf("missing revision = %+v", missing)
			}
			saved := patch(draft.Revision, newer)
			if saved.Status != "OK" {
				t.Fatalf("valid save = %+v", saved)
			}
			if stale := patch(draft.Revision, original); stale.Status != "FAILED_PRECONDITION" {
				t.Fatalf("stale save = %+v", stale)
			}
			read := decodePrivateEnvelope(t, performPrivateJSON(fixture.router, http.MethodGet, path+"/content/raw", fixture.writerToken, ""))
			var snapshot struct {
				shared.ContentDTO
				Draft shared.DraftDTO `json:"draft"`
			}
			if err := json.Unmarshal(read.Detail, &snapshot); err != nil {
				t.Fatal(err)
			}
			if snapshot.Content != newer || snapshot.Hash != snapshot.Draft.RawContentHash || snapshot.Draft.Revision == draft.Revision || snapshot.Draft.Revision == "" {
				t.Fatalf("coherent snapshot after rejected save = %+v", snapshot)
			}
			if retry := patch(snapshot.Draft.Revision, newer+"\n"); retry.Status != "OK" {
				t.Fatalf("reconciled save = %+v", retry)
			}
		})
	}
}
