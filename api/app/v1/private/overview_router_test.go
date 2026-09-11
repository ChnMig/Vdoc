package private

import (
	"encoding/json"
	"net/http"
	"testing"

	app "vdoc/appstore"
)

func TestDocumentOverviewRoutesPreserveEnvelopeAndPermissions(t *testing.T) {
	router := setupPrivateRouter(t)
	store := app.DefaultStore()
	user, err := store.Register("overview@example.test", "Overview", privateTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	team, err := store.CreateTeam(user.ID, "Overview", "")
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(user.ID, team.ID, "Overview", "", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	document, err := store.CreateDocument(user.ID, project.ID, "Guide", app.DocumentTypeMarkdown, "guide.md", "")
	if err != nil {
		t.Fatal(err)
	}
	token := issuePrivateTestToken(t, user.ID)
	path := "/api/v1/private/projects/" + project.ID + "/documents/" + document.ID
	for _, suffix := range []string{"/overview", "/mcp-readiness"} {
		envelope := decodePrivateEnvelope(t, performPrivateJSON(router, http.MethodGet, path+suffix, token, ""))
		if envelope.Code != 200 {
			t.Fatalf("%s: %+v", suffix, envelope)
		}
		var detail map[string]any
		if err := json.Unmarshal(envelope.Detail, &detail); err != nil {
			t.Fatal(err)
		}
		if suffix == "/overview" && (detail["version_count"] != float64(0) || detail["latest_version"] != nil) {
			t.Fatalf("empty overview: %v", detail)
		}
		if suffix == "/mcp-readiness" && detail["last_read_at"] != nil {
			t.Fatalf("empty readiness: %v", detail)
		}
		for _, forbidden := range []string{"raw_schema", "normalized_schema", "token_ciphertext", "api_key"} {
			if _, exists := detail[forbidden]; exists {
				t.Fatal("internal DTO field exposed")
			}
		}
		unauthenticated := decodePrivateEnvelope(t, performPrivateJSON(router, http.MethodGet, path+suffix, "", ""))
		if unauthenticated.Status != "UNAUTHENTICATED" {
			t.Fatalf("unauthenticated %s: %+v", suffix, unauthenticated)
		}
	}
	outsider, err := store.CreateUser(user.ID, "outsider-overview@example.test", "Outsider", privateTestPassword, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"/overview", "/mcp-readiness"} {
		envelope := decodePrivateEnvelope(t, performPrivateJSON(router, http.MethodGet, path+suffix, issuePrivateTestToken(t, outsider.ID), ""))
		if envelope.Status != "PERMISSION_DENIED" {
			t.Fatalf("cross-project %s: %+v", suffix, envelope)
		}
	}
}
