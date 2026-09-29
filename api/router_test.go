package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"vdoc/api/middleware"
	"vdoc/config"

	"github.com/gin-gonic/gin"
)

// openHealthResponse 用于解析通过路由访问健康检查接口的统一响应
type openHealthResponse struct {
	Code   int                    `json:"code"`
	Status string                 `json:"status"`
	Detail map[string]interface{} `json:"detail"`
}

// 测试开放路由是否按分层注册成功（health）
func TestOpenHealth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// 避免未加载配置导致请求体限制为0
	config.MaxBodySize = 10 << 20 // 10MB

	r := InitApi()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/open/health", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body openHealthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	if body.Code != 200 {
		t.Fatalf("unexpected code: %v", body.Code)
	}

	if body.Status != "OK" {
		t.Fatalf("unexpected wrapper status: %v", body.Status)
	}

	status, ok := body.Detail["status"].(string)
	if !ok || status != "ok" {
		t.Fatalf("unexpected detail.status: %v", body.Detail["status"])
	}
}

func TestInitApiMiddlewareOrder(t *testing.T) {
	preserveRouterGlobals(t)
	for _, enabled := range []bool{false, true} {
		config.EnableRateLimit = enabled
		router := InitApi()
		want := []string{".TraceID.func", ".AccessLog.func", ".Recovery.func", ".SecurityHeaders.func", ".CorsDomainHandler.func"}
		if enabled {
			want = append(want, ".IPRateLimit.func")
		}
		want = append(want, ".BodySizeLimit.func")
		if len(router.Handlers) != len(want) {
			t.Fatalf("global middleware count = %d, want %d", len(router.Handlers), len(want))
		}
		for i, namePart := range want {
			got := runtime.FuncForPC(reflect.ValueOf(router.Handlers[i]).Pointer()).Name()
			if !strings.Contains(got, namePart) {
				t.Fatalf("rate limit=%t middleware[%d] = %s, want name containing %s", enabled, i, got, namePart)
			}
		}
	}
}

func preserveRouterGlobals(t *testing.T) {
	t.Helper()
	enabled, rate, burst := config.EnableRateLimit, config.GlobalRateLimit, config.GlobalRateBurst
	origins, maxBody := config.CORSAllowedOrigins, config.MaxBodySize
	mode, writer, errorWriter := gin.Mode(), gin.DefaultWriter, gin.DefaultErrorWriter
	t.Cleanup(func() {
		config.EnableRateLimit, config.GlobalRateLimit, config.GlobalRateBurst = enabled, rate, burst
		config.CORSAllowedOrigins, config.MaxBodySize = origins, maxBody
		gin.SetMode(mode)
		gin.DefaultWriter, gin.DefaultErrorWriter = writer, errorWriter
		middleware.CleanupAllLimiters()
	})
}

