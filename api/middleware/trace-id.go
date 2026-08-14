package middleware

import (
	"strings"

	"vdoc/utils/contextkey"
	"vdoc/utils/id"
	serviceLog "vdoc/utils/log"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const (
	TraceIDHeaderKey  = contextkey.TraceIDHeader
	TraceIDContextKey = contextkey.TraceID
)

func TraceID() gin.HandlerFunc {
	return func(c *gin.Context) {
		traceID := c.GetHeader(TraceIDHeaderKey)
		if !validTraceID(traceID) {
			traceID = id.GenerateID()
		}

		c.Set(TraceIDContextKey, traceID)
		c.Request = c.Request.WithContext(serviceLog.WithTraceID(c.Request.Context(), traceID))
		c.Header(TraceIDHeaderKey, traceID)

		// 创建带上下文信息的 logger 并存入 context
		contextLogger := zap.L().With(
			zap.String("trace_id", traceID),
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.String("client_ip", c.ClientIP()),
		)
		c.Set(contextkey.Logger, contextLogger)

		// 记录请求开始（调试级别）
		contextLogger.Debug("Request started")

		c.Next()

		// 记录请求完成（包含状态码，调试级别）
		contextLogger.Debug("Request completed",
			zap.Int("status_code", c.Writer.Status()),
		)
	}
}

func validTraceID(traceID string) bool {
	if traceID == "" || len(traceID) > 128 || strings.TrimSpace(traceID) != traceID {
		return false
	}
	for _, char := range traceID {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' {
			continue
		}
		switch char {
		case '-', '_', '.', ':':
			continue
		default:
			return false
		}
	}
	return true
}
