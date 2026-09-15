package private

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	app "vdoc/appstore"
)

func reviewInputForTest(t *testing.T, store *app.Store, actorID, projectID, documentID, draftID string) app.DraftReviewInput {
	t.Helper()
	draft, err := store.Draft(actorID, projectID, documentID, draftID)
	if err != nil {
		t.Fatal(err)
	}
	return app.DraftReviewInput{ExpectedReviewRevision: draft.ReviewRevision()}
}

// 旧工作流用例显式读取审核快照，再附上新契约要求的审核标识。
func performPrivateReview(t *testing.T, router *gin.Engine, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	draftPath := path[:strings.LastIndex(path, "/")]
	envelope := decodePrivateEnvelope(t, performPrivateJSON(router, http.MethodGet, draftPath, token, ""))
	if envelope.Code != 200 {
		t.Fatalf("read review snapshot: %s", envelope.Message)
	}
	var snapshot struct {
		ReviewRevision string `json:"review_revision"`
	}
	if err := json.Unmarshal(envelope.Detail, &snapshot); err != nil {
		t.Fatal(err)
	}
	values := map[string]any{}
	if strings.TrimSpace(body) != "" {
		if err := json.Unmarshal([]byte(body), &values); err != nil {
			t.Fatal(err)
		}
	}
	values["expected_review_revision"] = snapshot.ReviewRevision
	encoded, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	return performPrivateJSON(router, method, path, token, string(encoded))
}

func TestPrivateReviewsRequireTheViewedSnapshot(t *testing.T) {
	for _, markdown := range []bool{false, true} {
		name, kind := "openapi", app.DocumentTypeOpenAPI
		original, changed := privateTestOpenAPI("oldReview"), privateTestOpenAPI("newReview")
		if markdown {
			name, kind, original, changed = "markdown", app.DocumentTypeMarkdown, "# Old review", "# New review"
		}
		t.Run(name, func(t *testing.T) {
			fixture := setupPrivateTask5Project(t)
			store := app.DefaultStore()
			document, err := store.CreateDocument(fixture.adminUser.ID, fixture.project.ID, name, kind, "review/"+name+".md", "")
			if err != nil {
				t.Fatal(err)
			}
			branches, err := store.ListBranches(fixture.adminUser.ID, fixture.project.ID, document.ID)
			if err != nil {
				t.Fatal(err)
			}
			create, submit, update := store.CreateDraft, store.SubmitDraft, store.UpdateDraft
			if markdown {
				create, submit, update = store.CreateMarkdownDraft, store.SubmitMarkdownDraft, store.UpdateMarkdownDraft
			}
			d, err := create(fixture.adminUser.ID, fixture.project.ID, document.ID, app.DraftInput{BranchID: branches[0].ID, VersionName: "1.0.0", SchemaContent: original})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := submit(fixture.adminUser.ID, fixture.project.ID, document.ID, d.ID); err != nil {
				t.Fatal(err)
			}
			path := "/api/v1/private/projects/" + fixture.project.ID + "/documents/" + document.ID + "/drafts/" + d.ID
			getSnapshot := func() string {
				t.Helper()
				envelope := decodePrivateEnvelope(t, performPrivateJSON(fixture.router, http.MethodGet, path+"/content/raw", fixture.adminToken, ""))
				if envelope.Code != 200 {
					t.Fatalf("snapshot error: %s", envelope.Message)
				}
				var snapshot struct {
					Content string `json:"content"`
					Draft   struct {
						ReviewRevision string `json:"review_revision"`
					} `json:"draft"`
				}
				if err := json.Unmarshal(envelope.Detail, &snapshot); err != nil {
					t.Fatal(err)
				}
				if snapshot.Draft.ReviewRevision == "" || snapshot.Content == "" {
					t.Fatal("missing coherent review snapshot")
				}
				return snapshot.Draft.ReviewRevision
			}
			old := getSnapshot()
			review := func(action, body string) privateTestEnvelope {
				t.Helper()
				return decodePrivateEnvelope(t, performPrivateJSON(fixture.router, http.MethodPost, path+"/"+action, fixture.adminToken, body))
			}
			for _, action := range []string{"approve", "request-changes", "reject"} {
				for _, body := range []string{"", "{}", `{"expected_review_revision":" "}`, `{"expected_review_revision":null}`} {
					if result := review(action, body); result.Status != "INVALID_ARGUMENT" {
						t.Fatalf("missing snapshot %s: %+v", action, result)
					}
				}
			}
			oldBody := `{"expected_review_revision":` + jsonString(old) + `,"comment":"Snapshot review"}`
			if result := review("request-changes", oldBody); result.Status != "OK" {
				t.Fatalf("initial review: %+v", result)
			}
			returned, err := store.Draft(fixture.adminUser.ID, fixture.project.ID, document.ID, d.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := update(fixture.adminUser.ID, fixture.project.ID, document.ID, d.ID, app.DraftPatchInput{ExpectedRevision: returned.Revision(), SchemaContent: changed}); err != nil {
				t.Fatal(err)
			}
			if _, err := submit(fixture.adminUser.ID, fixture.project.ID, document.ID, d.ID); err != nil {
				t.Fatal(err)
			}
			for _, action := range []string{"approve", "request-changes", "reject"} {
				if result := review(action, oldBody); result.Status != "FAILED_PRECONDITION" {
					t.Fatalf("stale %s: %+v", action, result)
				}
			}
			fresh := getSnapshot()
			if fresh == old {
				t.Fatal("new submission reused old review snapshot")
			}
			if result := review("approve", `{"expected_review_revision":`+jsonString(fresh)+`}`); result.Status != "OK" {
				t.Fatalf("fresh review: %+v", result)
			}
		})
	}
}