func TestInitApiRateLimitPreservesCORSAndPreflightQuota(t *testing.T) {
	preserveRouterGlobals(t)
	for _, tc := range []struct {
		name, origin, wantOrigin string
		allowed                  []string
		preflightStatus          int
	}{
		{"wildcard", "https://admin.example.test", "*", []string{"*"}, http.StatusNoContent},
		{"exact", "https://admin.example.test", "https://admin.example.test", []string{"https://admin.example.test"}, http.StatusNoContent},
		{"disallowed", "https://other.example.test", "", []string{"https://admin.example.test"}, http.StatusForbidden},
		{"no origin", "", "", []string{"*"}, http.StatusNoContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			middleware.CleanupAllLimiters()
			// 零补充速率让测试不依赖执行时间；初始桶容量仍为 1。
			config.EnableRateLimit, config.GlobalRateLimit, config.GlobalRateBurst = true, 0, 1
			config.CORSAllowedOrigins, config.MaxBodySize = tc.allowed, 10<<20
			router := InitApi()
			calls := 0
			router.GET("/_test/cors", func(c *gin.Context) {
				calls++
				c.String(http.StatusOK, "ok")
			})
			request := func(method string) *httptest.ResponseRecorder {
				t.Helper()
				r := httptest.NewRequest(method, "/_test/cors", nil)
				r.RemoteAddr = "198.51.100.17:40000"
				r.Header.Set("Origin", tc.origin)
				if method == http.MethodOptions {
					r.Header.Set("Access-Control-Request-Method", "GET")
					r.Header.Set("Access-Control-Request-Headers", "authorization")
				}
				w := httptest.NewRecorder()
				router.ServeHTTP(w, r)
				if got := w.Header().Get("Access-Control-Allow-Origin"); got != tc.wantOrigin {
					t.Fatalf("%s allow origin=%q, want %q", method, got, tc.wantOrigin)
				}
				if w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("X-Trace-ID") == "" {
					t.Fatalf("%s missing security/trace headers: %v", method, w.Header())
				}
				if w.Header().Get("Access-Control-Allow-Credentials") != "" {
					t.Fatal("CORS must not enable cookie credentials")
				}
				return w
			}
			preflight := func() {
				t.Helper()
				w := request(http.MethodOptions)
				if w.Code != tc.preflightStatus || w.Body.Len() != 0 {
					t.Fatalf("preflight=%d %s, want %d", w.Code, w.Body.String(), tc.preflightStatus)
				}
				if tc.wantOrigin != "" && !strings.Contains(w.Header().Get("Access-Control-Allow-Headers"), "Authorization") {
					t.Fatal("preflight did not permit token authentication")
				}
			}
			preflight()
			preflight()
			if w := request(http.MethodGet); w.Code != http.StatusOK || w.Body.String() != "ok" {
				t.Fatalf("preflight consumed the business quota: %d %s", w.Code, w.Body.String())
			}
			w := request(http.MethodGet)
			var body openHealthResponse
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusOK || body.Code != 429 || body.Status != "RESOURCE_EXHAUSTED" || calls != 1 {
				t.Fatalf("rate limit contract changed: %d %s calls=%d", w.Code, w.Body.String(), calls)
			}
			preflight()
		})
	}
}

func TestInitApiUsesOnlyExplicitTrustedProxiesForClientIP(t *testing.T) {
	originalProxies := append([]string(nil), config.TrustedProxies...)
	originalMaxBodySize := config.MaxBodySize
	t.Cleanup(func() {
		config.TrustedProxies = originalProxies
		config.MaxBodySize = originalMaxBodySize
	})
	config.MaxBodySize = 10 << 20

	for _, tc := range []struct {
		name           string
		trustedProxies []string
		remoteAddr     string
		forwardedFor   string
		want           string
	}{
		{
			name:           "trusted reverse proxy",
			trustedProxies: []string{"192.0.2.10"},
			remoteAddr:     "192.0.2.10:443",
			forwardedFor:   "198.51.100.25",
			want:           "198.51.100.25",
		},
		{
			name:           "untrusted direct client cannot spoof header",
			trustedProxies: []string{"192.0.2.10"},
			remoteAddr:     "203.0.113.30:443",
			forwardedFor:   "198.51.100.25",
			want:           "203.0.113.30",
		},
		{
			name:         "direct deployment defaults to remote address",
			remoteAddr:   "203.0.113.40:443",
			forwardedFor: "198.51.100.25",
			want:         "203.0.113.40",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config.TrustedProxies = append([]string(nil), tc.trustedProxies...)
			router := InitApi()
			router.GET("/_test/client-ip", func(c *gin.Context) {
				c.String(http.StatusOK, c.ClientIP())
			})
			request := httptest.NewRequest(http.MethodGet, "/_test/client-ip", nil)
			request.RemoteAddr = tc.remoteAddr
			request.Header.Set("X-Forwarded-For", tc.forwardedFor)
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, request)
			if got := recorder.Body.String(); got != tc.want {
				t.Fatalf("ClientIP() = %q, want %q", got, tc.want)
			}
		})
	}
}
