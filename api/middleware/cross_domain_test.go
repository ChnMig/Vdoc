package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCorsDomainHandlerAllowsOnlyConfiguredOrigins(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(CorsDomainHandler("https://admin.example.test"))
	router.GET("/resource", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	allowed := httptest.NewRequest(http.MethodGet, "/resource", nil)
	allowed.Header.Set("Origin", "https://admin.example.test")
	allowedRecorder := httptest.NewRecorder()
	router.ServeHTTP(allowedRecorder, allowed)
	if got := allowedRecorder.Header().Get("Access-Control-Allow-Origin"); got != "https://admin.example.test" {
		t.Fatalf("allowed origin header = %q", got)
	}
	if got := allowedRecorder.Header().Get("Access-Control-Allow-Headers"); got != "Authorization, Content-Type, X-Vdoc-Share-Unlock" {
		t.Fatalf("allowed headers = %q", got)
	}

	denied := httptest.NewRequest(http.MethodOptions, "/resource", nil)
	denied.Header.Set("Origin", "https://attacker.example.test")
	deniedRecorder := httptest.NewRecorder()
	router.ServeHTTP(deniedRecorder, denied)
	if deniedRecorder.Code != http.StatusForbidden || deniedRecorder.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("denied preflight status=%d headers=%v", deniedRecorder.Code, deniedRecorder.Header())
	}
}

func TestCorsDomainHandlerReturnsNoContentForAllowedPreflight(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(CorsDomainHandler("https://admin.example.test"))

	request := httptest.NewRequest(http.MethodOptions, "/resource", nil)
	request.Header.Set("Origin", "https://admin.example.test")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
}
