package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	corsAllowedMethods = "GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS"
	corsAllowedHeaders = "Authorization, Content-Type, X-Trace-ID, X-Vdoc-Share-Unlock"
	corsExposedHeaders = "Content-Disposition, Content-Type, X-Trace-ID"
)

// CorsDomainHandler 支持精确 Origin 或显式配置的 *，保持固定的方法/请求头范围。
// 通配模式不启用跨站 Cookie 凭据；JWT、MCP 和分享令牌仍由各自中间件校验。
func CorsDomainHandler(allowedOrigins ...string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	allowAll := false
	for _, origin := range allowedOrigins {
		normalized := strings.TrimSpace(strings.TrimSuffix(origin, "/"))
		if normalized == "*" {
			allowAll = true
		} else if normalized != "" {
			allowed[normalized] = struct{}{}
		}
	}
	return func(c *gin.Context) {
		method := c.Request.Method
		origin := strings.TrimSpace(c.Request.Header.Get("Origin"))
		if origin != "" {
			if _, ok := allowed[origin]; !ok && !allowAll {
				if method == http.MethodOptions {
					c.AbortWithStatus(http.StatusForbidden)
					return
				}
				c.Next()
				return
			}
			if allowAll {
				c.Header("Access-Control-Allow-Origin", "*")
			} else {
				c.Header("Access-Control-Allow-Origin", origin)
				c.Header("Vary", "Origin")
			}
			c.Header("Access-Control-Allow-Methods", corsAllowedMethods)
			c.Header("Access-Control-Allow-Headers", corsAllowedHeaders)
			c.Header("Access-Control-Expose-Headers", corsExposedHeaders)
			c.Header("Access-Control-Max-Age", "172800")
		}

		if method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
