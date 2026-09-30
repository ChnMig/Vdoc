package response

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vdoc/utils/contextkey"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestErrorResponseLogsRequestCancellation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, cancelDeadline := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelDeadline()
	cases := []struct {
		name  string
		ctx   context.Context
		data  responseData
		level string
	}{
		{name: "missing request", data: INTERNAL, level: "error"},
		{name: "active request", ctx: context.Background(), data: INTERNAL, level: "error"},
		{name: "canceled request", ctx: canceled, data: INTERNAL, level: "debug"},
		{name: "deadline exceeded", ctx: expired, data: INTERNAL, level: "error"},
		{name: "business error", ctx: context.Background(), data: INVALID_ARGUMENT, level: "warn"},
		{name: "canceled business error", ctx: canceled, data: INVALID_ARGUMENT, level: "debug"},
		{name: "expired business error", ctx: expired, data: INVALID_ARGUMENT, level: "warn"},
		{name: "timeout response", ctx: expired, data: DEADLINE_EXCEEDED, level: "error"},
		{name: "canceled response", ctx: context.Background(), data: CANCELLED, level: "debug"},
	}
	responders := []struct {
		name   string
		call   func(*gin.Context, responseData)
		detail bool
	}{
		{name: "ReturnError", call: func(c *gin.Context, data responseData) {
			ReturnError(c, data, "response message")
		}},
		{name: "ReturnErrorWithData", detail: true, call: func(c *gin.Context, data responseData) {
			ReturnErrorWithData(c, data, map[string]string{"token": "private-response-token"})
		}},
	}
	for _, responder := range responders {
		t.Run(responder.name, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					var output bytes.Buffer
					logger := zap.New(zapcore.NewCore(
						zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()), zapcore.AddSync(&output), zapcore.DebugLevel,
					))
					recorder := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(recorder)
					traceID := "response-trace"
					if tc.ctx != nil {
						traceID = "standard-response-trace"
						ctx := contextkey.WithTraceID(tc.ctx, traceID)
						c.Request = httptest.NewRequestWithContext(ctx, http.MethodGet, "/test?token=private-query-token", nil)
					}
					c.Set(contextkey.Logger, logger)
					if tc.ctx == nil {
						c.Set(contextkey.TraceID, traceID)
					}
					data := tc.data
					data.Message = "response message"
					responder.call(c, data)
					var entry struct {
						Level    string       `json:"level"`
						TraceID  string       `json:"trace_id"`
						Response responseData `json:"response"`
					}
					if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &entry); err != nil {
						t.Fatalf("expected one response log: %v; logs: %s", err, output.String())
					}
					if entry.Level != tc.level {
						t.Errorf("expected %s log, got %s", tc.level, entry.Level)
					}
					if entry.Response.Detail != nil || strings.Contains(output.String(), "private-response-token") || strings.Contains(output.String(), "private-query-token") {
						t.Errorf("sensitive values entered response log: %s", output.String())
					}
					if entry.TraceID != traceID || entry.Response.TraceID != traceID {
						t.Errorf("missing log trace metadata: %s", output.String())
					}
					if recorder.Code != http.StatusOK || !c.IsAborted() {
						t.Errorf("expected HTTP 200 and aborted context, got status=%d aborted=%t", recorder.Code, c.IsAborted())
					}
					if c.GetInt(contextkey.AppCode) != data.Code || c.GetString(contextkey.AppStatus) != data.Status {
						t.Error("changed access-log business outcome")
					}
					var body responseData
					if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
						t.Fatalf("invalid response JSON: %v", err)
					}
					if body.Code != data.Code || body.Status != data.Status || body.Description != data.Description || body.Message != data.Message {
						t.Errorf("response error fields changed: %#v", body)
					}
					if body.TraceID != traceID || body.Timestamp == 0 {
						t.Errorf("missing response metadata: %#v", body)
					}
					if responder.detail {
						detail, ok := body.Detail.(map[string]any)
						if !ok || detail["token"] != "private-response-token" {
							t.Errorf("response detail changed: %#v", body.Detail)
						}
					} else if body.Detail != nil {
						t.Errorf("unexpected error detail: %#v", body.Detail)
					}
				})
			}
		})
	}
}
