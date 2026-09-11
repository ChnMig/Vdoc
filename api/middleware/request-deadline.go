package middleware

import (
	"context"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// RequestDeadline 约束业务处理时间，同时保留客户端断开时的取消信号。
// AI 交互和 MCP 允许较长调用；数据库和对象存储必须使用该 context。
func RequestDeadline() gin.HandlerFunc {
	return func(c *gin.Context) {
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
