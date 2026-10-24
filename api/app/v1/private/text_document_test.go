package private

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"vdoc/api/app/v1/private/shared"
	app "vdoc/appstore"
)

func TestPrivateTXTContentLifecycleUsesStableDTOs(t *testing.T) {
	fixture := setupPrivateTask5Project(t)
	store := app.DefaultStore()
	document, err := store.CreateDocument(fixture.adminUser.ID, fixture.project.ID, "Plain notes", app.DocumentTypeText, "notes/plain.txt", "")
	if err != nil {
		t.Fatal(err)
	}
	branches, err := store.ListBranches(fixture.adminUser.ID, fixture.project.ID, document.ID)
	if err != nil {
		t.Fatal(err)
	}
	branchID := branchesByName(branches)["dev"].ID
	base := "/api/v1/private/projects/" + fixture.project.ID + "/documents/" + document.ID
	for _, body := range []string{
		`{"branch_id":` + jsonString(branchID) + `,"version_name":"1.0.0"}`,
		`{"branch_id":` + jsonString(branchID) + `,"version_name":"1.0.0","content":null}`,
	} {
		result := decodePrivateEnvelope(t, performPrivateJSON(fixture.router, http.MethodPost, base+"/drafts", fixture.writerToken, body))
		if result.Status != "INVALID_ARGUMENT" {
			t.Fatalf("missing TXT content = %+v", result)
		}
	}
	call := func(method, path, token string, body any, target any) json.RawMessage {
		t.Helper()
		encoded := ""
		if body != nil {
			payload, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			encoded = string(payload)
		}
		envelope := decodePrivateEnvelope(t, performPrivateJSON(fixture.router, method, base+path, token, encoded))
		if envelope.Code != 200 || envelope.Status != "OK" {
			t.Fatalf("%s %s: %+v", method, path, envelope)
		}
		if target != nil {
			if err := json.Unmarshal(envelope.Detail, target); err != nil {
				t.Fatal(err)
			}
		}
		return envelope.Detail
	}
	var draft shared.DraftDTO
	created := call(http.MethodPost, "/drafts", fixture.writerToken, map[string]any{"branch_id": branchID, "version_name": "1.0.0", "content": ""}, &draft)
	if draft.ID == "" || draft.DocumentFormat != app.DocumentFormatText || draft.RawContentHash == "" || draft.StableContentHash != draft.RawContentHash || draft.NormalizedContentHash != "" {
		t.Fatalf("TXT draft DTO = %+v", draft)
	}
	if bytes.Contains(created, []byte("object_key")) || bytes.Contains(created, []byte("normalized_content_hash")) {
		t.Fatalf("TXT draft exposed non-public fields: %s", created)
	}
	literal := "# literal\r\n<em>plain text</em>\r\n中文  \r\n"
	draftPath := "/drafts/" + draft.ID
	call(http.MethodPatch, draftPath, fixture.writerToken, map[string]any{"expected_revision": draft.Revision, "content": literal}, &draft)
	var snapshot struct {
		shared.ContentDTO
		Draft shared.DraftDTO `json:"draft"`
	}
	call(http.MethodGet, draftPath+"/content/stable", fixture.writerToken, nil, &snapshot)
	if snapshot.Content != literal || snapshot.Hash != snapshot.Draft.StableContentHash || snapshot.Draft.Revision != draft.Revision {
		t.Fatalf("TXT draft bytes or snapshot changed: %+v", snapshot)
	}
	for _, body := range []string{
		`{"expected_revision":` + jsonString(draft.Revision) + `}`,
		`{"expected_revision":` + jsonString(draft.Revision) + `,"content":null}`,
	} {
		result := decodePrivateEnvelope(t, performPrivateJSON(fixture.router, http.MethodPatch, base+draftPath, fixture.writerToken, body))
		if result.Status != "INVALID_ARGUMENT" {
			t.Fatalf("missing TXT update content = %+v", result)
		}
	}
	call(http.MethodGet, draftPath+"/content/stable", fixture.writerToken, nil, &snapshot)
	if snapshot.Content != literal || snapshot.Draft.Revision != draft.Revision {
		t.Fatalf("missing TXT update content changed the draft: %+v", snapshot)
	}
	// 清空 TXT 和正文缺失不同；清空后仍能继续编辑并提交审核。
	call(http.MethodPatch, draftPath, fixture.writerToken, map[string]any{"expected_revision": draft.Revision, "content": ""}, &draft)
	call(http.MethodGet, draftPath+"/content/raw", fixture.writerToken, nil, &snapshot)
	if snapshot.Content != "" {
		t.Fatalf("empty TXT update kept old content: %q", snapshot.Content)
	}
	call(http.MethodPatch, draftPath, fixture.writerToken, map[string]any{"expected_revision": draft.Revision, "content": literal}, &draft)
	call(http.MethodPost, draftPath+"/submit", fixture.writerToken, nil, &draft)
	call(http.MethodGet, draftPath+"/content/stable", fixture.adminToken, nil, &snapshot)
	var version shared.VersionDTO
	call(http.MethodPost, draftPath+"/approve", fixture.adminToken, map[string]any{"expected_review_revision": snapshot.Draft.ReviewRevision}, &version)
	if version.DocumentFormat != app.DocumentFormatText || version.StableContentHash != version.RawContentHash || version.NormalizedContentHash != "" {
		t.Fatalf("TXT published DTO = %+v", version)
	}
	for _, kind := range []string{"raw", "stable"} {
		var content shared.ContentDTO
		call(http.MethodGet, "/versions/"+version.ID+"/content/"+kind, fixture.writerToken, nil, &content)
		if content.Content != literal || content.Hash != version.StableContentHash {
			t.Fatalf("TXT %s version bytes changed: %+v", kind, content)
		}
	}
	call(http.MethodPost, "/drafts", fixture.writerToken, map[string]any{"branch_id": branchID, "version_name": "1.1.0", "content": literal + "added\r\n"}, &draft)
	draftPath = "/drafts/" + draft.ID
	call(http.MethodPost, draftPath+"/submit", fixture.writerToken, nil, &draft)
	call(http.MethodGet, draftPath+"/content/stable", fixture.adminToken, nil, &snapshot)
	var next shared.VersionDTO
	call(http.MethodPost, draftPath+"/approve", fixture.adminToken, map[string]any{"expected_review_revision": snapshot.Draft.ReviewRevision}, &next)
	diff := call(http.MethodPost, "/diffs", fixture.writerToken, map[string]any{"from_version_id": version.ID, "to_version_id": next.ID}, nil)
	if !bytes.Contains(diff, []byte(`"items"`)) || !bytes.Contains(diff, []byte("added")) {
		t.Fatalf("TXT comparison did not return a line diff: %s", diff)
	}
}
