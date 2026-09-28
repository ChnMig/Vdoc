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
	if got := allowedRecorder.Header().Get("Access-Control-Allow-Headers"); got != corsAllowedHeaders {
		t.Fatalf("allowed headers = %q", got)
	}
	if got := allowedRecorder.Header().Get("Access-Control-Allow-Methods"); got != corsAllowedMethods {
		t.Fatalf("allowed methods = %q", got)
	}
	if got := allowedRecorder.Header().Get("Access-Control-Expose-Headers"); got != corsExposedHeaders {
		t.Fatalf("exposed headers = %q", got)
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

func TestCorsWildcardAllowsBrowserOriginsWithoutBypassingAuthorization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(CorsDomainHandler("*"))
	router.GET("/private", func(c *gin.Context) { c.AbortWithStatus(http.StatusUnauthorized) })
	for _, origin := range []string{"https://admin.example.test", "https://another-client.example.test", "null"} {
		for _, method := range []string{http.MethodOptions, http.MethodGet} {
			t.Run(origin+"/"+method, func(t *testing.T) {
				request := httptest.NewRequest(method, "/private", nil)
				request.Header.Set("Origin", origin)
				request.Header.Set("Access-Control-Request-Headers", "authorization,content-type")
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, request)
				wantStatus := http.StatusUnauthorized
				if method == http.MethodOptions {
					wantStatus = http.StatusNoContent
				}
				if recorder.Code != wantStatus || recorder.Header().Get("Access-Control-Allow-Origin") != "*" {
					t.Fatalf("status=%d headers=%v", recorder.Code, recorder.Header())
				}
				if recorder.Header().Get("Access-Control-Allow-Credentials") != "" {
					t.Fatal("wildcard mode must not enable ambient browser credentials")
				}
				if recorder.Header().Get("Access-Control-Allow-Headers") != corsAllowedHeaders {
					t.Fatal("wildcard mode must retain the supported authorization and share headers")
				}
			})
		}
	}
}
