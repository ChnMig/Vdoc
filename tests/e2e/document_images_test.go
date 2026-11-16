package e2e

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"vdoc/db/pgdb"
	pgdbvdoc "vdoc/db/pgdb/vdoc"
	"vdoc/domain/documentimage"
	repo "vdoc/domain/documentrepo"
	app "vdoc/services/vdoc"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Exercise the full HTTP contract in both the memory harness and the disposable
// PostgreSQL/object-storage harness, including reads after a process restart.
func TestMarkdownImagesRESTMCPAndPinnedPublicShare(t *testing.T) {
	live := liveE2ERequested()
	f := newE2EFixture(t, e2eFixtureOptions{LivePersistence: live})
	w := createWorkspace(t, f, e2eRunID())
	projectBase := "/api/v1/private/projects/" + w.ProjectID
	repositoryBase := projectBase + "/repository"
	branches := decodeDetail[[]repo.Branch](t, f.requireOK(t, http.MethodPost, repositoryBase+"/bootstrap", w.AdminToken, map[string]any{}))
	if len(branches) == 0 {
		t.Fatal("bootstrap returned no branches")
	}
	branch := decodeDetail[repo.Branch](t, f.requireOK(t, http.MethodPost, repositoryBase+"/branches", w.AdminToken, repo.BranchInput{Name: "images", FromCommitID: branches[0].HeadCommitID}))
	initial := decodeDetail[repo.Commit](t, f.requireOK(t, http.MethodPost, repositoryBase+"/branches/"+branch.ID+"/commits", w.WriterToken, repo.SaveInput{
		ExpectedHeadCommitID: branch.HeadCommitID, ExpectedGeneration: branch.Generation, IdempotencyKey: "images-create-" + w.RunID,
		Changes: []repo.Change{{Operation: "upsert", Name: "图片说明", RelativePath: "docs/images.md", DocumentType: app.DocumentTypeMarkdown, Content: "# 图片说明\n"}},
	}))
	var document repo.File
	for _, file := range initial.Files {
		if file.RelativePath == "docs/images.md" {
			document = file
		}
	}
	if document.DocumentID == "" {
		t.Fatal("initial save did not create the Markdown document")
	}
	branch.HeadCommitID, branch.Generation = initial.ID, branch.Generation+1
	imageBase := projectBase + "/documents/" + document.DocumentID + "/images"
	encodedImage := func(red uint8) (string, []byte) {
		t.Helper()
		picture := image.NewRGBA(image.Rect(0, 0, 2, 2))
		picture.Set(0, 0, color.RGBA{R: red, A: 255})
		var body bytes.Buffer
		if err := png.Encode(&body, picture); err != nil {
			t.Fatal(err)
		}
		return base64.StdEncoding.EncodeToString(body.Bytes()), body.Bytes()
	}
	one, oneBytes := encodedImage(1)
	two, twoBytes := encodedImage(2)
	three, _ := encodedImage(3)
	firstEnvelope := f.requireOK(t, http.MethodPost, imageBase, w.WriterToken, map[string]any{"filename": "screen.png", "content_base64": one})
	first := decodeDetail[documentimage.Asset](t, firstEnvelope)
	if bytes.Contains(firstEnvelope.Detail, []byte("object_key")) || !documentimage.ValidID(first.AssetID) || first.Reference != documentimage.ReferencePrefix+first.AssetID || first.Width != 2 || first.Height != 2 || first.ByteLength != len(oneBytes) {
		t.Fatal("upload leaked storage details or returned invalid image metadata")
	}
	f.requireStatus(t, http.MethodPost, imageBase, w.ReaderToken, map[string]any{"filename": "denied.png", "content_base64": one}, 403, "PERMISSION_DENIED")
	f.requireStatus(t, http.MethodPost, imageBase, w.WriterToken, map[string]any{"filename": "image.svg", "content_base64": base64.StdEncoding.EncodeToString([]byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`))}, 400, "INVALID_ARGUMENT")
	writer := decodeDetail[e2eMCPToken](t, f.requireOK(t, http.MethodPost, "/api/v1/private/mcp-tokens", w.WriterToken, map[string]any{"name": "图片上传", "scopes": []int{app.ScopeDocDraft}}))
	reader := decodeDetail[e2eMCPToken](t, f.requireOK(t, http.MethodPost, "/api/v1/private/mcp-tokens", w.ReaderToken, map[string]any{"name": "只读图片", "scopes": []int{app.ScopeDocRead}}))
	uploadMCP := func(filename, encoded string) documentimage.Asset {
		t.Helper()
		response := f.callTool(t, writer.Token, "upload_document_image", map[string]any{"project_id": w.ProjectID, "document_id": document.DocumentID, "filename": filename, "content_base64": encoded})
		if response.Error != nil {
			t.Fatalf("upload_document_image failed: %s %s", response.Error.Message, response.Error.Data)
		}
		return requireRPCResult[documentimage.Asset](t, response, "upload_document_image")
	}
	second := uploadMCP("mcp.png", two)
	staged := uploadMCP("staged.png", three)
	requireRPCError(t, f.callTool(t, reader.Token, "upload_document_image", map[string]any{"project_id": w.ProjectID, "document_id": document.DocumentID, "filename": "denied.png", "content_base64": one}), -32003, "PERMISSION_DENIED")

	content := "# 图片说明\r\n\r\n![后台](" + first.Reference + ")\r\n\r\n![MCP](" + second.Reference + ")\r\n\r\n```md\n![未插入](" + staged.Reference + ")\n```\n"
	change := repo.Change{Operation: "upsert", DocumentID: document.DocumentID, Name: document.Name, RelativePath: document.RelativePath, DocumentType: app.DocumentTypeMarkdown, Content: content}
	saved := decodeDetail[repo.Commit](t, f.requireOK(t, http.MethodPost, repositoryBase+"/branches/"+branch.ID+"/commits", w.WriterToken, repo.SaveInput{
		ExpectedHeadCommitID: branch.HeadCommitID, ExpectedGeneration: branch.Generation, IdempotencyKey: "images-save-" + w.RunID, Changes: []repo.Change{change},
	}))
	branch.HeadCommitID, branch.Generation = saved.ID, branch.Generation+1
	release := decodeDetail[repo.Release](t, f.requireOK(t, http.MethodPost, repositoryBase+"/releases", w.AdminToken, repo.PublishInput{BranchID: branch.ID, CommitID: saved.ID, ExpectedGeneration: branch.Generation}))
	shareBase := repositoryBase + "/releases/" + release.ID + "/documents/" + document.DocumentID + "/shares"
	capability := decodeDetail[struct {
		Share  repo.Share `json:"share"`
		Secret string     `json:"secret"`
	}](t, f.requireOK(t, http.MethodPost, shareBase, w.AdminToken, map[string]any{"expiry_preset": "permanent", "password": "e2e-images-password-2026"}))
	publicBase := "/api/v1/open/document-shares/" + capability.Share.ID
	request := func(method, path, authorization, proof, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if authorization != "" {
			r.Header.Set("Authorization", authorization)
		}
		if proof != "" {
			r.Header.Set("X-Vdoc-Share-Unlock", proof)
		}
		response := httptest.NewRecorder()
		f.router.ServeHTTP(response, r)
		if response.Code != http.StatusOK {
			t.Fatalf("%s %s returned HTTP %d", method, path, response.Code)
		}
		return response
	}
	decodeEnvelope := func(response *httptest.ResponseRecorder) e2eEnvelope {
		t.Helper()
		var result e2eEnvelope
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatalf("decode image error envelope: %v", err)
		}
		return result
	}
	publicImage := func(asset documentimage.Asset, version string) string {
		return publicBase + "/versions/" + version + "/images/" + asset.AssetID
	}
	secretHeader := "VdocShare " + capability.Secret
	if denied := decodeEnvelope(request(http.MethodGet, publicImage(first, release.ID), "", "", "")); denied.Status != "NOT_FOUND" {
		t.Fatalf("anonymous image error = %s", denied.Status)
	}
	if locked := decodeEnvelope(request(http.MethodGet, publicImage(first, release.ID), secretHeader, "", "")); locked.Status != "PASSWORD_REQUIRED" {
		t.Fatalf("protected image error = %s", locked.Status)
	}
	grant := decodeDetail[struct {
		Proof string `json:"unlock_proof"`
	}](t, decodeEnvelope(request(http.MethodPost, publicBase+"/unlock", secretHeader, "", `{"password":"e2e-images-password-2026"}`)))
	if grant.Proof == "" {
		t.Fatal("share unlock returned no proof")
	}
	assertImage := func(path, auth, proof string, expected []byte) {
		t.Helper()
		response := request(http.MethodGet, path, auth, proof, "")
		if response.Header().Get("Content-Type") != "image/png" || !bytes.Equal(response.Body.Bytes(), expected) || !strings.Contains(response.Header().Get("Cache-Control"), "no-store") || response.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.HasPrefix(response.Header().Get("Content-Disposition"), "inline;") {
			t.Fatalf("image bytes or privacy headers changed: type=%s length=%d", response.Header().Get("Content-Type"), response.Body.Len())
		}
	}
	assertReads := func(t *testing.T) {
		t.Helper()
		assertImage(imageBase+"/"+first.AssetID, w.ReaderToken, "", oneBytes)
		assertImage(imageBase+"/"+second.AssetID, w.ReaderToken, "", twoBytes)
		assertImage(publicImage(first, release.ID), secretHeader, grant.Proof, oneBytes)
		assertImage(publicImage(second, release.ID), secretHeader, grant.Proof, twoBytes)
		for _, path := range []string{publicImage(staged, release.ID), publicImage(first, saved.ID)} {
			if denied := decodeEnvelope(request(http.MethodGet, path, secretHeader, grant.Proof, "")); denied.Status != "NOT_FOUND" {
				t.Fatalf("image outside pinned Markdown leaked: %s", denied.Status)
			}
		}
		loaded := decodeDetail[struct {
			Content string `json:"content"`
		}](t, decodeEnvelope(request(http.MethodGet, publicBase+"/versions/"+release.ID+"/content", secretHeader, grant.Proof, "")))
		if loaded.Content != content {
			t.Fatal("published Markdown lost its stable references or original bytes")
		}
	}
	assertReads(t)
	// Image traffic has its own budget so a document containing many pictures can
	// still fetch its metadata and raw content without exhausting the text quota.
	for i := 0; i < 25; i++ {
		assertImage(publicImage(first, release.ID), secretHeader, grant.Proof, oneBytes)
	}
	if metadata := decodeEnvelope(request(http.MethodGet, publicBase, secretHeader, grant.Proof, "")); metadata.Code != 200 {
		t.Fatalf("image requests exhausted metadata quota: %s", metadata.Status)
	}
	if loaded := decodeEnvelope(request(http.MethodGet, publicBase+"/versions/"+release.ID+"/content", secretHeader, grant.Proof, "")); loaded.Code != 200 {
		t.Fatalf("image requests exhausted content quota: %s", loaded.Status)
	}
	// Publishing a later image does not extend an existing fixed release share.
	change.Content = "![后来插入](" + staged.Reference + ")"
	f.requireOK(t, http.MethodPost, repositoryBase+"/branches/"+branch.ID+"/commits", w.WriterToken, repo.SaveInput{ExpectedHeadCommitID: branch.HeadCommitID, ExpectedGeneration: branch.Generation, IdempotencyKey: "images-later-" + w.RunID, Changes: []repo.Change{change}})
	if live {
		restartLiveDefaultStore(t)
		t.Run("after-restart", assertReads)
	}
	f.requireOK(t, http.MethodPost, shareBase+"/"+capability.Share.ID+"/revoke", w.AdminToken, nil)
	if revoked := decodeEnvelope(request(http.MethodGet, publicImage(first, release.ID), secretHeader, grant.Proof, "")); revoked.Status != "NOT_FOUND" {
		t.Fatal("revoked share still served image bytes")
	}
	var verifyPhysicalDeletion func()
	if live {
		verifyPhysicalDeletion = requireLiveDocumentImageObjects(t, w.ProjectID, document.DocumentID, []string{first.AssetID, second.AssetID, staged.AssetID})
	}
	f.requireOK(t, http.MethodDelete, repositoryBase+"/documents/"+document.DocumentID, w.AdminToken, nil)
	if verifyPhysicalDeletion != nil {
		verifyPhysicalDeletion()
	}
	if deleted := decodeEnvelope(request(http.MethodGet, imageBase+"/"+first.AssetID, w.ReaderToken, "", "")); deleted.Status != "NOT_FOUND" {
		t.Fatal("deleted document still served private image bytes")
	}
	t.Logf("%s: REST + write-only MCP uploads, exact saved/published Markdown, protected pinned images, rate-budget isolation, revocation and document deletion passed", f.mode)
}

// Capture and verify the actual private objects before deletion so the final
// check cannot pass merely because the API hides bytes that remain in storage.
func requireLiveDocumentImageObjects(t *testing.T, project, document string, assets []string) func() {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	database, err := pgdb.OpenWithConfig(ctx, pgdb.Config{DSN: os.Getenv("VDOC_TEST_DATABASE_DSN"), MaxOpenConn: 1, MaxIdleConn: 1, RunMigration: false})
	if err != nil {
		t.Fatalf("open image-object verification database: %v", err)
	}
	defer database.Close()
	storage := pgdbvdoc.NewRepository(database.DB())
	options := &minio.Options{Creds: credentials.NewStaticV4(os.Getenv("VDOC_TEST_STORAGE_ACCESS_KEY"), os.Getenv("VDOC_TEST_STORAGE_SECRET_KEY"), ""), Secure: boolEnv("VDOC_TEST_STORAGE_USE_SSL"), Region: os.Getenv("VDOC_TEST_STORAGE_REGION")}
	if boolEnvDefault("VDOC_TEST_STORAGE_PATH_STYLE", true) {
		options.BucketLookup = minio.BucketLookupPath
	}
	objects, err := minio.New(os.Getenv("VDOC_TEST_STORAGE_ENDPOINT"), options)
	if err != nil {
		t.Fatalf("open image-object verification storage: %v", err)
	}
	bucket := os.Getenv("VDOC_TEST_STORAGE_BUCKET")
	keys := make([]string, 0, len(assets))
	for _, asset := range assets {
		record, err := storage.RepositoryRecord(ctx, project, "document_image", document+":"+asset)
		if err != nil {
			t.Fatalf("read image object metadata: %v", err)
		}
		var metadata struct {
			ObjectKey string `json:"object_key"`
		}
		if err := json.Unmarshal(record.Payload, &metadata); err != nil || metadata.ObjectKey == "" {
			t.Fatal("live image lacks a persisted object key")
		}
		if _, err := objects.StatObject(ctx, bucket, metadata.ObjectKey, minio.StatObjectOptions{}); err != nil {
			t.Fatalf("image object missing before document deletion: %v", err)
		}
		keys = append(keys, metadata.ObjectKey)
	}
	return func() {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, key := range keys {
			_, err := objects.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
			code := minio.ToErrorResponse(err).Code
			if code != "NoSuchKey" && code != "NoSuchObject" && code != "NotFound" {
				t.Fatalf("document deletion retained an image object or storage lookup failed: %v", err)
			}
		}
		t.Logf("verified physical deletion of all %d document image objects", len(keys))
	}
}
