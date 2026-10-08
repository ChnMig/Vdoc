package private

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"vdoc/api/app/v1/private/shared"
	app "vdoc/appstore"
)

func TestPrivateDraftRejectsPublishedVersionNameBeforeMutation(t *testing.T) {
	for _, document := range []struct {
		name                 string
		kind                 int
		base, original, edit string
	}{
		{"openapi", app.DocumentTypeOpenAPI, privateTestOpenAPI("base"), privateTestOpenAPI("original"), privateTestOpenAPI("edited")},
		{"markdown", app.DocumentTypeMarkdown, "# Base\n", "# Original\n", "# Edited\n"},
	} {
		t.Run(document.name, func(t *testing.T) {
			for _, operation := range []string{"update", "submit"} {
				t.Run(operation, func(t *testing.T) {
					fixture := setupPrivateTask5Project(t)
					store := app.DefaultStore()
					doc, err := store.CreateDocument(fixture.adminUser.ID, fixture.project.ID, document.name, document.kind, document.name+"/versions.md", "")
					if err != nil {
						t.Fatal(err)
					}
					branches, err := store.ListBranches(fixture.adminUser.ID, fixture.project.ID, doc.ID)
					if err != nil || len(branches) == 0 {
						t.Fatalf("list branches: %v, %v", branches, err)
					}
					basePath := "/api/v1/private/projects/" + fixture.project.ID + "/documents/" + doc.ID + "/drafts"
					body := func(values map[string]any) string {
						t.Helper()
						encoded, err := json.Marshal(values)
						if err != nil {
							t.Fatal(err)
						}
						return string(encoded)
					}
					request := func(method, path, token, content string, code int, status string) privateTestEnvelope {
						t.Helper()
						result := decodePrivateEnvelope(t, performPrivateJSON(fixture.router, method, path, token, content))
						if result.Code != code || result.Status != status {
							t.Fatalf("%s %s = %+v, want code=%d status=%s", method, path, result, code, status)
						}
						return result
					}
					create := func(versionName, content string) shared.DraftDTO {
						t.Helper()
						result := request(http.MethodPost, basePath, fixture.writerToken, body(map[string]any{"branch_id": branches[0].ID, "version_name": versionName, "content": content}), 200, "OK")
						var draft shared.DraftDTO
						if err := json.Unmarshal(result.Detail, &draft); err != nil || draft.ID == "" || draft.Revision == "" {
							t.Fatalf("create draft: %+v, %v", draft, err)
						}
						return draft
					}
					publish := func(draftID, versionName string) {
						t.Helper()
						path := basePath + "/" + draftID
						request(http.MethodPost, path+"/submit", fixture.writerToken, "{}", 200, "OK")
						result := decodePrivateEnvelope(t, performPrivateReview(t, fixture.router, http.MethodPost, path+"/approve", fixture.adminToken, "{}"))
						if result.Code != 200 || result.Status != "OK" {
							t.Fatalf("approve: %+v", result)
						}
						var version shared.VersionDTO
						if err := json.Unmarshal(result.Detail, &version); err != nil || version.ID == "" || version.VersionName != versionName {
							t.Fatalf("published version: %+v, %v", version, err)
						}
					}
					candidateName := "1.1.0"
					if operation == "submit" {
						candidateName = "1.0.0"
					}
					candidate := create(candidateName, document.original)
					baseline := create("1.0.0", document.base)
					publish(baseline.ID, "1.0.0")
					path := basePath + "/" + candidate.ID
					type snapshot struct {
						shared.ContentDTO
						Draft shared.DraftDTO `json:"draft"`
					}
					read := func() snapshot {
						t.Helper()
						result := request(http.MethodGet, path+"/content/raw", fixture.writerToken, "", 200, "OK")
						var current snapshot
						if err := json.Unmarshal(result.Detail, &current); err != nil {
							t.Fatal(err)
						}
						if current.Content != document.original || current.Hash != current.Draft.RawContentHash {
							t.Fatalf("draft snapshot: %+v", current)
						}
						return current
					}
					before := read()
					if operation == "update" {
						request(http.MethodPatch, path, fixture.writerToken, body(map[string]any{"expected_revision": before.Draft.Revision, "version_name": "1.0.0", "content": document.edit, "changelog": "must not persist"}), 409, "ALREADY_EXISTS")
					} else {
						request(http.MethodPost, path+"/submit", fixture.writerToken, "{}", 409, "ALREADY_EXISTS")
					}
					after := read()
					if !reflect.DeepEqual(before, after) || after.Draft.Status != app.DraftStatusDraft || after.Draft.SubmittedAt != nil {
						t.Fatalf("rejected %s mutated draft: before=%+v after=%+v", operation, before, after)
					}
					if operation == "submit" {
						request(http.MethodPatch, path, fixture.writerToken, body(map[string]any{"expected_revision": before.Draft.Revision, "version_name": "1.1.0", "content": document.original}), 200, "OK")
						renamed := read()
						if renamed.Draft.VersionName != "1.1.0" || renamed.Draft.Revision == before.Draft.Revision {
							t.Fatalf("rename after rejected submit: %+v", renamed)
						}
					}
					publish(candidate.ID, "1.1.0")
				})
			}
		})
	}
}
