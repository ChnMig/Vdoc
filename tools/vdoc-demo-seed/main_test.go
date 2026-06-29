package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseConfig_prefersFlagsOverEnv_whenBothProvided(t *testing.T) {
	// Given
	env := map[string]string{
		"VDOC_BASE_URL": "http://env.example.test",
		"VDOC_EMAIL":    "env@example.test",
		"VDOC_PASSWORD": "env-password",
	}

	// When
	cfg, err := parseConfig([]string{"--base-url", "http://flag.example.test", "--email", "flag@example.test", "--password", "flag-password"}, mapEnv(env))

	// Then
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	if cfg.BaseURL != "http://flag.example.test" || cfg.Email != "flag@example.test" || cfg.Password != "flag-password" {
		t.Fatalf("config = %+v", cfg)
	}
}

func TestDecodeEnvelope_returnsTypedDetail_whenEnvelopeOK(t *testing.T) {
	// Given
	body := []byte(`{"code":200,"status":"OK","detail":{"id":"team_1"}}`)

	// When
	got, err := decodeEnvelope[resourceID](body)

	// Then
	if err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if got.ID != "team_1" {
		t.Fatalf("id = %q", got.ID)
	}
}

func TestDecodeEnvelope_returnsError_whenEnvelopeReportsFailure(t *testing.T) {
	// Given
	body := []byte(`{"code":401,"status":"UNAUTHENTICATED","message":"认证失败"}`)

	// When
	_, err := decodeEnvelope[resourceID](body)

	// Then
	if err == nil || !strings.Contains(err.Error(), "UNAUTHENTICATED") {
		t.Fatalf("error = %v, want status", err)
	}
}

func TestRunSeed_createsWorkspaceAndMasksSecrets_whenServerOK(t *testing.T) {
	// Given
	fake := newSeedServer(t)
	defer fake.Close()
	out := &strings.Builder{}
	cfg := config{BaseURL: fake.URL, Timeout: defaultTimeout, RunID: "test-run"}

	// When
	err := runSeed(context.Background(), cfg, out)

	// Then
	if err != nil {
		t.Fatalf("run seed: %v", err)
	}
	wantOrder := []string{
		"POST /api/v1/open/auth/register",
		"POST /api/v1/open/auth/login",
		"POST /api/v1/private/system/users",
		"POST /api/v1/open/auth/login",
		"POST /api/v1/private/teams",
		"POST /api/v1/private/projects",
		"POST /api/v1/private/projects/project_1/members",
		"POST /api/v1/private/projects/project_1/documents",
		"GET /api/v1/private/projects/project_1/documents/document_1/branches",
		"POST /api/v1/private/projects/project_1/documents/document_1/drafts",
		"POST /api/v1/private/projects/project_1/documents/document_1/drafts/draft_1/submit",
		"POST /api/v1/private/projects/project_1/documents/document_1/drafts/draft_1/approve",
		"POST /api/v1/private/projects/project_1/documents/document_1/drafts",
		"POST /api/v1/private/projects/project_1/documents/document_1/drafts/draft_2/submit",
		"POST /api/v1/private/projects/project_1/documents/document_1/drafts/draft_2/approve",
		"POST /api/v1/private/projects/project_1/documents/document_1/diffs",
		"POST /api/v1/private/mcp-tokens",
	}
	if strings.Join(fake.calls, "\n") != strings.Join(wantOrder, "\n") {
		t.Fatalf("calls = %#v", fake.calls)
	}
	output := out.String()
	for _, forbidden := range []string{"jwt-admin-secret", "jwt-writer-secret", "mcp-raw-secret", defaultPassword} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("output leaked %q: %s", forbidden, output)
		}
	}
	for _, required := range []string{"team_1", "project_1", "document_1", "diff_1", "mcp_token_1", "VDOC_MCP_TOKEN=<redacted>"} {
		if !strings.Contains(output, required) {
			t.Fatalf("output missing %q: %s", required, output)
		}
	}
}

type seedServer struct {
	*httptest.Server
	calls []string
}

func newSeedServer(t *testing.T) *seedServer {
	t.Helper()
	fake := &seedServer{}
	fake.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.calls = append(fake.calls, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/api/v1/open/auth/register":
			writeOK(w, authDetail{User: userDetail{ID: "admin_1", Email: "admin@example.test"}, Token: "jwt-register-secret"})
		case "/api/v1/open/auth/login":
			var body struct {
				Email string `json:"email"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode login: %v", err)
			}
			if strings.Contains(body.Email, "writer") {
				writeOK(w, authDetail{User: userDetail{ID: "writer_1", Email: body.Email}, Token: "jwt-writer-secret"})
				return
			}
			writeOK(w, authDetail{User: userDetail{ID: "admin_1", Email: body.Email}, Token: "jwt-admin-secret"})
		case "/api/v1/private/system/users":
			requireAuth(t, r, "jwt-admin-secret")
			writeOK(w, userDetail{ID: "writer_1", Email: "writer@example.test"})
		case "/api/v1/private/teams":
			requireAuth(t, r, "jwt-admin-secret")
			writeOK(w, resourceID{ID: "team_1"})
		case "/api/v1/private/projects":
			requireAuth(t, r, "jwt-admin-secret")
			writeOK(w, resourceID{ID: "project_1"})
		case "/api/v1/private/projects/project_1/members":
			requireAuth(t, r, "jwt-admin-secret")
			writeOK(w, resourceID{ID: "member_1"})
		case "/api/v1/private/projects/project_1/documents":
			requireAuth(t, r, "jwt-admin-secret")
			writeOK(w, resourceID{ID: "document_1"})
		case "/api/v1/private/projects/project_1/documents/document_1/branches":
			requireAuth(t, r, "jwt-admin-secret")
			writeOK(w, []branch{{ID: "branch_dev", Name: "dev"}})
		case "/api/v1/private/projects/project_1/documents/document_1/drafts":
			requireAuth(t, r, "jwt-writer-secret")
			if strings.Count(strings.Join(fake.calls, "\n"), r.URL.Path) == 1 {
				writeOK(w, resourceID{ID: "draft_1"})
				return
			}
			writeOK(w, resourceID{ID: "draft_2"})
		case "/api/v1/private/projects/project_1/documents/document_1/drafts/draft_1/submit", "/api/v1/private/projects/project_1/documents/document_1/drafts/draft_2/submit":
			requireAuth(t, r, "jwt-writer-secret")
			writeOK(w, resourceID{ID: "submitted"})
		case "/api/v1/private/projects/project_1/documents/document_1/drafts/draft_1/approve":
			requireAuth(t, r, "jwt-admin-secret")
			writeOK(w, resourceID{ID: "version_1"})
		case "/api/v1/private/projects/project_1/documents/document_1/drafts/draft_2/approve":
			requireAuth(t, r, "jwt-admin-secret")
			writeOK(w, resourceID{ID: "version_2"})
		case "/api/v1/private/projects/project_1/documents/document_1/diffs":
			requireAuth(t, r, "jwt-admin-secret")
			writeOK(w, resourceID{ID: "diff_1"})
		case "/api/v1/private/mcp-tokens":
			requireAuth(t, r, "jwt-admin-secret")
			writeOK(w, mcpToken{ID: "mcp_token_1", Token: "mcp-raw-secret"})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	return fake
}

func requireAuth(t *testing.T, r *http.Request, token string) {
	t.Helper()
	if got := r.Header.Get("Authorization"); got != token {
		t.Fatalf("Authorization = %q, want %q", got, token)
	}
}

func writeOK(w http.ResponseWriter, detail any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "status": "OK", "detail": detail})
}

func mapEnv(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}
