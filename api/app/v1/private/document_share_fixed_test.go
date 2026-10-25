package private

import (
	"encoding/json"
	"net/http"
	"testing"

	publicshare "vdoc/api/app/v1/open/documentshare"
	"vdoc/api/middleware"
	app "vdoc/appstore"
)

func TestFixedVersionShareHTTPContract(t *testing.T) {
	fixture := setupPrivateTask5Project(t)
	middleware.CleanupAllLimiters()
	t.Cleanup(middleware.CleanupAllLimiters)
	publicshare.RegisterOpenRoutes(fixture.router.Group("/api/v1/open"))
	store := app.DefaultStore()
	document, err := store.CreateDocument(fixture.adminUser.ID, fixture.project.ID, "Shared guide", app.DocumentTypeMarkdown, "docs/shared.md", "")
	if err != nil {
		t.Fatal(err)
	}
	branches, err := store.ListBranches(fixture.adminUser.ID, fixture.project.ID, document.ID)
	if err != nil {
		t.Fatal(err)
	}
	branchID := branchesByName(branches)["dev"].ID
	publish := func(name, content string) *app.ContractVersion {
		t.Helper()
		draft, err := store.CreateMarkdownDraft(fixture.adminUser.ID, fixture.project.ID, document.ID, app.DraftInput{BranchID: branchID, VersionName: name, SchemaContent: content})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = store.SubmitMarkdownDraft(fixture.adminUser.ID, fixture.project.ID, document.ID, draft.ID); err != nil {
			t.Fatal(err)
		}
		published, err := store.ReviewMarkdownDraft(fixture.adminUser.ID, fixture.project.ID, document.ID, draft.ID, "approve", reviewInputForTest(t, store, fixture.adminUser.ID, fixture.project.ID, document.ID, draft.ID))
		if err != nil {
			t.Fatal(err)
		}
		return published.(*app.ContractVersion)
	}
	first := publish("1.0.0", "# First version\n")
	second := publish("2.0.0", "# Second version\n")
	base := "/api/v1/private/projects/" + fixture.project.ID + "/documents/" + document.ID + "/shares"
	for _, body := range []string{
		`{"branch_id":` + jsonString(branchID) + `,"version_scope":3,"expiry_preset":"permanent"}`,
		`{"branch_id":` + jsonString(branchID) + `,"version_scope":3,"version_id":"","expiry_preset":"permanent"}`,
		`{"branch_id":` + jsonString(branchID) + `,"version_scope":1,"version_id":` + jsonString(first.ID) + `,"expiry_preset":"permanent"}`,
	} {
		envelope := decodePrivateEnvelope(t, performPrivateJSON(fixture.router, http.MethodPost, base, fixture.adminToken, body))
		if envelope.Code != 400 || envelope.Status != "INVALID_ARGUMENT" {
			t.Fatalf("invalid share response = %+v", envelope)
		}
	}
	body := `{"branch_id":` + jsonString(branchID) + `,"version_scope":3,"version_id":` + jsonString(first.ID) + `,"expiry_preset":"permanent"}`
	envelope := decodePrivateEnvelope(t, performPrivateJSON(fixture.router, http.MethodPost, base, fixture.adminToken, body))
	if envelope.Code != 200 || envelope.Status != "OK" {
		t.Fatalf("create response = %+v", envelope)
	}
	var created struct {
		Share struct {
			ID           string `json:"id"`
			VersionScope int    `json:"version_scope"`
			VersionID    string `json:"version_id"`
		} `json:"share"`
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(envelope.Detail, &created); err != nil {
		t.Fatal(err)
	}
	if created.Share.VersionScope != 3 || created.Share.VersionID != first.ID || created.Secret == "" {
		t.Fatalf("create DTO = %+v", created)
	}
	for _, suffix := range []string{"", "/" + created.Share.ID + "/reveal"} {
		method := http.MethodGet
		if suffix != "" {
			method = http.MethodPost
		}
		result := decodePrivateEnvelope(t, performPrivateJSON(fixture.router, method, base+suffix, fixture.adminToken, ""))
		var detail any
		if err := json.Unmarshal(result.Detail, &detail); err != nil {
			t.Fatal(err)
		}
		var share map[string]any
		if suffix == "" {
			share = detail.([]any)[0].(map[string]any)
		} else {
			share = detail.(map[string]any)["share"].(map[string]any)
		}
		if result.Code != 200 || share["version_id"] != first.ID {
			t.Fatalf("share DTO %s = %+v", suffix, result)
		}
	}
	publicBase := "/api/v1/open/document-shares/" + created.Share.ID
	authorization := "VdocShare " + created.Secret
	metadataEnvelope := decodePrivateEnvelope(t, performPrivateJSON(fixture.router, http.MethodGet, publicBase, authorization, ""))
	var metadata struct {
		CurrentVersion struct {
			ID string `json:"id"`
		} `json:"current_version"`
	}
	if err := json.Unmarshal(metadataEnvelope.Detail, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadataEnvelope.Code != 200 || metadata.CurrentVersion.ID != first.ID {
		t.Fatalf("public metadata = %+v, %+v", metadataEnvelope, metadata)
	}
	for _, path := range []string{"/content", "/download"} {
		result := performPrivateJSON(fixture.router, http.MethodGet, publicBase+"/versions/"+first.ID+path, authorization, "")
		if path == "/content" {
			response := decodePrivateEnvelope(t, result)
			if response.Code != 200 {
				t.Fatalf("content response = %+v", response)
			}
		} else if result.Code != 200 || result.Body.String() != "# First version\n" {
			t.Fatalf("download response = %d %s", result.Code, result.Body.String())
		}
	}
	for _, suffix := range []string{"/versions", "/versions/" + second.ID + "/content", "/versions/" + second.ID + "/download"} {
		result := decodePrivateEnvelope(t, performPrivateJSON(fixture.router, http.MethodGet, publicBase+suffix, authorization, ""))
		if result.Code != 404 || result.Status != "NOT_FOUND" {
			t.Fatalf("forbidden route %s response = %+v", suffix, result)
		}
	}
	revokedEnvelope := decodePrivateEnvelope(t, performPrivateJSON(fixture.router, http.MethodPost, base+"/"+created.Share.ID+"/revoke", fixture.adminToken, ""))
	var revoked struct {
		VersionID string `json:"version_id"`
		Status    int    `json:"status"`
	}
	if err := json.Unmarshal(revokedEnvelope.Detail, &revoked); err != nil {
		t.Fatal(err)
	}
	if revokedEnvelope.Code != 200 || revoked.VersionID != first.ID || revoked.Status != app.DocumentShareStatusRevoked {
		t.Fatalf("revoke DTO = %+v, %+v", revokedEnvelope, revoked)
	}
	result := decodePrivateEnvelope(t, performPrivateJSON(fixture.router, http.MethodGet, publicBase, authorization, ""))
	if result.Code != 404 {
		t.Fatalf("revoked link remained public: %+v", result)
	}
}
