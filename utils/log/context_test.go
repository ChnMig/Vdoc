package log

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	ctx.Request = httptest.NewRequest(http.MethodPost, "/projects/project-1?token=query-secret&search=private", nil)
	ctx.Params = gin.Params{{Key: "project_id", Value: "project-1"}}
	ctx.Set(contextkey.Logger, logger)
	ctx.Set(BoundParamsKey, map[string]string{"token": "body-secret"})

	WithRequest(ctx).Error("operation failed")

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

func TestFromContextFallsBackToStandardTraceContext(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	ctx.Request = request.WithContext(WithTraceID(request.Context(), "trace-standard-1"))

	logger := FromContext(ctx)
	if logger == nil {
		t.Fatal("FromContext() returned nil")
	}
}

func TestWithRequestAcceptsNilContext(t *testing.T) {
	if logger := WithRequest(nil); logger == nil {
		t.Fatal("WithRequest(nil) returned nil")
	}
}

func TestWithRequestDoesNotDuplicateInjectedRequestFields(t *testing.T) {
	var output bytes.Buffer
	logger := zap.New(zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()), zapcore.AddSync(&output), zap.DebugLevel,
	)).With(
		zap.String("method", http.MethodGet),
		zap.String("path", "/projects/project-1"),
	)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/projects/project-1?view=summary", nil)
	ctx.Set(contextkey.Logger, logger)

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
}
