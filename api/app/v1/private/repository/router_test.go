package repository

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	app "vdoc/appstore"
	"vdoc/utils/contextkey"

	"github.com/gin-gonic/gin"
)

func TestRepositoryHTTPUsesServerIdentifiersStrictInputsAndSafeDTOs(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	app.ResetDefaultStoreForTest()
	t.Cleanup(func() { app.ResetDefaultStoreForTest(); gin.SetMode(oldMode) })
	s := app.DefaultStore()
	user, err := s.Register("repository-http@example.test", "Owner", "Repository-http-password-2026!")
	if err != nil {
		t.Fatal(err)
	}
	team, err := s.CreateTeam(user.ID, "HTTP", "")
	if err != nil {
		t.Fatal(err)
	}
	project, err := s.CreateProject(user.ID, team.ID, "HTTP", "", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	group := router.Group("/api/v1/private", func(c *gin.Context) { c.Set(contextkey.JWTData, map[string]any{"user_id": user.ID}); c.Next() })
	RegisterRoutes(group)
	prefix := "/api/v1/private/projects/" + project.ID + "/repository"
	call := func(method, path string, body any) (int, json.RawMessage, []byte) {
		t.Helper()
		raw, _ := json.Marshal(body)
		request := httptest.NewRequest(method, prefix+path, bytes.NewReader(raw))
		request.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, request)
		if w.Code != http.StatusOK {
			t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
		}
		var envelope struct {
			Code   int             `json:"code"`
			Detail json.RawMessage `json:"detail"`
		}
		if e := json.Unmarshal(w.Body.Bytes(), &envelope); e != nil {
			t.Fatal(e)
		}
		return envelope.Code, envelope.Detail, w.Body.Bytes()
	}
	code, detail, body := call("GET", "/branches", nil)
	if code != 200 {
		t.Fatalf("initialize: %s", body)
	}
	var branches []branchDTO
	if err = json.Unmarshal(detail, &branches); err != nil || len(branches) != 1 || branches[0].ImportRequired {
		t.Fatalf("branches=%s %v", detail, err)
	}
	code, detail, body = call("POST", "/branches", map[string]any{"name": "work", "from_commit_id": branches[0].HeadCommitID})
	if code != 200 {
		t.Fatalf("branch: %s", body)
	}
	var branch branchDTO
	_ = json.Unmarshal(detail, &branch)
	input := map[string]any{"expected_head_commit_id": branch.HeadCommitID, "expected_generation": branch.Generation, "idempotency_key": "http-save-key", "changes": []map[string]any{{"operation": "upsert", "name": "Guide", "relative_path": "guide.md", "document_type": app.DocumentTypeMarkdown, "content": "# Guide\r\n"}}, "version_name": "client-generated"}
	code, _, body = call("POST", "/branches/"+branch.ID+"/commits", input)
	if code != 400 || !strings.Contains(string(body), "INVALID_ARGUMENT") {
		t.Fatalf("client version accepted: %s", body)
	}
	delete(input, "version_name")
	code, detail, body = call("POST", "/branches/"+branch.ID+"/commits", input)
	if code != 200 {
		t.Fatalf("save: %s", body)
	}
	var commit commitDTO
	_ = json.Unmarshal(detail, &commit)
	if len(commit.ID) != 64 || len(commit.Files) != 1 || len(commit.Files[0].DocumentID) != 32 || bytes.Contains(body, []byte("object_key")) {
		t.Fatalf("commit DTO=%s", body)
	}
	code, detail, body = call("GET", fmt.Sprintf("/commits/%s/documents/%s/content", commit.ID, commit.Files[0].DocumentID), nil)
	if code != 200 || bytes.Contains(body, []byte("object_key")) {
		t.Fatalf("content DTO=%s", body)
	}
	var content struct {
		Content string `json:"content"`
	}
	_ = json.Unmarshal(detail, &content)
	if content.Content != "# Guide\r\n" {
		t.Fatalf("content bytes=%q", content.Content)
	}
	code, detail, body = call("GET", "/commits?page=true&branch_id="+branch.ID, nil)
	var page struct {
		Commits    []commitDTO `json:"commits"`
		NextCursor string      `json:"next_cursor"`
	}
	if e := json.Unmarshal(detail, &page); e != nil || code != 200 || len(page.Commits) != 2 || page.NextCursor != "" {
		t.Fatalf("history page=%s %v", body, e)
	}
	code, _, body = call("POST", "/releases", map[string]any{"branch_id": branch.ID, "commit_id": commit.ID, "expected_generation": branch.Generation + 1, "version_name": "user-version"})
	if code != 400 {
		t.Fatalf("client publication version accepted: %s", body)
	}
	code, detail, body = call("POST", "/releases", map[string]any{"branch_id": branch.ID, "commit_id": commit.ID, "expected_generation": branch.Generation + 1})
	var release releaseDTO
	_ = json.Unmarshal(detail, &release)
	if code != 200 || release.CommitID != commit.ID || release.VersionName == "" {
		t.Fatalf("release=%s", body)
	}
}
