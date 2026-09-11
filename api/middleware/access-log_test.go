package middleware

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"vdoc/api/response"
	"vdoc/config"
	httplog "vdoc/utils/log"

	"github.com/gin-gonic/gin"
)

func TestAccessLogWritesStructuredSummaryFields(t *testing.T) {
	gin.SetMode(gin.TestMode)

	oldStdout := os.Stdout
	oldRunModel := config.RunModel
	oldLogLevel := config.LogLevel
	oldGinLogLevel := config.GinLogLevel

	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}

	restored := false
	restoreGlobals := func() {
		os.Stdout = oldStdout
		config.RunModel = oldRunModel
		config.LogLevel = oldLogLevel
		config.GinLogLevel = oldGinLogLevel
		httplog.SetLogger()
	}
	restore := func() {
		if restored {
			return
		}
		restored = true
		restoreGlobals()
		_ = readPipe.Close()
		_ = writePipe.Close()
	}
	t.Cleanup(restore)

	type capturedOutput struct {
		body []byte
		err  error
	}
	captured := make(chan capturedOutput, 1)
	go func() { body, err := io.ReadAll(readPipe); captured <- capturedOutput{body, err} }()
	os.Stdout = writePipe
	config.RunModel = config.RunModelDevValue
	config.LogLevel = "info"
	config.GinLogLevel = "info"
	httplog.SetLogger()

	router := gin.New()
	router.Use(TraceID(), AccessLog(), Recovery())
	router.POST("/ok", func(c *gin.Context) {
		response.ReturnSuccess(c)
	})

	router.GET("/denied/:resource", func(c *gin.Context) { response.ReturnError(c, response.PERMISSION_DENIED, "denied") })
	router.GET("/panic", func(c *gin.Context) { panic("test panic") })
	req := httptest.NewRequest(http.MethodPost, "/ok?foo=bar&token=secret-query", bytes.NewBufferString("secret-body"))
	req.Header.Set("User-Agent", "access-log-test")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/denied/resource-1", nil))
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/panic", nil))

	_ = httplog.GetGinLogger().Sync()
	restoreGlobals()
	_ = writePipe.Close()
	outputResult := <-captured
	outBytes, readErr := outputResult.body, outputResult.err
	if readErr != nil {
		t.Fatalf("read access log output: %v", readErr)
	}
	restored = true
	_ = readPipe.Close()

	output := string(outBytes)
	for _, want := range []string{
		`"method": "POST"`,
		`"path": "/ok"`,
		`"route": "/ok"`,
		`"app_code": 200`,
		`"app_status": "OK"`,
		`"route": "/denied/:resource"`,
		`"app_code": 403`,
		`"app_status": "PERMISSION_DENIED"`,
		`"app_code": 500`,
		`"app_status": "INTERNAL"`,
		`"query_keys": [`,
		`"foo"`,
		`"token"`,
		`"status": 200`,
		`"latency":`,
		`"client_ip":`,
		`"user_agent": "access-log-test"`,
		`"trace_id":`,
		`"error": ""`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("access log output missing %s: %s", want, output)
		}
	}
	if strings.Contains(output, "secret-body") {
		t.Fatalf("access log output should not contain request body: %s", output)
	}
	if strings.Contains(output, "secret-query") || strings.Contains(output, "foo=bar") {
		t.Fatalf("access log output should not contain query values: %s", output)
	}
}
