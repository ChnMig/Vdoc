package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"vdoc/config"
)

func TestInitApiUsesConfiguredStaticDirectory(t *testing.T) {
	originalStaticDir := config.StaticDir
	originalMaxBodySize := config.MaxBodySize
	originalRateLimit := config.EnableRateLimit
	originalProxies := append([]string(nil), config.TrustedProxies...)
	t.Cleanup(func() {
		config.StaticDir = originalStaticDir
		config.MaxBodySize = originalMaxBodySize
		config.EnableRateLimit = originalRateLimit
		config.TrustedProxies = originalProxies
	})

	staticDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticDir, "probe.txt"), []byte("static-ok"), 0o600); err != nil {
		t.Fatalf("write static fixture: %v", err)
	}
	config.StaticDir = staticDir
	config.MaxBodySize = 1 << 20
	config.EnableRateLimit = false
	config.TrustedProxies = nil

	recorder := httptest.NewRecorder()
	InitApi().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/static/probe.txt", nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() != "static-ok" {
		t.Fatalf("static response = %d %q, want 200 static-ok", recorder.Code, recorder.Body.String())
	}
}

func TestInitApiCanDisableStaticDirectory(t *testing.T) {
	originalStaticDir := config.StaticDir
	originalMaxBodySize := config.MaxBodySize
	originalRateLimit := config.EnableRateLimit
	originalProxies := append([]string(nil), config.TrustedProxies...)
	t.Cleanup(func() {
		config.StaticDir = originalStaticDir
		config.MaxBodySize = originalMaxBodySize
		config.EnableRateLimit = originalRateLimit
		config.TrustedProxies = originalProxies
	})

	config.StaticDir = ""
	config.MaxBodySize = 1 << 20
	config.EnableRateLimit = false
	config.TrustedProxies = nil
	recorder := httptest.NewRecorder()
	InitApi().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/static/probe.txt", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("disabled static status = %d, want 404", recorder.Code)
	}
}
