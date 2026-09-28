package middleware

import (
	"errors"
	"net/http"
	"runtime/debug"
	"syscall"

	"vdoc/api/response"
	httplog "vdoc/utils/log"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Recovery 将普通 panic 转换为统一响应；连接或流中断交给 net/http 处理。
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				if err, ok := recovered.(error); ok && (errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, http.ErrAbortHandler)) {
					response.SetOutcome(c, response.CANCELLED.Code, response.CANCELLED.Status)
					httplog.GetGinErrorLogger().Debug("HTTP request aborted",
						zap.String("method", c.Request.Method),
						zap.String("path", c.Request.URL.Path),
						zap.String("trace_id", traceIDFromContext(c)),
					)
					c.Abort()
					// 交给 net/http 中止连接或流，避免 Gin 结束时补写成功响应。
					// 不传播原始错误文本，net/http 也不会为此哨兵错误打印堆栈。
					panic(http.ErrAbortHandler)
				}
				httplog.GetGinErrorLogger().Error("HTTP panic recovered",
					zap.Any("panic", recovered),
					zap.String("method", c.Request.Method),
					zap.String("path", c.Request.URL.Path),
					zap.Strings("query_keys", httplog.QueryKeys(c.Request.URL.RawQuery)),
					zap.String("client_ip", c.ClientIP()),
					zap.String("user_agent", c.Request.UserAgent()),
					zap.String("trace_id", traceIDFromContext(c)),
					zap.ByteString("stack", debug.Stack()),
				)
				response.ReturnError(c, response.INTERNAL, "服务内部错误")
			}
		}()

		c.Next()
	}
}
