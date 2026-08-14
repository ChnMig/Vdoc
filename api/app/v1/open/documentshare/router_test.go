package documentshare

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"vdoc/api/middleware"

	"github.com/gin-gonic/gin"
)

func TestPublicShareRoutesSetPrivacyHeadersBeforeAuthorization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterOpenRoutes(router.Group("/api/v1/open"))

	request := httptest.NewRequest(http.MethodGet, "/api/v1/open/document-shares/00000000000000000000000000000000", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	for header, want := range map[string]string{
		"Cache-Control":           "no-store, max-age=0",
		"Pragma":                  "no-cache",
		"Referrer-Policy":         "no-referrer",
		"X-Robots-Tag":            "noindex, nofollow, noarchive",
		"Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'",
	} {
		if got := recorder.Header().Get(header); got != want {
			t.Fatalf("%s = %q, want %q", header, got, want)
		}
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, want envelope status 200", recorder.Code)
	}
}

func TestPublicShareRateLimitCannotBeBypassedWithDistinctShareIDs(t *testing.T) {
	middleware.CleanupAllLimiters()
	t.Cleanup(middleware.CleanupAllLimiters)
	gin.SetMode(gin.TestMode)
	handled := 0
	router := gin.New()
	group := router.Group("/document-shares/:share_id")
	group.Use(noStoreHeaders(), publicShareRateLimit())
	group.GET("", func(c *gin.Context) {
		handled++
		c.Status(http.StatusNoContent)
	})

	for index := 0; index < 60; index++ {
		request := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/document-shares/%032x", index), nil)
		request.RemoteAddr = "192.0.2.20:1234"
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if recorder.Header().Get("Cache-Control") != "no-store, max-age=0" {
			t.Fatalf("throttled response lost privacy headers")
		}
	}
	if handled >= 60 {
		t.Fatalf("distinct share IDs bypassed the per-IP limiter: handled=%d", handled)
	}
}

func TestPublicCapabilityRejectsMalformedAuthorization(t *testing.T) {
	for _, value := range []string{"", "Bearer secret", "VdocShare", "VdocShare  secret", "VdocShare secret ", "VdocShare secret extra"} {
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Params = gin.Params{{Key: "share_id", Value: "share"}}
		context.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		context.Request.Header.Set("Authorization", value)
		if _, _, ok := publicCapability(context); ok {
			t.Fatalf("Authorization %q accepted", value)
		}
	}
}
