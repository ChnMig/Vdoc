package response

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"vdoc/utils/contextkey"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestReturnOk(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	testData := map[string]string{"message": "test"}
	ReturnOk(c, testData)

	// 验证响应
	if w.Code != 200 {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var resp responseData
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if resp.Code != 200 {
		t.Errorf("Expected code 200, got %d", resp.Code)
	}

	if resp.Status != "OK" {
		t.Errorf("Expected status 'OK', got '%s'", resp.Status)
	}

	if resp.Timestamp == 0 {
		t.Error("Expected non-zero timestamp")
	}

	if resp.Detail == nil {
		t.Error("Expected detail to be set")
	}
}

func TestReturnOkWithTotal(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	testData := []string{"item1", "item2"}
	total := 100
	ReturnOkWithTotal(c, total, testData)

	// 验证响应
	if w.Code != 200 {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var resp responseData
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if resp.Total == nil {
		t.Fatal("Expected total to be set")
	}

	if *resp.Total != total {
		t.Errorf("Expected total %d, got %d", total, *resp.Total)
	}
}

func TestReturnError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	errorMsg := "Invalid parameter"
	ReturnError(c, INVALID_ARGUMENT, errorMsg)

	// 验证响应
	if w.Code != 200 {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var resp responseData
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if resp.Code != 400 {
		t.Errorf("Expected code 400, got %d", resp.Code)
	}

	if resp.Status != "INVALID_ARGUMENT" {
		t.Errorf("Expected status 'INVALID_ARGUMENT', got '%s'", resp.Status)
	}

	if resp.Message != errorMsg {
		t.Errorf("Expected message '%s', got '%s'", errorMsg, resp.Message)
	}

	if resp.Timestamp == 0 {
		t.Error("Expected non-zero timestamp")
	}
}

func TestReturnSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	ReturnSuccess(c)

	// 验证响应
	if w.Code != 200 {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var resp responseData
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if resp.Code != 200 {
		t.Errorf("Expected code 200, got %d", resp.Code)
	}

	if resp.Status != "OK" {
		t.Errorf("Expected status 'OK', got '%s'", resp.Status)
	}

	if resp.Detail != nil {
		t.Error("Expected detail to be nil")
	}
}

func TestReturnErrorWithData(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	testDetail := map[string]string{"field": "username", "error": "required"}
	ReturnErrorWithData(c, INVALID_ARGUMENT, testDetail)

	// 验证响应
	if w.Code != 200 {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var resp responseData
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if resp.Code != 400 {
		t.Errorf("Expected code 400, got %d", resp.Code)
	}

	if resp.Detail == nil {
		t.Error("Expected detail to be set")
	}
}

func TestResponseForLogRedactsDetail(t *testing.T) {
	total := 1
	data := responseData{Code: 200, Status: "OK", Detail: map[string]string{"token": "vdoc_secret"}, Total: &total}

	logged := responseForLog(data)
	if logged.Detail != nil {
		t.Fatalf("logged detail = %#v, want nil", logged.Detail)
	}
	if data.Detail == nil {
		t.Fatal("responseForLog mutated original response detail")
	}
	if logged.Total == nil || *logged.Total != total || logged.Code != data.Code || logged.Status != data.Status {
		t.Fatalf("logged response metadata = %+v, want code/status/total preserved", logged)
	}
}

// 测试 trace_id 是否正确设置
func TestTraceIDInResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	expectedTraceID := "test-trace-id-12345"
	c.Set(contextkey.TraceID, expectedTraceID)

	testData := map[string]string{"message": "test"}
	ReturnOk(c, testData)

	var resp responseData
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if resp.TraceID != expectedTraceID {
		t.Errorf("Expected trace_id '%s', got '%s'", expectedTraceID, resp.TraceID)
	}
}

// 测试错误响应中的 trace_id
func TestTraceIDInErrorResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	expectedTraceID := "error-trace-id-67890"
	c.Set(contextkey.TraceID, expectedTraceID)

	ReturnError(c, INVALID_ARGUMENT, "Test error")

	var resp responseData
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if resp.TraceID != expectedTraceID {
		t.Errorf("Expected trace_id '%s', got '%s'", expectedTraceID, resp.TraceID)
	}
}

// 测试没有 trace_id 的情况
func TestNoTraceID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	// 不设置 Request ID
	testData := map[string]string{"message": "test"}
	ReturnOk(c, testData)

	var resp responseData
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	// trace_id 应该为空字符串
	if resp.TraceID != "" {
		t.Errorf("Expected empty trace_id, got '%s'", resp.TraceID)
	}
}

func TestTraceIDInStandardRequestContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	request := httptest.NewRequest("GET", "/", nil)
	c.Request = request.WithContext(contextkey.WithTraceID(context.Background(), "trace-standard-context"))

	ReturnSuccess(c)

	var resp responseData
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	if resp.TraceID != "trace-standard-context" {
		t.Fatalf("trace_id = %q, want trace-standard-context", resp.TraceID)
	}
}

func TestLogErrorResponseUsesSeverityBySemanticCode(t *testing.T) {
	for _, tt := range []struct {
		name  string
		data  responseData
		level string
	}{
		{name: "cancelled", data: CANCELLED, level: "debug"},
		{name: "client error", data: INVALID_ARGUMENT, level: "warn"},
		{name: "server error", data: INTERNAL, level: "error"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			logger := zap.New(zapcore.NewCore(
				zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()), zapcore.AddSync(&output), zap.DebugLevel,
			))

			logErrorResponse(logger, "response failed", tt.data)

			var entry map[string]any
			if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
				t.Fatalf("decode log entry: %v", err)
			}
			if got := entry["level"]; got != tt.level {
				t.Fatalf("log level = %v, want %s", got, tt.level)
			}
		})
	}
}

// 测试所有预定义的错误码
func TestAllErrorCodes(t *testing.T) {
	testCases := []struct {
		name         string
		errorData    responseData
		expectedCode int
	}{
		{"OK", OK, 200},
		{"INVALID_ARGUMENT", INVALID_ARGUMENT, 400},
		{"FAILED_PRECONDITION", FAILED_PRECONDITION, 400},
		{"OUT_OF_RANGE", OUT_OF_RANGE, 400},
		{"UNAUTHENTICATED", UNAUTHENTICATED, 401},
		{"PASSWORD_REQUIRED", PASSWORD_REQUIRED, 401},
		{"PERMISSION_DENIED", PERMISSION_DENIED, 403},
		{"NOT_FOUND", NOT_FOUND, 404},
		{"ABORTED", ABORTED, 409},
		{"ALREADY_EXISTS", ALREADY_EXISTS, 409},
		{"RESOURCE_EXHAUSTED", RESOURCE_EXHAUSTED, 429},
		{"CANCELLED", CANCELLED, 499},
		{"DATA_LOSS", DATA_LOSS, 500},
		{"UNKNOWN", UNKNOWN, 500},
		{"INTERNAL", INTERNAL, 500},
		{"NOT_IMPLEMENTED", NOT_IMPLEMENTED, 501},
		{"UNAVAILABLE", UNAVAILABLE, 503},
		{"DEADLINE_EXCEEDED", DEADLINE_EXCEEDED, 504},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.errorData.Code != tc.expectedCode {
				t.Errorf("%s: expected code %d, got %d", tc.name, tc.expectedCode, tc.errorData.Code)
			}
			if tc.errorData.Status == "" {
				t.Errorf("%s: status should not be empty", tc.name)
			}
			if tc.errorData.Description == "" {
				t.Errorf("%s: description should not be empty", tc.name)
			}
		})
	}
}
