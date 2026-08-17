package documentshare

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"vdoc/api/middleware"
	app "vdoc/appstore"

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

func TestReturnPublicShareErrorOnlyChallengesValidatedCapabilities(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantCode   int
		wantStatus string
	}{
		{
			name:       "password proof required",
			err:        fmt.Errorf("wrapped: %w", app.ErrPublicSharePasswordRequired),
			wantCode:   http.StatusUnauthorized,
			wantStatus: "PASSWORD_REQUIRED",
		},
		{name: "unavailable", err: app.ErrNotFound, wantCode: http.StatusNotFound, wantStatus: "NOT_FOUND"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			returnPublicShareError(context, test.err)

			var envelope struct {
				Code   int    `json:"code"`
				Status string `json:"status"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if envelope.Code != test.wantCode || envelope.Status != test.wantStatus {
				t.Fatalf("response = (%d, %q), want (%d, %q)", envelope.Code, envelope.Status, test.wantCode, test.wantStatus)
			}
		})
	}
}
