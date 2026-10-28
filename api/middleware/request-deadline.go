package middleware

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// RequestDeadline 约束业务处理时间，同时保留客户端断开时的取消信号。
// AI 交互和 MCP 允许较长调用；数据库和对象存储必须使用该 context。
func RequestDeadline() gin.HandlerFunc {
	return func(c *gin.Context) {
		if longAIRequest(c.Request.Method, c.FullPath()) {
			// AI 调用由提供商的分阶段时限约束，0 可禁用；继续继承客户端取消。
			ctx, cancel := context.WithCancel(c.Request.Context())
			defer cancel()
			c.Request = c.Request.WithContext(ctx)
			// 清除 http.Server 的连接写入期限，避免长生成被全局 WriteTimeout 截断。
			_ = http.NewResponseController(c.Writer).SetWriteDeadline(time.Time{})
			c.Next()
			return
		}
		timeout := 30 * time.Second
		if strings.Contains(c.FullPath(), "/ai/") || strings.Contains(c.FullPath(), "/ai-summary/") || strings.HasSuffix(c.FullPath(), "/mcp") {
			timeout = 150 * time.Second
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

func longAIRequest(method, path string) bool {
	if method != http.MethodPost {
		return false
	}
	return (strings.Contains(path, "/ai/") &&
		(strings.HasSuffix(path, "/provider/test") || strings.HasSuffix(path, "/provider/models") ||
			strings.HasSuffix(path, "/chat-sessions/:session_id/messages"))) ||
		strings.HasSuffix(path, "/ai-summary/regenerate")
}
