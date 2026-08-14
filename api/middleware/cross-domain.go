package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// CorsDomainHandler permits only explicitly configured HTTP(S) origins and a
// fixed set of Vdoc methods and headers. Same-origin requests do not require
// CORS response headers and continue to work with an empty allowlist.
func CorsDomainHandler(allowedOrigins ...string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		if normalized := strings.TrimSpace(strings.TrimSuffix(origin, "/")); normalized != "" && normalized != "*" {
			allowed[normalized] = struct{}{}
		}
	}
	return func(c *gin.Context) {
		method := c.Request.Method
		origin := strings.TrimSpace(c.Request.Header.Get("Origin"))
		if origin != "" {
			if _, ok := allowed[origin]; !ok {
				if method == http.MethodOptions {
					c.AbortWithStatus(http.StatusForbidden)
					return
				}
				c.Next()
				return
			}
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Vdoc-Share-Unlock")
			c.Header("Access-Control-Expose-Headers", "Content-Disposition, Content-Type, X-Trace-ID")
			c.Header("Access-Control-Max-Age", "172800")
		}

		if method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
