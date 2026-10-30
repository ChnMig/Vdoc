package documentshare

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vdoc/api/app/v1/private/repositoryshare"
	"vdoc/api/middleware"
	app "vdoc/appstore"
	repo "vdoc/domain/documentrepo"
	"vdoc/utils/contextkey"

	"github.com/gin-gonic/gin"
)

func TestRepositoryShareManagementAndAnonymousHTTPContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app.ResetDefaultStoreForTest()
	t.Cleanup(app.ResetDefaultStoreForTest)
	middleware.CleanupAllLimiters()
	t.Cleanup(middleware.CleanupAllLimiters)
	store := app.DefaultStore()
	user, err := store.Register("repository-share-http@example.test", "Share Owner", "Repository-share-http-password-2026!")
	if err != nil {
		t.Fatal(err)
	}
	team, err := store.CreateTeam(user.ID, "Share HTTP", "")
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(user.ID, team.ID, "Share HTTP", "", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	branches, err := store.RepositoryBranches(user.ID, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	branch, err := store.RepositoryCreateBranch(user.ID, project.ID, repo.BranchInput{Name: "share-http", FromCommitID: branches[0].HeadCommitID})
	if err != nil {
		t.Fatal(err)
	}
	const content = "# Shared release\r\n\r\nExact bytes\r\n"
	commit, err := store.RepositorySave(user.ID, project.ID, branch.ID, repo.SaveInput{ExpectedHeadCommitID: branch.HeadCommitID, ExpectedGeneration: branch.Generation, IdempotencyKey: "repository-share-http", Changes: []repo.Change{{Operation: "upsert", Name: "Public document", RelativePath: "docs/release.md", DocumentType: app.DocumentTypeMarkdown, Content: content}}})
	if err != nil {
		t.Fatal(err)
	}
	release, err := store.RepositoryPublish(user.ID, project.ID, repo.PublishInput{BranchID: branch.ID, CommitID: commit.ID, ExpectedGeneration: branch.Generation + 1})
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	RegisterOpenRoutes(router.Group("/api/v1/open"))
	private := router.Group("/api/v1/private")
	private.Use(func(c *gin.Context) { c.Set(contextkey.JWTData, map[string]any{"user_id": user.ID}); c.Next() })
	repositoryshare.RegisterRoutes(private)
	request := func(method, path, body, secret, proof string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if secret != "" {
			r.Header.Set("Authorization", "VdocShare "+secret)
		}
		if proof != "" {
			r.Header.Set(unlockHeader, proof)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, r)
		if response.Code != http.StatusOK {
			t.Fatalf("HTTP status=%d", response.Code)
		}
		return response
	}
	type envelope struct {
		Code    int             `json:"code"`
		Status  string          `json:"status"`
		Message string          `json:"message"`
		Detail  json.RawMessage `json:"detail"`
	}
	decode := func(response *httptest.ResponseRecorder) envelope {
		t.Helper()
		var out envelope
		if err := json.Unmarshal(response.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	management := "/api/v1/private/projects/" + project.ID + "/repository/releases/" + release.ID + "/documents/" + commit.Files[0].DocumentID + "/shares"
	created := decode(request(http.MethodPost, management, `{"expiry_preset":"permanent","password":"share-http-password-2026"}`, "", ""))
	if created.Code != 200 {
		t.Fatalf("create share code=%d", created.Code)
	}
	var capability struct {
		Share struct {
			ID        string `json:"id"`
			ReleaseID string `json:"release_id"`
		} `json:"share"`
		Secret string `json:"secret"`
	}
	if err = json.Unmarshal(created.Detail, &capability); err != nil {
		t.Fatal(err)
	}
	if len(capability.Share.ID) != 32 || capability.Share.ReleaseID != release.ID || capability.Secret == "" {
		t.Fatal("invalid server-generated capability response")
	}
	listed := request(http.MethodGet, management, "", "", "")
	for _, forbidden := range []string{"capability", "ciphertext", "password_verifier", capability.Secret} {
		if strings.Contains(listed.Body.String(), forbidden) {
			t.Fatal("management list exposed secret material")
		}
	}
	public := "/api/v1/open/document-shares/" + capability.Share.ID
	locked := request(http.MethodGet, public, "", capability.Secret, "")
	if decode(locked).Status != "PASSWORD_REQUIRED" {
		t.Fatal("protected public link did not challenge")
	}
	for _, header := range []string{"Cache-Control", "Referrer-Policy", "X-Robots-Tag", "Content-Security-Policy"} {
		if locked.Header().Get(header) == "" {
			t.Fatalf("missing privacy header %s", header)
		}
	}
	invalid := decode(request(http.MethodGet, public, "", "vdoc_share_"+strings.Repeat("0", 48), ""))
	unknown := decode(request(http.MethodGet, "/api/v1/open/document-shares/"+strings.Repeat("1", 32), "", capability.Secret, ""))
	if invalid.Status != "NOT_FOUND" || invalid.Status != unknown.Status || invalid.Code != unknown.Code || invalid.Message != unknown.Message {
		t.Fatal("public error reveals invalid capability existence")
	}
	unlocked := decode(request(http.MethodPost, public+"/unlock", `{"password":"share-http-password-2026"}`, capability.Secret, ""))
	var grant struct {
		Proof string `json:"unlock_proof"`
	}
	if err = json.Unmarshal(unlocked.Detail, &grant); err != nil || grant.Proof == "" {
		t.Fatal("unlock proof missing")
	}
	metadata := decode(request(http.MethodGet, public, "", capability.Secret, grant.Proof))
	var meta struct {
		Scope   int `json:"version_scope"`
		Version struct {
			ID string `json:"id"`
		} `json:"current_version"`
	}
	if err = json.Unmarshal(metadata.Detail, &meta); err != nil || metadata.Code != 200 || meta.Scope != 3 || meta.Version.ID != release.ID {
		t.Fatal("public reader metadata contract changed")
	}
	loaded := decode(request(http.MethodGet, public+"/versions/"+release.ID+"/content", "", capability.Secret, grant.Proof))
	var text struct {
		VersionID string `json:"version_id"`
		Content   string `json:"content"`
	}
	if err = json.Unmarshal(loaded.Detail, &text); err != nil || text.Content != content || text.VersionID != release.ID {
		t.Fatal("exact raw public body contract failed")
	}
	download := request(http.MethodGet, public+"/versions/"+release.ID+"/download", "", capability.Secret, grant.Proof)
	if download.Body.String() != content || !strings.Contains(download.Header().Get("Content-Disposition"), "release.md") {
		t.Fatal("download lost pinned body or filename")
	}
	if decode(request(http.MethodGet, public+"/versions/"+commit.ID+"/content", "", capability.Secret, grant.Proof)).Status != "NOT_FOUND" {
		t.Fatal("commit ID bypassed published release scope")
	}
	if decode(request(http.MethodPost, management+"/"+capability.Share.ID+"/revoke", "", "", "")).Code != 200 {
		t.Fatal("revocation failed")
	}
	if decode(request(http.MethodGet, public+"/versions/"+release.ID+"/content", "", capability.Secret, grant.Proof)).Status != "NOT_FOUND" {
		t.Fatal("revoked public capability readable")
	}
}
