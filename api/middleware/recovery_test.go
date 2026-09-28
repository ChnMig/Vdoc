package middleware

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"syscall"
	"testing"

	"vdoc/api/response"
	"vdoc/utils/contextkey"

	"github.com/gin-gonic/gin"
)

func TestRecoveryReturnsUnifiedInternalResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(TraceID(), AccessLog(), Recovery())
	router.GET("/panic", func(c *gin.Context) {
		panic("boom")
	})

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	req.Header.Set(contextkey.TraceIDHeader, "trace-123")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, want %d", w.Code, http.StatusOK)
	}

	var body struct {
		Code    int    `json:"code"`
		Status  string `json:"status"`
		Message string `json:"message"`
		TraceID string `json:"trace_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	if body.Code != response.INTERNAL.Code || body.Status != response.INTERNAL.Status {
		t.Fatalf("响应状态 = %d/%s, want %d/%s", body.Code, body.Status, response.INTERNAL.Code, response.INTERNAL.Status)
	}
	if body.TraceID != "trace-123" {
		t.Fatalf("trace_id = %q, want trace-123", body.TraceID)
	}
	if body.Message != "服务内部错误" {
		t.Fatalf("message = %q, want 服务内部错误", body.Message)
	}
}

func TestRecoveryWritesResponseBeforeOuterAccessLogDefer(t *testing.T) {
	gin.SetMode(gin.TestMode)

	responseSizeInOuterDefer := -1
	router := gin.New()
	router.Use(TraceID())
	router.Use(func(c *gin.Context) {
		defer func() {
			responseSizeInOuterDefer = c.Writer.Size()
		}()
		c.Next()
	})
	router.Use(Recovery())
	router.GET("/panic", func(c *gin.Context) {
		panic("boom")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, want %d", w.Code, http.StatusOK)
	}
	if responseSizeInOuterDefer <= 0 {
		t.Fatalf("outer access-log-style defer saw response size %d, want a written recovery response", responseSizeInOuterDefer)
	}
}

func TestAccessLogDoesNotBlockRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(TraceID(), AccessLog())
	router.GET("/ok", func(c *gin.Context) {
		response.ReturnSuccess(c)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ok?foo=bar", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, want %d", w.Code, http.StatusOK)
	}
	if w.Header().Get(contextkey.TraceIDHeader) == "" {
		t.Fatalf("未写入 %s 响应头", contextkey.TraceIDHeader)
	}
}

func TestRecoveryAbortsTransportFailuresWithoutWritingResponse(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"broken pipe", fmt.Errorf("write failed: %w", syscall.EPIPE)},
		{"connection reset", fmt.Errorf("read failed: %w", syscall.ECONNRESET)},
		{"abort handler", http.ErrAbortHandler},
		{"wrapped abort handler", fmt.Errorf("stream failed: %w", http.ErrAbortHandler)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			var appCode int
			var appStatus string
			var written bool
			router := gin.New()
			router.Use(TraceID(), func(c *gin.Context) {
				defer func() {
					appCode = c.GetInt(contextkey.AppCode)
					appStatus = c.GetString(contextkey.AppStatus)
					written = c.Writer.Written()
				}()
				c.Next()
			}, Recovery())
			router.GET("/abort", func(c *gin.Context) { panic(tc.err) })
			w := httptest.NewRecorder()
			var recovered any
			func() {
				defer func() { recovered = recover() }()
				router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/abort", nil))
			}()
			if recovered != http.ErrAbortHandler {
				t.Fatalf("transport panic = %v, want http.ErrAbortHandler", recovered)
			}
			if written || w.Body.Len() != 0 {
				t.Fatalf("recovery wrote to an aborted response: %s", w.Body.String())
			}
			if appCode != response.CANCELLED.Code || appStatus != response.CANCELLED.Status {
				t.Fatalf("outer access-log outcome = %d/%s, want CANCELLED", appCode, appStatus)
			}
		})
	}
}

func TestRecoveryAbortDoesNotReturnSuccessfulHTTPResponse(t *testing.T) {
	router := gin.New()
	router.Use(TraceID(), AccessLog(), Recovery())
	router.POST("/abort", func(c *gin.Context) { panic(http.ErrAbortHandler) })
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	res, err := server.Client().Post(server.URL+"/abort", "application/json", nil)
	if res != nil {
		defer res.Body.Close()
	}
	if err == nil {
		t.Fatalf("aborted request returned HTTP %d, want a transport error", res.StatusCode)
	}
}
