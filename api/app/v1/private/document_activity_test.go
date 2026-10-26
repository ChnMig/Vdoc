package private

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	app "vdoc/appstore"
)

func TestPrivateDocumentActivityDTOAndPermissions(t *testing.T) {
	fixture := setupPrivateTask5Project(t)
	store := app.DefaultStore()
	document, branch := createReviewDocument(t, fixture, "Activity")
	draft := createSubmittedReviewDraft(t, reviewDraftInput{Fixture: fixture, Document: document, BranchID: branch, Version: "1.0", Operation: "activity"})
	_ = draft
	path := "/api/v1/private/projects/" + fixture.project.ID + "/documents/" + document.ID + "/activity"
	envelope := decodePrivateEnvelope(t, performPrivateJSON(fixture.router, http.MethodGet, path+"?limit=2", fixture.writerToken, ""))
	if envelope.Status != "OK" {
		t.Fatalf("writer activity = %+v", envelope)
	}
	var items []map[string]any
	if err := json.Unmarshal(envelope.Detail, &items); err != nil || len(items) != 2 {
		t.Fatalf("items = %s %v", envelope.Detail, err)
	}
	for _, forbidden := range []string{"metadata", "ip_address", "actor_token_id", "review_comment", "raw_schema", "content"} {
		if bytes.Contains(envelope.Detail, []byte(`"`+forbidden+`"`)) {
			t.Fatalf("activity leaked %s", forbidden)
		}
	}
	outsider, err := store.Register("activity-outsider@example.test", "Outsider", privateTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct{ token, suffix, status string }{
		{"", "", "UNAUTHENTICATED"}, {issuePrivateTestToken(t, outsider.ID), "", "PERMISSION_DENIED"}, {fixture.writerToken, "?limit=0", "INVALID_ARGUMENT"}, {fixture.writerToken, "?cursor=bad", "INVALID_ARGUMENT"},
	} {
		result := decodePrivateEnvelope(t, performPrivateJSON(fixture.router, http.MethodGet, path+check.suffix, check.token, ""))
		if result.Status != check.status {
			t.Fatalf("status = %s, want %s", result.Status, check.status)
		}
	}
}
