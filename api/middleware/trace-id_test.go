package middleware

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	serviceLog "vdoc/utils/log"

	"github.com/gin-gonic/gin"
)

func TestTraceIDGeneratesIDWhenMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(TraceID())
	router.GET("/", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	traceID := recorder.Header().Get(TraceIDHeaderKey)
	if !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(traceID) {
		t.Fatalf("generated trace ID = %q, want 32 lowercase hexadecimal characters", traceID)
	}
}

func TestTraceIDKeepsSafeCallerIDAndPropagatesStandardContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const expected = "trace-upstream_123"
	router := gin.New()
	router.Use(TraceID())
	var propagated string
	router.GET("/", func(c *gin.Context) {
		propagated, _ = serviceLog.TraceID(c.Request.Context())
		c.Status(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(TraceIDHeaderKey, expected)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if got := recorder.Header().Get(TraceIDHeaderKey); got != expected {
		t.Fatalf("response trace ID = %q, want %q", got, expected)
	}
	if propagated != expected {
		t.Fatalf("standard context trace ID = %q, want %q", propagated, expected)
	}
}

func TestTraceIDReplacesUnsafeOrOversizedCallerID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, callerID := range []string{"trace id with spaces", strings.Repeat("a", 129)} {
		t.Run(callerID[:min(len(callerID), 16)], func(t *testing.T) {
			router := gin.New()
			router.Use(TraceID())
			router.GET("/", func(c *gin.Context) { c.Status(http.StatusNoContent) })
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.Header.Set(TraceIDHeaderKey, callerID)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)

			got := recorder.Header().Get(TraceIDHeaderKey)
			if got == callerID || !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(got) {
				t.Fatalf("replacement trace ID = %q", got)
			}
		})
	}
}
