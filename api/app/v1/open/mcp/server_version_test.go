package mcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vdoc/api/middleware"
	app "vdoc/appstore"
	"vdoc/services/system"
)

func TestMCPReportsAuthenticatedRuntimeVersion(t *testing.T) {
	fixture := newMCPFixture(t)
	previous := system.CurrentVersion()
	t.Cleanup(func() { system.SetBuildInfo(previous.Version, previous.GitCommit, previous.BuildTime) })
	token, err := app.DefaultStore().CreateMCPToken(fixture.readerID, "version-check", []int{app.ScopeAPIRead}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"0.4.0-beta.6", "0.4.1", "dev"} {
		system.SetBuildInfo(version, "fixture-commit", "fixture-time")
		for _, body := range []string{
			`{"jsonrpc":"2.0","id":"version-check","method":"tools/list"}`,
			`{"tool":"list_projects","arguments":{}}`,
		} {
			for _, credential := range []string{token.Token, "", "vdoc_invalid_token"} {
				request := httptest.NewRequest(http.MethodPost, "/api/v1/open/mcp", strings.NewReader(body))
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set(middleware.AuthorizationHeader, credential)
				response := httptest.NewRecorder()
				fixture.router.ServeHTTP(response, request)
				want := ""
				if credential == token.Token {
					want = version
				}
				if got := response.Header().Get("X-Vdoc-Server-Version"); got != want {
					t.Fatalf("runtime=%q credential present=%t: version header=%q, want %q", version, credential != "", got, want)
				}
			}
		}
	}
}
