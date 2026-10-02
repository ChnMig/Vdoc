package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCorssDomainHandlerMatchesScaffoldAndPreservesRouteAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, origin := range []string{"https://admin.example.test", "https://another-client.example.test", "null", ""} {
		for _, method := range []string{http.MethodOptions, http.MethodGet} {
			t.Run(origin+"/"+method, func(t *testing.T) {
				router := gin.New()
				router.Use(CorssDomainHandler())
				handlerCalled := false
				router.GET("/private", func(c *gin.Context) {
					handlerCalled = true
					c.AbortWithStatus(http.StatusUnauthorized)
				})
				request := httptest.NewRequest(method, "/private", nil)
				request.Header.Set("Origin", origin)
				request.Header.Set("Access-Control-Request-Method", http.MethodGet)
				request.Header.Set("Access-Control-Request-Headers", "authorization,cache-control,pragma")
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, request)

				if method == http.MethodOptions {
					if recorder.Code != http.StatusOK || recorder.Body.String() != "Options Request!" || handlerCalled {
						t.Fatalf("preflight status=%d message=%q handlerCalled=%t", recorder.Code, recorder.Body.String(), handlerCalled)
					}
					if got := recorder.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
						t.Fatalf("preflight content type = %q, want text/plain; charset=utf-8", got)
					}
				} else if recorder.Code != http.StatusUnauthorized || !handlerCalled {
					t.Fatalf("CORS bypassed route authorization: status=%d handlerCalled=%t", recorder.Code, handlerCalled)
				}
				for _, header := range []string{"Access-Control-Allow-Origin", "Access-Control-Allow-Methods", "Access-Control-Allow-Headers", "Access-Control-Expose-Headers"} {
					want := "*"
					if origin == "" {
						want = ""
					}
					if got := recorder.Header().Get(header); got != want {
						t.Errorf("%s = %q, want %q", header, got, want)
					}
				}
				wantMaxAge := "172800"
				if origin == "" {
					wantMaxAge = ""
				}
				if got := recorder.Header().Get("Access-Control-Max-Age"); got != wantMaxAge {
					t.Errorf("max age = %q, want %q", got, wantMaxAge)
				}
				if recorder.Header().Get("Access-Control-Allow-Credentials") != "" {
					t.Fatal("scaffold CORS must not enable browser credentials")
				}
			})
		}
	}
}
