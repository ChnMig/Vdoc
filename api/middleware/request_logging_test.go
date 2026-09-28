package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestRequestFailureLogsRetainContextWithoutRequestValues(t *testing.T) {
	for _, tc := range []struct {
		name    string
		handler gin.HandlerFunc
		code    int
		unread  bool
	}{
		{"binding", func(c *gin.Context) {
			CheckJSONParamWithMessage(&struct {
				Count int `json:"count"`
			}{}, c, "invalid request")
		}, 400, false},
		{"authentication", TokenVerify, 401, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			logger := zap.New(zapcore.NewCore(
				zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()), zapcore.AddSync(&output), zap.DebugLevel,
			))
			undo := zap.ReplaceGlobals(logger)
			t.Cleanup(undo)
			router := gin.New()
			router.Use(TraceID())
			router.POST("/check", tc.handler)
			const body = `{"count":[1],"password":"body-secret"}`
			req := httptest.NewRequest(http.MethodPost, "/check?token=query-secret", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set(TraceIDHeaderKey, "trace-request-log")
			req.Header.Set(AuthorizationHeader, "header-secret")
			req.Header.Set("Cookie", "session=cookie-secret")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			var response struct {
				Code    int    `json:"code"`
				TraceID string `json:"trace_id"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusOK || response.Code != tc.code || response.TraceID != "trace-request-log" {
				t.Fatalf("response contract changed: HTTP %d, %s", w.Code, w.Body.String())
			}
			for _, secret := range []string{"body-secret", "query-secret", "header-secret", "cookie-secret"} {
				if strings.Contains(output.String(), secret) {
					t.Fatalf("request value leaked into log: %s", output.String())
				}
			}
			decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))
			warnings := 0
			for {
				var entry map[string]any
				if err := decoder.Decode(&entry); err == io.EOF {
					break
				} else if err != nil {
					t.Fatal(err)
				}
				if entry["trace_id"] != "trace-request-log" || entry["path"] != "/check" || entry["method"] != http.MethodPost {
					t.Fatalf("missing request metadata: %#v", entry)
				}
				if entry["level"] == "warn" {
					warnings++
				}
			}
			if warnings != 1 {
				t.Fatalf("warning count = %d, want one unified diagnostic: %s", warnings, output.String())
			}
			if tc.unread {
				remaining, err := io.ReadAll(req.Body)
				if err != nil || string(remaining) != body {
					t.Fatalf("authentication rejection consumed body: %q, %v", remaining, err)
				}
			}
		})
	}
}
