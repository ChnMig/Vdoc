package private

import (
	"encoding/json"
	"net/http"
	"testing"

	"vdoc/api/app/v1/private/documentfolder"
	"vdoc/api/app/v1/private/shared"
	app "vdoc/appstore"
)

func TestPrivateDocumentFoldersAndInitialUpload(t *testing.T) {
	fixture := setupPrivateTask5Project(t)
	base := "/api/v1/private/projects/" + fixture.project.ID
	call := func(method, path, token string, body any, target any) privateTestEnvelope {
		t.Helper()
		payload := ""
		if body != nil {
			encoded, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			payload = string(encoded)
		}
		envelope := decodePrivateEnvelope(t, performPrivateJSON(fixture.router, method, base+path, token, payload))
		if envelope.Code != 200 || envelope.Status != "OK" {
			t.Fatalf("%s %s: %+v", method, path, envelope)
		}
		if target != nil {
			if err := json.Unmarshal(envelope.Detail, target); err != nil {
				t.Fatal(err)
			}
		}
		return envelope
	}
	var root, child documentfolder.FolderDTO
	call(http.MethodPost, "/folders", fixture.adminToken, map[string]any{"name": "Guides", "parent_id": nil}, &root)
	if root.ID == "" || root.ParentID != nil || root.Path != "Guides" {
		t.Fatalf("root folder = %+v", root)
	}
	call(http.MethodPost, "/folders", fixture.adminToken, map[string]any{"name": "Setup", "parent_id": root.ID}, &child)
	if child.ParentID == nil || *child.ParentID != root.ID || child.Path != "Guides/Setup" {
		t.Fatalf("nested folder = %+v", child)
	}
	var folders []documentfolder.FolderDTO
	listed := call(http.MethodGet, "/folders", fixture.writerToken, nil, &folders)
	if len(folders) != 2 || listed.Total == nil || *listed.Total != 2 {
		t.Fatalf("folder listing = %+v, %+v", folders, listed)
	}
	denied := decodePrivateEnvelope(t, performPrivateJSON(fixture.router, http.MethodPost, base+"/folders", fixture.writerToken, `{"name":"Forbidden"}`))
	if denied.Status != "PERMISSION_DENIED" {
		t.Fatalf("writer folder creation = %+v", denied)
	}
	duplicate := decodePrivateEnvelope(t, performPrivateJSON(fixture.router, http.MethodPost, base+"/folders", fixture.adminToken, `{"name":"guides"}`))
	if duplicate.Status != "ALREADY_EXISTS" {
		t.Fatalf("case-insensitive duplicate folder = %+v", duplicate)
	}
	var created struct {
		shared.DocumentDTO
		InitialDraftID  string `json:"initial_draft_id"`
		InitialBranchID string `json:"initial_branch_id"`
	}
	call(http.MethodPost, "/documents", fixture.adminToken, map[string]any{
		"name": "Visible document title", "document_type": app.DocumentTypeText,
		"file_name": "install", "folder_id": child.ID,
		"initial_content": map[string]any{"content": ""},
	}, &created)
	if created.ID == "" || created.Name != "Visible document title" || created.RelativePath != "Guides/Setup/install.txt" || created.InitialDraftID == "" || created.InitialBranchID == "" {
		t.Fatalf("initial upload document = %+v", created)
	}
	var draft shared.DraftDTO
	call(http.MethodGet, "/documents/"+created.ID+"/drafts/"+created.InitialDraftID, fixture.adminToken, nil, &draft)
	if draft.Status != app.DraftStatusDraft || draft.VersionName != "0.1.0" || draft.BranchID != created.InitialBranchID {
		t.Fatalf("initial upload draft = %+v", draft)
	}
	var versions []shared.VersionDTO
	call(http.MethodGet, "/documents/"+created.ID+"/versions", fixture.writerToken, nil, &versions)
	if len(versions) != 0 {
		t.Fatalf("initial upload unexpectedly published versions: %+v", versions)
	}
	var moved shared.DocumentDTO
	call(http.MethodPatch, "/documents/"+created.ID, fixture.adminToken, map[string]any{"folder_id": nil}, &moved)
	if moved.RelativePath != "install.txt" || moved.Name != created.Name {
		t.Fatalf("move to project root = %+v", moved)
	}
	call(http.MethodPatch, "/documents/"+created.ID, fixture.adminToken, map[string]any{"file_name": "renamed"}, &moved)
	if moved.RelativePath != "renamed.txt" || moved.Name != created.Name {
		t.Fatalf("filename rename changed display name: %+v", moved)
	}
}

func TestPrivateDocumentLocationJSONValidationAndAtomicFailure(t *testing.T) {
	fixture := setupPrivateTask5Project(t)
	base := "/api/v1/private/projects/" + fixture.project.ID
	for _, body := range []string{
		`{"name":"Mixed","file_name":"notes.txt","relative_path":""}`,
		`{"name":"Mixed null","file_name":"notes.txt","relative_path":null}`,
		`{"name":"Missing filename","folder_id":null}`,
		`{"name":"Null filename","file_name":null}`,
		`{"name":"Bad folder","file_name":"notes.txt","folder_id":1}`,
		`{"name":"Missing content","initial_content":{}}`,
		`{"name":"Null content","initial_content":{"content":null}}`,
		`{"name":"Null upload","initial_content":null}`,
	} {
		result := decodePrivateEnvelope(t, performPrivateJSON(fixture.router, http.MethodPost, base+"/documents", fixture.adminToken, body))
		if result.Status != "INVALID_ARGUMENT" {
			t.Fatalf("invalid request %s: %+v", body, result)
		}
	}
	failed := decodePrivateEnvelope(t, performPrivateJSON(fixture.router, http.MethodPost, base+"/documents", fixture.adminToken, `{"name":"Invalid schema","document_type":1,"relative_path":"temporary/new/schema.yaml","initial_content":{"content":"invalid OpenAPI"}}`))
	if failed.Code == 200 {
		t.Fatalf("invalid OpenAPI upload succeeded: %+v", failed)
	}
	for _, suffix := range []string{"/documents", "/folders"} {
		result := decodePrivateEnvelope(t, performPrivateJSON(fixture.router, http.MethodGet, base+suffix, fixture.adminToken, ""))
		if result.Code != 200 || result.Total == nil || *result.Total != 0 {
			t.Fatalf("failed upload left %s: %+v", suffix, result)
		}
	}
	document, err := app.DefaultStore().CreateDocument(fixture.adminUser.ID, fixture.project.ID, "Existing", app.DocumentTypeText, "existing.txt", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		`{"file_name":"new.txt","relative_path":""}`,
		`{"folder_id":null,"relative_path":null}`,
		`{"file_name":null}`,
		`{"folder_id":false}`,
	} {
		result := decodePrivateEnvelope(t, performPrivateJSON(fixture.router, http.MethodPatch, base+"/documents/"+document.ID, fixture.adminToken, body))
		if result.Status != "INVALID_ARGUMENT" {
			t.Fatalf("invalid patch %s: %+v", body, result)
		}
	}
}
