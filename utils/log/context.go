package log

import (
	"context"
	"net/url"
	"sort"

	"vdoc/utils/contextkey"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const traceIDKey = "trace_id"

// BoundParamsKey 是 Gin context 中已绑定业务参数的统一 key。
const BoundParamsKey = contextkey.BoundParams

// TraceIDHeader 是跨 HTTP 服务传递请求追踪 ID 的 header。
const TraceIDHeader = contextkey.TraceIDHeader

// WithTraceID 返回携带请求追踪 ID 的标准 context。
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return contextkey.WithTraceID(ctx, traceID)
}

// TraceID 从标准 context 读取请求追踪 ID。
func TraceID(ctx context.Context) (string, bool) {
	return contextkey.TraceIDFromContext(ctx)
}

// FromStandardContext 基于全局 logger 创建携带 trace_id 的 logger。
func FromStandardContext(ctx context.Context) *zap.Logger {
	logger := GetLogger()
	traceID, ok := TraceID(ctx)
	if !ok {
		return logger
	}
	return logger.With(zap.String(traceIDKey, traceID))
}

// FromContext 优先返回 Gin 中间件注入的请求 logger，并安全回退到标准 context 或全局 logger。
func FromContext(ctx *gin.Context) *zap.Logger {
	if logger, ok := ginRequestLogger(ctx); ok {
		return logger
	}
	if ctx != nil && ctx.Request != nil {
		return FromStandardContext(ctx.Request.Context())
	}
	return GetLogger()
}

// WithRequest 只提取不含值的请求摘要。禁止记录 query value、form、
// multipart 或绑定后的业务参数，避免密码、token、API key 和文档正文落盘。
func WithRequest(ctx *gin.Context) *zap.Logger {
	base, hasInjectedRequestFields := ginRequestLogger(ctx)
	if !hasInjectedRequestFields {
		base = FromContext(ctx)
	}
	if ctx == nil || ctx.Request == nil {
		return base
	}

	fields := make([]zap.Field, 0, 4)
	if !hasInjectedRequestFields {
		fields = append(fields, zap.String("method", ctx.Request.Method))
	}
	if ctx.Request.URL != nil {
		if !hasInjectedRequestFields {
			fields = append(fields, zap.String("path", ctx.Request.URL.Path))
		}
		if rawQuery := ctx.Request.URL.RawQuery; rawQuery != "" {
			fields = append(fields, zap.Strings("query_keys", QueryKeys(rawQuery)))
		}
	}
	if len(ctx.Params) > 0 {
		pathParams := make(map[string]string, len(ctx.Params))
		for _, param := range ctx.Params {
			pathParams[param.Key] = param.Value
		}
		fields = append(fields, zap.Any("path_params", pathParams))
	}
	return base.With(fields...)
}

// ginRequestLogger 返回 TraceID 中间件注入、且已携带 method/path/client_ip 的 logger。
func ginRequestLogger(ctx *gin.Context) (*zap.Logger, bool) {
	if ctx == nil {
		return nil, false
	}
	value, exists := ctx.Get(contextkey.Logger)
	if !exists {
		return nil, false
	}
	logger, ok := value.(*zap.Logger)
	return logger, ok && logger != nil
}

// QueryKeys 解析并排序 query 参数名，不返回任何参数值。解析失败时仅返回固定标记。
func QueryKeys(rawQuery string) []string {
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return []string{"<invalid>"}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
