package log

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"vdoc/utils/contextkey"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestWithRequestRedactsRequestValues(t *testing.T) {
	var output bytes.Buffer
	logger := zap.New(zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()), zapcore.AddSync(&output), zap.DebugLevel,
	))
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	const body = `{"password":"body-secret"}`
	ctx.Request = httptest.NewRequest(http.MethodPost, "/projects/project-1?token=query-secret&search=private", strings.NewReader(body))
	ctx.Request.Header.Set("Authorization", "header-secret")
	ctx.Request.Header.Set("Cookie", "session=cookie-secret")
	ctx.Request.PostForm = url.Values{"password": {"form-secret"}}
	ctx.Request.MultipartForm = &multipart.Form{Value: url.Values{"api_key": {"multipart-secret"}}}
	ctx.Params = gin.Params{{Key: "project_id", Value: "project-1"}}
	ctx.Set(contextkey.Logger, logger)
	ctx.Set(BoundParamsKey, map[string]string{"token": "body-secret"})

	WithRequest(ctx).Error("operation failed")
	for _, secret := range []string{"query-secret", "body-secret", "header-secret", "cookie-secret", "form-secret", "multipart-secret"} {
		if strings.Contains(output.String(), secret) {
			t.Fatalf("request value %q leaked into log: %s", secret, output.String())
		}
	}
	remaining, err := io.ReadAll(ctx.Request.Body)
	if err != nil || string(remaining) != body {
		t.Fatalf("logging changed request body: %q, %v", remaining, err)
	}

	var entry map[string]any
	if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
		t.Fatalf("decode log entry: %v", err)
	}
	if _, exists := entry["query"]; exists {
		t.Fatalf("log contains raw query: %#v", entry["query"])
	}
	if _, exists := entry["params"]; exists {
		t.Fatalf("log contains bound params: %#v", entry["params"])
	}
	keys, ok := entry["query_keys"].([]any)
	if !ok || len(keys) != 2 || keys[0] != "search" || keys[1] != "token" {
		t.Fatalf("query_keys = %#v, want [search token]", entry["query_keys"])
	}
}

func TestFromContextFallsBackToStandardTraceContextWithBaseLogger(t *testing.T) {
	var output bytes.Buffer
	base := zap.New(zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()), zapcore.AddSync(&output), zap.DebugLevel,
	))
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set(contextkey.Logger, base)
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	ctx.Request = request.WithContext(WithTraceID(request.Context(), "trace-standard-1"))

	FromContext(ctx).Info("standard context fallback")
	var entry map[string]any
	if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry["trace_id"] != "trace-standard-1" || entry["method"] != http.MethodGet || entry["path"] != "/health" || entry["client_ip"] != "192.0.2.1" {
		t.Fatalf("missing request metadata: %s", output.String())
	}
}

func TestWithRequestAcceptsNilContext(t *testing.T) {
	if logger := WithRequest(nil); logger == nil {
		t.Fatal("WithRequest(nil) returned nil")
	}
}

func TestWithRequestDoesNotDuplicateRequestFields(t *testing.T) {
	var output bytes.Buffer
	logger := zap.New(zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()), zapcore.AddSync(&output), zap.DebugLevel,
	))
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/projects/project-1?view=summary", nil)
	ctx.Set(contextkey.Logger, logger)
	ctx.Set(contextkey.TraceID, "gin-trace")
	ctx.Request = ctx.Request.WithContext(WithTraceID(ctx.Request.Context(), "standard-trace"))

	WithRequest(ctx).Warn("request failed")

	entry := output.String()
	if got := strings.Count(entry, `"method"`); got != 1 {
		t.Fatalf("method field count = %d, want 1: %s", got, entry)
	}
	if got := strings.Count(entry, `"path"`); got != 1 {
		t.Fatalf("path field count = %d, want 1: %s", got, entry)
	}
	if !strings.Contains(entry, `"query_keys"`) {
		t.Fatalf("query_keys missing: %s", entry)
	}
	if strings.Count(entry, `"trace_id"`) != 1 || !strings.Contains(entry, `"trace_id":"gin-trace"`) {
		t.Fatalf("request trace missing or duplicated: %s", entry)
	}
}
