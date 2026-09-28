package e2e

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vdoc/api/middleware"
	"vdoc/config"
	app "vdoc/services/vdoc"
)

func TestStorageLimitsAndPathsExtensions(t *testing.T) {
	live := liveE2ERequested()
	fixture := newE2EFixture(t, e2eFixtureOptions{LivePersistence: live})
	workspace := createWorkspace(t, fixture, e2eRunID())
	schema := `{"openapi":"3.1.0","info":{"title":"Storage boundary","version":"1","description":"` + strings.Repeat("<", 2<<20) + `"},"paths":{"/values":{"get":{"responses":{"200":{"description":"ok"}}}}}}`
	parsed, err := app.ParseOpenAPI(schema)
	if err != nil || int64(len(parsed.Normalized)) <= config.MaxBodySize {
		t.Fatalf("expected valid source with oversized normalized content: %v", err)
	}
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	// 保留客户端可合法发送的原始字符，避免请求本身先被体积中间件拦截。
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(map[string]any{
		"branch_id": workspace.BranchID, "version_name": "oversized", "schema_content": schema,
	}); err != nil {
		t.Fatal(err)
	}
	requestBytes := body.Len()
	if int64(requestBytes) >= config.MaxBodySize {
		t.Fatalf("request itself exceeds middleware limit: %d", requestBytes)
	}
	request := httptest.NewRequest(http.MethodPost, draftCollectionPath(workspace), &body)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(middleware.AuthorizationHeader, workspace.WriterToken)
	recorder := httptest.NewRecorder()
	fixture.router.ServeHTTP(recorder, request)
	var envelope e2eEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK || envelope.Code != 400 || envelope.Status != "INVALID_ARGUMENT" || envelope.TraceID == "" {
		t.Fatalf("oversized normalized content response: HTTP %d, code %d, status %q, trace %q", recorder.Code, envelope.Code, envelope.Status, envelope.TraceID)
	}
	drafts := decodeDetail[[]e2eResourceID](t, fixture.requireOK(t, http.MethodGet, draftCollectionPath(workspace), workspace.WriterToken, nil))
	if len(drafts) != 0 {
		t.Fatalf("rejected upload persisted %d drafts", len(drafts))
	}

	schema = `{"openapi":"3.1.0","info":{"title":"Extensions","version":"1"},"paths":{"x-notes":{"$ref":"literal-data","get":{"responses":{"200":{"description":"not an operation"}}}},"/values":{"get":{"responses":{"200":{"description":"ok"}}}}}}`
	version := publishVersion(t, fixture, workspace, "1.0.0", schema)
	if live {
		restartLiveDefaultStore(t)
	}
	endpoints := decodeDetail[[]e2eEndpoint](t, fixture.requireOK(t, http.MethodGet, endpointsPath(workspace, version.ID), workspace.ReaderToken, nil))
	if len(endpoints) != 1 || endpoints[0].Path != "/values" {
		t.Fatalf("published endpoints included extension data: %+v", endpoints)
	}
	for _, kind := range []string{"raw", "normalized"} {
		content := decodeDetail[e2eSchemaDocument](t, fixture.requireOK(t, http.MethodGet, versionPath(workspace, version.ID)+"/content/"+kind, workspace.ReaderToken, nil))
		if !strings.Contains(content.Content, `"x-notes":`) || (kind == "raw" && content.Content != schema) {
			t.Fatalf("%s content lost extension data", kind)
		}
	}
	t.Logf("%s: rejected %d-byte request whose normalized object exceeds %d bytes; valid Paths extension published and remained readable", fixture.mode, requestBytes, config.MaxBodySize)
}
