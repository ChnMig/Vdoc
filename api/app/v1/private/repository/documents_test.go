package repository

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	app "vdoc/appstore"
	repo "vdoc/domain/documentrepo"
	"vdoc/utils/contextkey"

	"github.com/gin-gonic/gin"
)

func TestRepositoryDocumentHTTPManagementRequiresAdminAndDisablesDeletedReferences(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	app.ResetDefaultStoreForTest()
	t.Cleanup(func() { app.ResetDefaultStoreForTest(); gin.SetMode(oldMode) })
	s := app.DefaultStore()
	owner, err := s.Register("crud-owner@example.test", "Owner", "Crud-owner-test-password-2026!")
	if err != nil {
		t.Fatal(err)
	}
	team, err := s.CreateTeam(owner.ID, "CRUD", "")
	if err != nil {
		t.Fatal(err)
	}
	project, err := s.CreateProject(owner.ID, team.ID, "CRUD", "", owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := s.Register("crud-writer@example.test", "Writer", "Crud-writer-test-password-2026!")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AddProjectMember(owner.ID, project.ID, writer.ID, app.MemberRoleWriter); err != nil {
		t.Fatal(err)
	}
	created, err := s.RepositoryCreateDocument(writer.ID, project.ID, repo.DocumentInput{Name: "Guide", RelativePath: "guide.md", DocumentType: app.DocumentTypeMarkdown, Content: "original\r\n", IdempotencyKey: "http-managed-document"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateDocumentFolderAtPath(owner.ID, project.ID, "", "Manual"); err != nil {
		t.Fatal(err)
	}
	actorID := owner.ID
	router := gin.New()
	group := router.Group("/api/v1/private", func(c *gin.Context) { c.Set(contextkey.JWTData, map[string]any{"user_id": actorID}); c.Next() })
	RegisterRoutes(group)
	prefix := "/api/v1/private/projects/" + project.ID + "/repository"
	endpoint := "/documents/" + created.Document.ID
	call := func(method, endpoint, body string, expected int) json.RawMessage {
		t.Helper()
		request := httptest.NewRequest(method, prefix+endpoint, bytes.NewBufferString(body))
		request.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, request)
		var envelope struct {
			Code   int             `json:"code"`
			Detail json.RawMessage `json:"detail"`
		}
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &envelope) != nil || envelope.Code != expected {
			t.Fatalf("%s %s expected envelope %d: HTTP %d %s", method, endpoint, expected, w.Code, w.Body.String())
		}
		if bytes.Contains(w.Body.Bytes(), []byte("object_key")) {
			t.Fatal("catalog endpoint exposed storage key")
		}
		return envelope.Detail
	}
	call("GET", endpoint, "", 200)
	actorID = writer.ID
	call("PATCH", endpoint, `{"name":"Denied"}`, 403)
	call("DELETE", endpoint, "", 403)
	actorID = owner.ID
	for _, body := range []string{`{}`, `null`, `{"name":null}`, `{"name":"x","description":null}`, `{"name":1}`, `{"status":2}`, `{"name":"x"} {}`} {
		call("PATCH", endpoint, body, 400)
	}
	call("PATCH", endpoint, `{"relative_path":"missing/guide.md"}`, 404)
	value := call("PATCH", endpoint, `{"name":"Renamed","relative_path":"Manual/renamed.md","description":"Current"}`, 200)
	var document documentDTO
	if err = json.Unmarshal(value, &document); err != nil || document.Name != "Renamed" || document.RelativePath != "Manual/renamed.md" || document.ID != created.Document.ID || document.DefaultBranchID != created.Branch.ID {
		t.Fatalf("document DTO: %s %v", value, err)
	}
	call("GET", "/commits/"+created.Commit.ID+"/documents/"+created.Document.ID+"/content", "", 200)
	call("PATCH", endpoint, `{"relative_path":"renamed.md"}`, 200)
	deleted := call("DELETE", endpoint, "", 200)
	if string(deleted) != `{"deleted":true}` {
		t.Fatalf("delete response: %s", deleted)
	}
	call("DELETE", endpoint, "", 200)
	call("GET", endpoint, "", 404)
	call("PATCH", endpoint, `{"name":"resurrect"}`, 404)
	call("GET", "/commits/"+created.Commit.ID+"/documents/"+created.Document.ID+"/content", "", 404)
	if list := call("GET", "/documents", "", 200); string(list) != "[]" {
		t.Fatalf("deleted document listed: %s", list)
	}
}
